package store

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// fakeClassifierExtractor implements Extractor (via the embedded
// fakeExtractor) and Classifier (Phase 7, D15) with controllable behavior:
// a fixed relation, or a fixed err to exercise the safe fallback.
type fakeClassifierExtractor struct {
	fakeExtractor
	relation string
	err      error
	calls    int
}

func (f *fakeClassifierExtractor) Classify(_ context.Context, _, _ string) (string, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	return f.relation, nil
}

// vectorEmbedServer starts an OpenAI-compatible embeddings fake that
// returns a fixed vector per input text (the vectors table) — it controls
// cosine similarity precisely, unlike the generic fakeVector of
// store_test.go (good for exact dedup, bad for pinning down an
// intermediate band like RelatedSimilarityThreshold). Text outside the
// table falls back to the normal fakeVector.
func vectorEmbedServer(t *testing.T, vectors map[string][]float32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /embeddings", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		vec, ok := vectors[req.Input]
		if !ok {
			vec = fakeVector(req.Input)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": vec}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// ── similarityBand (pure) ──

func TestSimilarityBand(t *testing.T) {
	cases := []struct {
		similarity float64
		want       string
	}{
		{1.0, "dedup"},
		{DedupSimilarityThreshold, "dedup"},
		{0.94, "related"},
		{RelatedSimilarityThreshold, "related"},
		{0.64, "none"},
		{0.0, "none"},
	}
	for _, c := range cases {
		if got := similarityBand(c.similarity); got != c.want {
			t.Errorf("similarityBand(%.2f) = %q, want %q", c.similarity, got, c.want)
		}
	}
}

// ── withRelatedTo / splitRelatedTo (pure) ──

func TestWithRelatedToAddsNewID(t *testing.T) {
	md := withRelatedTo(nil, "mem_a")
	if md[metadataKeyRelatedTo] != "mem_a" {
		t.Errorf("related_to = %q, want %q", md[metadataKeyRelatedTo], "mem_a")
	}
}

func TestWithRelatedToAppendsWithoutDuplicatingOrMutatingBase(t *testing.T) {
	base := map[string]string{metadataKeyRelatedTo: "mem_a"}
	md2 := withRelatedTo(base, "mem_b")
	if md2[metadataKeyRelatedTo] != "mem_a,mem_b" {
		t.Errorf("related_to = %q, want %q", md2[metadataKeyRelatedTo], "mem_a,mem_b")
	}
	md3 := withRelatedTo(md2, "mem_a")
	if md3[metadataKeyRelatedTo] != "mem_a,mem_b" {
		t.Errorf("expected no duplication, got %q", md3[metadataKeyRelatedTo])
	}
	if base[metadataKeyRelatedTo] != "mem_a" {
		t.Error("withRelatedTo should not mutate the base map")
	}
}

func TestSplitRelatedToEmptyReturnsNil(t *testing.T) {
	if got := splitRelatedTo(""); got != nil {
		t.Errorf("splitRelatedTo(\"\") = %v, want nil", got)
	}
}

// ── withValidUntil / isExpired (pure) ──

func TestWithValidUntilFormatsRFC3339(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	md := withValidUntil(nil, now)
	if md[metadataKeyValidUntil] != "2026-07-16T12:00:00Z" {
		t.Errorf("valid_until = %q", md[metadataKeyValidUntil])
	}
}

func TestIsExpired(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	cases := []struct {
		name string
		md   map[string]string
		want bool
	}{
		{"no valid_until", map[string]string{}, false},
		{"valid_until in the past", map[string]string{metadataKeyValidUntil: past.Format(time.RFC3339)}, true},
		{"valid_until exactly now", map[string]string{metadataKeyValidUntil: now.Format(time.RFC3339)}, true},
		{"valid_until in the future", map[string]string{metadataKeyValidUntil: future.Format(time.RFC3339)}, false},
		{"malformed valid_until", map[string]string{metadataKeyValidUntil: "not a date"}, false},
	}
	for _, c := range cases {
		e := schema.Entry{Metadata: c.md}
		if got := isExpired(e, now); got != c.want {
			t.Errorf("%s: isExpired = %v, want %v", c.name, got, c.want)
		}
	}
}

// ── linkRelated (Classifier: contradicts/unrelated/safe fallback) ──

func TestLinkRelatedContradictsInvalidatesCandidate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	addOut, err := s.Add(ctx, AddInput{Content: "uses Python 3.10", Domain: "code:m3m0s"}, now)
	if err != nil {
		t.Fatalf("Add candidate: %v", err)
	}
	candidate := addOut.Entries[0].Entry

	entry, err := schema.NewEntry("uses Python 3.12", "code:m3m0s", "manual", "", nil, nil, now, "")
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}

	cl := &fakeClassifierExtractor{relation: relationContradicts}
	s.SetExtractor(cl)

	if err := s.linkRelated(ctx, &entry, &candidate, now); err != nil {
		t.Fatalf("linkRelated: %v", err)
	}
	if cl.calls != 1 {
		t.Errorf("expected 1 call to Classify, got %d", cl.calls)
	}
	if got := entry.Metadata[metadataKeyRelatedTo]; got != candidate.ID {
		t.Errorf("entry.related_to = %q, want %q", got, candidate.ID)
	}

	updatedCandidate, err := s.Get(candidate.ID, time.Now())
	if err != nil {
		t.Fatalf("Get candidate: %v", err)
	}
	if updatedCandidate.Metadata[metadataKeyRelatedTo] != entry.ID {
		t.Errorf("candidate.related_to = %q, want %q (bidirectional link)", updatedCandidate.Metadata[metadataKeyRelatedTo], entry.ID)
	}
	if updatedCandidate.Metadata[metadataKeyValidUntil] == "" {
		t.Error("expected candidate invalidated (valid_until set) after \"contradicts\"")
	}
}

