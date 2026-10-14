package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/project"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// promptSearchTimeout bounds the synchronous search on every prompt (D44):
// one embedding call plus a local index lookup. Past it the prompt goes
// through without extra context.
const promptSearchTimeout = 4 * time.Second

const (
	promptMinChars      = 20   // shorter prompts ("ok", "pode seguir") carry no topic
	promptQueryMaxChars = 500  // the search query is the head of the prompt
	promptSearchLimit   = 5    // candidates asked from the server
	promptMaxInjected   = 3    // entries injected per prompt
	promptTokenBudget   = 500  // estimated tokens injected per prompt
	promptDefaultScore  = 0.60 // cosine floor; see D44
)

// promptMinScore reads M3M0S_PROMPT_MIN_SCORE (0..1), falling back to
// promptDefaultScore.
func promptMinScore(getenv func(string) string) float64 {
	if v := getenv("M3M0S_PROMPT_MIN_SCORE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 1 {
			return f
		}
	}
	return promptDefaultScore
}

type promptOutput struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

// RunPromptSearch is the per-prompt "read" side (D44): it searches memory by
// the text of the user's prompt and writes the hits above the relevance
// floor as additional context. Silent whenever nothing qualifies (short or
// slash-command prompt, no credentials, server down or slow, nothing
// relevant, everything already injected in this session).
func RunPromptSearch(ctx context.Context, payload Payload, deps Deps) {
	if deps.Getenv("M3M0S_PROMPT_INJECT") == "off" {
		return
	}
	prompt := strings.TrimSpace(payload.Prompt)
	if utf8.RuneCountInString(prompt) < promptMinChars || strings.HasPrefix(prompt, "/") {
		return
	}
	if r := []rune(prompt); len(r) > promptQueryMaxChars {
		prompt = string(r[:promptQueryMaxChars])
	}

	baseURL, apiKey, ok := ResolveCredentials(deps.Getenv, deps.HomeDir)
	if !ok {
		return
	}
	c := deps.NewClient(baseURL, apiKey)

	ctx, cancel := context.WithTimeout(ctx, promptSearchTimeout)
	defer cancel()

	// Same layered scope as SessionStart (D34): global plus the session's
	// project; no project means global only.
	proj := deps.Getenv("M3M0S_INJECT_PROJECT")
	if proj == "" {
		proj = project.Resolve(deps.Getenv, deps.HomeDir, payload.Cwd)
	}
	scope := ""
	if proj == "" {
		scope = "global"
	}
	// Vector mode: only it yields a cosine score that can be thresholded;
	// the hybrid fusion score is rank-based and keyword-only hits get 0.
	resp, err := c.Search(ctx, apitypes.SearchRequest{
		Query:   prompt,
		Limit:   promptSearchLimit,
		Mode:    "vector",
		Project: proj,
		Scope:   scope,
	})
	if err != nil {
		_, _ = fmt.Fprintf(deps.Stderr, "m3m0s hook claude-code: prompt search failed: %v\n", err)
		return
	}

	floor := promptMinScore(deps.Getenv)
	seenPath, seen := loadSeen(deps.HomeDir, payload.SessionID)
	var hits []schema.Entry
	for _, e := range InjectableEntries(resp.Results) {
		if e.Score == nil || *e.Score < floor || seen[e.ID] {
			continue
		}
		hits = append(hits, e)
		if len(hits) == promptMaxInjected {
			break
		}
	}
	hits = apitypes.FitTokenBudget(hits, promptTokenBudget)
	if len(hits) == 0 {
		return
	}

	for _, e := range hits {
		seen[e.ID] = true
	}
	saveSeen(seenPath, seen)

	block := apitypes.BuildContextBlock(hits)
	var out promptOutput
	out.HookSpecificOutput.HookEventName = "UserPromptSubmit"
	out.HookSpecificOutput.AdditionalContext = block
	data, err := json.Marshal(out)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(deps.Stderr, "m3m0s hook claude-code: prompt search injected %d memories, ~%d tokens\n", len(hits), apitypes.EstimateTokens(block))
	_, _ = fmt.Fprintln(deps.Stdout, string(data))
}

// loadSeen returns the ids already injected by prompt search in this
// session, so the same memory is not repeated on every prompt. Any error
// degrades to "nothing seen yet".
func loadSeen(homeDir, sessionID string) (string, map[string]bool) {
	seen := map[string]bool{}
	if sessionID == "" {
		return "", seen
	}
	dir := filepath.Join(homeDir, ".m3m0s", "hooks", "claude-code")
	if os.MkdirAll(dir, 0o700) != nil {
		return "", seen
	}
	path := filepath.Join(dir, sessionID+".prompt-seen.json")
	if data, err := os.ReadFile(path); err == nil {
		var ids []string
		if json.Unmarshal(data, &ids) == nil {
			for _, id := range ids {
				seen[id] = true
			}
		}
	}
	return path, seen
}

func saveSeen(path string, seen map[string]bool) {
	if path == "" {
		return
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	if data, err := json.Marshal(ids); err == nil {
		_ = os.WriteFile(path, data, 0o600)
	}
}
