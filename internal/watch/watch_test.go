package watch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/client"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// fakeServe simulates enough of an m3m0s serve (add + delete by file_path)
// to test reconciliation without depending on real embeddings/network —
// same pattern as internal/mcpserver/mcp_test.go.
type fakeServe struct {
	mu          sync.Mutex
	addCalls    int
	deleteCalls int
	byFilePath  map[string][]string // domain|filePath -> ids still "alive"
	nextID      int
	// entriesPerAdd simulates fact extraction (Phase 4, D12): when > 1,
	// each Add returns several entries (one chunk becomes N facts). Default 0
	// is treated as 1 (pre-Phase-4 behavior).
	entriesPerAdd int
}

func newFakeServe() *fakeServe {
	return &fakeServe{byFilePath: make(map[string][]string)}
}

func (f *fakeServe) key(domain, filePath string) string { return domain + "|" + filePath }

func (f *fakeServe) server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/memories", func(w http.ResponseWriter, r *http.Request) {
		var req apitypes.AddRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		n := f.entriesPerAdd
		if n <= 0 {
			n = 1
		}

		f.mu.Lock()
		f.addCalls++
		entries := make([]apitypes.AddedEntry, 0, n)
		for i := 0; i < n; i++ {
			f.nextID++
			id := req.IDPrefix
			if id == "" {
				id = "mem"
			}
			id = id + "_" + strconv.Itoa(f.nextID)
			k := f.key(req.Domain, req.Metadata["file_path"])
			f.byFilePath[k] = append(f.byFilePath[k], id)
			entries = append(entries, apitypes.AddedEntry{Entry: schema.Entry{
				ID: id, Content: req.Content, Domain: req.Domain,
				DomainCategory: schema.DomainCategory(req.Domain), Source: req.Source,
				Metadata: req.Metadata, CreatedAt: time.Now(), UpdatedAt: time.Now(),
			}})
		}
		f.mu.Unlock()

		resp := apitypes.AddResponse{Entries: entries, Extracted: n > 1}
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("DELETE /api/v1/memories", func(w http.ResponseWriter, r *http.Request) {
		domain := r.URL.Query().Get("domain")
		filePath := r.URL.Query().Get("filePath")

		f.mu.Lock()
		f.deleteCalls++
		k := f.key(domain, filePath)
		count := len(f.byFilePath[k])
		delete(f.byFilePath, k)
		f.mu.Unlock()

		json.NewEncoder(w).Encode(apitypes.DeleteByFilePathResponse{DeletedCount: count})
	})
	return httptest.NewServer(mux)
}

func testClient(t *testing.T, srv *httptest.Server) *client.Client {
	t.Helper()
	return client.New(srv.URL, "irrelevant-fake-does-not-validate-auth")
}

func writeFile(t *testing.T, root, relPath, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func newState(t *testing.T) *State {
	t.Helper()
	st, err := LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	return st
}

func TestMatchGlobAllowDenyAndDoubleStar(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"AGENTS.md", "AGENTS.md", true},
		{"AGENTS.md", "sub/AGENTS.md", false},
		{"docs/**/*.md", "docs/decisions.md", true},
		{"docs/**/*.md", "docs/sub/decisions.md", true},
		{"docs/**/*.md", "docs/sub/x.txt", false},
		{"node_modules/**", "node_modules/package/README.md", true},
		{"node_modules/**", "node_modules", true},
		{"node_modules/**", "other/folder/x.md", false},
	}
	for _, c := range cases {
		if got := matchGlob(c.pattern, c.path); got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestShouldIndexDenyWinsOverAllow(t *testing.T) {
	allow := []string{"docs/**/*.md"}
	deny := []string{"docs/generated/**"}
	if shouldIndex(allow, deny, "docs/generated/x.md") {
		t.Error("deny should win over allow when both match")
	}
	if !shouldIndex(allow, deny, "docs/decisions.md") {
		t.Error("should index when only allow matches")
	}
}

func TestReconcileFileHashDiffSkipsUnchanged(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "AGENTS.md", "# Title\n\ncontent")

	fake := newFakeServe()
	srv := fake.server()
	defer srv.Close()
	c := testClient(t, srv)
	state := newState(t)
	cfg := Config{Root: root, Domain: "codigo:test"}

	if err := ReconcileFile(context.Background(), c, cfg, state, "AGENTS.md"); err != nil {
		t.Fatalf("ReconcileFile: %v", err)
	}
	firstAddCalls := fake.addCalls
	if firstAddCalls == 0 {
		t.Fatal("expected at least 1 Add on the first reconciliation")
	}

	if err := ReconcileFile(context.Background(), c, cfg, state, "AGENTS.md"); err != nil {
		t.Fatalf("ReconcileFile (second time, no change): %v", err)
	}
	if fake.addCalls != firstAddCalls {
		t.Errorf("content did not change, but addCalls went from %d to %d (hash diff should have skipped)", firstAddCalls, fake.addCalls)
	}
}

