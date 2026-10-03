package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/client"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// fakeServe simulates enough of an m3m0s serve (POST /api/v1/memories) to
// test the hook pipeline without depending on real embeddings/network; same
// pattern as internal/watch/watch_test.go and internal/mcpserver/mcp_test.go.
type fakeServe struct {
	mu       sync.Mutex
	requests []apitypes.AddRequest
}

func (f *fakeServe) server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/memories", func(w http.ResponseWriter, r *http.Request) {
		var req apitypes.AddRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.requests = append(f.requests, req)
		f.mu.Unlock()

		resp := apitypes.AddResponse{
			Entries: []apitypes.AddedEntry{{Entry: schema.Entry{ID: "mem_hooktest"}}},
		}
		json.NewEncoder(w).Encode(resp)
	})
	return httptest.NewServer(mux)
}

func (f *fakeServe) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeServe) lastRequest() apitypes.AddRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

func testDeps(t *testing.T, home string, srv *httptest.Server) (Deps, *bytes.Buffer) {
	t.Helper()
	var stderr bytes.Buffer
	env := map[string]string{"M3M0S_URL": srv.URL, "M3M0S_API_KEY": "test-key"}
	deps := Deps{
		Stderr:  &stderr,
		HomeDir: home,
		Getenv:  func(k string) string { return env[k] },
		Domain:  func(cwd string) string { return "code:" + filepath.Base(cwd) },
		NewClient: func(baseURL, apiKey string) *client.Client {
			return client.New(baseURL, apiKey)
		},
	}
	return deps, &stderr
}

func writeTranscript(t *testing.T, path string, lines ...string) {
	t.Helper()
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing transcript: %v", err)
	}
}

func payloadJSON(t *testing.T, sessionID, transcriptPath, cwd string) []byte {
	t.Helper()
	p := Payload{SessionID: sessionID, TranscriptPath: transcriptPath, Cwd: cwd, HookEventName: "PreCompact"}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return b
}

func TestRunSendsDigestWithExpectedFields(t *testing.T) {
	fake := &fakeServe{}
	srv := fake.server()
	defer srv.Close()

	dir := t.TempDir()
	transcript := filepath.Join(dir, "session.jsonl")
	writeTranscript(t, transcript,
		`{"type":"user","message":{"role":"user","content":"user question"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"assistant answer"}]}}`,
	)

	home := t.TempDir()
	deps, stderr := testDeps(t, home, srv)

	stdin := bytes.NewReader(payloadJSON(t, "sess-1", transcript, "/path/to/repo/some-repo"))
	Run(context.Background(), stdin, deps)

	if fake.callCount() != 1 {
		t.Fatalf("expected 1 POST, got %d (stderr: %s)", fake.callCount(), stderr.String())
	}
	req := fake.lastRequest()
	if req.Source != ClaudeCodeSource {
		t.Errorf("Source = %q, want %q", req.Source, ClaudeCodeSource)
	}
	if req.Domain != "code:some-repo" {
		t.Errorf("Domain = %q, want %q", req.Domain, "code:some-repo")
	}
	if !strings.Contains(req.Content, "user question") || !strings.Contains(req.Content, "assistant answer") {
		t.Errorf("Content does not contain the expected text: %q", req.Content)
	}
	if req.Agent != "" {
		t.Errorf("Agent should be empty, got %q", req.Agent)
	}
}

func TestRunIncrementalCheckpointAvoidsDuplicateSend(t *testing.T) {
	fake := &fakeServe{}
	srv := fake.server()
	defer srv.Close()

	dir := t.TempDir()
	transcript := filepath.Join(dir, "session.jsonl")
	writeTranscript(t, transcript,
		`{"type":"user","message":{"role":"user","content":"first message"}}`,
	)

	home := t.TempDir()
	deps, _ := testDeps(t, home, srv)

	stdin1 := bytes.NewReader(payloadJSON(t, "sess-2", transcript, dir))
	Run(context.Background(), stdin1, deps) // simulates PreCompact
	if fake.callCount() != 1 {
		t.Fatalf("expected 1 POST after the 1st call, got %d", fake.callCount())
	}

	// SessionEnd firing right after, with no new lines in the transcript;
	// nothing should be resent (the checkpoint already covers those lines).
	stdin2 := bytes.NewReader(payloadJSON(t, "sess-2", transcript, dir))
	Run(context.Background(), stdin2, deps)
	if fake.callCount() != 1 {
		t.Fatalf("expected still 1 POST (no new lines), got %d", fake.callCount())
	}

	// New line added -> only it should go into the next digest.
	f, err := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("opening transcript for append: %v", err)
	}
	if _, err := f.WriteString(`{"type":"assistant","message":{"role":"assistant","content":"second message, only this one is new"}}` + "\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
	f.Close()

	stdin3 := bytes.NewReader(payloadJSON(t, "sess-2", transcript, dir))
	Run(context.Background(), stdin3, deps)
	if fake.callCount() != 2 {
		t.Fatalf("expected 2 POSTs after the new line, got %d", fake.callCount())
	}
	last := fake.lastRequest()
	if strings.Contains(last.Content, "first message") {
		t.Errorf("3rd-call digest should not re-include the already processed message: %q", last.Content)
	}
	if !strings.Contains(last.Content, "second message, only this one is new") {
		t.Errorf("3rd-call digest should contain only the new message: %q", last.Content)
	}
}