func TestLinkRelatedUnrelatedDoesNotLinkAnything(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	addOut, err := s.Add(ctx, AddInput{Content: "base fact", Domain: "code:m3m0s"}, now)
	if err != nil {
		t.Fatalf("Add candidate: %v", err)
	}
	candidate := addOut.Entries[0].Entry

	entry, err := schema.NewEntry("completely different fact", "code:m3m0s", "manual", "", nil, nil, now, "")
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}

	cl := &fakeClassifierExtractor{relation: relationUnrelated}
	s.SetExtractor(cl)

	if err := s.linkRelated(ctx, &entry, &candidate, now); err != nil {
		t.Fatalf("linkRelated: %v", err)
	}
	if _, ok := entry.Metadata[metadataKeyRelatedTo]; ok {
		t.Errorf("did not expect related_to on entry for relation \"unrelated\", metadata=%v", entry.Metadata)
	}
	updatedCandidate, err := s.Get(candidate.ID, time.Now())
	if err != nil {
		t.Fatalf("Get candidate: %v", err)
	}
	if _, ok := updatedCandidate.Metadata[metadataKeyRelatedTo]; ok {
		t.Error("did not expect related_to on candidate for relation \"unrelated\"")
	}
}

func TestLinkRelatedFallsBackToRelatedWithoutClassifier(t *testing.T) {
	s := newTestStore(t) // no SetExtractor: extraction off, no Classifier available
	ctx := context.Background()
	now := time.Now()

	addOut, err := s.Add(ctx, AddInput{Content: "base fact", Domain: "code:m3m0s"}, now)
	if err != nil {
		t.Fatalf("Add candidate: %v", err)
	}
	candidate := addOut.Entries[0].Entry
	entry, err := schema.NewEntry("related fact", "code:m3m0s", "manual", "", nil, nil, now, "")
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}

	if err := s.linkRelated(ctx, &entry, &candidate, now); err != nil {
		t.Fatalf("linkRelated: %v", err)
	}
	if entry.Metadata[metadataKeyRelatedTo] != candidate.ID {
		t.Errorf("expected related_to=%q, got %q", candidate.ID, entry.Metadata[metadataKeyRelatedTo])
	}
	updatedCandidate, err := s.Get(candidate.ID, time.Now())
	if err != nil {
		t.Fatalf("Get candidate: %v", err)
	}
	if updatedCandidate.Metadata[metadataKeyValidUntil] != "" {
		t.Error("with no classifier available, should not invalidate the candidate")
	}
}

