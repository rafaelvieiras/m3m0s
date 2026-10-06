package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/project"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// sessionStartTimeout is the context ceiling applied only to the
// RunSessionStart call (D17), via context.WithTimeout rather than the generic
// client.New (whose 30s timeout is designed for asynchronous/CLI calls,
// where waiting bothers nobody). It is much shorter because this call is
// synchronous and blocks the start of ANY Claude Code session in ANY project
// (not only those using m3m0s). If the m3m0s server does not answer quickly,
// it is better to open the session without extra context than to make the
// user wait.
const sessionStartTimeout = 5 * time.Second

// SessionStartRecentLimit is how many memories (already filtered, see
// InjectableEntries) go into the injected digest. It is small on purpose:
// each entry is an atomic fact curated by extraction (Phase 4/D12), so even
// at the limit this stays well below what it would cost the model to
// rediscover this context by reading files/exploring the repo from scratch.
// Exported (D29) so internal/httpapi/preview.go replicates exactly the same
// cut in the WebUI injection preview, without duplicating the number.
const SessionStartRecentLimit = 15

// SessionStartFetchLimit is how many memories to request from the server
// BEFORE the InjectableEntries filter. It is deliberately larger than
// SessionStartRecentLimit (D17, found in production): entries without
// extraction (raw-content fallback when the fact pipeline times out, D12
// item 5) tend to cluster at the top of /recent, since they are the most
// recent by definition, having just been inserted. Requesting only 15 and
// filtering afterwards could leave almost nothing useful in a session where
// the last 2-3 digests timed out in a row; requesting a larger window before
// filtering gives the filter room not to empty the result needlessly.
// Exported (D29) for the same reason as SessionStartRecentLimit.
const SessionStartFetchLimit = 40

// DefaultInjectTokenBudget caps the estimated tokens of the context injected
// at session start (F5). Override per client with M3M0S_INJECT_MAX_TOKENS
// (0 disables the cap). Exported so the WebUI injection preview applies the
// same default.
const DefaultInjectTokenBudget = 1000

// injectTokenBudget reads M3M0S_INJECT_MAX_TOKENS (a non-negative integer),
// falling back to DefaultInjectTokenBudget.
func injectTokenBudget(getenv func(string) string) int {
	if v := getenv("M3M0S_INJECT_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return DefaultInjectTokenBudget
}

// injectableSources are the Payload.Source values for which injecting
// context makes sense (D17): "startup" (new session) and "clear" (/clear)
// are the cases where the session genuinely has no context at all, which is
// where token savings are real. "resume" already carries the full history of
// the previous session; "compact" already has the summary Claude Code itself
// just generated. Injecting in those two would be, at best, redundant and,
// at worst, noise competing with context that already exists.
var injectableSources = map[string]bool{
	"startup": true,
	"clear":   true,
}

// sessionStartOutput is the shape documented by Claude Code for a
// SessionStart hook to inject context: JSON on stdout, exit 0, without
// "async": true (asynchronous hooks cannot inject anything, since they run
// after the session has already started).
type sessionStartOutput struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

// RunSessionStart implements the "read" side of the hook (D17, mirroring the
// "write" side of D14): it fetches the recent memories of the current domain
// and, if there are any, writes to deps.Stdout the JSON that Claude Code
// interprets as additional context to inject into the new session. It is
// silent in every case that does not justify injection (ineligible source,
// no credentials, m3m0s down, or a domain with no memories yet) and never
// writes anything to deps.Stdout in those cases (D17 item 5: injecting "found
// nothing" would cost tokens for no reason).
func RunSessionStart(ctx context.Context, payload Payload, deps Deps) {
	if !injectableSources[payload.Source] {
		return
	}

	baseURL, apiKey, ok := ResolveCredentials(deps.Getenv, deps.HomeDir)
	if !ok {
		_, _ = fmt.Fprintf(deps.Stderr, "m3m0s hook claude-code: missing M3M0S_URL/M3M0S_API_KEY credentials (environment or ~/.m3m0s/hook.env), context injection skipped\n")
		return
	}

	c := deps.NewClient(baseURL, apiKey)
	domain := deps.Domain(payload.Cwd)

	ctx, cancel := context.WithTimeout(ctx, sessionStartTimeout)
	defer cancel()

	// Layered recall: global memory plus the session's project; a session
	// with no project sees only global memory, never other projects' (D34).
	proj := deps.Getenv("M3M0S_INJECT_PROJECT")
	if proj == "" {
		proj = project.Resolve(deps.Getenv, deps.HomeDir, payload.Cwd)
	}
	scope := ""
	if proj == "" {
		scope = "global"
	}
	resp, err := c.Recent(ctx, apitypes.RecentParams{
		Domain:  domain,
		Limit:   SessionStartFetchLimit,
		Source:  deps.Getenv("M3M0S_INJECT_SOURCE"),
		Project: proj,
		Scope:   scope,
	})
	if err != nil {
		_, _ = fmt.Fprintf(deps.Stderr, "m3m0s hook claude-code: GET /api/v1/recent failed: %v\n", err)
		return
	}

	budget := injectTokenBudget(deps.Getenv)
	extracted := apitypes.FitTokenBudget(InjectableEntries(resp.Results), budget)
	if len(extracted) == 0 {
		return
	}

	block := apitypes.BuildContextBlock(extracted)
	_, _ = fmt.Fprintf(deps.Stderr, "m3m0s hook claude-code: injected %d memories, ~%d tokens (budget %d) for %s\n", len(extracted), apitypes.EstimateTokens(block), budget, domain)

	var out sessionStartOutput
	out.HookSpecificOutput.HookEventName = "SessionStart"
	out.HookSpecificOutput.AdditionalContext = block

	data, err := json.Marshal(out)
	if err != nil {
		_, _ = fmt.Fprintf(deps.Stderr, "m3m0s hook claude-code: marshaling additionalContext failed: %v\n", err)
		return
	}
	_, _ = fmt.Fprintln(deps.Stdout, string(data))
}

// InjectableEntries filters to keep only entries that actually went through
// the fact extraction pipeline (Phase 4/D12, signaled by
// metadata.source_content) and cuts the result at SessionStartRecentLimit.
// It exists to never inject the raw-content fallback when extraction times
// out (D12 item 5): a whole, uncurated compaction digest is noise at the
// start of a new session, sometimes literally redundant with the summary
// Claude Code itself just generated (found in production, D17). Those
// entries continue to exist normally and are picked up by retroactive
// extraction (D16) in the job's next round; this filter only avoids
// injecting them BEFORE they are curated, it does not discard them.
// Exported (D29) so internal/httpapi/preview.go reproduces exactly the same
// filter in the "hook" mode of the WebUI injection preview.
func InjectableEntries(entries []schema.Entry) []schema.Entry {
	out := make([]schema.Entry, 0, len(entries))
	for _, e := range entries {
		if e.Metadata[apitypes.MetadataKeySourceContent] == "" {
			continue
		}
		out = append(out, e)
		if len(out) == SessionStartRecentLimit {
			break
		}
	}
	return out
}
