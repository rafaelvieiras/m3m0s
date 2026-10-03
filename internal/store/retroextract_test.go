package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRunRetroactiveExtractionNoopWithoutExtractor(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Add(context.Background(), AddInput{
		Content: "memory with no extractor configured at all",
		Domain:  "code:m3m0s",
	}, time.Now()); err != nil {
		t.Fatalf("Add: %v", err)
	}

	processed, err := s.RunRetroactiveExtraction(context.Background())
	if err != nil {
		t.Fatalf("RunRetroactiveExtraction: %v", err)
	}
	if processed != 0 {
		t.Fatalf("expected processed=0 without a configured extractor, got %d", processed)
	}
}

func TestRunRetroactiveExtractionProcessesPendingEntry(t *testing.T) {
	s := newTestStore(t)

	// Simulates an entry inserted BEFORE extraction was enabled: Add without
	// an extractor, then turn the extractor on only afterwards — exactly the
	// scenario the job needs to resolve retroactively.
	out, err := s.Add(context.Background(), AddInput{
		Content: "Alice prefers Docker for deployment and uses Go in the m3m0s core.",
		Domain:  "code:m3m0s",
		Tags:    []string{"preference"},
	}, time.Now())
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	originalID := out.Entries[0].Entry.ID
	if _, has := out.Entries[0].Entry.Metadata[metadataKeySourceContent]; has {
		t.Fatalf("entry should not have source_content before the retroactive extraction")
	}

	// fact1/fact2 are deliberately "noise" strings (not natural sentences) so
	// they stay below DedupSimilarityThreshold when embedded by this
	// package's fakeVector (store_test.go): fakeVector is a sum of character
	// codes per position (mod 8), so sentences with similar vocabulary/
	// structure converge to an almost identical direction and would collide
	// as "duplicates" of each other — an artifact only of the fake test
	// embedding, not of OpenAI's real embedding.
	const fact1 = "m|!x(*u|"
	const fact2 = "8'z'^x)%"
	extractor := &fakeExtractor{facts: []string{fact1, fact2}}
	s.SetExtractor(extractor)

	processed, err := s.RunRetroactiveExtraction(context.Background())
	if err != nil {
		t.Fatalf("RunRetroactiveExtraction: %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected processed=1, got %d", processed)
	}
	if extractor.calls.Load() != 1 {
		t.Fatalf("expected 1 call to the extractor, got %d", extractor.calls.Load())
	}

	if _, err := s.Get(originalID, time.Now()); err == nil {
		t.Fatalf("original entry %s should have been deleted after the retroactive extraction", originalID)
	}

	all := s.Recent(10, Filter{}, time.Now())
	if len(all) != 2 {
		t.Fatalf("expected 2 entries (1 per extracted fact), got %d: %+v", len(all), all)
	}
	for _, e := range all {
		if e.Content != fact1 && e.Content != fact2 {
			t.Errorf("entry with unexpected content: %q", e.Content)
		}
		if e.Tags[0] != "preference" {
			t.Errorf("entry %s did not preserve the original tag: %+v", e.ID, e.Tags)
		}
		src, has := e.Metadata[metadataKeySourceContent]
		if !has {
			t.Errorf("entry %s should have metadata.source_content after the retroactive extraction", e.ID)
		}
		if src != "Alice prefers Docker for deployment and uses Go in the m3m0s core." {
			t.Errorf("unexpected source_content: %q", src)
		}
	}
}

func TestRunRetroactiveExtractionSkipsAlreadyExtractedEntries(t *testing.T) {
	s := newTestStore(t)
	s.SetExtractor(&fakeExtractor{facts: []string{"single fact extracted at add time"}})

	if _, err := s.Add(context.Background(), AddInput{
		Content: "content that already goes through Add's normal extraction",
		Domain:  "code:m3m0s",
	}, time.Now()); err != nil {
		t.Fatalf("Add: %v", err)
	}

	extractor := &fakeExtractor{facts: []string{"should not run"}}
	s.SetExtractor(extractor)

	processed, err := s.RunRetroactiveExtraction(context.Background())
	if err != nil {
		t.Fatalf("RunRetroactiveExtraction: %v", err)
	}
	if processed != 0 {
		t.Fatalf("expected processed=0 (entry already extracted at Add), got %d", processed)
	}
	if extractor.calls.Load() != 0 {
		t.Fatalf("extractor should not have been called, it was called %d time(s)", extractor.calls.Load())
	}
}

