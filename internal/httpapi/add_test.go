package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/auth"
	"github.com/rafaelvieiras/m3m0s/internal/store"
)

// delayedExtractor implements store.Extractor with a delay controllable by the
// test (same spirit as fakeExtractor in internal/store/store_test.go, which
// has no delay support; just what this package's tests need to force
// handleAdd to exceed addSyncTimeout on purpose, D26).
type delayedExtractor struct {
	delay time.Duration
	facts []string
}

func (f *delayedExtractor) Extract(ctx context.Context, _, _ string) ([]string, error) {
	select {
	case <-time.After(f.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return f.facts, nil
}

// newTestServerWithExtractor is like newTestServerWithAddTimeout, but the
// *store.MemoryStore already has the given extractor configured via
// SetExtractor; used by the handleAdd/handlePendingAdd tests (D26) that need
// to control how long the Add pipeline takes to finish.
func newTestServerWithExtractor(t *testing.T, addSyncTimeout time.Duration, extractor store.Extractor) (*Server, string) {
	t.Helper()
	embed := fakeEmbedServer(t)
	memStore, err := store.New(t.TempDir(), "fake-key", embed.URL)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	memStore.SetExtractor(extractor)
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

func doAddRequest(t *testing.T, httpSrv *httptest.Server, rawKey string, body apitypes.AddRequest) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, httpSrv.URL+"/api/v1/memories", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+rawKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	return resp
}

// TestHandleAddSyncWhenFast checks the usual synchronous path: a generous
// addSyncTimeout (1s) and an instant extraction finish well within the
// window, so the response is a direct 200, with Status "completed" (additive
// field, D26) and the entries already resolved, with no operationId and no
// need to poll.
func TestHandleAddSyncWhenFast(t *testing.T) {
	srv, rawKey := newTestServerWithExtractor(t, 1*time.Second, &delayedExtractor{delay: 0, facts: []string{"quick fact"}})
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	resp := doAddRequest(t, httpSrv, rawKey, apitypes.AddRequest{
		Content: "synchronous test content",
		Domain:  "code:m3m0s",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out apitypes.AddResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Status != "completed" {
		t.Errorf("Status = %q, want %q", out.Status, "completed")
	}
	if len(out.Entries) != 1 {
		t.Fatalf("len(Entries) = %d, want 1", len(out.Entries))
	}
	if out.Entries[0].Entry.Content != "quick fact" {
		t.Errorf("Entries[0].Entry.Content = %q, want %q", out.Entries[0].Entry.Content, "quick fact")
	}
}

// TestHandleAddAsyncWhenSlowThenPollCompletes checks the asynchronous path
// (D26): a very short addSyncTimeout (50ms) against a deliberately slow
// extraction (300ms) forces handleAdd to answer 202 before the Add finishes.
// The test then polls GET /memories/pending/:id until the operation leaves
// "pending", confirming that the final result matches what a synchronous Add
// would have returned.
func TestHandleAddAsyncWhenSlowThenPollCompletes(t *testing.T) {
	srv, rawKey := newTestServerWithExtractor(t, 50*time.Millisecond, &delayedExtractor{delay: 300 * time.Millisecond, facts: []string{"slow fact"}})
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	resp := doAddRequest(t, httpSrv, rawKey, apitypes.AddRequest{
		Content: "asynchronous test content",
		Domain:  "code:m3m0s",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	var pendingResp apitypes.AddPendingResponse
	if err := json.NewDecoder(resp.Body).Decode(&pendingResp); err != nil {
		t.Fatalf("decode 202: %v", err)
	}
	if pendingResp.Status != "pending" {
		t.Errorf("Status = %q, want %q", pendingResp.Status, "pending")
	}
	if pendingResp.OperationID == "" {
		t.Fatal("empty OperationID in the 202 response")
	}

	pollURL := httpSrv.URL + "/api/v1/memories/pending/" + pendingResp.OperationID
	deadline := time.Now().Add(2 * time.Second)
	var final apitypes.PendingAddResponse
	for {
		pollReq, err := http.NewRequest(http.MethodGet, pollURL, nil)
		if err != nil {
			t.Fatalf("NewRequest poll: %v", err)
		}
		pollReq.Header.Set("Authorization", "Bearer "+rawKey)
		pollResp, err := http.DefaultClient.Do(pollReq)
		if err != nil {
			t.Fatalf("Do poll: %v", err)
		}
		if pollResp.StatusCode != http.StatusOK {
			pollResp.Body.Close()
			t.Fatalf("poll status = %d, want 200", pollResp.StatusCode)
		}
		if err := json.NewDecoder(pollResp.Body).Decode(&final); err != nil {
			pollResp.Body.Close()
			t.Fatalf("decode poll: %v", err)
		}
		pollResp.Body.Close()

		if final.Status != "pending" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("poll never left pending within the deadline")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if final.Status != "completed" {
		t.Fatalf("Status final = %q, want %q (Error=%q)", final.Status, "completed", final.Error)
	}
	if final.Result == nil {
		t.Fatal("Result is nil on a completed operation")
	}
	if len(final.Result.Entries) != 1 || final.Result.Entries[0].Entry.Content != "slow fact" {
		t.Errorf("unexpected Result.Entries: %+v", final.Result.Entries)
	}
	if final.Result.Status != "completed" {
		t.Errorf("Result.Status = %q, want %q", final.Result.Status, "completed")
	}
}

// TestHandleAddSyncTimeoutDisabledAlwaysWaits checks the D26 escape hatch:
// addSyncTimeout <= 0 always waits for the Add to really finish, even if
// extraction takes longer than any short timeout used in the other tests; it
// should never return 202 in this mode.
func TestHandleAddSyncTimeoutDisabledAlwaysWaits(t *testing.T) {
	srv, rawKey := newTestServerWithExtractor(t, 0, &delayedExtractor{delay: 80 * time.Millisecond, facts: []string{"awaited fact"}})
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	resp := doAddRequest(t, httpSrv, rawKey, apitypes.AddRequest{
		Content: "escape hatch test content",
		Domain:  "code:m3m0s",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (addSyncTimeout <= 0 should never return 202)", resp.StatusCode)
	}
	var out apitypes.AddResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Entries) != 1 || out.Entries[0].Entry.Content != "awaited fact" {
		t.Errorf("unexpected Entries: %+v", out.Entries)
	}
}

// TestHandlePendingAddNotFound checks 404 for an ID that never existed; same
// error shape as the other 404s in the package (e.g. handleGet).
func TestHandlePendingAddNotFound(t *testing.T) {
	srv, rawKey := newTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	req, err := http.NewRequest(http.MethodGet, httpSrv.URL+"/api/v1/memories/pending/op_nonexistent", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+rawKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	var errBody apitypes.ErrorBody
	if err := json.NewDecoder(resp.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errBody.ErrorInfo.Code != "not_found" {
		t.Errorf("error.code = %q, want %q", errBody.ErrorInfo.Code, "not_found")
	}
}
