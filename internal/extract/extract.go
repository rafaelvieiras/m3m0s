// Package extract implements atomic-fact extraction via LLM (Phase 4, D12
// in docs/decisions/): a hand-written chat completions client over
// OpenRouter's OpenAI-compatible API, with no new dependency in go.mod —
// same spirit as the hand-made glob matcher in internal/watch/match.go in
// Phase 2.
package extract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/factkind"
)

// DefaultBaseURL and DefaultModel are the defaults documented in
// docs/api-contract.md, section "Fact extraction (Phase 4, D12)".
const (
	DefaultBaseURL = "https://openrouter.ai/api/v1"
	DefaultModel   = "deepseek/deepseek-v4-flash"
)

// Extractor calls an LLM via OpenAI-compatible chat completions to turn
// raw text into self-contained atomic facts (D12, item 2).
type Extractor struct {
	apiKey  string
	baseURL string
	model   string
	http    *http.Client

	// promptTokens/completionTokens accumulate the usage reported by the
	// provider (the `usage` field of the chat completions response,
	// OpenAI-compatible standard) across all Extract/Classify calls of this
	// instance — exposed via TokensUsed for GET /stats (telemetry). A
	// process-level count, not persisted: it resets along with m3m0s serve,
	// like Uptime. int64 + atomic because Extract/Classify may run
	// concurrently (independent calls from different HTTP handlers).
	fallbackModel    string
	promptTokens     int64
	completionTokens int64
}

// New creates an Extractor. Empty baseURL/model fall back to the defaults
// above — same firstNonEmpty pattern already used in cmd/m3m0s/serve.go.
func New(apiKey, baseURL, model string) *Extractor {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if model == "" {
		model = DefaultModel
	}
	return &Extractor{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		// Bounded so a stuck provider cannot hold an Add forever; when it
		// times out the caller falls back to raw content (D12, item 5) and
		// the retroactive job repairs it later. Configurable (#65): the
		// provider's tail latency routinely exceeds the old 20 s.
		http: &http.Client{Timeout: DefaultTimeout},
	}
}

// DefaultTimeout is the per-request HTTP timeout of an extraction call.
const DefaultTimeout = 45 * time.Second

// SetTimeout overrides the per-request HTTP timeout (M3M0S_EXTRACT_TIMEOUT).
// Non-positive values are ignored.
func (e *Extractor) SetTimeout(d time.Duration) {
	if d > 0 {
		e.http.Timeout = d
	}
}

// Timeout returns the per-request HTTP timeout.
func (e *Extractor) Timeout() time.Duration { return e.http.Timeout }

// SetFallbackModel sets the model used by ExtractFactsFallback — the retry
// attempt when the primary model timed out or failed (M3M0S_EXTRACT_FALLBACK_MODEL).
// Empty disables it.
func (e *Extractor) SetFallbackModel(m string) { e.fallbackModel = strings.TrimSpace(m) }

// HasFallback reports whether a fallback model is configured.
func (e *Extractor) HasFallback() bool {
	return e.fallbackModel != "" && e.fallbackModel != e.model
}

// ExtractFactsFallback is ExtractFacts against the fallback model. Without
// one configured it behaves exactly like ExtractFacts.
func (e *Extractor) ExtractFactsFallback(ctx context.Context, content, domain string) ([]factkind.Fact, error) {
	if !e.HasFallback() {
		return e.ExtractFacts(ctx, content, domain)
	}
	return e.extractFactsWith(ctx, e.fallbackModel, content, domain)
}

// Model returns the configured model — used by
// internal/store.MemoryStore.ExtractionInfo (Phase 5, D13) to fill the
// `extraction.model` field of GET /stats without internal/store needing to
// know the internal/extract package directly (the store only sees the
// Extractor interface; ExtractionInfo does an optional type assertion for
// this method, see the comment in internal/store/store.go).
func (e *Extractor) Model() string {
	return e.model
}

// TokensUsed returns the accumulated total of prompt_tokens/completion_tokens
// the provider reported (`usage` field) across all Extract/Classify calls
// made by this instance since the process started — used by
// internal/store.MemoryStore.ExtractionTokensUsed via optional
// type-assertion (same pattern as Model()/modelProvider), to fill
// `telemetry.extractionTokensUsed` in GET /stats.
func (e *Extractor) TokensUsed() (prompt, completion int64) {
	return atomic.LoadInt64(&e.promptTokens), atomic.LoadInt64(&e.completionTokens)
}

