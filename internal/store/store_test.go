package store

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// fakeEmbedServer starts a local OpenAI-compatible embeddings server
// (same contract as chromem.NewEmbeddingFuncOpenAICompat: POST
// /embeddings, {"input":...}, response {"data":[{"embedding":[...]}]}) —
// deterministic enough for dedup to work (identical text produces an
// identical vector, hence similarity 1.0), with no real network. Same
// spirit as the e2e/embed_test.go fake, minimal version just for this
// package.
func fakeEmbedServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /embeddings", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": fakeVector(req.Input)}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func fakeVector(text string) []float32 {
	const dims = 8
	v := make([]float32, dims)
	for i, r := range text {
		v[i%dims] += float32(r)
	}
	var normSq float64
	for _, x := range v {
		normSq += float64(x) * float64(x)
	}
	if normSq == 0 {
		v[0] = 1
		return v
	}
	norm := math.Sqrt(normSq)
	for i := range v {
		v[i] = float32(float64(v[i]) / norm)
	}
	return v
}

func newTestStore(t *testing.T) *MemoryStore {
	t.Helper()
	embed := fakeEmbedServer(t)
	s, err := New(t.TempDir(), "fake-key", embed.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// fakeExtractor implements Extractor with behavior controllable by the
// tests (fixed facts, or a fixed error to exercise the D12 item 5
// fallback). calls is atomic (not a plain int) since D18: extractPieces
// calls Extract concurrently (1 goroutine per piece) when the content is
// large enough to slice, so multiple goroutines may increment calls at the
// same time.
type fakeExtractor struct {
	facts []string
	err   error
	calls atomic.Int32
}

func (f *fakeExtractor) Extract(_ context.Context, _, _ string) ([]string, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	return f.facts, nil
}

// TestImportLegacyDataNormalizesNilMetadata covers legacy data: an Entry
// stored in index.json (pre-D28 architecture) with "metadata":null
// (pre-fix bug in schema.NewEntry/store.Update — entries created via
// add/update with no metadata field ended up with nil Metadata, which
// json.Marshal serializes as `null`). A client typed as object (e.g. the
// WebUI's Zod schema) rejects `null` in a `record` field, so the
// in-memory index must never reintroduce that nil after the import.
func TestImportLegacyDataNormalizesNilMetadata(t *testing.T) {
	dataDir := t.TempDir()
	indexJSON := `[{
		"id": "mem_legacy",
		"content": "entry stored before the fix, with no metadata",
		"domain": "code:m3m0s",
		"domainCategory": "code",
		"source": "manual",
		"tags": [],
		"metadata": null,
		"createdAt": "2026-07-16T00:00:00Z",
		"updatedAt": "2026-07-16T00:00:00Z"
	}]`
	if err := os.WriteFile(filepath.Join(dataDir, "index.json"), []byte(indexJSON), 0o644); err != nil {
		t.Fatalf("write test index.json: %v", err)
	}

	embed := fakeEmbedServer(t)
	s, err := New(dataDir, "fake-key", embed.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.ImportLegacyData(context.Background()); err != nil {
		t.Fatalf("ImportLegacyData: %v", err)
	}

	entry, err := s.Get("mem_legacy", time.Now())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if entry.Metadata == nil {
		t.Error("Metadata is nil after the legacy import — should normalize to map[string]string{}")
	}
}

func TestAddWithoutExtractorReturnsSingleRawEntry(t *testing.T) {
	s := newTestStore(t)

	out, err := s.Add(context.Background(), AddInput{
		Content: "Alice prefers Docker for deployment.",
		Domain:  "code:m3m0s",
	}, time.Now())
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if out.Extracted {
		t.Error("expected Extracted=false without a configured extractor")
	}
	if len(out.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(out.Entries))
	}
	if out.Entries[0].Entry.Content != "Alice prefers Docker for deployment." {
		t.Errorf("unexpected content: %q", out.Entries[0].Entry.Content)
	}
}

func TestAddWithExtractorReturnsMultipleEntries(t *testing.T) {
	s := newTestStore(t)
	ext := &fakeExtractor{facts: []string{"Alice prefers Docker.", "Alice uses a Raspberry Pi."}}
	s.SetExtractor(ext)

	out, err := s.Add(context.Background(), AddInput{
		Content: "Alice prefers Docker for deployment and uses a Raspberry Pi.",
		Domain:  "code:m3m0s",
	}, time.Now())
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !out.Extracted {
		t.Error("expected Extracted=true with a configured extractor")
	}
	if ext.calls.Load() != 1 {
		t.Errorf("expected 1 call to the extractor, got %d", ext.calls.Load())
	}
	if len(out.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(out.Entries))
	}
	if out.Entries[0].Entry.Content != "Alice prefers Docker." {
		t.Errorf("unexpected fact 1: %q", out.Entries[0].Entry.Content)
	}
	if out.Entries[1].Entry.Content != "Alice uses a Raspberry Pi." {
		t.Errorf("unexpected fact 2: %q", out.Entries[1].Entry.Content)
	}
	for _, e := range out.Entries {
		if e.Entry.Domain != "code:m3m0s" {
			t.Errorf("domain not propagated to the fact: %q", e.Entry.Domain)
		}
	}
}

func TestAddFallsBackWhenExtractorFails(t *testing.T) {
	s := newTestStore(t)
	ext := &fakeExtractor{err: errors.New("openrouter unavailable")}
	s.SetExtractor(ext)

	out, err := s.Add(context.Background(), AddInput{
		Content: "original content without extraction",
		Domain:  "code:m3m0s",
	}, time.Now())
	if err != nil {
		t.Fatalf("Add should not propagate the extractor error as a failure: %v", err)
	}
	if out.Extracted {
		t.Error("expected Extracted=false when the extractor fails (raw content fallback)")
	}
	if len(out.Entries) != 1 {
		t.Fatalf("expected 1 entry in the fallback, got %d", len(out.Entries))
	}
	if out.Entries[0].Entry.Content != "original content without extraction" {
		t.Errorf("fallback did not preserve the original content: %q", out.Entries[0].Entry.Content)
	}
}

func TestAddDedupWithinExtractedFacts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	ext1 := &fakeExtractor{facts: []string{"Alice prefers Docker.", "Unique fact from the first add."}}
	s.SetExtractor(ext1)
	first, err := s.Add(ctx, AddInput{Content: "irrelevant, the fake extractor ignores it", Domain: "code:m3m0s"}, time.Now())
	if err != nil {
		t.Fatalf("first Add: %v", err)
	}
	if len(first.Entries) != 2 {
		t.Fatalf("expected 2 entries in the first add, got %d", len(first.Entries))
	}

	// The second add extracts a fact identical to the first ("Alice prefers
	// Docker.") — within the same domain, it must be treated as a duplicate
	// (same text => same embedding => similarity 1.0 >= threshold).
	ext2 := &fakeExtractor{facts: []string{"Alice prefers Docker.", "xyz"}}
	s.SetExtractor(ext2)
	second, err := s.Add(ctx, AddInput{Content: "irrelevant again", Domain: "code:m3m0s"}, time.Now())
	if err != nil {
		t.Fatalf("second Add: %v", err)
	}
	if len(second.Entries) != 2 {
		t.Fatalf("expected 2 entries in the second add, got %d", len(second.Entries))
	}
	if !second.Entries[0].Deduped {
		t.Error("expected the repeated fact marked as Deduped")
	}
	if second.Entries[0].Entry.ID != first.Entries[0].Entry.ID {
		t.Errorf("dedup should preserve the original ID %s, got %s", first.Entries[0].Entry.ID, second.Entries[0].Entry.ID)
	}
	if second.Entries[1].Deduped {
		t.Error("new fact should not be marked as Deduped")
	}
}

