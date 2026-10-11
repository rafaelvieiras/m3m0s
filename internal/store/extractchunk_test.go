package store

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ── splitForExtraction / hardSplit ──

func TestSplitForExtractionReturnsSingleChunkWhenUnderTarget(t *testing.T) {
	content := "short content, well below the target."
	got := splitForExtraction(content, 8000)
	if len(got) != 1 || got[0] != content {
		t.Fatalf("expected 1 piece identical to the original, got %#v", got)
	}
}

func TestSplitForExtractionPacksParagraphsGreedily(t *testing.T) {
	p1 := strings.Repeat("a", 30)
	p2 := strings.Repeat("b", 30)
	p3 := strings.Repeat("c", 30)
	content := strings.Join([]string{p1, p2, p3}, "\n\n")

	// target small enough to pack at most 2 paragraphs per piece (30+2+30=62
	// fits in 70, but +30 from a third would overflow).
	got := splitForExtraction(content, 70)
	if len(got) != 2 {
		t.Fatalf("expected 2 pieces (p1+p2, then p3), got %d: %#v", len(got), got)
	}
	if got[0] != p1+"\n\n"+p2 {
		t.Errorf("piece 0 = %q, want %q", got[0], p1+"\n\n"+p2)
	}
	if got[1] != p3 {
		t.Errorf("piece 1 = %q, want %q", got[1], p3)
	}
}

func TestSplitForExtractionHardSplitsOversizedSingleParagraph(t *testing.T) {
	content := strings.Repeat("x", 25) // a single paragraph, with no "\n\n" at all
	got := splitForExtraction(content, 10)

	if len(got) != 3 {
		t.Fatalf("expected 3 pieces (10+10+5), got %d: %#v", len(got), got)
	}
	rejoined := strings.Join(got, "")
	if rejoined != content {
		t.Errorf("hard split lost content: rejoined=%q, want %q", rejoined, content)
	}
	for i, c := range got {
		if len([]rune(c)) > 10 {
			t.Errorf("piece %d has %d runes, > targetSize=10", i, len([]rune(c)))
		}
	}
}

func TestSplitForExtractionOversizedParagraphAmongNormalOnes(t *testing.T) {
	small1 := strings.Repeat("a", 5)
	huge := strings.Repeat("h", 25)
	small2 := strings.Repeat("b", 5)
	content := strings.Join([]string{small1, huge, small2}, "\n\n")

	got := splitForExtraction(content, 10)
	// small1 becomes its own piece (flush before huge), huge becomes 3
	// hard-split pieces (10+10+5), small2 becomes its own piece.
	if len(got) != 5 {
		t.Fatalf("expected 5 pieces, got %d: %#v", len(got), got)
	}
	if got[0] != small1 {
		t.Errorf("piece 0 = %q, want %q", got[0], small1)
	}
	if got[len(got)-1] != small2 {
		t.Errorf("last piece = %q, want %q", got[len(got)-1], small2)
	}
}

func TestSplitForExtractionNeverExceedsTargetSize(t *testing.T) {
	var paras []string
	for i := 0; i < 50; i++ {
		paras = append(paras, strings.Repeat("z", 137)) // deliberately "odd" size
	}
	content := strings.Join(paras, "\n\n")

	got := splitForExtraction(content, 1000)
	if len(got) < 2 {
		t.Fatalf("expected more than 1 piece for large content, got %d", len(got))
	}
	for i, c := range got {
		if n := len([]rune(c)); n > 1000 {
			t.Errorf("piece %d has %d runes, > targetSize=1000", i, n)
		}
	}
}

// ── extractPieces ──

func TestExtractPiecesSingleCallForSmallContent(t *testing.T) {
	s := newTestStore(t)
	ext := &fakeExtractor{facts: []string{"single fact"}}
	s.SetExtractor(ext)

	pieces := s.extractPieces(context.Background(), "small content", "code:m3m0s")

	if ext.calls.Load() != 1 {
		t.Fatalf("expected 1 call to the extractor, got %d", ext.calls.Load())
	}
	if len(pieces) != 1 || !pieces[0].extracted || pieces[0].sourceContent != "small content" {
		t.Fatalf("expected 1 extracted piece with sourceContent = original content, got %#v", pieces)
	}
}