func TestRunRetroactiveExtractionSkipsEntryUpdatedDuringExtraction(t *testing.T) {
	s := newTestStore(t)

	out, err := s.Add(context.Background(), AddInput{
		Content: "original content, before any concurrent edit",
		Domain:  "code:m3m0s",
	}, time.Now())
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	id := out.Entries[0].Entry.ID

	// funcExtractor simulates an extraction that takes long enough for a
	// concurrent edit (memory_update) to happen midway — exactly the race
	// window the UpdatedAt check in retroExtractOne exists to close.
	racing := funcExtractor{fn: func(ctx context.Context, content, domain string) ([]string, error) {
		if _, err := s.Update(context.Background(), id, UpdatePatch{
			Content: strPtr("content edited by memory_update while the extraction was running"),
		}, time.Now()); err != nil {
			t.Fatalf("concurrent Update: %v", err)
		}
		return []string{"fact extracted from the OLD content, already obsolete"}, nil
	}}
	s.SetExtractor(racing)

	processed, err := s.RunRetroactiveExtraction(context.Background())
	if err != nil {
		t.Fatalf("RunRetroactiveExtraction: %v", err)
	}
	if processed != 0 {
		t.Fatalf("expected processed=0 (entry changed mid-extraction), got %d", processed)
	}

	current, err := s.Get(id, time.Now())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if current.Content != "content edited by memory_update while the extraction was running" {
		t.Fatalf("concurrent edit was lost — job deleted the new content using facts from the old one: %q", current.Content)
	}
}

func TestRunRetroactiveExtractionRespectsBatchLimit(t *testing.T) {
	s := newTestStore(t)

	total := retroExtractionBatchLimit + 5
	for i := 0; i < total; i++ {
		// unique domain per entry (not just content): findDuplicate only compares
		// within the same domain (where={"domain": domain}), so this guarantees
		// the N entries never collide by dedup with each other in this round,
		// even with the test fakeVector's bias (sum of character codes per
		// position, which converges to almost identical directions for similar
		// texts — see the comment in
		// TestRunRetroactiveExtractionProcessesPendingEntry).
		if _, err := s.Add(context.Background(), AddInput{
			Content: fmt.Sprintf("memory number %d of the test batch", i),
			Domain:  fmt.Sprintf("code:batch-%03d", i),
		}, time.Now()); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}

	extractor := &fakeExtractor{facts: []string{"retroactive fact"}}
	s.SetExtractor(extractor)

	processed, err := s.RunRetroactiveExtraction(context.Background())
	if err != nil {
		t.Fatalf("RunRetroactiveExtraction: %v", err)
	}
	if processed != retroExtractionBatchLimit {
		t.Fatalf("expected processed=%d (batch ceiling), got %d", retroExtractionBatchLimit, processed)
	}

	pending := s.pendingExtractionCandidates(1000)
	if len(pending) != 5 {
		t.Fatalf("expected 5 candidates left for the next run, left %d", len(pending))
	}
}

// TestRunRetroactiveExtractionShrinksEntryOnPartialFailure covers the
// production finding that motivated D18: a giant raw entry (D12 item 5
// fallback) whose extraction fails because the content is too large for 1
// call only. Before D18, retroExtractOne treated this as all-or-nothing —
// if the ONLY Extract call timed out, the whole entry stayed stuck,
// retrying from scratch on every job run, forever. With the content
// sliced, even if 1 piece keeps failing, the other(s) advance: the
// original entry is replaced by a mix of extracted facts (with
// source_content) and a much smaller raw piece (without source_content) —
// which is a candidate again on the next run, but now small enough to
// have a real chance of succeeding.
func TestRunRetroactiveExtractionShrinksEntryOnPartialFailure(t *testing.T) {
	s := newTestStore(t)

	blockOK := "prefixOK-" + repeatByte('o', 5000)
	blockFail := "prefixFAIL-" + repeatByte('f', 5000)
	giant := blockOK + "\n\n" + blockFail

	out, err := s.Add(context.Background(), AddInput{Content: giant, Domain: "code:m3m0s"}, time.Now())
	if err != nil {
		t.Fatalf("Add (no extractor, raw): %v", err)
	}
	originalID := out.Entries[0].Entry.ID

	extractor := funcExtractor{fn: func(_ context.Context, chunk, _ string) ([]string, error) {
		if strings.HasPrefix(chunk, "prefixFAIL") {
			return nil, errors.New("simulated timeout, only on this piece")
		}
		return []string{"m|!x(*u|"}, nil // low-similarity fact (see comment above)
	}}
	s.SetExtractor(extractor)

	processed, err := s.RunRetroactiveExtraction(context.Background())
	if err != nil {
		t.Fatalf("RunRetroactiveExtraction: %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected processed=1 (partial progress still counts), got %d", processed)
	}

	if _, err := s.Get(originalID, time.Now()); err == nil {
		t.Fatal("giant original entry should have been deleted and replaced")
	}

	pending := s.pendingExtractionCandidates(1000)
	if len(pending) != 1 {
		t.Fatalf("expected exactly 1 candidate left (the piece that failed), got %d", len(pending))
	}
	if pending[0].Content != blockFail {
		t.Errorf("remaining candidate should be only the piece that failed (much smaller than the original), got %d chars", len(pending[0].Content))
	}
	if len([]rune(pending[0].Content)) >= len([]rune(giant)) {
		t.Error("remaining candidate should be smaller than the original entry — without that, future runs do not converge")
	}
}

func repeatByte(b byte, n int) string {
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = b
	}
	return string(buf)
}

type funcExtractor struct {
	fn func(ctx context.Context, content, domain string) ([]string, error)
}

func (f funcExtractor) Extract(ctx context.Context, content, domain string) ([]string, error) {
	return f.fn(ctx, content, domain)
}