func (e *Extractor) recordUsage(u chatUsage) {
	if u.PromptTokens > 0 {
		atomic.AddInt64(&e.promptTokens, int64(u.PromptTokens))
	}
	if u.CompletionTokens > 0 {
		atomic.AddInt64(&e.completionTokens, int64(u.CompletionTokens))
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string           `json:"model"`
	Messages       []chatMessage    `json:"messages"`
	ResponseFormat *responseFormat  `json:"response_format,omitempty"`
	Reasoning      *reasoningConfig `json:"reasoning,omitempty"`
	// MaxTokens caps the generation (D41). Only the extraction call sets it;
	// Classify answers a single word and leaves it zero (omitted).
	MaxTokens int `json:"max_tokens,omitempty"`
}

// extractMaxTokens caps the completion of one extraction call (D41). The
// prompt asks for at most maxFacts short facts (~250 tokens); this ceiling
// only stops a runaway generation from holding a worker for the whole
// budget. A reply cut by it is invalid JSON and takes the usual raw-fallback
// path, so it must stay comfortably above the expected size.
const extractMaxTokens = 1024

// responseFormat={"type":"json_object"} is the OpenAI-compatible field
// (also supported by OpenRouter) that asks the provider to restrict the
// output to valid JSON — used only in Extract (D19), never in Classify
// (which returns a single bare word, not JSON). It exists to tackle the
// slice of production failures that were not timeouts: empty responses and
// the model returning loose prose (sometimes in an unexpected language)
// instead of the requested array, found in the logs that motivated D19.
// Not every provider behind OpenRouter necessarily honors it — parseFacts
// stays tolerant of the old format (bare array) as a fallback, so a
// provider that ignores the field breaks nothing, it just loses the
// reinforcement.
type responseFormat struct {
	Type string `json:"type"`
}

// reasoningConfig={"enabled":false} is the OpenRouter field that turns off
// chain-of-thought reasoning for reasoning models (D20 in docs/decisions/)
// — DefaultModel (deepseek/deepseek-v4-flash) is one of those despite the
// "flash" name: it was found in production that a trivial call spent ~475
// reasoning tokens and 8.5s before answering, and real (non-repetitive)
// content systematically blew past e.http's 20s timeout because of it —
// not because of response format (D19) or volume (D18). Used in both
// Extract and Classify: neither task needs visible reasoning (the answer
// is always a short JSON or a single word), only the final result.
type reasoningConfig struct {
	Enabled bool `json:"enabled"`
}

type chatChoice struct {
	Message chatMessage `json:"message"`
}

// chatUsage mirrors the `usage` field of OpenAI-compatible chat
// completions responses (OpenRouter included) — absent/zeroed on providers
// that do not fill it, treated as "nothing to account for" (see
// recordUsage).
type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Usage   chatUsage    `json:"usage"`
}

// systemPrompt is pinned by testdata/system_prompt.golden.txt (see
// TestSystemPromptGolden): a prompt change must show up as a diff there.
const systemPrompt = `You extract atomic facts from a text for a semantic memory system.
Rules:
- Each fact must be self-contained (makes sense on its own, without the original text) and reusable in future searches.
- Do not invent information that is not in the text.
- If the text is already a single atomic fact, return it as is — do not invent artificial subdivisions.
- Write every fact in the same language as the input text. Do not translate.
- Return at most 6 facts: only the most durable and reusable ones, most important first. Never pad the list.
- Keep each fact to one short sentence (about 25 words at most): the conclusion and, when it matters, the why. Do not quote or paraphrase the text at length.
- Skip noise entirely: no boilerplate, diff trivia or descriptions of the text itself ("the text explains ...").
- Give each fact a "kind", exactly one of: decision, rule, preference, gotcha, procedure, state, other.
  decision: a choice that was made and, ideally, why.
  rule: a convention, constraint or standing instruction ("always ...", "never ...").
  preference: a stated preference of the user.
  gotcha: a root cause, pitfall or non-obvious behaviour.
  procedure: how to do something (steps, commands, configuration).
  state: a transient status (task progress, current branch, a number that will change).
  other: when none of the above clearly applies.
- Reply ONLY with a JSON object in the format {"facts": [{"text": "fact 1", "kind": "decision"}, {"text": "fact 2", "kind": "state"}]}, nothing else: no markdown, no explanation, no text before or after.`

// SystemPrompt returns the fact-extraction system prompt (used by the golden
// test and by tooling that compares prompt versions).
func SystemPrompt() string { return systemPrompt }