func TestLinkRelatedFallsBackToRelatedOnClassifyError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	addOut, err := s.Add(ctx, AddInput{Content: "base fact", Domain: "code:m3m0s"}, now)
	if err != nil {
		t.Fatalf("Add candidate: %v", err)
	}
	candidate := addOut.Entries[0].Entry
	entry, err := schema.NewEntry("related fact", "code:m3m0s", "manual", "", nil, nil, now, "")
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}

	cl := &fakeClassifierExtractor{err: errors.New("openrouter unavailable")}
	s.SetExtractor(cl)

	if err := s.linkRelated(ctx, &entry, &candidate, now); err != nil {
		t.Fatalf("linkRelated should not propagate the classifier error: %v", err)
	}
	if entry.Metadata[metadataKeyRelatedTo] != candidate.ID {
		t.Error("expected \"related\" fallback even with a classifier error")
	}
	updatedCandidate, err := s.Get(candidate.ID, time.Now())
	if err != nil {
		t.Fatalf("Get candidate: %v", err)
	}
	if updatedCandidate.Metadata[metadataKeyValidUntil] != "" {
		t.Error("a classifier error should never invalidate the candidate")
	}
}

func TestLinkRelatedFallsBackToRelatedOnUnrecognizedResponse(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	addOut, err := s.Add(ctx, AddInput{Content: "base fact", Domain: "code:m3m0s"}, now)
	if err != nil {
		t.Fatalf("Add candidate: %v", err)
	}
	candidate := addOut.Entries[0].Entry
	entry, err := schema.NewEntry("related fact", "code:m3m0s", "manual", "", nil, nil, now, "")
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}

	cl := &fakeClassifierExtractor{relation: "maybe"}
	s.SetExtractor(cl)

	if err := s.linkRelated(ctx, &entry, &candidate, now); err != nil {
		t.Fatalf("linkRelated: %v", err)
	}
	if entry.Metadata[metadataKeyRelatedTo] != candidate.ID {
		t.Error("expected \"related\" fallback for an unrecognized response")
	}
	updatedCandidate, err := s.Get(candidate.ID, time.Now())
	if err != nil {
		t.Fatalf("Get candidate: %v", err)
	}
	if updatedCandidate.Metadata[metadataKeyValidUntil] != "" {
		t.Error("an unrecognized response should never invalidate the candidate")
	}
}

func TestLinkRelatedFallsBackWhenExtractorLacksClassifier(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	addOut, err := s.Add(ctx, AddInput{Content: "base fact", Domain: "code:m3m0s"}, now)
	if err != nil {
		t.Fatalf("Add candidate: %v", err)
	}
	candidate := addOut.Entries[0].Entry
	entry, err := schema.NewEntry("related fact", "code:m3m0s", "manual", "", nil, nil, now, "")
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}

	s.SetExtractor(&fakeExtractor{}) // implements Extractor, not Classifier

	if err := s.linkRelated(ctx, &entry, &candidate, now); err != nil {
		t.Fatalf("linkRelated: %v", err)
	}
	if entry.Metadata[metadataKeyRelatedTo] != candidate.ID {
		t.Error("expected \"related\" fallback when the configured extractor does not implement Classifier")
	}
}

// ── expandRelated (cap, seen, expired) ──

