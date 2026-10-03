package store

import (
	"context"
	"testing"
	"time"
)

// fakeTokenUsageExtractor implements Extractor (via the embedded
// fakeExtractor) and tokenUsageProvider (TokensUsed) with fixed values —
// used only to test that MemoryStore.ExtractionTokensUsed delegates
// correctly via optional type-assertion (same pattern as
// fakeClassifierExtractor in related_test.go).
type fakeTokenUsageExtractor struct {
	fakeExtractor
	prompt, completion int64
}

func (f *fakeTokenUsageExtractor) TokensUsed() (prompt, completion int64) {
	return f.prompt, f.completion
}

func TestApproxTokensHeuristic(t *testing.T) {
	if got := approxTokens(""); got != 0 {
		t.Errorf("approxTokens(\"\") = %d, want 0", got)
	}
	if got := approxTokens("ab"); got != 1 {
		t.Errorf("approxTokens(\"ab\") = %d, want 1 (never 0 for non-empty text)", got)
	}
	if got := approxTokens("12345678"); got != 2 {
		t.Errorf("approxTokens(8 chars) = %d, want 2 (~4 chars/token)", got)
	}
}

func TestExtractionTokensUsedZeroWithoutExtractor(t *testing.T) {
	s := newTestStore(t)
	if got := s.ExtractionTokensUsed(); got != 0 {
		t.Errorf("ExtractionTokensUsed() without extractor = %d, want 0", got)
	}
}

func TestExtractionTokensUsedDelegatesToProvider(t *testing.T) {
	s := newTestStore(t)
	s.SetExtractor(&fakeTokenUsageExtractor{prompt: 100, completion: 40})

	if got, want := s.ExtractionTokensUsed(), int64(140); got != want {
		t.Errorf("ExtractionTokensUsed() = %d, want %d", got, want)
	}
}

func TestExtractionTokensUsedZeroWhenExtractorLacksProvider(t *testing.T) {
	s := newTestStore(t)
	s.SetExtractor(&fakeExtractor{}) // implements Extractor, not tokenUsageProvider

	if got := s.ExtractionTokensUsed(); got != 0 {
		t.Errorf("ExtractionTokensUsed() = %d, want 0 (extractor without TokensUsed)", got)
	}
}

func TestRecordCompactionSavingsAccumulatesOnAdd(t *testing.T) {
	s := newTestStore(t)
	ext := &fakeExtractor{facts: []string{"short fact"}}
	s.SetExtractor(ext)

	original := "This is original content much longer than the atomic fact extracted from it, enough to guarantee positive token savings."
	if _, err := s.Add(context.Background(), AddInput{
		Content: original,
		Domain:  "code:m3m0s",
	}, time.Now()); err != nil {
		t.Fatalf("Add: %v", err)
	}

	saved := s.TokensSavedByCompaction()
	if saved <= 0 {
		t.Fatalf("TokensSavedByCompaction() = %d, want > 0", saved)
	}

	wantSaved := approxTokens(original) - approxTokens("short fact")
	if saved != wantSaved {
		t.Errorf("TokensSavedByCompaction() = %d, want %d", saved, wantSaved)
	}
}

func TestRecordCompactionSavingsSkipsNonPositive(t *testing.T) {
	s := newTestStore(t)
	longFact := "a rewritten fact that happened to end up longer than the short original content"
	ext := &fakeExtractor{facts: []string{longFact}}
	s.SetExtractor(ext)

	if _, err := s.Add(context.Background(), AddInput{
		Content: "curto",
		Domain:  "code:m3m0s",
	}, time.Now()); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if saved := s.TokensSavedByCompaction(); saved != 0 {
		t.Errorf("TokensSavedByCompaction() = %d, want 0 when the facts do not shrink the content", saved)
	}
}

func TestRecordCompactionSavingsSkippedWithoutExtraction(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.Add(context.Background(), AddInput{
		Content: "any content, with no extractor configured",
		Domain:  "code:m3m0s",
	}, time.Now()); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if saved := s.TokensSavedByCompaction(); saved != 0 {
		t.Errorf("TokensSavedByCompaction() = %d, want 0 without extraction", saved)
	}
}

func TestTokensSavedByCompactionPersistsAcrossReload(t *testing.T) {
	dataDir := t.TempDir()
	embed := fakeEmbedServer(t)

	s1, err := New(dataDir, "fake-key", embed.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s1.SetExtractor(&fakeExtractor{facts: []string{"fact"}})

	original := "original content long enough to generate measurable token savings after extraction"
	if _, err := s1.Add(context.Background(), AddInput{
		Content: original,
		Domain:  "code:m3m0s",
	}, time.Now()); err != nil {
		t.Fatalf("Add: %v", err)
	}
	want := s1.TokensSavedByCompaction()
	if want <= 0 {
		t.Fatalf("invalid setup: TokensSavedByCompaction() = %d, want > 0", want)
	}

	s2, err := New(dataDir, "fake-key", embed.URL)
	if err != nil {
		t.Fatalf("second New: %v", err)
	}
	if got := s2.TokensSavedByCompaction(); got != want {
		t.Errorf("second Store: TokensSavedByCompaction() = %d, want %d (persisted in the telemetry table)", got, want)
	}
}

func TestDiskUsageBytesReflectsPersistedFiles(t *testing.T) {
	s := newTestStore(t)

	before, err := s.DiskUsageBytes()
	if err != nil {
		t.Fatalf("DiskUsageBytes: %v", err)
	}

	if _, err := s.Add(context.Background(), AddInput{
		Content: "content that needs to be persisted to disk",
		Domain:  "code:m3m0s",
	}, time.Now()); err != nil {
		t.Fatalf("Add: %v", err)
	}

	after, err := s.DiskUsageBytes()
	if err != nil {
		t.Fatalf("DiskUsageBytes: %v", err)
	}
	if after <= before {
		t.Errorf("DiskUsageBytes() after Add = %d, want > %d (before)", after, before)
	}
}
