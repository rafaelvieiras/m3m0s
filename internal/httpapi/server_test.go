package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/auth"
	"github.com/rafaelvieiras/m3m0s/internal/store"
)

// fakeEmbedServer starts a minimal OpenAI-compatible embeddings fake; same
// contract used in internal/store, a local version so this package does not
// depend on test exports from another package.
func fakeEmbedServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /embeddings", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": []float32{1, 0, 0, 0}}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// newTestServer creates a test Server with addSyncTimeout = 0, the D26 escape
// hatch (docs/decisions/), which always waits for the Add to really finish.
// Most tests in this package do not care about the asynchronous path; those
// that need a short timeout to exercise the 202 use
// newTestServerWithAddTimeout.
func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	return newTestServerWithAddTimeout(t, 0)
}

// newTestServerWithAddTimeout is newTestServer with a configurable
// addSyncTimeout (D26); used by the handleAdd/handlePendingAdd tests that need
// to force the synchronous path (long timeout/0) or the asynchronous one
// (short timeout, extractor with a controllable delay).
func newTestServerWithAddTimeout(t *testing.T, addSyncTimeout time.Duration) (*Server, string) {
	t.Helper()
	embed := fakeEmbedServer(t)
	memStore, err := store.New(t.TempDir(), "fake-key", embed.URL)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	keyStore, err := auth.LoadStore(memStore.DB())
	if err != nil {
		t.Fatalf("auth.LoadStore: %v", err)
	}
	rawKey, _, err := keyStore.Create("test")
	if err != nil {
		t.Fatalf("keyStore.Create: %v", err)
	}
	return New(memStore, keyStore, nil, nil, addSyncTimeout, nil, nil), rawKey
}

// TestHandleStatsIncludesTelemetry checks that GET /stats returns the
// telemetry field with the expected operational data: 1 key (the bootstrap
// one created above), positive process memory and uptime, and zeroed token
// counters (no extractor configured in this test).
func TestHandleStatsIncludesTelemetry(t *testing.T) {
	srv, rawKey := newTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	req, err := http.NewRequest(http.MethodGet, httpSrv.URL+"/api/v1/stats", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+rawKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var out apitypes.StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if out.Telemetry.APIKeyCount != 1 {
		t.Errorf("Telemetry.APIKeyCount = %d, want 1", out.Telemetry.APIKeyCount)
	}
	if out.Telemetry.MemoryUsageMB <= 0 {
		t.Errorf("Telemetry.MemoryUsageMB = %v, want > 0", out.Telemetry.MemoryUsageMB)
	}
	if out.Telemetry.UptimeSeconds < 0 {
		t.Errorf("Telemetry.UptimeSeconds = %v, want >= 0", out.Telemetry.UptimeSeconds)
	}
	if out.Telemetry.DatabaseSizeMB < 0 {
		t.Errorf("Telemetry.DatabaseSizeMB = %v, want >= 0", out.Telemetry.DatabaseSizeMB)
	}
	if out.Telemetry.ExtractionTokensUsed != 0 {
		t.Errorf("Telemetry.ExtractionTokensUsed = %d, want 0 without extractor", out.Telemetry.ExtractionTokensUsed)
	}
	if out.Telemetry.TokensSavedByCompaction != 0 {
		t.Errorf("Telemetry.TokensSavedByCompaction = %d, want 0 without entries", out.Telemetry.TokensSavedByCompaction)
	}
}

func TestBytesToMB(t *testing.T) {
	cases := []struct {
		bytes uint64
		want  float64
	}{
		{0, 0},
		{1024 * 1024, 1},
		{1536 * 1024, 1.5},
		{1024*1024*1 + 512*1024, 1.5},
	}
	for _, c := range cases {
		if got := bytesToMB(c.bytes); got != c.want {
			t.Errorf("bytesToMB(%d) = %v, want %v", c.bytes, got, c.want)
		}
	}
}

func TestHandleSearchModesAndTokenBudget(t *testing.T) {
	srv, rawKey := newTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	post := func(body string) (*http.Response, apitypes.SearchResponse) {
		req, _ := http.NewRequest("POST", httpSrv.URL+"/api/v1/search", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+rawKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var out apitypes.SearchResponse
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp, out
	}
	for i, c := range []string{"alpha notes: servers reboot nightly", "!!! 12345 alpha notes ### 67890", "zzzz qqqq alpha notes xxxx jjjj"} {
		req, _ := http.NewRequest("POST", httpSrv.URL+"/api/v1/memories", strings.NewReader(fmt.Sprintf(`{"content":%q,"domain":"code:t%d","source":"t"}`, c+strings.Repeat(" pad", i*6), i)))
		req.Header.Set("Authorization", "Bearer "+rawKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode >= 300 {
			t.Fatalf("seed %d: %v %v", i, err, resp)
		}
		_ = resp.Body.Close()
	}

	resp, out := post(`{"query":"alpha notes","mode":"keyword","limit":5}`)
	if resp.StatusCode != 200 || len(out.Results) != 3 || out.EstimatedTokens == 0 {
		t.Fatalf("keyword search: status %d, %d results, %d tokens", resp.StatusCode, len(out.Results), out.EstimatedTokens)
	}
	_, capped := post(`{"query":"alpha notes","mode":"keyword","limit":5,"maxTokens":30}`)
	if len(capped.Results) >= 3 || capped.EstimatedTokens > 30 {
		t.Errorf("maxTokens 30: %d results, ~%d tokens", len(capped.Results), capped.EstimatedTokens)
	}
	if resp, _ := post(`{"query":"x","mode":"bogus"}`); resp.StatusCode != 400 {
		t.Errorf("invalid mode: status %d, want 400", resp.StatusCode)
	}
}

// A misspelled term is repaired by the search handler (D51): the response
// names the query actually searched and the context block says so.
func TestHandleSearchReportsCorrectedQuery(t *testing.T) {
	srv, rawKey := newTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	do := func(method, path, body string) *http.Response {
		req, _ := http.NewRequest(method, httpSrv.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+rawKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	for i, c := range []string{"The orchestrator schedules nightly batches", "The orchestrator retries failed batches", "Restart the orchestrator after an upgrade"} {
		resp := do("POST", "/api/v1/memories", fmt.Sprintf(`{"content":%q,"domain":"code:t%d","source":"t"}`, c, i))
		if resp.StatusCode >= 300 {
			t.Fatalf("seed %d: status %d", i, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	resp := do("POST", "/api/v1/search", `{"query":"orchestrater","mode":"keyword","limit":5}`)
	defer func() { _ = resp.Body.Close() }()
	var out apitypes.SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || len(out.Results) != 3 {
		t.Fatalf("status %d, %d results", resp.StatusCode, len(out.Results))
	}
	if out.CorrectedQuery != "orchestrator" || !strings.Contains(out.ContextBlock, "misspelled terms were corrected") {
		t.Errorf("correctedQuery = %q, block = %q", out.CorrectedQuery, out.ContextBlock)
	}
}