func TestReconcileFileReindexOnChangeDeletesOldFirst(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "AGENTS.md", "original content")

	fake := newFakeServe()
	srv := fake.server()
	defer srv.Close()
	c := testClient(t, srv)
	state := newState(t)
	cfg := Config{Root: root, Domain: "codigo:test"}

	if err := ReconcileFile(context.Background(), c, cfg, state, "AGENTS.md"); err != nil {
		t.Fatalf("ReconcileFile: %v", err)
	}
	if fake.deleteCalls != 0 {
		t.Fatalf("first reconciliation should not call delete, called %d times", fake.deleteCalls)
	}

	writeFile(t, root, "AGENTS.md", "content quite different from the original")
	if err := ReconcileFile(context.Background(), c, cfg, state, "AGENTS.md"); err != nil {
		t.Fatalf("ReconcileFile (changed): %v", err)
	}
	if fake.deleteCalls != 1 {
		t.Errorf("reindexing on change should call delete-by-filepath once before re-add, called %d", fake.deleteCalls)
	}
}

func TestSyncPurgesFilesRemovedFromDisk(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "AGENTS.md", "content")

	fake := newFakeServe()
	srv := fake.server()
	defer srv.Close()
	c := testClient(t, srv)
	state := newState(t)
	cfg := Config{Root: root, Domain: "codigo:test", Allow: []string{"AGENTS.md"}}

	if err := Sync(context.Background(), c, cfg, state); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}
	if len(state.Paths()) != 1 {
		t.Fatalf("expected 1 tracked file after the initial sync, got %d", len(state.Paths()))
	}

	if err := os.Remove(filepath.Join(root, "AGENTS.md")); err != nil {
		t.Fatalf("remove file: %v", err)
	}
	if err := Sync(context.Background(), c, cfg, state); err != nil {
		t.Fatalf("Sync (file removed): %v", err)
	}
	if len(state.Paths()) != 0 {
		t.Errorf("expected 0 tracked files after purge, got %d: %v", len(state.Paths()), state.Paths())
	}
	if fake.deleteCalls == 0 {
		t.Error("Sync should have called delete-by-filepath for the removed file")
	}
}

// TestReconcileFileTracksAllIDsWhenChunkYieldsMultipleEntries checks that,
// when fact extraction (Phase 4, D12) turns 1 chunk into N entries, all IDs
// go into the local state's ChunkIDs — needed so the cascading purge deletes
// everything that came from that file, not just the first fact.
func TestReconcileFileTracksAllIDsWhenChunkYieldsMultipleEntries(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "AGENTS.md", "# Single header\n\nsome paragraph")

	fake := newFakeServe()
	fake.entriesPerAdd = 3 // simulates 1 chunk -> 3 atomic facts
	srv := fake.server()
	defer srv.Close()
	c := testClient(t, srv)
	state := newState(t)
	cfg := Config{Root: root, Domain: "codigo:test"}

	if err := ReconcileFile(context.Background(), c, cfg, state, "AGENTS.md"); err != nil {
		t.Fatalf("ReconcileFile: %v", err)
	}

	fs, ok := state.Get("AGENTS.md")
	if !ok {
		t.Fatal("expected AGENTS.md tracked in the state")
	}
	if len(fs.ChunkIDs) != 3 {
		t.Fatalf("expected 3 tracked IDs (1 chunk x 3 extracted facts), got %d: %v", len(fs.ChunkIDs), fs.ChunkIDs)
	}

	// Editing the file and reindexing must delete the 3 old IDs before
	// inserting the new ones (delete-by-filepath already covers that — here we
	// only confirm the local state did not lose any ID along the way).
	writeFile(t, root, "AGENTS.md", "# Single header\n\nanother quite different paragraph")
	if err := ReconcileFile(context.Background(), c, cfg, state, "AGENTS.md"); err != nil {
		t.Fatalf("ReconcileFile (changed): %v", err)
	}
	if fake.deleteCalls != 1 {
		t.Errorf("expected 1 delete-by-filepath on reindexing, got %d", fake.deleteCalls)
	}
	fs, ok = state.Get("AGENTS.md")
	if !ok || len(fs.ChunkIDs) != 3 {
		t.Fatalf("expected 3 new tracked IDs after reindexing, got %v (ok=%v)", fs.ChunkIDs, ok)
	}
}

func TestSyncOnlyIndexesAllowedFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "AGENTS.md", "allowed content")
	writeFile(t, root, "main.go", "package main")
	writeFile(t, root, "node_modules/package/README.md", "should not be indexed")

	fake := newFakeServe()
	srv := fake.server()
	defer srv.Close()
	c := testClient(t, srv)
	state := newState(t)
	cfg := Config{
		Root:   root,
		Domain: "codigo:test",
		Allow:  []string{"AGENTS.md", "CLAUDE.md", "MEMORY.md", "docs/**/*.md"},
		Deny:   []string{"node_modules/**"},
	}

	if err := Sync(context.Background(), c, cfg, state); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	paths := state.Paths()
	if len(paths) != 1 || paths[0] != "AGENTS.md" {
		t.Errorf("expected only AGENTS.md tracked, got %v", paths)
	}
}
