package selfindex

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	selfdocs "github.com/rafaelvieiras/m3m0s"
	"github.com/rafaelvieiras/m3m0s/internal/store"
)

// fakeEmbedServer/fakeVector/newTestStore mirror internal/store/store_test.go
// (they cannot be reused directly: they are unexported, different package).
// failMarker, if not empty, makes the server return 500 for any input
// containing that text — used to simulate an embedding failure (network,
// provider down) on a specific chunk without depending on a real network.
func fakeEmbedServer(t *testing.T, failMarker string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /embeddings", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if failMarker != "" && strings.Contains(req.Input, failMarker) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": fakeVector(req.Input)}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// fakeVector here is hash-based (sha256), not the raw per-position sum
// used in internal/store/store_test.go — the raw version converges to an
// almost uniform direction for any text long enough to pass the chunker's
// floor (minChunkableChars=300), making any 2 distinct chunks look like
// near-duplicates (similarity >0.95, D12) and triggering the store's real
// dedup — exactly what this file's per-chunk isolation tests need to
// AVOID in order to observe 2 truly distinct chunks coexisting. Identical
// content still produces an identical vector (deterministic via hash),
// preserving the "same text = same embedding" semantics the other tests
// assume.
func fakeVector(text string) []float32 {
	const dims = 32
	sum := sha256.Sum256([]byte(text))
	v := make([]float32, dims)
	for i := 0; i < dims; i++ {
		v[i] = float32(sum[i%len(sum)]) - 128
	}
	var normSq float64
	for _, x := range v {
		normSq += float64(x) * float64(x)
	}
	norm := math.Sqrt(normSq)
	for i := range v {
		v[i] = float32(float64(v[i]) / norm)
	}
	return v
}

func newTestStore(t *testing.T, failMarker string) *store.MemoryStore {
	t.Helper()
	embed := fakeEmbedServer(t, failMarker)
	s, err := store.New(t.TempDir(), "fake-key", embed.URL)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	return s
}

// countByFilePath counts how many memories exist in Domain with the given
// file_path, via Recent — good enough for these assertions (there are not
// that many entries in an isolated test store).
func countByFilePath(t *testing.T, s *store.MemoryStore, filePath string) int {
	t.Helper()
	entries := s.Recent(1000, store.Filter{Domain: Domain}, time.Now())
	n := 0
	for _, e := range entries {
		if e.Metadata["file_path"] == filePath {
			n++
		}
	}
	return n
}

func TestRunFilesIndexesNewFiles(t *testing.T) {
	s := newTestStore(t, "")
	ctx := context.Background()
	dataDir := t.TempDir()

	files := []selfdocs.File{
		{Path: "AGENTS.md", Content: "# Title\n\nTest AGENTS.md content."},
		{Path: "docs/api-contract.md", Content: "# Contract\n\nTest contract content."},
	}

	result, err := RunFiles(ctx, s, dataDir, files)
	if err != nil {
		t.Fatalf("RunFiles: %v", err)
	}
	if result.FilesTotal != 2 || result.FilesIndexed != 2 || result.FilesUnchanged != 0 || result.FilesFailed != 0 {
		t.Fatalf("unexpected result on the 1st indexing: %+v", result)
	}
	if n := countByFilePath(t, s, "AGENTS.md"); n == 0 {
		t.Fatalf("expected at least 1 chunk indexed for AGENTS.md, got %d", n)
	}
}

func TestRunFilesSkipsUnchangedContentOnSecondRun(t *testing.T) {
	s := newTestStore(t, "")
	ctx := context.Background()
	dataDir := t.TempDir()

	files := []selfdocs.File{
		{Path: "AGENTS.md", Content: "# Title\n\nContent that is stable across runs."},
	}

	if _, err := RunFiles(ctx, s, dataDir, files); err != nil {
		t.Fatalf("1st RunFiles: %v", err)
	}
	before := countByFilePath(t, s, "AGENTS.md")

	result, err := RunFiles(ctx, s, dataDir, files)
	if err != nil {
		t.Fatalf("2nd RunFiles: %v", err)
	}
	if result.FilesUnchanged != 1 || result.FilesIndexed != 0 {
		t.Fatalf("expected to skip file with equal hash, got %+v", result)
	}
	after := countByFilePath(t, s, "AGENTS.md")
	if after != before {
		t.Fatalf("expected stable count (no duplicates), before=%d after=%d", before, after)
	}
}

