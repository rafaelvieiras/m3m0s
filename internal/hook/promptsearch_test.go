package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

func scored(e schema.Entry, s float64) schema.Entry { e.Score = &s; return e }

func promptServer(t *testing.T, results []schema.Entry, calls *int, mode *string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/search", func(w http.ResponseWriter, r *http.Request) {
		*calls++
		var req apitypes.SearchRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		*mode = req.Mode
		_ = json.NewEncoder(w).Encode(apitypes.SearchResponse{Results: results})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestRunPromptSearchInjectsOnlyRelevantAndNotRepeated(t *testing.T) {
	results := []schema.Entry{
		scored(extractedEntry("mem_hi", "The compose lives in sala-notebook/stacks/m3m0s."), 0.68),
		scored(extractedEntry("mem_lo", "Unrelated note."), 0.45),
		scored(rawEntry("mem_raw", "raw fallback dump"), 0.9),
	}
	var calls int
	var mode string
	srv := promptServer(t, results, &calls, &mode)
	home := t.TempDir()
	payload := Payload{SessionID: "s1", HookEventName: "UserPromptSubmit", Cwd: "/x/some-repo", Prompt: "onde está o compose do m3m0s na sala?"}

	var out, errb bytes.Buffer
	RunPromptSearch(context.Background(), payload, sessionStartDeps(home, srv, &out, &errb))
	if mode != "vector" {
		t.Fatalf("mode = %q, want vector", mode)
	}
	got := out.String()
	if !strings.Contains(got, "UserPromptSubmit") || !strings.Contains(got, "mem_hi") && !strings.Contains(got, "compose lives") {
		t.Fatalf("expected injection, got %q", got)
	}
	if strings.Contains(got, "Unrelated") || strings.Contains(got, "raw fallback") {
		t.Fatalf("low-score or raw entry injected: %q", got)
	}

	// Same session, same hit: nothing is repeated.
	out.Reset()
	RunPromptSearch(context.Background(), payload, sessionStartDeps(home, srv, &out, &errb))
	if out.Len() != 0 {
		t.Fatalf("repeated injection: %q", out.String())
	}
}

func TestRunPromptSearchSkipsShortAndSlashPrompts(t *testing.T) {
	var calls int
	var mode string
	srv := promptServer(t, nil, &calls, &mode)
	for _, p := range []string{"ok", "pode seguir", "/compact now please do it", ""} {
		var out, errb bytes.Buffer
		RunPromptSearch(context.Background(), Payload{SessionID: "s", Prompt: p}, sessionStartDeps(t.TempDir(), srv, &out, &errb))
		if out.Len() != 0 {
			t.Fatalf("%q produced output", p)
		}
	}
	if calls != 0 {
		t.Fatalf("server called %d times for skipped prompts", calls)
	}
}

func TestRunDispatchesUserPromptSubmitByFlag(t *testing.T) {
	var calls int
	var mode string
	srv := promptServer(t, []schema.Entry{scored(extractedEntry("m", "fact about deploys"), 0.7)}, &calls, &mode)
	var out, errb bytes.Buffer
	deps := sessionStartDeps(t.TempDir(), srv, &out, &errb)
	deps.PromptInject = true
	b, _ := json.Marshal(Payload{SessionID: "s", HookEventName: "UserPromptSubmit", Cwd: "/x/r", Prompt: "how do we deploy the service to production?"})
	Run(context.Background(), bytes.NewReader(b), deps)
	if calls != 1 || !strings.Contains(out.String(), "fact about deploys") {
		t.Fatalf("calls=%d out=%q", calls, out.String())
	}
}
