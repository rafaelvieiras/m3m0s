package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
)

// TestClientAddSyncPassesThrough checks that an Add resolved right away (200)
// behaves as before — the client returns the decoded AddResponse directly,
// without attempting any poll (D26, docs/decisions/).
func TestClientAddSyncPassesThrough(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/memories", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apitypes.AddResponse{
			Entries:   []apitypes.AddedEntry{{Deduped: false}},
			Extracted: true,
			Status:    "completed",
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := New(srv.URL, "fake-key")
	out, err := c.Add(context.Background(), apitypes.AddRequest{Content: "content", Domain: "codigo:m3m0s"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if out.Status != "completed" || !out.Extracted || len(out.Entries) != 1 {
		t.Errorf("unexpected out: %+v", out)
	}
}

// TestClientAddPollsUntilCompleted checks the asynchronous path (D26): the
// fake server returns 202 on the first POST /memories call, and the first 2
// GET /memories/pending/:id calls return "pending" before finally returning
// "completed" with a Result — the client must poll transparently and return
// only the final result.
func TestClientAddPollsUntilCompleted(t *testing.T) {
	const operationID = "op_test123456"
	var pollCount atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/memories", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(apitypes.AddPendingResponse{
			OperationID: operationID,
			Status:      "pending",
		})
	})
	mux.HandleFunc("GET /api/v1/memories/pending/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != operationID {
			t.Errorf("poll asked for id = %q, want %q", r.PathValue("id"), operationID)
		}
		n := pollCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if n < 3 {
			_ = json.NewEncoder(w).Encode(apitypes.PendingAddResponse{
				OperationID: operationID,
				Status:      "pending",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(apitypes.PendingAddResponse{
			OperationID: operationID,
			Status:      "completed",
			Result: &apitypes.AddResponse{
				Entries:   []apitypes.AddedEntry{{Deduped: false}},
				Extracted: true,
				Status:    "completed",
			},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := New(srv.URL, "fake-key")
	start := time.Now()
	out, err := c.Add(context.Background(), apitypes.AddRequest{Content: "slow content", Domain: "codigo:m3m0s"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Add took %s, expected a few short polls", elapsed)
	}
	if pollCount.Load() < 3 {
		t.Errorf("pollCount = %d, want >= 3 (the client must have actually polled)", pollCount.Load())
	}
	if out.Status != "completed" || !out.Extracted || len(out.Entries) != 1 {
		t.Errorf("unexpected out: %+v", out)
	}
}

// TestClientAddPollingReportsFailure checks that status "failed" in the poll
// becomes a normal Go error, with the Error field's message embedded.
func TestClientAddPollingReportsFailure(t *testing.T) {
	const operationID = "op_fail123456"

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/memories", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(apitypes.AddPendingResponse{OperationID: operationID, Status: "pending"})
	})
	mux.HandleFunc("GET /api/v1/memories/pending/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apitypes.PendingAddResponse{
			OperationID: operationID,
			Status:      "failed",
			Error:       "openrouter unavailable",
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := New(srv.URL, "fake-key")
	_, err := c.Add(context.Background(), apitypes.AddRequest{Content: "content", Domain: "codigo:m3m0s"})
	if err == nil {
		t.Fatal("Add should have failed")
	}
	if !strings.Contains(err.Error(), "openrouter unavailable") {
		t.Errorf("error = %q, want message containing %q", err.Error(), "openrouter unavailable")
	}
}

// TestClientAddPollingRespectsContextCancel checks that polling does not run
// forever if the caller's ctx is canceled — the fake server never leaves
// "pending", so without respecting ctx this test would hang.
func TestClientAddPollingRespectsContextCancel(t *testing.T) {
	const operationID = "op_stuck123456"

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/memories", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(apitypes.AddPendingResponse{OperationID: operationID, Status: "pending"})
	})
	mux.HandleFunc("GET /api/v1/memories/pending/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apitypes.PendingAddResponse{OperationID: operationID, Status: "pending"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := New(srv.URL, "fake-key")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.Add(ctx, apitypes.AddRequest{Content: "content", Domain: "codigo:m3m0s"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Add should have failed due to context cancellation")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("Add took %s to fail, expected it to exit quickly after ctx expired", elapsed)
	}
}