func TestRunEmptyDigestSkipsSendSilently(t *testing.T) {
	fake := &fakeServe{}
	srv := fake.server()
	defer srv.Close()

	dir := t.TempDir()
	transcript := filepath.Join(dir, "session.jsonl")
	// Only lines without real text (tool_use/thinking) or ignored types.
	writeTranscript(t, transcript,
		`{"type":"summary","summary":"summary"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}`,
	)

	home := t.TempDir()
	deps, stderr := testDeps(t, home, srv)

	stdin := bytes.NewReader(payloadJSON(t, "sess-3", transcript, dir))
	Run(context.Background(), stdin, deps)

	if fake.callCount() != 0 {
		t.Fatalf("should not send anything, but there were %d POST(s)", fake.callCount())
	}
	if stderr.Len() != 0 {
		t.Errorf("should not log anything to stderr for an empty digest, got: %q", stderr.String())
	}
}

func TestRunMissingTranscriptFileExitsSilently(t *testing.T) {
	fake := &fakeServe{}
	srv := fake.server()
	defer srv.Close()

	home := t.TempDir()
	deps, _ := testDeps(t, home, srv)

	stdin := bytes.NewReader(payloadJSON(t, "sess-4", filepath.Join(t.TempDir(), "does-not-exist.jsonl"), "/tmp"))
	Run(context.Background(), stdin, deps)

	if fake.callCount() != 0 {
		t.Fatalf("should not send anything when transcript_path does not exist, got %d POST(s)", fake.callCount())
	}
}

func TestRunMissingTranscriptPathInPayloadExitsSilently(t *testing.T) {
	fake := &fakeServe{}
	srv := fake.server()
	defer srv.Close()

	home := t.TempDir()
	deps, _ := testDeps(t, home, srv)

	stdin := bytes.NewReader([]byte(`{"session_id":"sess-5","cwd":"/tmp","hook_event_name":"SessionEnd"}`))
	Run(context.Background(), stdin, deps)

	if fake.callCount() != 0 {
		t.Fatalf("should not send anything without transcript_path, got %d POST(s)", fake.callCount())
	}
}

func TestRunMalformedPayloadJSONExitsSilentlyWithStderrLog(t *testing.T) {
	fake := &fakeServe{}
	srv := fake.server()
	defer srv.Close()

	home := t.TempDir()
	deps, stderr := testDeps(t, home, srv)

	stdin := bytes.NewReader([]byte(`this is not JSON`))
	Run(context.Background(), stdin, deps)

	if fake.callCount() != 0 {
		t.Fatalf("should not send anything with a malformed payload, got %d POST(s)", fake.callCount())
	}
	if stderr.Len() == 0 {
		t.Error("expected a stderr log for a malformed payload")
	}
}

func TestRunMissingCredentialsSkipsSendAndLogs(t *testing.T) {
	fake := &fakeServe{}
	srv := fake.server()
	defer srv.Close()

	dir := t.TempDir()
	transcript := filepath.Join(dir, "session.jsonl")
	writeTranscript(t, transcript, `{"type":"user","message":{"role":"user","content":"any message"}}`)

	home := t.TempDir()
	var stderr bytes.Buffer
	deps := Deps{
		Stderr:  &stderr,
		HomeDir: home,
		Getenv:  func(string) string { return "" }, // no env and no hook.env in home
		Domain:  func(cwd string) string { return "code:" + filepath.Base(cwd) },
		NewClient: func(baseURL, apiKey string) *client.Client {
			return client.New(baseURL, apiKey)
		},
	}

	stdin := bytes.NewReader(payloadJSON(t, "sess-6", transcript, dir))
	Run(context.Background(), stdin, deps)

	if fake.callCount() != 0 {
		t.Fatalf("should not send anything without credentials, got %d POST(s)", fake.callCount())
	}
	if stderr.Len() == 0 {
		t.Error("expected a stderr log about missing credentials")
	}
}

func TestRunTruncatesOversizedDigestAndLogsWarning(t *testing.T) {
	fake := &fakeServe{}
	srv := fake.server()
	defer srv.Close()

	dir := t.TempDir()
	transcript := filepath.Join(dir, "session.jsonl")
	bigText := strings.Repeat("y", maxDigestChars+10000)
	line, err := json.Marshal(map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": bigText},
	})
	if err != nil {
		t.Fatalf("marshal big line: %v", err)
	}
	writeTranscript(t, transcript, string(line))

	home := t.TempDir()
	deps, stderr := testDeps(t, home, srv)

	stdin := bytes.NewReader(payloadJSON(t, "sess-7", transcript, dir))
	Run(context.Background(), stdin, deps)

	if fake.callCount() != 1 {
		t.Fatalf("expected 1 POST, got %d", fake.callCount())
	}
	req := fake.lastRequest()
	if len([]rune(req.Content)) != maxDigestChars {
		t.Errorf("len(Content) = %d, want %d (truncated)", len([]rune(req.Content)), maxDigestChars)
	}
	if !strings.Contains(stderr.String(), "truncated") {
		t.Errorf("expected truncation warning in stderr, got: %q", stderr.String())
	}
}
