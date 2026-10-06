package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/client"
	"github.com/rafaelvieiras/m3m0s/internal/project"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// fakeRecentServe simulates enough of an m3m0s serve (GET /api/v1/recent) to
// test RunSessionStart without real network/embeddings; same pattern as
// fakeServe in hook_test.go, just for the other endpoint.
type fakeRecentServe struct {
	results     []schema.Entry
	lastDomain  string
	lastProject string
	lastScope   string
	lastLimit   string
	delay       time.Duration
	callCount   int
	failWithErr bool
}

func (f *fakeRecentServe) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/recent", func(w http.ResponseWriter, r *http.Request) {
		f.callCount++
		f.lastDomain = r.URL.Query().Get("domain")
		f.lastProject = r.URL.Query().Get("project")
		f.lastScope = r.URL.Query().Get("scope")
		f.lastLimit = r.URL.Query().Get("limit")
		if f.delay > 0 {
			select {
			case <-time.After(f.delay):
			case <-r.Context().Done():
				return
			}
		}
		if f.failWithErr {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(apitypes.RecentResponse{Results: f.results})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// extractedEntry builds an entry that went through fact extraction (it has
// metadata.source_content): the "normal" case, injectable by
// InjectableEntries. Contrast with rawEntry.
func extractedEntry(id, content string) schema.Entry {
	return schema.Entry{
		ID: id, Domain: "code:some-repo", Source: "claude-code", Content: content,
		CreatedAt: time.Now(),
		Metadata:  map[string]string{apitypes.MetadataKeySourceContent: "original digest from which " + id + " was extracted"},
	}
}

// rawEntry builds an entry WITHOUT metadata.source_content: the raw-content
// fallback when extraction times out (D12 item 5). InjectableEntries must
// always exclude this case (D17, found in production).
func rawEntry(id, content string) schema.Entry {
	return schema.Entry{
		ID: id, Domain: "code:some-repo", Source: "claude-code", Content: content, CreatedAt: time.Now(),
	}
}

func sessionStartDeps(home string, srv *httptest.Server, stdout, stderr *bytes.Buffer) Deps {
	env := map[string]string{"M3M0S_URL": srv.URL, "M3M0S_API_KEY": "test-key"}
	return Deps{
		Stdout:  stdout,
		Stderr:  stderr,
		HomeDir: home,
		Getenv:  func(k string) string { return env[k] },
		Domain:  func(cwd string) string { return "code:" + filepath.Base(cwd) },
		NewClient: func(baseURL, apiKey string) *client.Client {
			return client.New(baseURL, apiKey)
		},
	}
}

func TestRunSessionStartInjectsContextOnStartup(t *testing.T) {
	fake := &fakeRecentServe{results: []schema.Entry{
		extractedEntry("mem_1", "The maintainer prefers Docker for deployment."),
	}}
	srv := fake.server(t)

	var stdout, stderr bytes.Buffer
	deps := sessionStartDeps(t.TempDir(), srv, &stdout, &stderr)

	payload := Payload{HookEventName: "SessionStart", Source: "startup", Cwd: "/path/to/repo/some-repo"}
	RunSessionStart(context.Background(), payload, deps)

	if fake.callCount != 1 {
		t.Fatalf("expected 1 GET /recent, got %d (stderr: %s)", fake.callCount, stderr.String())
	}
	if fake.lastDomain != "code:some-repo" {
		t.Errorf("domain = %q, want %q", fake.lastDomain, "code:some-repo")
	}
	// SessionStartFetchLimit (D17): asks for more than
	// SessionStartRecentLimit on purpose; the InjectableEntries filter runs
	// afterwards, in memory, not via query param.
	if fake.lastLimit != "40" {
		t.Errorf("limit = %q, want %q", fake.lastLimit, "40")
	}

	var out sessionStartOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("stdout is not the expected JSON: %v (stdout: %s)", err, stdout.String())
	}
	if out.HookSpecificOutput.HookEventName != "SessionStart" {
		t.Errorf("hookEventName = %q, want SessionStart", out.HookSpecificOutput.HookEventName)
	}
	if !strings.Contains(out.HookSpecificOutput.AdditionalContext, "The maintainer prefers Docker for deployment.") {
		t.Errorf("additionalContext does not contain the expected memory: %q", out.HookSpecificOutput.AdditionalContext)
	}
}

