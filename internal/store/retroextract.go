package store

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/redact"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// retroExtractionBatchLimit is the cap on memories processed per
// RunRetroactiveExtraction execution — it keeps a run from locking up the
// process by sweeping a large index all at once (each processed memory is
// 1 network call to extraction). What is left stays without
// metadata.source_content and becomes a candidate again on the job's next
// run (cmd/m3m0s/serve.go registers the job with an Interval of hours —
// see docs/decisions/).
const retroExtractionBatchLimit = 25

// RunRetroactiveExtraction sweeps the index looking for memories that
// never went through the fact-extraction pipeline (Phase 4/D12) — either
// because they were inserted with extraction disabled at Add time, or
// because extraction failed and fell back to raw content (D12, item 5; a
// real production finding: a network timeout to the extraction provider on
// a large digest) — and runs extraction on them now. Each successfully
// processed memory is replaced by N new entries (1 per extracted fact,
// each carrying metadata.source_content — same contract as Add, above),
// and the original entry is deleted. With no Extractor configured, it is a
// no-op (processed=0, err=nil): there is no extraction to run.
//
// A failure on 1 memory (extraction, or the entry changed mid-processing —
// see retroExtractOne) does not abort the whole batch; it is only logged
// and skipped, the entry remains a candidate on the next run. processed
// only counts memories actually replaced by the extracted facts — not the
// "skips" (entry already extracted, deleted or edited in the meantime),
// which retroExtractOne returns without error but without processedOK.
func (s *MemoryStore) RunRetroactiveExtraction(ctx context.Context) (processed int, err error) {
	if s.extractor == nil {
		return 0, nil
	}

	for _, e := range s.pendingExtractionCandidates(retroExtractionBatchLimit) {
		ok, err := s.retroExtractOne(ctx, e)
		if err != nil {
			log.Printf("m3m0s: retroactive extraction failed for %s: %v", e.ID, err)
			continue
		}
		if ok {
			processed++
		}
	}
	return processed, nil
}

// pendingExtractionCandidates lists, from oldest (CreatedAt) to newest, up
// to limit memories without metadata.source_content — the only signal
// available for "this memory never went through extraction" (D12/D13):
// source_content only exists on entries that Add created from
// extractedFacts != nil.
func (s *MemoryStore) pendingExtractionCandidates(limit int) []schema.Entry {
	s.mu.RLock()
	pending := make([]schema.Entry, 0, len(s.index))
	for _, e := range s.index {
		// s.index holds the internal form (D21): extracted entries carry
		// metadataKeySourceContentRef, no longer metadataKeySourceContent — the
		// same signal as always ("already went through extraction"), only the key
		// changed.
		if _, has := e.Metadata[metadataKeySourceContentRef]; has {
			continue
		}
		pending = append(pending, e)
	}
	s.mu.RUnlock()

	sort.Slice(pending, func(i, j int) bool { return pending[i].CreatedAt.Before(pending[j].CreatedAt) })
	if len(pending) > limit {
		pending = pending[:limit]
	}
	return pending
}

