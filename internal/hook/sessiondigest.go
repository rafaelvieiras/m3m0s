package hook

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// DigestFetcher returns up to limit memories of one kind ("" = any kind) for
// the session's layered scope. It may be called concurrently.
type DigestFetcher func(kind string, limit int) []schema.Entry

// defaultDigestKinds is the kind priority of the session-start digest (D57):
// standing instructions and stated preferences of the project travel between
// sessions, while /recent alone mostly returns whatever the last minutes of
// capture produced.
const defaultDigestKinds = "rule,preference"

// DigestKinds reads M3M0S_INJECT_KINDS: a comma-separated kind priority for
// the digest, "off" to keep plain /recent order, empty for the default.
// Exported so the WebUI injection preview (D29) builds the same digest.
func DigestKinds(getenv func(string) string) []string {
	raw := strings.TrimSpace(getenv("M3M0S_INJECT_KINDS"))
	if raw == "" {
		raw = defaultDigestKinds
	}
	if strings.EqualFold(raw, "off") {
		return nil
	}
	var out []string
	for _, k := range strings.Split(raw, ",") {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

// digestFillerReserve is how many slots of the digest stay for plain
// recency when priority kinds are configured: a digest made only of standing
// rules loses the "what were we doing" thread, which is what /recent is for.
const digestFillerReserve = 3

// BuildSessionDigest assembles the memory block injected at session start (D57).
//
// One fetch per priority kind plus one plain /recent fetch run in parallel;
// results are merged in that order (kind by kind, each in the server's own
// recency order) and the plain fetch fills whatever the priority kinds did
// not supply. The merged list then goes through InjectableEntries (raw
// fallbacks dropped, hard cap) and FitTokenBudget (same caps as D17).
//
// Why not simply reorder one large /recent window: at a capture rate of tens
// of thousands of facts per day, a window big enough to reach an older rule
// would be thousands of entries, and the store has no "kind + recency"
// index anyway; kind filters are cheap (D57).
func BuildSessionDigest(fetch DigestFetcher, kinds []string, budget int) []schema.Entry {
	// Priority kinds first, in the configured order and capped so that
	// digestFillerReserve slots remain; the plain fetch comes last and only
	// fills what they did not supply ("" = any kind).
	quota := SessionStartFetchLimit
	if len(kinds) > 0 {
		quota = (SessionStartRecentLimit - digestFillerReserve) / len(kinds)
		if quota < 1 {
			quota = 1
		}
	}
	order := append(append([]string{}, kinds...), "")
	limits := make([]int, len(order))
	for i := range order {
		if i < len(kinds) {
			limits[i] = quota
		} else {
			limits[i] = SessionStartFetchLimit
		}
	}
	lists := make([][]schema.Entry, len(order))
	var wg sync.WaitGroup
	for i, kind := range order {
		wg.Add(1)
		go func(i int, kind string, limit int) {
			defer wg.Done()
			lists[i] = fetch(kind, limit)
		}(i, kind, limits[i])
	}
	wg.Wait()

	seen := make(map[string]bool)
	merged := make([]schema.Entry, 0, SessionStartRecentLimit)
	for _, list := range lists {
		for _, e := range list {
			if seen[e.ID] {
				continue
			}
			seen[e.ID] = true
			merged = append(merged, e)
		}
	}
	return apitypes.FitTokenBudget(InjectableEntries(merged), budget)
}

// recentFetcher adapts the API client to DigestFetcher for one fixed scope.
// A failed kind is skipped (the digest is best-effort, D17 item 5) but the
// error is written once to errw, so the hook log still shows it.
func recentFetcher(ctx context.Context, c recentCaller, params apitypes.RecentParams, errw io.Writer) DigestFetcher {
	var once sync.Once
	return func(kind string, limit int) []schema.Entry {
		p := params
		p.Kind = kind
		p.Limit = limit
		resp, err := c.Recent(ctx, p)
		if err != nil {
			once.Do(func() {
				_, _ = fmt.Fprintf(errw, "m3m0s hook claude-code: GET /api/v1/recent failed: %v\n", err)
			})
			return nil
		}
		return resp.Results
	}
}

// recentCaller is the subset of *client.Client the digest needs; an interface
// so tests can inject a fake without a server.
type recentCaller interface {
	Recent(ctx context.Context, params apitypes.RecentParams) (apitypes.RecentResponse, error)
}
