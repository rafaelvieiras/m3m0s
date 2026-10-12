package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
)

// #65: the server accepts (202) but the extraction is slow. The hook must not
// spool the payload nor report the server as unavailable.
func TestRunTreatsSlowAcceptedAddAsDelivered(t *testing.T) {
	var posts atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/memories", func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(apitypes.AddPendingResponse{OperationID: "op_slow", Status: "pending"})
	})
	mux.HandleFunc("GET /api/v1/memories/pending/{id}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(apitypes.PendingAddResponse{OperationID: "op_slow", Status: "pending"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	oldWait := addWaitBound
	addWaitBound = 150 * time.Millisecond
	defer func() { addWaitBound = oldWait }()

	dir := t.TempDir()
	transcript := filepath.Join(dir, "session.jsonl")
	writeTranscript(t, transcript,
		`{"type":"user","message":{"role":"user","content":"slow extraction question"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"answer"}]}}`,
	)
	home := t.TempDir()
	deps, stderr := testDeps(t, home, srv)

	Run(context.Background(), bytes.NewReader(payloadJSON(t, "sess-slow", transcript, dir)), deps)

	if names, _ := spoolFiles(home); len(names) != 0 {
		t.Fatalf("accepted payload must not be spooled, got %d files (stderr: %s)", len(names), stderr.String())
	}
	if posts.Load() != 1 {
		t.Errorf("posts = %d, want 1", posts.Load())
	}
}

// A spool full of payloads the server accepts slowly drains completely: one
// slow operation no longer stops the replay.
func TestReplayDrainsSpoolWhenServerAnswersSlowly(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/memories", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(apitypes.AddPendingResponse{OperationID: "op_slow", Status: "pending"})
	})
	mux.HandleFunc("GET /api/v1/memories/pending/{id}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(apitypes.PendingAddResponse{OperationID: "op_slow", Status: "pending"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	oldWait := addWaitBound
	addWaitBound = 100 * time.Millisecond
	defer func() { addWaitBound = oldWait }()

	home := t.TempDir()
	now := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := Enqueue(home, apitypes.AddRequest{Content: "c" + string(rune('a'+i)), Domain: "codigo:x"}, context.DeadlineExceeded, now.Add(time.Duration(i))); err != nil {
			t.Fatal(err)
		}
	}
	deps, _ := testDeps(t, home, srv)
	res := Replay(context.Background(), home, deps.NewClient(srv.URL, "k"), 0, now)
	if res.Unreachable || res.Sent != 3 {
		t.Fatalf("replay = %+v, want 3 sent and reachable", res)
	}
	if names, _ := spoolFiles(home); len(names) != 0 {
		t.Fatalf("spool not drained: %v", names)
	}
}
