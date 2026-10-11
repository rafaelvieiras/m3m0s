package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/auth"
	"github.com/rafaelvieiras/m3m0s/internal/factkind"
	"github.com/rafaelvieiras/m3m0s/internal/jobs"
	"github.com/rafaelvieiras/m3m0s/internal/store"
)

// kindProvider is a fake extraction provider: ExtractFacts is never reached
// (the tests add with extraction off), ClassifyKinds answers "rule".
type kindProvider struct{}

func (kindProvider) Extract(context.Context, string, string) ([]string, error) { return nil, nil }
func (kindProvider) ClassifyKinds(_ context.Context, facts []string) ([]string, error) {
	out := make([]string, len(facts))
	for i := range out {
		out[i] = factkind.Rule
	}
	return out, nil
}

func newKindServer(t *testing.T, memStore *store.MemoryStore) (base, key string) {
	t.Helper()
	keyStore, _ := auth.LoadStore(memStore.DB())
	rawKey, _, _ := keyStore.Create("test")
	sched := jobs.NewScheduler(time.Hour)
	sched.Register(jobs.Job{
		Name: store.KindBackfillJobName,
		Run:  func(ctx context.Context) (string, error) { return memStore.RunKindBackfill(ctx) },
	})
	srv := httptest.NewServer(New(memStore, keyStore, nil, nil, 0, sched, memStore.DB()).Handler())
	t.Cleanup(srv.Close)
	return srv.URL, rawKey
}

func TestKindOnTheWire(t *testing.T) {
	memStore, err := store.New(t.TempDir(), "fake-key", fakeEmbedServer(t).URL)
	if err != nil {
		t.Fatal(err)
	}
	base, key := newKindServer(t, memStore)

	add := func(content, domain string) string {
		code, body := doReq(t, base, key, "POST", "/api/v1/memories", `{"content":"`+content+`","domain":"`+domain+`"}`)
		if code/100 != 2 {
			t.Fatalf("add = %d %s", code, body)
		}
		var r struct {
			Entries []struct {
				Entry struct{ ID string }
			}
		}
		_ = json.Unmarshal([]byte(body), &r)
		return r.Entries[0].Entry.ID
	}
	ruleID := add("Always run the linter before pushing.", "code:one")
	add("Status: waiting for review on branch alpha.", "code:two")

	// kind and kindSource are top-level fields of the memory JSON
	code, body := doReq(t, base, key, "GET", "/api/v1/memories/"+ruleID, "")
	var e struct {
		Kind       string
		KindSource string
		Metadata   map[string]string
	}
	_ = json.Unmarshal([]byte(body), &e)
	if code != 200 || e.Kind != "rule" || e.KindSource != "heuristic" || e.Metadata["kind"] != "rule" {
		t.Fatalf("get = %d %s", code, body)
	}

	// filters on recent and search
	code, body = doReq(t, base, key, "GET", "/api/v1/recent?kind=state", "")
	if code != 200 || strings.Count(body, `"id"`) != 1 || !strings.Contains(body, `"kind":"state"`) {
		t.Fatalf("recent kind=state = %d %s", code, body)
	}
	code, body = doReq(t, base, key, "POST", "/api/v1/search", `{"query":"linter","domain":"code:one","kind":"rule"}`)
	if code != 200 || !strings.Contains(body, ruleID) {
		t.Fatalf("search kind=rule = %d %s", code, body)
	}
	code, body = doReq(t, base, key, "POST", "/api/v1/search", `{"query":"linter","domain":"code:one","kind":"state"}`)
	if code != 200 || strings.Contains(body, ruleID) {
		t.Fatalf("search kind=state must exclude it = %d %s", code, body)
	}
	if code, body = doReq(t, base, key, "GET", "/api/v1/recent?kind=bogus", ""); code != 400 || !strings.Contains(body, "invalid_kind") {
		t.Fatalf("recent kind=bogus = %d %s", code, body)
	}
	if code, body = doReq(t, base, key, "POST", "/api/v1/search", `{"query":"x","kind":"bogus"}`); code != 400 || !strings.Contains(body, "invalid_kind") {
		t.Fatalf("search kind=bogus = %d %s", code, body)
	}

	// PATCH kind: manual correction
	code, body = doReq(t, base, key, "PATCH", "/api/v1/memories/"+ruleID, `{"kind":"preference"}`)
	_ = json.Unmarshal([]byte(body), &e)
	if code != 200 || e.Kind != "preference" || e.KindSource != "manual" {
		t.Fatalf("patch kind = %d %s", code, body)
	}
	if code, body = doReq(t, base, key, "PATCH", "/api/v1/memories/"+ruleID, `{"kind":"bogus"}`); code != 400 || !strings.Contains(body, "invalid_kind") {
		t.Fatalf("patch bogus kind = %d %s", code, body)
	}
	if code, _ = doReq(t, base, key, "PATCH", "/api/v1/memories/"+ruleID, `{"kind":3}`); code != 400 {
		t.Fatalf("patch non-string kind = %d", code)
	}

	// insights overview carries the distribution
	code, body = doReq(t, base, key, "GET", "/api/v1/insights/overview", "")
	var ov struct {
		ByKind []struct {
			Key   string
			Count int
		} `json:"by_kind"`
		ByKindSource []struct {
			Key   string
			Count int
		} `json:"by_kind_source"`
	}
	_ = json.Unmarshal([]byte(body), &ov)
	got := map[string]int{}
	for _, c := range ov.ByKind {
		got[c.Key] = c.Count
	}
	if code != 200 || len(ov.ByKind) != len(factkind.All) || got["preference"] != 1 || got["state"] != 1 || len(ov.ByKindSource) != 4 {
		t.Fatalf("overview = %d %s", code, body)
	}

	// sample endpoint: auth, bounds, content
	if code, _ = doReq(t, base, "", "GET", "/api/v1/kinds/sample", ""); code != http.StatusUnauthorized {
		t.Errorf("anonymous sample = %d", code)
	}
	if code, _ = doReq(t, base, key, "GET", "/api/v1/kinds/sample?n=0", ""); code != 400 {
		t.Errorf("sample n=0 = %d", code)
	}
	if code, _ = doReq(t, base, key, "GET", "/api/v1/kinds/sample?n=100000", ""); code != 400 {
		t.Errorf("sample n too large = %d", code)
	}
	code, body = doReq(t, base, key, "GET", "/api/v1/kinds/sample?n=5&seed=3", "")
	if code != 200 || strings.Count(body, `"label":""`) != 2 {
		t.Fatalf("sample = %d %s", code, body)
	}
}