func TestExtractPiecesSplitsLargeContentIntoMultipleCalls(t *testing.T) {
	s := newTestStore(t)

	blockA := strings.Repeat("A", 5000)
	blockB := strings.Repeat("B", 5000)
	content := blockA + "\n\n" + blockB

	ext := funcExtractor{fn: func(_ context.Context, content, _ string) ([]string, error) {
		return []string{"fact from: " + content[:1]}, nil
	}}
	s.SetExtractor(ext)

	pieces := s.extractPieces(context.Background(), content, "code:m3m0s")

	if len(pieces) != 2 {
		t.Fatalf("expected 2 pieces (blockA + blockB > extractionChunkSize), got %d", len(pieces))
	}
	for i, p := range pieces {
		if !p.extracted {
			t.Errorf("piece %d should have been extracted successfully", i)
		}
	}
	// blockA and blockB become distinct pieces, each with its own
	// sourceContent — never the whole content (a regression D18 fixes).
	sources := map[string]bool{pieces[0].sourceContent: true, pieces[1].sourceContent: true}
	if !sources[blockA] || !sources[blockB] {
		t.Errorf("the pieces' sourceContent should be blockA/blockB in isolation, got: %q / %q", pieces[0].sourceContent, pieces[1].sourceContent)
	}
	if pieces[0].sourceContent == content || pieces[1].sourceContent == content {
		t.Error("a piece's sourceContent should not be the whole content")
	}
}

// TestExtractPiecesPartialFailureFallsBackOnlyForThatPiece covers D18's
// main gain: a piece that fails (simulated timeout) does not take down
// the others — it falls back to raw content only for itself.
func TestExtractPiecesPartialFailureFallsBackOnlyForThatPiece(t *testing.T) {
	s := newTestStore(t)

	blockOK := strings.Repeat("O", 5000)
	blockFail := strings.Repeat("F", 5000)
	content := blockOK + "\n\n" + blockFail

	ext := funcExtractor{fn: func(_ context.Context, chunk, _ string) ([]string, error) {
		if strings.HasPrefix(chunk, "F") {
			return nil, errors.New("simulated timeout only for this piece")
		}
		return []string{"fact extracted from blockOK"}, nil
	}}
	s.SetExtractor(ext)

	pieces := s.extractPieces(context.Background(), content, "code:m3m0s")

	if len(pieces) != 2 {
		t.Fatalf("expected 2 pieces, got %d", len(pieces))
	}
	if !anyExtracted(pieces) {
		t.Fatal("expected at least 1 extracted piece (blockOK)")
	}

	var okCount, failCount int
	for _, p := range pieces {
		if p.extracted {
			okCount++
			if len(p.facts) != 1 || p.facts[0] != "fact extracted from blockOK" {
				t.Errorf("extracted piece with unexpected facts: %#v", p.facts)
			}
		} else {
			failCount++
			if len(p.facts) != 1 || p.facts[0] != blockFail {
				t.Errorf("fallback piece should contain the raw piece itself, got: %#v", p.facts)
			}
		}
	}
	if okCount != 1 || failCount != 1 {
		t.Fatalf("expected 1 ok piece + 1 in fallback, got %d ok / %d fail", okCount, failCount)
	}
}

// ── extractChunk (retry, D19) ──

func TestExtractChunkRetriesOnceAfterFailure(t *testing.T) {
	s := newTestStore(t)

	var calls atomic.Int32
	ext := funcExtractor{fn: func(_ context.Context, _, _ string) ([]string, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("first attempt fails (e.g. empty response)")
		}
		return []string{"fact on the second attempt"}, nil
	}}
	s.SetExtractor(ext)

	facts, err := s.extractChunk(context.Background(), "small piece", "code:m3m0s")
	if err != nil {
		t.Fatalf("extractChunk: %v", err)
	}
	if len(facts) != 1 || facts[0].Text != "fact on the second attempt" {
		t.Fatalf("expected the fact from the 2nd attempt, got %#v", facts)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected exactly 2 attempts, got %d", calls.Load())
	}
}

func TestExtractChunkGivesUpAfterMaxAttempts(t *testing.T) {
	s := newTestStore(t)

	var calls atomic.Int32
	ext := funcExtractor{fn: func(_ context.Context, _, _ string) ([]string, error) {
		calls.Add(1)
		return nil, errors.New("always fails")
	}}
	s.SetExtractor(ext)

	_, err := s.extractChunk(context.Background(), "small piece", "code:m3m0s")
	if err == nil {
		t.Fatal("expected an error after exhausting the attempts")
	}
	if calls.Load() != extractionMaxAttempts {
		t.Fatalf("expected exactly %d attempts (ceiling), got %d", extractionMaxAttempts, calls.Load())
	}
}

