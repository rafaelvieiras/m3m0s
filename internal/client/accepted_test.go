package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
)

// A 202 whose poll never finishes before the caller's deadline is an
// acceptance, not a failure (#65): Add returns an AcceptedError carrying the
// operation id.
func TestClientAddReportsAcceptedWhenWaitTimesOut(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/memories", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(apitypes.AddPendingResponse{OperationID: "op_slow", Status: "pending"})
	})
	mux.HandleFunc("GET /api/v1/memories/pending/{id}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(apitypes.PendingAddResponse{OperationID: "op_slow", Status: "pending"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := New(srv.URL, "k").Add(ctx, apitypes.AddRequest{Content: "c", Domain: "code:x"})
	if !IsAccepted(err) {
		t.Fatalf("want AcceptedError, got %v", err)
	}
}

// An unreachable server (the POST itself fails) is NOT an acceptance.
func TestClientAddUnreachableIsNotAccepted(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := New(url, "k").Add(context.Background(), apitypes.AddRequest{Content: "c", Domain: "code:x"})
	if err == nil || IsAccepted(err) {
		t.Fatalf("want a plain transport error, got %v", err)
	}
}