func TestRunSessionStartInjectsContextOnClear(t *testing.T) {
	fake := &fakeRecentServe{results: []schema.Entry{
		extractedEntry("mem_1", "any fact"),
	}}
	srv := fake.server(t)

	var stdout, stderr bytes.Buffer
	deps := sessionStartDeps(t.TempDir(), srv, &stdout, &stderr)

	payload := Payload{HookEventName: "SessionStart", Source: "clear", Cwd: "/tmp/some-repo"}
	RunSessionStart(context.Background(), payload, deps)

	if fake.callCount != 1 {
		t.Fatalf("expected 1 GET /recent for source=clear, got %d", fake.callCount)
	}
	if stdout.Len() == 0 {
		t.Error("expected additionalContext on stdout for source=clear")
	}
}

// TestRunSessionStartExcludesRawUnextractedEntries covers the production
// finding that motivated the filter (D17): a raw-content fallback (D12 item
// 5, extraction timed out) must not appear in the injected digest, only
// entries that already went through fact extraction.
func TestRunSessionStartExcludesRawUnextractedEntries(t *testing.T) {
	fake := &fakeRecentServe{results: []schema.Entry{
		rawEntry("mem_raw", "[user]\n/compact\n\n[user]\nThis session is being continued..."),
		extractedEntry("mem_ok", "curated fact that should appear"),
	}}
	srv := fake.server(t)

	var stdout, stderr bytes.Buffer
	deps := sessionStartDeps(t.TempDir(), srv, &stdout, &stderr)

	payload := Payload{HookEventName: "SessionStart", Source: "startup", Cwd: "/tmp/some-repo"}
	RunSessionStart(context.Background(), payload, deps)

	var out sessionStartOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("stdout is not the expected JSON: %v (stdout: %s)", err, stdout.String())
	}
	ctx := out.HookSpecificOutput.AdditionalContext
	if strings.Contains(ctx, "/compact") {
		t.Errorf("additionalContext should not contain the raw unextracted entry, got: %q", ctx)
	}
	if !strings.Contains(ctx, "curated fact that should appear") {
		t.Errorf("additionalContext should contain the extracted entry, got: %q", ctx)
	}
}

// TestRunSessionStartSkipsSilentlyWhenAllResultsAreRaw covers exactly the
// scenario reported by the maintainer: the most recent items of a domain
// timed out in extraction in a row (D12 item 5). With no extracted entry yet,
// the hook must inject nothing (not even the raw fallback).
func TestRunSessionStartSkipsSilentlyWhenAllResultsAreRaw(t *testing.T) {
	fake := &fakeRecentServe{results: []schema.Entry{
		rawEntry("mem_raw1", "raw digest 1"),
		rawEntry("mem_raw2", "raw digest 2"),
	}}
	srv := fake.server(t)

	var stdout, stderr bytes.Buffer
	deps := sessionStartDeps(t.TempDir(), srv, &stdout, &stderr)

	payload := Payload{HookEventName: "SessionStart", Source: "startup", Cwd: "/tmp/some-repo"}
	RunSessionStart(context.Background(), payload, deps)

	if stdout.Len() != 0 {
		t.Errorf("with only raw entries (no extraction), nothing should be written to stdout, got: %q", stdout.String())
	}
}

// TestInjectableEntriesFiltersAndCapsAtRecentLimit covers the filter
// function in isolation: interleaved raw entries do not count toward the
// SessionStartRecentLimit cut, and the order/content of the extracted ones is
// preserved.
func TestInjectableEntriesFiltersAndCapsAtRecentLimit(t *testing.T) {
	var entries []schema.Entry
	for i := 0; i < SessionStartRecentLimit+5; i++ {
		entries = append(entries, rawEntry(fmt.Sprintf("mem_raw_%d", i), "raw"))
		entries = append(entries, extractedEntry(fmt.Sprintf("mem_ok_%d", i), fmt.Sprintf("fact %d", i)))
	}

	got := InjectableEntries(entries)
	if len(got) != SessionStartRecentLimit {
		t.Fatalf("expected %d entries after filter+cut, got %d", SessionStartRecentLimit, len(got))
	}
	for _, e := range got {
		if e.Metadata[apitypes.MetadataKeySourceContent] == "" {
			t.Errorf("entry %s should not have passed the filter (no source_content)", e.ID)
		}
	}
	if got[0].ID != "mem_ok_0" {
		t.Errorf("expected original order preserved, first entry = %s, want mem_ok_0", got[0].ID)
	}
}