func TestKindBackfillEndpoints(t *testing.T) {
	memStore, err := store.New(t.TempDir(), "fake-key", fakeEmbedServer(t).URL)
	if err != nil {
		t.Fatal(err)
	}
	memStore.SetKindBackfillOptions(store.KindBackfillOptions{BatchSize: 10, RatePerSec: 10000})
	base, key := newKindServer(t, memStore)
	for i, c := range []string{"alpha one", "beta two"} {
		doReq(t, base, key, "POST", "/api/v1/memories", `{"content":"`+c+`","domain":"code:d`+string(rune('a'+i))+`"}`)
	}
	memStore.SetExtractor(kindProvider{})
	// The entries above were tagged by the heuristic, which an LLM run improves.

	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/v1/kinds/backfill/estimate"}, {"GET", "/api/v1/kinds/backfill"},
		{"POST", "/api/v1/kinds/backfill"}, {"DELETE", "/api/v1/kinds/backfill"},
	} {
		if code, _ := doReq(t, base, "", tc.method, tc.path, ""); code != http.StatusUnauthorized {
			t.Errorf("anonymous %s %s = %d, want 401", tc.method, tc.path, code)
		}
	}

	code, body := doReq(t, base, key, "POST", "/api/v1/kinds/backfill/estimate", "")
	var est map[string]any
	_ = json.Unmarshal([]byte(body), &est)
	if code != 200 || est["mode"] != "llm" || est["pending"] != float64(2) || est["requests"] != float64(1) || est["approximate"] != true || est["estimated_cost_usd"] != nil {
		t.Fatalf("estimate = %d %s", code, body)
	}

	// Without confirmation nothing starts and the provider is not touched.
	if code, body = doReq(t, base, key, "POST", "/api/v1/kinds/backfill", ""); code != http.StatusConflict || !strings.Contains(body, "backfill_confirmation_required") {
		t.Fatalf("unconfirmed start = %d %s", code, body)
	}
	if code, body = doReq(t, base, key, "POST", "/api/v1/kinds/backfill", `{"confirm":false}`); code != http.StatusConflict {
		t.Fatalf("confirm=false start = %d %s", code, body)
	}
	if code, _ = doReq(t, base, key, "DELETE", "/api/v1/kinds/backfill", ""); code != http.StatusConflict {
		t.Errorf("cancel with nothing running = %d, want 409", code)
	}

	if code, body = doReq(t, base, key, "POST", "/api/v1/kinds/backfill", `{"confirm":true}`); code != http.StatusAccepted {
		t.Fatalf("confirmed start = %d %s", code, body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, body = doReq(t, base, key, "GET", "/api/v1/kinds/backfill", "")
		if strings.Contains(body, `"finished_at"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job never finished: %s", body)
		}
		time.Sleep(5 * time.Millisecond)
	}
	var pr struct {
		Running             bool
		Mode                string
		Total, Done, Errors int
	}
	_ = json.Unmarshal([]byte(body), &pr)
	if pr.Running || pr.Mode != "llm" || pr.Done != 2 || pr.Total != 2 || pr.Errors != 0 {
		t.Errorf("final progress = %s", body)
	}
	if code, body = doReq(t, base, key, "GET", "/api/v1/recent?kind=rule", ""); code != 200 || strings.Count(body, `"kindSource":"llm"`) != 2 {
		t.Errorf("entries should be tagged rule/llm: %d %s", code, body)
	}
}
