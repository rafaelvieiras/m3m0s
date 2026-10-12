package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
)

// flakyExtractor fails the first n calls, then returns one fact.
type flakyExtractor struct {
	failFirst int32
	calls     atomic.Int32
}

func (f *flakyExtractor) Extract(_ context.Context, _, _ string) ([]string, error) {
	if f.calls.Add(1) <= f.failFirst {
		return nil, errors.New("provider timeout")
	}
	return []string{"extracted fact"}, nil
}

func fastIngest(t *testing.T) {
	t.Helper()
	oldB, oldP := ingestBackoff, ingestPollInterval
	ingestBackoff = []time.Duration{20 * time.Millisecond, 20 * time.Millisecond}
	ingestPollInterval = 20 * time.Millisecond
	t.Cleanup(func() { ingestBackoff, ingestPollInterval = oldB, oldP })
}

func pollPending(t *testing.T, base, key, id string) apitypes.PendingAddResponse {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, base+"/api/v1/memories/pending/"+id, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var out apitypes.PendingAddResponse
		_ = json.NewDecoder(resp.Body).Decode(&out)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK && out.Status != "pending" {
			return out
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("operation %s did not finish", id)
	return apitypes.PendingAddResponse{}
}

func TestIngestRetriesExtractionBeforeStoringRaw(t *testing.T) {
	fastIngest(t)
	ext := &flakyExtractor{failFirst: 2} // both in-piece attempts (D19) fail once; the queue retry then succeeds
	srv, key := newTestServerWithExtractor(t, 20*time.Millisecond, ext)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)

	resp := doAddRequest(t, hs, key, apitypes.AddRequest{Content: "retry me", Domain: "code:m3m0s"})
	var accepted apitypes.AddPendingResponse
	_ = json.NewDecoder(resp.Body).Decode(&accepted)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (extraction is retrying)", resp.StatusCode)
	}
	if accepted.OperationID == "" {
		t.Fatal("202 without operationId")
	}
	out := pollPending(t, hs.URL, key, accepted.OperationID)
	if out.Status != "completed" || out.Result == nil || len(out.Result.Entries) != 1 {
		t.Fatalf("poll = %+v", out)
	}
	e := out.Result.Entries[0].Entry
	if e.Content != "extracted fact" || e.Metadata["extraction"] == "raw-fallback" {
		t.Fatalf("entry = %q %v, want the extracted fact, not a raw fallback", e.Content, e.Metadata)
	}
}

func TestIngestStoresRawFallbackOnLastAttempt(t *testing.T) {
	fastIngest(t)
	ext := &flakyExtractor{failFirst: 100}
	srv, key := newTestServerWithExtractor(t, 20*time.Millisecond, ext)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)

	resp := doAddRequest(t, hs, key, apitypes.AddRequest{Content: "never extracted", Domain: "code:m3m0s"})
	var accepted apitypes.AddPendingResponse
	_ = json.NewDecoder(resp.Body).Decode(&accepted)
	_ = resp.Body.Close()
	out := pollPending(t, hs.URL, key, accepted.OperationID)
	if out.Status != "completed" || out.Result == nil || len(out.Result.Entries) != 1 {
		t.Fatalf("poll = %+v", out)
	}
	if got := out.Result.Entries[0].Entry.Metadata["extraction"]; got != "raw-fallback" {
		t.Fatalf("metadata.extraction = %q, want raw-fallback", got)
	}
	// 3 queue attempts (2 retries + the final raw one) x 2 in-piece attempts (D19).
	if n := ext.calls.Load(); n != 6 {
		t.Errorf("extractor calls = %d, want 6", n)
	}
}

// A row left in the database by a previous process is picked up at boot.
func TestIngestRecoversQueuedOperationAfterRestart(t *testing.T) {
	fastIngest(t)
	srv, key := newTestServerWithExtractor(t, 20*time.Millisecond, &flakyExtractor{})
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)

	payload, _ := json.Marshal(apitypes.AddRequest{Content: "survived a restart", Domain: "code:m3m0s"})
	ctx := context.Background()
	if err := srv.store.DB().IngestEnqueue(ctx, "op_restart1", "code:m3m0s", string(payload), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := srv.store.DB().IngestMarkRunning(ctx, "op_restart1", time.Now()); err != nil { // as a crash would leave it
		t.Fatal(err)
	}
	srv.StartIngest() // boot: recover running rows and start workers

	out := pollPending(t, hs.URL, key, "op_restart1")
	if out.Status != "completed" || out.Result == nil || len(out.Result.Entries) != 1 {
		t.Fatalf("poll = %+v", out)
	}
}

func TestIngestQueueFullAnswers503(t *testing.T) {
	srv, key := newTestServerWithExtractor(t, 20*time.Millisecond, &flakyExtractor{})
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)

	srv.ingest.queueMax = 1
	ctx := context.Background()
	db := srv.store.DB()
	if err := db.IngestEnqueue(ctx, "op_blocker", "code:m3m0s", `{}`, time.Now()); err != nil {
		t.Fatal(err)
	}
	_ = db.IngestRetry(ctx, "op_blocker", 1, time.Now().Add(time.Hour), "x", time.Now()) // pending far in the future

	resp := doAddRequest(t, hs, key, apitypes.AddRequest{Content: "overflow", Domain: "code:m3m0s"})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("status = %d retry-after=%q, want 503 with Retry-After", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
}