func TestExtractChunkDoesNotRetryAfterBudgetExpires(t *testing.T) {
	s := newTestStore(t)

	origBudget := extractionChunkBudget
	extractionChunkBudget = 30 * time.Millisecond
	t.Cleanup(func() { extractionChunkBudget = origBudget })

	var calls atomic.Int32
	ext := funcExtractor{fn: func(ctx context.Context, _, _ string) ([]string, error) {
		calls.Add(1)
		<-ctx.Done() // consumes the entire budget, as a real timeout would
		return nil, ctx.Err()
	}}
	s.SetExtractor(ext)

	pieces := s.extractPieces(context.Background(), "small piece", "code:m3m0s")
	if len(pieces) != 1 || pieces[0].extracted {
		t.Fatalf("expected 1 piece in raw fallback, got %#v", pieces)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected only 1 attempt (the already expired ctx blocks the retry), got %d", calls.Load())
	}
}

func TestExtractPiecesRetrySucceedsForOneChunkAmongMany(t *testing.T) {
	s := newTestStore(t)

	blockA := strings.Repeat("A", 5000)
	blockB := strings.Repeat("B", 5000)
	content := blockA + "\n\n" + blockB

	var callsA, callsB atomic.Int32
	ext := funcExtractor{fn: func(_ context.Context, chunk, _ string) ([]string, error) {
		if strings.HasPrefix(chunk, "A") {
			if callsA.Add(1) == 1 {
				return nil, errors.New("fails only on the 1st attempt of block A")
			}
			return []string{"fact from block A"}, nil
		}
		callsB.Add(1)
		return []string{"fact from block B"}, nil
	}}
	s.SetExtractor(ext)

	pieces := s.extractPieces(context.Background(), content, "code:m3m0s")

	if len(pieces) != 2 {
		t.Fatalf("expected 2 pieces, got %d", len(pieces))
	}
	for i, p := range pieces {
		if !p.extracted {
			t.Errorf("piece %d should have been extracted (with a retry if necessary)", i)
		}
	}
	if callsA.Load() != 2 {
		t.Errorf("expected 2 attempts on block A (1 failure + 1 successful retry), got %d", callsA.Load())
	}
	if callsB.Load() != 1 {
		t.Errorf("expected 1 attempt on block B (success on the first try, no retry), got %d", callsB.Load())
	}
}

// ── Add() with content large enough to slice ── ──

func TestAddSplitsLargeContentAndTracksSourceContentPerChunk(t *testing.T) {
	s := newTestStore(t)

	blockA := strings.Repeat("A", 5000)
	blockB := strings.Repeat("B", 5000)
	content := blockA + "\n\n" + blockB

	// fact1/fact2 are the same low-noise strings already validated in
	// retroextract_test.go (similarity ~0.634 under this package's
	// fakeVector — well below DedupSimilarityThreshold) — it prevents the 2
	// facts from colliding by dedup within the same domain.
	const fact1 = "m|!x(*u|"
	const fact2 = "8'z'^x)%"
	ext := funcExtractor{fn: func(_ context.Context, chunk, _ string) ([]string, error) {
		if strings.HasPrefix(chunk, "A") {
			return []string{fact1}, nil
		}
		return []string{fact2}, nil
	}}
	s.SetExtractor(ext)

	out, err := s.Add(context.Background(), AddInput{Content: content, Domain: "code:m3m0s"}, time.Now())
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !out.Extracted {
		t.Error("expected Extracted=true")
	}
	if len(out.Entries) != 2 {
		t.Fatalf("expected 2 entries (1 per piece), got %d", len(out.Entries))
	}

	sourceContents := map[string]bool{}
	for _, e := range out.Entries {
		src := e.Entry.Metadata[metadataKeySourceContent]
		if src == "" {
			t.Errorf("entry %s without metadata.source_content", e.Entry.ID)
		}
		if src == content {
			t.Errorf("entry %s has source_content = whole content, expected only the piece", e.Entry.ID)
		}
		sourceContents[src] = true
	}
	if len(sourceContents) != 2 {
		t.Errorf("expected 2 distinct source_content values (1 per piece), got %d: %v", len(sourceContents), sourceContents)
	}
}
