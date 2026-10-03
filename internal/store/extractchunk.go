package store

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

// extractionChunkSize is the target size (in runes) of each piece sent in
// an Extract call — below the ceiling where the fixed 20s client timeouts
// of internal/extract became systematic in production (D18): PreCompact/
// SessionEnd digests of 30-40 thousand characters (close to
// internal/hook's maxDigestChars) almost always timed out. 8,000
// characters (~2 thousand tokens) is conservative enough for an
// individual call to finish well within the timeout in practice.
const extractionChunkSize = 8000

// extractionChunkBudget is the total time ceiling (1st attempt + retry)
// per piece (D19 in docs/decisions/) — it was found in production that
// even after D18 sliced the content into 8,000-character pieces, a good
// share of calls kept failing for reasons that were not volume: empty
// response, loose prose response instead of JSON, and still timeouts
// (internal/extract's 20s client). As the pieces of the same content run
// concurrently (extractPieces), this ceiling is per piece, not summed — it
// stays below the 30s timeout of the HTTP client used by CLI/MCP/watch to
// talk to this server (internal/client.Client), leaving margin for the
// work that runs after the WaitGroup (addOne per fact: embedding+dedup).
// var, not const: extractchunk_test.go shrinks it temporarily to exercise
// the retry cutoff on an expired budget without waiting a real 25s in a
// test.
var extractionChunkBudget = 25 * time.Second

// extractionMaxAttempts limits how many times extractChunk tries 1 piece
// before giving up and falling back to raw — 2 (1 original attempt + 1
// retry), no more than that: an unbounded retry within
// extractionChunkBudget is already blocked by the expired context, but the
// explicit limit exists so as not to depend only on timing to stop trying.
const extractionMaxAttempts = 2

// extractedPiece is the result of 1 Extract call over 1 piece from
// splitForExtraction. extracted=false when that specific piece failed
// (network, timeout, invalid response) and fell back to raw content ONLY
// FOR ITSELF — D18: a piece that fails does not take down the others,
// unlike the previous behavior (1 giant, atomic call for the whole
// content). facts is always []string{the raw piece} when extracted is
// false. sourceContent is only populated when extracted is true.
type extractedPiece struct {
	facts         []string
	extracted     bool
	sourceContent string
}

// anyExtracted says whether at least 1 piece of pieces actually went
// through extraction — used by Add (to decide AddOutput.Extracted) and by
// retroactive extraction (D16, to decide whether it is worth deleting +
// reinserting the original entry or leaving it intact for the next run).
func anyExtracted(pieces []extractedPiece) bool {
	for _, p := range pieces {
		if p.extracted {
			return true
		}
	}
	return false
}

// extractPieces splits content into pieces of up to extractionChunkSize
// characters (splitForExtraction) and calls s.extractor.Extract on each,
// concurrently — in the same spirit as internal/extract.Extractor already
// supporting concurrent calls (promptTokens/completionTokens use atomic
// precisely for this). Concurrent, not sequential, on purpose (D18):
// sequential would multiply the worst-case latency per piece (N pieces *
// up to extractionChunkBudget each, easily blowing past the 30s default of
// the HTTP client used by CLI/MCP/watch to talk to this server) —
// concurrent keeps the worst case tied to ~extractionChunkBudget (D19:
// 25s, covering 1st attempt + retry), no matter how many pieces exist.
//
// Never called with s.extractor == nil — the callers (Add,
// retroextract.go) already checked that beforehand.
func (s *MemoryStore) extractPieces(ctx context.Context, content, domain string) []extractedPiece {
	chunks := splitForExtraction(content, extractionChunkSize)
	pieces := make([]extractedPiece, len(chunks))

	var wg sync.WaitGroup
	for i, chunk := range chunks {
		wg.Add(1)
		go func(i int, chunk string) {
			defer wg.Done()
			facts, err := s.extractChunk(ctx, chunk, domain)
			if err != nil {
				log.Printf("m3m0s: fact extraction failed on a piece (%d/%d, domain=%s), inserting that piece raw as a fallback: %v",
					i+1, len(chunks), domain, err)
				pieces[i] = extractedPiece{facts: []string{chunk}}
				return
			}
			s.recordCompactionSavings(ctx, chunk, facts)
			pieces[i] = extractedPiece{facts: facts, extracted: true, sourceContent: chunk}
		}(i, chunk)
	}
	wg.Wait()

	return pieces
}

// extractChunk calls s.extractor.Extract on chunk, with up to
// extractionMaxAttempts attempts within a total time budget of
// extractionChunkBudget (D19) — it exists because not every failure
// observed in production was a timeout (empty response, response outside
// the requested format); for those, an immediate second attempt often
// resolves it without costing the whole budget. If the 1st attempt already
// consumes the entire budget (a genuine timeout), the 2nd does not really
// run — ctx.Err() already non-nil breaks the loop, so the worst case stays
// ~1 extractionChunkBudget, not double.
func (s *MemoryStore) extractChunk(ctx context.Context, chunk, domain string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, extractionChunkBudget)
	defer cancel()

	var lastErr error
	for attempt := 1; attempt <= extractionMaxAttempts; attempt++ {
		facts, err := s.extractor.Extract(ctx, chunk, domain)
		if err == nil {
			return facts, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	return nil, lastErr
}

// splitForExtraction splits content into pieces of up to targetSize runes
// each, respecting paragraph boundaries ("\n\n") when possible — it
// exists so that Extract calls never send too much content at once (D18).
// Content of targetSize runes or fewer always returns 1 single piece,
// identical to the original content — unchanged behavior for the common
// case (the overwhelming majority of Add calls never come close to that
// size).
//
// Paragraphs larger than targetSize on their own (rare — a "fact" or
// pasted snippet with no line break) are cut in the middle, by rune, via
// hardSplit — never losing content nor risking an infinite loop.
func splitForExtraction(content string, targetSize int) []string {
	if targetSize <= 0 || len([]rune(content)) <= targetSize {
		return []string{content}
	}

	paragraphs := strings.Split(content, "\n\n")
	var chunks []string
	var current []string
	currentLen := 0 // in runes, already counting the "\n\n" separators between accumulated paragraphs

	flush := func() {
		if len(current) > 0 {
			chunks = append(chunks, strings.Join(current, "\n\n"))
			current = nil
			currentLen = 0
		}
	}

	for _, p := range paragraphs {
		pLen := len([]rune(p))

		if pLen > targetSize {
			flush()
			chunks = append(chunks, hardSplit(p, targetSize)...)
			continue
		}

		addedLen := pLen
		if len(current) > 0 {
			addedLen += 2 // "\n\n"
		}
		if currentLen+addedLen > targetSize && len(current) > 0 {
			flush()
			addedLen = pLen
		}
		current = append(current, p)
		currentLen += addedLen
	}
	flush()

	return chunks
}

// hardSplit cuts s into pieces of up to size runes, without respecting any
// semantic boundary — just splitForExtraction's fallback for a single
// paragraph larger than the target.
func hardSplit(s string, size int) []string {
	runes := []rune(s)
	var out []string
	for len(runes) > 0 {
		n := size
		if n > len(runes) {
			n = len(runes)
		}
		out = append(out, string(runes[:n]))
		runes = runes[n:]
	}
	return out
}