// Extract sends content (already redacted by the caller — D12, item 4) to
// the configured model and returns the list of extracted atomic facts.
// domain enters only as context in the prompt (D12/api-contract.md); it is
// not validated here, the caller already validated it.
//
// Every error (network, HTTP >=400, unparseable JSON, empty response) is
// returned as an error — never a silent "no facts". The internal/store.Add
// pipeline treats any error from here as a signal to fall back to raw
// content (D12, item 5), so there is no need to distinguish error types
// here.
func (e *Extractor) Extract(ctx context.Context, content, domain string) ([]string, error) {
	facts, err := e.ExtractFacts(ctx, content, domain)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(facts))
	for i, f := range facts {
		out[i] = f.Text
	}
	return out, nil
}

// ExtractFacts is Extract plus the nature (kind, D38) the model gave each
// fact, from the same call and the same response — no extra LLM call. A kind
// outside the closed enum is normalized to "other"; a fact the model gave as
// a bare string (a provider that ignores the schema) has an empty Kind, and
// the caller decides what to do (the store falls back to the heuristic).
func (e *Extractor) ExtractFacts(ctx context.Context, content, domain string) ([]factkind.Fact, error) {
	return e.extractFactsWith(ctx, e.model, content, domain)
}

func (e *Extractor) extractFactsWith(ctx context.Context, model, content, domain string) ([]factkind.Fact, error) {
	userPrompt := fmt.Sprintf("Domain: %s\n\nText:\n%s", domain, content)

	reqBody, err := json.Marshal(chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		ResponseFormat: &responseFormat{Type: "json_object"},
		Reasoning:      &reasoningConfig{Enabled: false},
		MaxTokens:      extractMaxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("extract request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("extract responded %d: %s", resp.StatusCode, string(body))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("unmarshal chat response: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return nil, errors.New("chat response has no choices")
	}
	e.recordUsage(chatResp.Usage)

	return parseFacts(chatResp.Choices[0].Message.Content)
}

const kindsSystemPrompt = `You classify facts from a semantic memory system by their nature.
Each fact gets exactly one kind: decision, rule, preference, gotcha, procedure, state, noise, other.
- decision: a choice that was made and, ideally, why.
- rule: a convention, constraint or standing instruction ("always ...", "never ...").
- preference: a stated preference of the user.
- gotcha: a root cause, pitfall or non-obvious behaviour.
- procedure: how to do something (steps, commands, configuration).
- state: a transient status (task progress, current branch, a number that will change).
- noise: meta-description, boilerplate or diff trivia.
- other: when none of the above clearly applies.
The facts may be written in any language. Reply ONLY with a JSON object {"kinds": ["kind for fact 1", "kind for fact 2", ...]} with exactly one entry per fact, in the same order.`

// ClassifyKinds asks the model for the kind of each fact in one call (the
// backfill of facts stored before D38 uses it: a classification is far
// cheaper than re-extracting). The answer must have one entry per fact;
// unknown values are normalized to "other".
func (e *Extractor) ClassifyKinds(ctx context.Context, facts []string) ([]string, error) {
	var b strings.Builder
	for i, f := range facts {
		fmt.Fprintf(&b, "%d. %q\n", i+1, f)
	}
	raw, err := e.complete(ctx, kindsSystemPrompt, b.String(), true)
	if err != nil {
		return nil, err
	}
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return nil, fmt.Errorf("kinds response has no JSON object: %q", raw)
	}
	var obj struct {
		Kinds []string `json:"kinds"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &obj); err != nil {
		return nil, fmt.Errorf("parse kinds object: %w", err)
	}
	if len(obj.Kinds) != len(facts) {
		return nil, fmt.Errorf("kinds response has %d entries for %d facts", len(obj.Kinds), len(facts))
	}
	out := make([]string, len(obj.Kinds))
	for i, k := range obj.Kinds {
		out[i] = factkind.Normalize(k)
	}
	return out, nil
}

// complete runs one chat completion and returns the assistant text.
func (e *Extractor) complete(ctx context.Context, system, user string, jsonMode bool) (string, error) {
	req := chatRequest{
		Model:     e.model,
		Messages:  []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}},
		Reasoning: &reasoningConfig{Enabled: false},
	}
	if jsonMode {
		req.ResponseFormat = &responseFormat{Type: "json_object"}
	}
	reqBody, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal chat request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+e.apiKey)
	resp, err := e.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("chat request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("chat responded %d: %s", resp.StatusCode, string(body))
	}
	var chatResp chatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", fmt.Errorf("unmarshal chat response: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return "", errors.New("chat response has no choices")
	}
	e.recordUsage(chatResp.Usage)
	return chatResp.Choices[0].Message.Content, nil
}

const classifySystemPrompt = `You compare two facts from a semantic memory system and classify the relation between them.
The facts may be written in any language; compare their meaning regardless of language.
Reply with EXACTLY one of these three words, in lowercase, nothing else: contradicts, related, unrelated.
- contradicts: the new fact replaces/invalidates the existing fact (e.g. "uses Python 3.10" vs "uses Python 3.12" about the same subject).
- related: the two facts are about connected subjects, but neither invalidates the other.
- unrelated: the two facts have no relevant relation, despite being textually similar.`

// Classify compares newFact with existingFact (already stored) and asks
// the same LLM configured for Extract to judge the relation between the
// two — Phase 7, D15 in docs/decisions/. It satisfies the store.Classifier
// interface via optional type-assertion (same pattern as Model() for
// modelProvider), called only when internal/store finds a candidate in the
// "related, but not a duplicate" band (RelatedSimilarityThreshold).
//
// It returns the model's raw, lowercased answer (expected: "contradicts",
// "related" or "unrelated") — the caller decides what to do with
// unexpected values; there is no normalization here beyond
// TrimSpace+ToLower, so as not to mask a truly ambiguous answer as if it
// were a valid value. An error (network, timeout, HTTP >=400, response
// without choices) is returned as an error, like Extract — the caller
// treats it as "not classified" and falls back to the safe path (D15:
// never invalidates because of a failure here).
func (e *Extractor) Classify(ctx context.Context, newFact, existingFact string) (string, error) {
	userPrompt := fmt.Sprintf("New fact: %q\nExisting fact: %q", newFact, existingFact)

	reqBody, err := json.Marshal(chatRequest{
		Model: e.model,
		Messages: []chatMessage{
			{Role: "system", Content: classifySystemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Reasoning: &reasoningConfig{Enabled: false},
	})
	if err != nil {
		return "", fmt.Errorf("marshal chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("classify request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("classify responded %d: %s", resp.StatusCode, string(body))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", fmt.Errorf("unmarshal chat response: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return "", errors.New("chat response has no choices")
	}
	e.recordUsage(chatResp.Usage)

	return strings.ToLower(strings.TrimSpace(chatResp.Choices[0].Message.Content)), nil
}

// rawFact is one element of the "facts" array: the current format is an
// object {"text", "kind"}, but a bare string (the pre-D38 format, or a
// provider that ignores the schema) is still accepted and carries no kind.
type rawFact struct {
	Text string
	Kind string
}

func (f *rawFact) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		f.Text, f.Kind = s, ""
		return nil
	}
	var obj struct {
		Text string `json:"text"`
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	f.Text, f.Kind = obj.Text, obj.Kind
	return nil
}

// parseFacts first tries the format requested via response_format (D19):
// the JSON object {"facts": [...]}. If that yields no fact (missing field,
// a provider that ignores response_format, or the response is not an
// object), it falls back to the old format — a bare array, tolerant of
// markdown fences or surrounding text the model sometimes inserts despite
// the instruction — so as not to break against a provider that does not
// honor response_format.
func parseFacts(raw string) ([]factkind.Fact, error) {
	if facts, err := parseFactsObject(raw); err == nil {
		if out, ferr := finalizeFacts(facts); ferr == nil {
			return out, nil
		}
	}

	facts, err := parseFactsArray(raw)
	if err != nil {
		return nil, err
	}
	return finalizeFacts(facts)
}

func parseFactsObject(raw string) ([]rawFact, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < 0 || end < start {
		return nil, fmt.Errorf("response has no JSON object: %q", raw)
	}

	var obj struct {
		Facts []rawFact `json:"facts"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &obj); err != nil {
		return nil, fmt.Errorf("parse facts object: %w", err)
	}
	return obj.Facts, nil
}

func parseFactsArray(raw string) ([]rawFact, error) {
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end < 0 || end < start {
		return nil, fmt.Errorf("response has neither a JSON array nor object: %q", raw)
	}

	var facts []rawFact
	if err := json.Unmarshal([]byte(raw[start:end+1]), &facts); err != nil {
		return nil, fmt.Errorf("parse facts array: %w", err)
	}
	return facts, nil
}

// finalizeFacts trims whitespace, discards empty facts and normalizes the
// kind (unknown values become "other"; an absent kind stays empty) — an
// error if no usable fact remains.
func finalizeFacts(facts []rawFact) ([]factkind.Fact, error) {
	out := make([]factkind.Fact, 0, len(facts))
	for _, f := range facts {
		text := strings.TrimSpace(f.Text)
		if text == "" {
			continue
		}
		kind := ""
		if strings.TrimSpace(f.Kind) != "" {
			kind = factkind.Normalize(f.Kind)
		}
		out = append(out, factkind.Fact{Text: text, Kind: kind})
	}
	if len(out) == 0 {
		return nil, errors.New("no non-empty fact extracted")
	}
	return out, nil
}