func TestRunSessionStartSkipsResumeAndCompact(t *testing.T) {
	for _, source := range []string{"resume", "compact"} {
		t.Run(source, func(t *testing.T) {
			fake := &fakeRecentServe{results: []schema.Entry{
				{ID: "mem_1", Domain: "code:some-repo", Content: "should not be fetched", CreatedAt: time.Now()},
			}}
			srv := fake.server(t)

			var stdout, stderr bytes.Buffer
			deps := sessionStartDeps(t.TempDir(), srv, &stdout, &stderr)

			payload := Payload{HookEventName: "SessionStart", Source: source, Cwd: "/tmp/some-repo"}
			RunSessionStart(context.Background(), payload, deps)

			if fake.callCount != 0 {
				t.Errorf("source=%s should not trigger GET /recent, triggered %d time(s)", source, fake.callCount)
			}
			if stdout.Len() != 0 {
				t.Errorf("source=%s should not write anything to stdout, got: %q", source, stdout.String())
			}
		})
	}
}

func TestRunSessionStartSkipsSilentlyWhenNoResults(t *testing.T) {
	fake := &fakeRecentServe{results: nil}
	srv := fake.server(t)

	var stdout, stderr bytes.Buffer
	deps := sessionStartDeps(t.TempDir(), srv, &stdout, &stderr)

	payload := Payload{HookEventName: "SessionStart", Source: "startup", Cwd: "/tmp/empty-repo"}
	RunSessionStart(context.Background(), payload, deps)

	if stdout.Len() != 0 {
		t.Errorf("no results, should not write anything to stdout, got: %q", stdout.String())
	}
}

func TestRunSessionStartSkipsSilentlyWithoutCredentials(t *testing.T) {
	fake := &fakeRecentServe{results: []schema.Entry{{ID: "mem_1", Content: "x", CreatedAt: time.Now()}}}
	fake.server(t) // registered but should never be called; RunSessionStart bails before any request

	var stdout, stderr bytes.Buffer
	deps := Deps{
		Stdout:  &stdout,
		Stderr:  &stderr,
		HomeDir: t.TempDir(),
		Getenv:  func(string) string { return "" },
		Domain:  func(cwd string) string { return "code:" + filepath.Base(cwd) },
		NewClient: func(baseURL, apiKey string) *client.Client {
			return client.New(baseURL, apiKey)
		},
	}

	payload := Payload{HookEventName: "SessionStart", Source: "startup", Cwd: "/tmp/repo"}
	RunSessionStart(context.Background(), payload, deps)

	if fake.callCount != 0 {
		t.Errorf("without credentials, should not call GET /recent")
	}
	if stdout.Len() != 0 {
		t.Errorf("without credentials, should not write anything to stdout")
	}
	if stderr.Len() == 0 {
		t.Error("expected a stderr log about missing credentials")
	}
}

