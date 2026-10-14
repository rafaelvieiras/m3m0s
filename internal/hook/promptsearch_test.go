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
	srv := promptServer(t, []schema.Entry{scored(extractedEntry("m", "fact about the production service"), 0.7)}, &calls, &mode)
	var out, errb bytes.Buffer
	deps := sessionStartDeps(t.TempDir(), srv, &out, &errb)
	deps.PromptInject = true
	b, _ := json.Marshal(Payload{SessionID: "s", HookEventName: "UserPromptSubmit", Cwd: "/x/r", Prompt: "how do we deploy the service to production?"})
	Run(context.Background(), bytes.NewReader(b), deps)
	if calls != 1 || !strings.Contains(out.String(), "fact about the production service") {
		t.Fatalf("calls=%d out=%q", calls, out.String())
	}
}

func TestSpecificTerms(t *testing.T) {
	cases := []struct {
		prompt string
		want   int // number of specific terms
	}{
		{"pode fazer merge e deploy", 0},
		{"sim, pode mergear e começar a d47", 1}, // d47
		{"vamos ver o F3 na sequencia", 1},       // f3
		{"pode seguir", 0},
		{"corrige o bug do timeout no hook", 3}, // corrige, bug, timeout, hook -> bug is 3 chars
		{"onde fica o compose do m3m0s na sala", 3},
	}
	for _, c := range cases {
		got := specificTerms(c.prompt)
		if c.want == 3 {
			if len(got) < 2 {
				t.Errorf("%q: terms=%v, want several", c.prompt, got)
			}
			continue
		}
		if len(got) != c.want {
			t.Errorf("%q: terms=%v, want %d", c.prompt, got, c.want)
		}
	}
}

func TestRunPromptSearchRequiresSharedTerm(t *testing.T) {
	results := []schema.Entry{
		scored(extractedEntry("mem_a", "O deploy do merge chegou em produção."), 0.77),
		scored(extractedEntry("mem_b", "O compose do m3m0s fica na sala."), 0.70),
	}
	var calls int
	var mode string
	srv := promptServer(t, results, &calls, &mode)

	var out, errb bytes.Buffer
	RunPromptSearch(context.Background(), Payload{SessionID: "g", Cwd: "/x/r", Prompt: "pode fazer merge e deploy agora"}, sessionStartDeps(t.TempDir(), srv, &out, &errb))
	if out.Len() != 0 || calls != 0 {
		t.Fatalf("generic prompt must not search: calls=%d out=%q", calls, out.String())
	}

	RunPromptSearch(context.Background(), Payload{SessionID: "h", Cwd: "/x/r", Prompt: "onde fica o compose do m3m0s?"}, sessionStartDeps(t.TempDir(), srv, &out, &errb))
	got := out.String()
	if !strings.Contains(got, "compose do m3m0s") || strings.Contains(got, "chegou em produção") {
		t.Fatalf("only the hit sharing a term must be injected: %q", got)
	}
}