func TestRunFilesReindexesWhenContentChanges(t *testing.T) {
	s := newTestStore(t, "")
	ctx := context.Background()
	dataDir := t.TempDir()

	v1 := []selfdocs.File{{Path: "AGENTS.md", Content: "# Title\n\nVersion 1 of the content."}}
	if _, err := RunFiles(ctx, s, dataDir, v1); err != nil {
		t.Fatalf("RunFiles v1: %v", err)
	}

	v2 := []selfdocs.File{{Path: "AGENTS.md", Content: "# Title\n\nVersion 2, completely different from the previous content."}}
	result, err := RunFiles(ctx, s, dataDir, v2)
	if err != nil {
		t.Fatalf("RunFiles v2: %v", err)
	}
	if result.FilesIndexed != 1 || result.FilesUnchanged != 0 {
		t.Fatalf("expected to reindex file with changed hash, got %+v", result)
	}

	entries := s.Recent(1000, store.Filter{Domain: Domain}, time.Now())
	for _, e := range entries {
		if e.Metadata["file_path"] == "AGENTS.md" && e.Content == "# Title\n\nVersion 1 of the content." {
			t.Fatalf("old version chunk still present after reindexing: %+v", e)
		}
	}
}

// longSection generates a paragraph large enough to make the whole file
// pass the chunker's 300-character floor (internal/chunker,
// minChunkableChars) — below that the file always becomes a single chunk,
// even with headers, and the per-chunk isolation test would lose its
// point (we need 2+ real chunks in the same file).
func longSection(marker string) string {
	return strings.Repeat(marker+" ", 15)
}

func TestRunFilesIsolatesChunkFailureWithinAndAcrossFiles(t *testing.T) {
	const failMarker = "TRIGGERS_EMBEDDING_FAILURE"
	s := newTestStore(t, failMarker)
	ctx := context.Background()
	dataDir := t.TempDir()

	// multiChunk has 2 level-1 headers -> 2 chunks (chunker.Split); the
	// 1st carries the failure marker, the 2nd is normal.
	multiChunk := "# First Section\n\n" + longSection(failMarker) + "\n\n# Second Section\n\n" + longSection("normal content")

	files := []selfdocs.File{
		{Path: "with-failure.md", Content: multiChunk},
		{Path: "ok.md", Content: "# H1\n\n" + longSection("another normal content")},
	}

	result, err := RunFiles(ctx, s, dataDir, files)
	if err != nil {
		t.Fatalf("RunFiles should not return a fatal error on chunk/embedding failure: %v", err)
	}
	if result.FilesTotal != 2 {
		t.Fatalf("expected 2 files evaluated, got %d", result.FilesTotal)
	}
	if result.ChunksFailed == 0 {
		t.Fatalf("expected at least 1 failed chunk recorded, got %+v", result)
	}
	if result.FilesFailed != 1 {
		t.Fatalf("expected with-failure.md marked as FilesFailed (hash does not advance), got %+v", result)
	}
	// The 2nd chunk of with-failure.md (without the marker) should have been
	// indexed even though the 1st failed — isolation WITHIN the file.
	if n := countByFilePath(t, s, "with-failure.md"); n == 0 {
		t.Fatalf("expected the non-failing chunk of with-failure.md to be indexed despite the other chunk failing")
	}
	// ok.md has no relation to the file that failed — isolation BETWEEN
	// files.
	if n := countByFilePath(t, s, "ok.md"); n == 0 {
		t.Fatalf("expected ok.md indexed normally, regardless of the failure in with-failure.md")
	}

	// with-failure.md's hash should not have advanced: running again with
	// the SAME content tries to reindex again (does not fall into "no change").
	result2, err := RunFiles(ctx, s, dataDir, files)
	if err != nil {
		t.Fatalf("2nd RunFiles: %v", err)
	}
	sawRetry := false
	if result2.FilesFailed >= 1 || result2.FilesIndexed >= 1 {
		sawRetry = true
	}
	if !sawRetry {
		t.Fatalf("expected with-failure.md to be retried (hash did not advance in the 1st round), got %+v", result2)
	}
}