func TestRunSessionStartSkipsSilentlyOnServerError(t *testing.T) {
	fake := &fakeRecentServe{failWithErr: true}
	srv := fake.server(t)

	var stdout, stderr bytes.Buffer
	deps := sessionStartDeps(t.TempDir(), srv, &stdout, &stderr)

	payload := Payload{HookEventName: "SessionStart", Source: "startup", Cwd: "/tmp/repo"}
	RunSessionStart(context.Background(), payload, deps)

	if stdout.Len() != 0 {
		t.Errorf("server error should not write anything to stdout, got: %q", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Error("expected a stderr log about the GET /recent failure")
	}
}

// TestRunSessionStartRespectsShortTimeout ensures the sessionStartTimeout
// ceiling (D17) really cuts the call; without it, a slow m3m0s response would
// block the start of every Claude Code session.
func TestRunSessionStartRespectsShortTimeout(t *testing.T) {
	fake := &fakeRecentServe{
		results: []schema.Entry{{ID: "mem_1", Content: "x", CreatedAt: time.Now()}},
		delay:   sessionStartTimeout + 2*time.Second,
	}
	srv := fake.server(t)

	var stdout, stderr bytes.Buffer
	deps := sessionStartDeps(t.TempDir(), srv, &stdout, &stderr)

	payload := Payload{HookEventName: "SessionStart", Source: "startup", Cwd: "/tmp/repo"}

	done := make(chan struct{})
	start := time.Now()
	go func() {
		RunSessionStart(context.Background(), payload, deps)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(sessionStartTimeout + 3*time.Second):
		t.Fatal("RunSessionStart did not respect the short timeout; it blocked longer than expected")
	}
	elapsed := time.Since(start)
	if elapsed >= sessionStartTimeout+2*time.Second {
		t.Errorf("RunSessionStart took %s, expected to cut off around %s", elapsed, sessionStartTimeout)
	}
	if stdout.Len() != 0 {
		t.Error("a call cut off by timeout should not write anything to stdout")
	}
}

// TestRunDispatchesSessionStartFromPayload ensures Run (the real entry point
// used by cmd/m3m0s) correctly dispatches to RunSessionStart when
// hook_event_name is SessionStart, without needing transcript_path (unlike
// the PreCompact/SessionEnd path).
func TestRunDispatchesSessionStartFromPayload(t *testing.T) {
	fake := &fakeRecentServe{results: []schema.Entry{
		extractedEntry("mem_1", "fact via Run"),
	}}
	srv := fake.server(t)

	var stdout, stderr bytes.Buffer
	deps := sessionStartDeps(t.TempDir(), srv, &stdout, &stderr)

	stdin := bytes.NewReader([]byte(`{"session_id":"sess-x","cwd":"/tmp/repo","hook_event_name":"SessionStart","source":"startup"}`))
	Run(context.Background(), stdin, deps)

	if fake.callCount != 1 {
		t.Fatalf("expected Run to dispatch to RunSessionStart (1 GET /recent), got %d (stderr: %s)", fake.callCount, stderr.String())
	}
	if !strings.Contains(stdout.String(), "fact via Run") {
		t.Errorf("stdout does not contain the expected additionalContext: %q", stdout.String())
	}
}

func TestRunSessionStartHonorsTokenBudget(t *testing.T) {
	var results []schema.Entry
	for i := 0; i < 10; i++ {
		results = append(results, extractedEntry(fmt.Sprintf("mem_%d", i), strings.Repeat("fact ", 40)))
	}
	run := func(budget string) (count int, stderr string) {
		fake := &fakeRecentServe{results: results}
		srv := fake.server(t)
		var out, errBuf bytes.Buffer
		deps := sessionStartDeps(t.TempDir(), srv, &out, &errBuf)
		base := deps.Getenv
		deps.Getenv = func(k string) string {
			if k == "M3M0S_INJECT_MAX_TOKENS" {
				return budget
			}
			return base(k)
		}
		RunSessionStart(context.Background(), Payload{HookEventName: "SessionStart", Source: "startup", Cwd: "/x/some-repo"}, deps)
		var o sessionStartOutput
		if err := json.Unmarshal(out.Bytes(), &o); err != nil {
			t.Fatalf("budget %q: no JSON output: %v (stderr %s)", budget, err, errBuf.String())
		}
		return strings.Count(o.HookSpecificOutput.AdditionalContext, "\n   fact"), errBuf.String()
	}

	full, _ := run("0")
	capped, log := run("200")
	if full != 10 {
		t.Errorf("no cap: %d entries, want 10", full)
	}
	if capped >= full || capped == 0 {
		t.Errorf("budget 200 should keep fewer entries but not none: %d", capped)
	}
	if !strings.Contains(log, "budget 200") {
		t.Errorf("injection should be logged with its budget, got %q", log)
	}
}

// Recall scope (D34): a session with no project sees only global memory; a
// mapped directory (or M3M0S_PROJECT) recalls global plus that project.
func TestRunSessionStartScopesRecallByProject(t *testing.T) {
	fake := &fakeRecentServe{results: []schema.Entry{extractedEntry("mem_1", "any fact")}}
	srv := fake.server(t)
	home := t.TempDir()
	payload := Payload{HookEventName: "SessionStart", Source: "startup", Cwd: "/path/to/repo/some-repo"}

	var stdout, stderr bytes.Buffer
	deps := sessionStartDeps(home, srv, &stdout, &stderr)
	RunSessionStart(context.Background(), payload, deps)
	if fake.lastProject != "" || fake.lastScope != "global" {
		t.Errorf("no project: project=%q scope=%q, want global scope", fake.lastProject, fake.lastScope)
	}

	m := project.Map{}
	if err := m.Set("/path/to/repo", "mapped-proj"); err != nil {
		t.Fatal(err)
	}
	if err := m.Save(home); err != nil {
		t.Fatal(err)
	}
	RunSessionStart(context.Background(), payload, deps)
	if fake.lastProject != "mapped-proj" || fake.lastScope != "" {
		t.Errorf("mapped dir: project=%q scope=%q, want layered mapped-proj", fake.lastProject, fake.lastScope)
	}

	env := map[string]string{"M3M0S_URL": srv.URL, "M3M0S_API_KEY": "test-key", "M3M0S_INJECT_PROJECT": "forced"}
	deps.Getenv = func(k string) string { return env[k] }
	RunSessionStart(context.Background(), payload, deps)
	if fake.lastProject != "forced" {
		t.Errorf("M3M0S_INJECT_PROJECT: project=%q", fake.lastProject)
	}
}