func TestAddRedactsSecretsBeforeAndAfterExtraction(t *testing.T) {
	s := newTestStore(t)
	secret := "sk-abcdefghijklmnopqrstuvwx"
	// The fake extractor "leaks" the secret back into one of the facts
	// (simulating an extractor that does not filter secrets) — post-extraction
	// redaction (D12, item 4) must catch it anyway.
	ext := &fakeExtractor{facts: []string{"clean fact", "fact with " + secret + " leaked"}}
	s.SetExtractor(ext)

	out, err := s.Add(context.Background(), AddInput{
		Content: "original content with " + secret + " as well",
		Domain:  "code:m3m0s",
	}, time.Now())
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	for _, e := range out.Entries {
		if containsSecret(e.Entry.Content, secret) {
			t.Errorf("secret leaked into a stored entry: %q", e.Entry.Content)
		}
	}
}

// ── metadata.source_content (Phase 5, D13, item 3) ──

func TestAddInjectsSourceContentOnlyWhenExtracted(t *testing.T) {
	s := newTestStore(t)
	ext := &fakeExtractor{facts: []string{"fact 1", "fact 2"}}
	s.SetExtractor(ext)

	original := "Original text that generated the 2 extracted facts"
	out, err := s.Add(context.Background(), AddInput{Content: original, Domain: "code:m3m0s"}, time.Now())
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !out.Extracted {
		t.Fatal("setup: expected Extracted=true")
	}
	for _, e := range out.Entries {
		if e.Entry.Metadata["source_content"] != original {
			t.Errorf("expected metadata.source_content=%q, got %q", original, e.Entry.Metadata["source_content"])
		}
	}
}

func TestAddDoesNotInjectSourceContentWhenNotExtracted(t *testing.T) {
	s := newTestStore(t)
	out, err := s.Add(context.Background(), AddInput{Content: "raw content without extraction", Domain: "code:m3m0s"}, time.Now())
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if out.Extracted {
		t.Fatal("setup: expected Extracted=false (no extractor configured)")
	}
	if _, ok := out.Entries[0].Entry.Metadata["source_content"]; ok {
		t.Errorf("did not expect metadata.source_content without extraction, got %q", out.Entries[0].Entry.Metadata["source_content"])
	}
}