func TestExpandRelatedCapsSkipsSeenAndExpired(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	primary := schema.Entry{
		ID: "mem_primary",
		// Deliberate order: mem_seen and mem_expired come BEFORE mem_r1/r2/r3 to
		// prove they are skipped without consuming the maxRelatedExpansion cap
		// (otherwise the test would not tell "skipped" apart from "cut by the
		// cap").
		Metadata: map[string]string{metadataKeyRelatedTo: "mem_seen,mem_expired,mem_r1,mem_r2,mem_r3"},
	}
	r1 := schema.Entry{ID: "mem_r1", Metadata: map[string]string{}}
	r2 := schema.Entry{ID: "mem_r2", Metadata: map[string]string{}}
	r3 := schema.Entry{ID: "mem_r3", Metadata: map[string]string{}} // must be cut by the cap
	seenEntry := schema.Entry{ID: "mem_seen", Metadata: map[string]string{}}
	expired := schema.Entry{ID: "mem_expired", Metadata: map[string]string{metadataKeyValidUntil: now.Add(-time.Hour).Format(time.RFC3339)}}

	s.mu.Lock()
	s.index["mem_primary"] = primary
	s.index["mem_r1"] = r1
	s.index["mem_r2"] = r2
	s.index["mem_r3"] = r3
	s.index["mem_seen"] = seenEntry
	s.index["mem_expired"] = expired
	s.mu.Unlock()

	seen := map[string]struct{}{"mem_primary": {}, "mem_seen": {}}
	out := s.expandRelated([]schema.Entry{primary}, seen, Filter{}, now)

	if len(out) != 3 {
		t.Fatalf("expected 3 entries (primary + 2 related within the cap), got %d", len(out))
	}
	for _, e := range out[1:] {
		if e.ID != "mem_r1" && e.ID != "mem_r2" {
			t.Errorf("unexpected expanded entry: %s (mem_r3 should have been cut by the cap)", e.ID)
		}
		if e.RelatedVia == nil || *e.RelatedVia != "mem_primary" {
			t.Errorf("expected RelatedVia=mem_primary on %s, got %v", e.ID, e.RelatedVia)
		}
	}
}

// TestExpandRelatedSkipsEntriesNotMatchingTagFilter covers the leak found
// via E2E test: a main result (with the filter's tag) that is related_to
// an entry WITHOUT the tag should not bring that entry back through 1-hop
// expansion — otherwise the tag post-filter in Search's main loop
// (matchesTags) gets punctured by the expansion.
func TestExpandRelatedSkipsEntriesNotMatchingTagFilter(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	primary := schema.Entry{
		ID:       "mem_primary",
		Tags:     []string{"golang"},
		Metadata: map[string]string{metadataKeyRelatedTo: "mem_no_tag,mem_with_tag"},
	}
	noTag := schema.Entry{ID: "mem_no_tag", Tags: []string{"python"}, Metadata: map[string]string{}}
	withTag := schema.Entry{ID: "mem_with_tag", Tags: []string{"golang", "backend"}, Metadata: map[string]string{}}

	s.mu.Lock()
	s.index["mem_primary"] = primary
	s.index["mem_no_tag"] = noTag
	s.index["mem_with_tag"] = withTag
	s.mu.Unlock()

	seen := map[string]struct{}{"mem_primary": {}}
	out := s.expandRelated([]schema.Entry{primary}, seen, Filter{Tags: []string{"golang"}}, now)

	if len(out) != 2 {
		t.Fatalf("expected 2 entries (primary + only the related one with the golang tag), got %d: %+v", len(out), out)
	}
	if out[1].ID != "mem_with_tag" {
		t.Errorf("expected mem_with_tag expanded, got %s (mem_no_tag should have been filtered)", out[1].ID)
	}
	if _, stillSeen := seen["mem_no_tag"]; stillSeen {
		t.Error("mem_no_tag should not have been marked as seen — it was filtered, not expanded")
	}
}

// ── Recent: expired filter + 1-hop expansion (Phase 7, D15) ──

func TestRecentExcludesExpiredEntries(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	current, err := schema.NewEntry("current memory", "code:m3m0s", "manual", "", nil, nil, now, "")
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	expired, err := schema.NewEntry("expired memory", "code:m3m0s", "manual", "", nil,
		map[string]string{metadataKeyValidUntil: now.Add(-time.Hour).Format(time.RFC3339)}, now, "")
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}

	s.mu.Lock()
	s.index[current.ID] = current
	s.index[expired.ID] = expired
	s.mu.Unlock()

	results := s.Recent(10, Filter{Domain: "code:m3m0s"}, now)
	if len(results) != 1 || results[0].ID != current.ID {
		t.Fatalf("expected only the current entry, got %d result(s): %v", len(results), results)
	}
}