// retroExtractOne runs fact extraction over 1 memory (a snapshot, read by
// pendingExtractionCandidates before the extraction network call) —
// sliced into pieces since D18, same extractPieces as Add — and, if the
// index still matches that snapshot, replaces the original entry with the
// extracted facts (plus any piece that failed, inserted raw ONLY FOR
// ITSELF — it becomes a normal candidate on the next run, now much
// smaller than the original entry). processedOK is only true when that
// replacement actually happened; false without error covers the "nothing
// to do" cases (no piece extracted — not even partially —, entry already
// gone, entry changed).
//
// The check against current.UpdatedAt exists because Extract is a network
// call (it may take seconds) and, in that interval, memory_update (D13)
// may have overwritten this same entry's content — without the check, the
// job would delete the ID with the NEW content using facts extracted from
// the OLD content (silent data loss). If it changed (or was already
// deleted in the meantime), it skips this round without error: the entry
// stays without source_content, becomes a candidate again on the next
// run. This narrows the race window, it does not eliminate it — a smaller
// window still exists between the check and the Delete just below.
// Accepted as a known limitation of this first version of the job (see
// docs/decisions/).
func (s *MemoryStore) retroExtractOne(ctx context.Context, snapshot schema.Entry) (processedOK bool, err error) {
	pieces := s.extractPieces(ctx, snapshot.Content, snapshot.Domain)
	if !anyExtracted(pieces) {
		// No piece yielded real extraction, not even partial — deleting and
		// reinserting the same sliced raw content changes nothing (same bytes,
		// none more with source_content) and would only cost the original ID plus
		// any related_to that pointed to it. It stays a candidate for the next run
		// without touching anything (D18).
		return false, nil
	}

	// Direct read of the index (not s.Get): this is only an internal
	// concurrency check (did the entry change between the snapshot and now?),
	// not a real access — it should not count as reinforcement (D22) nor treat
	// soft-delete as ErrNotFound (an entry soft-deleted mid retroactive
	// extraction should still be skipped for safety, but the reason here is
	// "changed", handled below by the UpdatedAt comparison anyway, since
	// soft-delete also updates the entry).
	s.mu.RLock()
	current, ok := s.index[snapshot.ID]
	s.mu.RUnlock()
	if !ok {
		return false, nil
	}
	if !current.UpdatedAt.Equal(snapshot.UpdatedAt) {
		return false, nil
	}

	// Delete the original entry BEFORE inserting the extracted facts —
	// otherwise addOne (dedup/related, Phase 1 + Phase 7/D15) finds the
	// original entry itself as a candidate (by definition, semantically
	// similar to the facts it just generated) and links a related_to doomed to
	// point to an ID this very method would delete right after — or, worse, if
	// the similarity hits the dedup floor, it would merge the extracted fact
	// INTO the original entry itself, and the Delete at the end would erase
	// everything again. If addOne fails after this for some fact, the error
	// block below restores the raw original content as a fallback (same spirit
	// as Add's fallback when extraction fails, D12 item 5) — it never loses
	// the memory because of a failure in the middle of the loop.
	if _, err := s.Delete(ctx, snapshot.ID); err != nil {
		return false, fmt.Errorf("delete original: %w", err)
	}

	baseIn := AddInput{
		Domain:   snapshot.Domain,
		Source:   snapshot.Source,
		Agent:    snapshot.AgentValue(),
		Model:    snapshot.ModelValue(),
		Project:  snapshot.ProjectValue(),
		Tags:     snapshot.Tags,
		IDPrefix: idPrefixOf(snapshot.ID),
	}
	for _, piece := range pieces {
		for _, fact := range piece.facts {
			factContent := redact.Redact(fact).Text
			// withSourceContent copies snapshot.Metadata for each fact (never reuses
			// the same map across iterations) — otherwise the N facts would end up
			// pointing to the same map[string]string, and a future mutation on one
			// entry (e.g. linkRelated) would leak into the others. Same discipline as
			// the fact loop in Add, above. Pieces that failed extraction
			// (piece.extracted == false) do not receive source_content — they become
			// normal candidates of the next run, already much smaller than the
			// original entry (D18).
			factIn := baseIn
			if piece.extracted {
				factIn.Metadata = withSourceContent(snapshot.Metadata, piece.sourceContent)
			} else {
				factIn.Metadata = snapshot.Metadata
			}
			if _, err := s.addOne(ctx, factContent, factIn, time.Now()); err != nil {
				restoreIn := baseIn
				restoreIn.Metadata = snapshot.Metadata
				if _, restoreErr := s.addOne(ctx, snapshot.Content, restoreIn, time.Now()); restoreErr != nil {
					log.Printf("m3m0s: retroactive extraction: failed to restore %s after addOne failed: %v", snapshot.ID, restoreErr)
				}
				return false, fmt.Errorf("addOne: %w", err)
			}
		}
	}

	return true, nil
}

// idPrefixOf extracts the prefix of an ID generated by idgen.New
// ("<prefix>_<12 chars>"), so the entry recreated by retroactive
// extraction keeps the same prefix as the original (e.g. "chunk" for
// m3m0s watch, D11) instead of falling back to the default "mem".
func idPrefixOf(id string) string {
	if idx := strings.IndexByte(id, '_'); idx > 0 {
		return id[:idx]
	}
	return schema.DefaultIDPrefix
}