func TestAddSourceContentOverwritesCallerSuppliedValue(t *testing.T) {
	s := newTestStore(t)
	ext := &fakeExtractor{facts: []string{"single fact"}}
	s.SetExtractor(ext)

	original := "genuine original text"
	out, err := s.Add(context.Background(), AddInput{
		Content:  original,
		Domain:   "code:m3m0s",
		Metadata: map[string]string{"source_content": "value forged by the caller"},
	}, time.Now())
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if out.Entries[0].Entry.Metadata["source_content"] != original {
		t.Errorf("expected source_content overwritten by the pipeline (%q), got %q", original, out.Entries[0].Entry.Metadata["source_content"])
	}
}

func TestAddSourceContentMostRecentWinsOnDedup(t *testing.T) {
	s := newTestStore(t)

	ext1 := &fakeExtractor{facts: []string{"repeated stable fact"}}
	s.SetExtractor(ext1)
	first, err := s.Add(context.Background(), AddInput{Content: "source 1", Domain: "code:m3m0s"}, time.Now())
	if err != nil {
		t.Fatalf("first Add: %v", err)
	}
	if first.Entries[0].Entry.Metadata["source_content"] != "source 1" {
		t.Fatalf("setup: source_unexpected content no primeiro add: %q", first.Entries[0].Entry.Metadata["source_content"])
	}

	// The same fact extracted again (identical text => same embedding =>
	// dedup) from a different source — source_content must become the most
	// recent source, overwriting the old one (same "most recent wins" rule as
	// the rest of mergeForDedup).
	ext2 := &fakeExtractor{facts: []string{"repeated stable fact"}}
	s.SetExtractor(ext2)
	second, err := s.Add(context.Background(), AddInput{Content: "source 2, more recent", Domain: "code:m3m0s"}, time.Now())
	if err != nil {
		t.Fatalf("second Add: %v", err)
	}
	if !second.Entries[0].Deduped {
		t.Fatal("expected dedup in the second add (fact identical to the first)")
	}
	if second.Entries[0].Entry.ID != first.Entries[0].Entry.ID {
		t.Fatalf("expected the same id after dedup, got %s (original %s)", second.Entries[0].Entry.ID, first.Entries[0].Entry.ID)
	}
	if second.Entries[0].Entry.Metadata["source_content"] != "source 2, more recent" {
		t.Errorf("expected source_content updated to the most recent, got %q", second.Entries[0].Entry.Metadata["source_content"])
	}
}

// ── deduplicated source_content blob store (D21, docs/decisions/ — fixes A7) ──

// TestSourceContentDeduplicatedAcrossExtractedFacts is D21's central test:
// N facts extracted from the SAME piece of text share just 1 blob — before
// D21, each carried its own full copy of the source text in
// metadata.source_content, multiplying disk space by N (A7). The public
// contract (resolved source_content on each entry) stays identical — only
// the internal storage (index.json + chromem) changes.
func TestSourceContentDeduplicatedAcrossExtractedFacts(t *testing.T) {
	s := newTestStore(t)
	ext := &fakeExtractor{facts: []string{"fact 1", "fact 2", "fact 3"}}
	s.SetExtractor(ext)

	original := "Original text that generates 3 extracted facts, all from the same piece"
	out, err := s.Add(context.Background(), AddInput{Content: original, Domain: "code:m3m0s"}, time.Now())
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(out.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(out.Entries))
	}
	for _, e := range out.Entries {
		if e.Entry.Metadata["source_content"] != original {
			t.Errorf("source_content not resolved for %s: %q", e.Entry.ID, e.Entry.Metadata["source_content"])
		}
	}

	blobCount, err := s.db.CountSourceBlobs(context.Background())
	if err != nil {
		t.Fatalf("CountSourceBlobs: %v", err)
	}
	if blobCount != 1 {
		t.Errorf("expected 1 blob shared by the 3 facts, got %d", blobCount)
	}

	s.mu.RLock()
	for id, e := range s.index {
		if _, has := e.Metadata[metadataKeySourceContent]; has {
			t.Errorf("in-memory index should not hold the raw text for %s (should be only the reference)", id)
		}
		if _, has := e.Metadata[metadataKeySourceContentRef]; !has {
			t.Errorf("in-memory index should hold the reference for %s", id)
		}
	}
	s.mu.RUnlock()
}

// The migration of data stored before D21 (raw source_content, with no
// reference) used to be covered here via MigrateSourceContentBlobs —
// removed in D28 along with internal/store/migrate.go; the equivalent
// effect now runs once in the legacy data import
// (internal/store/importlegacy.go, TestImportLegacyDataConvertsRawSourceContent
// in importlegacy_test.go).

func containsSecret(s, secret string) bool {
	return len(s) >= len(secret) && (func() bool {
		for i := 0; i+len(secret) <= len(s); i++ {
			if s[i:i+len(secret)] == secret {
				return true
			}
		}
		return false
	})()
}