func TestRecentExpandsRelatedEntries(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	related, err := schema.NewEntry("related memory", "code:m3m0s", "manual", "", nil, nil, now, "")
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	primary, err := schema.NewEntry("main memory", "code:m3m0s", "manual", "", nil,
		map[string]string{metadataKeyRelatedTo: related.ID}, now.Add(time.Second), "")
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}

	s.mu.Lock()
	s.index[related.ID] = related
	s.index[primary.ID] = primary
	s.mu.Unlock()

	// limit=1: without the 1-hop expansion, "related" (older) would be left
	// out of the normal cut — it only shows up because primary.related_to
	// points to it. It proves expandRelated works beyond the requested limit,
	// not only when the related one would show up in the top-K anyway.
	results := s.Recent(1, Filter{Domain: "code:m3m0s"}, now)
	if len(results) != 2 {
		t.Fatalf("expected 2 results (main + expanded), got %d", len(results))
	}
	if results[0].ID != primary.ID || results[0].RelatedVia != nil {
		t.Errorf("expected the first result to be the main one, with no RelatedVia — got %+v", results[0])
	}
	if results[1].ID != related.ID || results[1].RelatedVia == nil || *results[1].RelatedVia != primary.ID {
		t.Errorf("expected the related entry marked with RelatedVia=%s — got %+v", primary.ID, results[1])
	}
}

// ── addOne (real pipeline, controlled embedding — Phase 7, D15) ──

func TestAddOneLinksRelatedFactFoundBySimilarity(t *testing.T) {
	const baseText = "base memory about any subject"
	const relatedText = "related memory, but not identical"
	vectors := map[string][]float32{
		baseText:    {1, 0, 0, 0, 0, 0, 0, 0},
		relatedText: {0.8, 0.6, 0, 0, 0, 0, 0, 0}, // cos with the base = 0.8, within [0.65, 0.95)
	}
	embed := vectorEmbedServer(t, vectors)
	s, err := New(t.TempDir(), "fake-key", embed.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	now := time.Now()

	baseOut, err := s.Add(ctx, AddInput{Content: baseText, Domain: "code:m3m0s"}, now)
	if err != nil {
		t.Fatalf("Add base: %v", err)
	}
	base := baseOut.Entries[0].Entry

	cl := &fakeClassifierExtractor{relation: relationContradicts}
	cl.facts = []string{relatedText}
	s.SetExtractor(cl)

	out, err := s.Add(ctx, AddInput{Content: "irrelevant, the fake extractor ignores it", Domain: "code:m3m0s"}, now)
	if err != nil {
		t.Fatalf("Add related: %v", err)
	}
	if len(out.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(out.Entries))
	}
	if out.Entries[0].Deduped {
		t.Fatal("similarity 0.8 is below the dedup floor — should not have been treated as a duplicate")
	}
	newEntry := out.Entries[0].Entry
	if newEntry.Metadata[metadataKeyRelatedTo] != base.ID {
		t.Errorf("expected related_to=%s on the new entry, got %q", base.ID, newEntry.Metadata[metadataKeyRelatedTo])
	}

	updatedBase, err := s.Get(base.ID, time.Now())
	if err != nil {
		t.Fatalf("Get base: %v", err)
	}
	if updatedBase.Metadata[metadataKeyValidUntil] == "" {
		t.Error("expected the base invalidated (valid_until) after \"contradicts\"")
	}
	if updatedBase.Metadata[metadataKeyRelatedTo] != newEntry.ID {
		t.Errorf("expected bidirectional link, base.related_to = %q, want %q", updatedBase.Metadata[metadataKeyRelatedTo], newEntry.ID)
	}
}
