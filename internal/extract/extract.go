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
		// Timeout short enough not to block the Add handler for too long —
		// this is a synchronous call inside the internal/store.Add pipeline
		// (D12, item 5: if it times out, the caller falls back to raw
		// content).
		http: &http.Client{Timeout: 20 * time.Second},
	}
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
}

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

const systemPrompt = `You extract atomic facts from a text for a semantic memory system.
Rules:
- Each fact must be self-contained (makes sense on its own, without the original text) and reusable in future searches.
- Do not invent information that is not in the text.
- If the text is already a single atomic fact, return it as is — do not invent artificial subdivisions.
- Write every fact in the same language as the input text. Do not translate.
- Reply ONLY with a JSON object in the format {"facts": ["fact 1", "fact 2"]}, nothing else: no markdown, no explanation, no text before or after.`

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
	userPrompt := fmt.Sprintf("Domain: %s\n\nText:\n%s", domain, content)

	reqBody, err := json.Marshal(chatRequest{
		Model: e.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		ResponseFormat: &responseFormat{Type: "json_object"},
		Reasoning:      &reasoningConfig{Enabled: false},
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
	defer resp.Body.Close()

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
	defer resp.Body.Close()

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

// parseFacts first tries the format requested via response_format (D19):
// the JSON object {"facts": [...]}. If that yields no fact (missing field,
// a provider that ignores response_format, or the response is not an
// object), it falls back to the old format — a bare array, tolerant of
// markdown fences or surrounding text the model sometimes inserts despite
// the instruction — so as not to break against a provider that does not
// honor response_format.
func parseFacts(raw string) ([]string, error) {
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

func parseFactsObject(raw string) ([]string, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < 0 || end < start {
		return nil, fmt.Errorf("response has no JSON object: %q", raw)
	}

	var obj struct {
		Facts []string `json:"facts"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &obj); err != nil {
		return nil, fmt.Errorf("parse facts object: %w", err)
	}
	return obj.Facts, nil
}

func parseFactsArray(raw string) ([]string, error) {
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end < 0 || end < start {
		return nil, fmt.Errorf("response has neither a JSON array nor object: %q", raw)
	}

	var facts []string
	if err := json.Unmarshal([]byte(raw[start:end+1]), &facts); err != nil {
		return nil, fmt.Errorf("parse facts array: %w", err)
	}
	return facts, nil
}

// finalizeFacts trims whitespace and discards empty strings — an error if
// no usable fact remains.
func finalizeFacts(facts []string) ([]string, error) {
	out := make([]string, 0, len(facts))
	for _, f := range facts {
		f = strings.TrimSpace(f)
		if f != "" {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no non-empty fact extracted")
	}
	return out, nil
}
