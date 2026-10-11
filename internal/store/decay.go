// Memory tiers / decay (D22, docs/decisions/ — closes A9), inspired by
// ai-memory's memory tiers, adapted to m3m0s's flat Entry model (no
// concept of a compiled "page"/session): instead of 4 explicit tiers, a
// single retention formula decides who is a candidate for forgetting, and
// only 2 metadata signals (access_count/last_accessed_at) play the role of
// their "reinforcement".
//
// Decisions settled with the maintainer before coding (17/Jul/2026): 1)
// destructive with a grace period — soft-delete first (disappears from
// search/recent, but the data persists), real hard-delete only after
// SoftDeleteGraceDays without restoration; 2) only memories that went
// through fact extraction (Phase 4/D12 — they have
// metadataKeySourceContentRef) decay by default. Raw content inserted
// manually or that fell back from extraction (D12, item 5) is treated as
// more deliberate and stays exempt, as does any entry marked
// metadata.pinned=true (an explicit-exemption mechanism, mirroring
// ai-memory's "pinned: true" — editable today even without a dedicated UI,
// via PATCH /memories/:id on metadata).
package store

import (
	"context"
	"errors"
	"log"
	"math"
	"strconv"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// Metadata keys reserved by D22 — same convention as
// metadataKeySourceContent/metadataKeyRelatedTo: they are not first-class
// fields of schema.Entry, they live inside the free-form metadata_json.
const (
	metadataKeyAccessCount  = "access_count"
	metadataKeyLastAccessed = "last_accessed_at"
	metadataKeyPinned       = "pinned"
	metadataKeyDeletedAt    = "deleted_at"
)

// ErrRetentionModelMismatch is returned by ForgetSweepModel when a real
// (not dry-run) sweep is asked to use a model other than the configured one:
// the model that deletes is only ever changed by the setting.
var ErrRetentionModelMismatch = errors.New("a real forget-sweep must use the configured retention model; only dry-run may preview another")

// Retention formula constants — all in days, chosen in the spirit of
// ai-memory's tiers ("30d hot") but without replicating their 4 tiers
// (working/episodic/semantic/procedural do not map cleanly to m3m0s's flat
// Entry model). Not critical infra: reasonable values to start with,
// adjustable without a schema migration (they are not written to disk,
// only used in the sweep calculation) — see D22 in docs/decisions/ for the
// reasoning.
const (
	// DecayAgeHalfLifeDays: with no access since creation, age alone drops the
	// retention term to 0.5 at 30 days — the same order of magnitude as
	// ai-memory's "30d hot".
	DecayAgeHalfLifeDays = 30.0

	// DecayReinforcementHalfLifeDays: the reinforcement (access) term decays
	// more slowly than pure age — a recent access "holds" the live memory for
	// longer than the mere passage of time would kill it on its own.
	DecayReinforcementHalfLifeDays = 45.0

	// DecayReinforcementWeight weighs the reinforcement term against the age
	// term — 0.5 means reinforcement alone never keeps a memory well above the
	// floor (accessCount would have to be very high), but it helps delay the
	// forgetting of something consulted frequently.
	DecayReinforcementWeight = 0.5

	// ColdRetentionThreshold: a retention score below this becomes a
	// soft-delete candidate in the next ForgetSweep.
	ColdRetentionThreshold = 0.15

	// SoftDeleteGraceDays: the reversible window (Restore) before a
	// soft-deleted entry becomes a real hard-delete candidate.
	SoftDeleteGraceDays = 30
)

const ln2 = 0.6931471805599453

// retentionScore combines 2 terms: a pure exp(-age/half-life) (decays with
// time, with no access) plus a weighted reinforcement term
// (log1p(access_count) * exp(-days_since_last_access/half-life) — more
// accesses and more recent accesses hold the score up). log1p (not raw
// access_count) for diminishing returns: the 10th access weighs much less
// than the 1st. Never negative; 1.0 is the ceiling for a freshly created,
// never accessed entry.
func retentionScore(e schema.Entry, now time.Time) float64 {
	ageDays := now.Sub(e.CreatedAt).Hours() / 24
	if ageDays < 0 {
		ageDays = 0
	}
	ageTerm := math.Exp(-ln2 * ageDays / DecayAgeHalfLifeDays)

	// With no metadata.last_accessed_at (never read via Get/Search since it
	// became eligible), use UpdatedAt as a proxy for the last known human
	// touch — more informative than CreatedAt when the entry was already
	// edited/merged by dedup, and never more recent than "now".
	lastAccess := e.UpdatedAt
	if raw := e.Metadata[metadataKeyLastAccessed]; raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			lastAccess = t
		}
	}
	daysSinceAccess := now.Sub(lastAccess).Hours() / 24
	if daysSinceAccess < 0 {
		daysSinceAccess = 0
	}

	accessCount := parseAccessCount(e.Metadata[metadataKeyAccessCount])
	reinforcement := math.Log1p(float64(accessCount)) * math.Exp(-ln2*daysSinceAccess/DecayReinforcementHalfLifeDays)

	return ageTerm + DecayReinforcementWeight*reinforcement
}

func parseAccessCount(raw string) int {
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// isDecayEligible reports whether e takes part in automatic decay (D22,
// question 2 of the decision round): it must have gone through fact
// extraction (metadataKeySourceContentRef present — e.Metadata here is the
// INTERNAL form kept in s.index, the same signal
// pendingExtractionCandidates uses) and must not be marked
// metadata.pinned=true.
func isDecayEligible(e schema.Entry) bool {
	if e.Metadata[metadataKeyPinned] == "true" {
		return false
	}
	_, extracted := e.Metadata[metadataKeySourceContentRef]
	return extracted
}

// isSoftDeleted reports whether e has metadata.deleted_at set (D22) —
// soft-deleted is excluded by default from Get/Search/Recent/dedup, exactly
// as isExpired (Phase 7/D15) already does for metadata.valid_until, but
// for a different reason (forgetting low-value content, not invalidation
// by contradiction).
func isSoftDeleted(e schema.Entry) bool {
	return e.Metadata[metadataKeyDeletedAt] != ""
}

func withDeletedAt(base map[string]string, now time.Time) map[string]string {
	md := make(map[string]string, len(base)+1)
	for k, v := range base {
		md[k] = v
	}
	md[metadataKeyDeletedAt] = now.UTC().Format(time.RFC3339)
	return md
}

func withoutDeletedAt(base map[string]string) map[string]string {
	md := make(map[string]string, len(base))
	for k, v := range base {
		if k == metadataKeyDeletedAt {
			continue
		}
		md[k] = v
	}
	return md
}

// bumpAccessMetadata returns a copy of md with access_count incremented
// and last_accessed_at updated to now — the "reinforcement" (M8-like) that
// Get and the primary Search results apply on every real read. Never
// mutates base.
func bumpAccessMetadata(base map[string]string, now time.Time) map[string]string {
	md := make(map[string]string, len(base)+3)
	for k, v := range base {
		md[k] = v
	}
	// access_days (D39) counts distinct UTC days with a read: a burst inside
	// one day adds to access_count (the legacy score) but only once here.
	days := parseAccessCount(md[metadataKeyAccessDays])
	sameDay := false
	if raw := md[metadataKeyLastAccessed]; raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			sameDay = t.UTC().Format("2006-01-02") == now.UTC().Format("2006-01-02")
		}
	}
	if !sameDay || days == 0 {
		days++
	}
	md[metadataKeyAccessDays] = strconv.Itoa(days)
	md[metadataKeyAccessCount] = strconv.Itoa(parseAccessCount(md[metadataKeyAccessCount]) + 1)
	md[metadataKeyLastAccessed] = now.UTC().Format(time.RFC3339)
	return md
}

// touchAccess records 1 real read of id (Get, or a primary Search result)
// — best-effort: id gone in the meantime, network/persistence error,
// everything is only logged, never propagated. Reinforcement is telemetry
// for decay, it must not take down a read that already happened (the
// caller already has the entry in hand before calling this).
func (s *MemoryStore) touchAccess(ctx context.Context, id string, now time.Time) {
	s.mu.Lock()
	entry, ok := s.index[id]
	s.mu.Unlock()
	if !ok {
		return
	}

	entry.Metadata = bumpAccessMetadata(entry.Metadata, now)
	// UpdateMetadata (internal/sqlstore, D28) rewrites only metadata_json/
	// updated_at, without touching the embedding column — equivalent to what
	// rewriteChromemMetadata did via a GetByID+AddDocument just to preserve
	// the already computed embedding (Content does not change here). Passes
	// entry.UpdatedAt (not now): access reinforcement should NEVER change
	// "when this entry was actually edited" — same behavior as before D28,
	// where only the metadata_json embedded in ToMetadata changed.
	if err := s.db.UpdateMetadata(ctx, entry.ID, entry.Metadata, entry.UpdatedAt); err != nil {
		log.Printf("m3m0s: access reinforcement on %s: %v", id, err)
		return
	}

	s.mu.Lock()
	s.index[entry.ID] = entry
	s.mu.Unlock()
}

// softDeleteOne marks e as soft-deleted (metadata.deleted_at = now) and
// persists it in index + SQLite. e is a snapshot already read under RLock
// by the caller (sweepCandidates) — it implicitly reloads the latest
// version only at the final write via s.index[e.ID] = e, same discipline
// as touchCandidate/Update.
func (s *MemoryStore) softDeleteOne(ctx context.Context, e schema.Entry, now time.Time) error {
	e.Metadata = withDeletedAt(e.Metadata, now)
	// Passes e.UpdatedAt (not now) — soft-delete never changed "when this
	// entry was actually edited" before D28, same rationale as touchAccess
	// above.
	if err := s.db.UpdateMetadata(ctx, e.ID, e.Metadata, e.UpdatedAt); err != nil {
		return err
	}
	s.mu.Lock()
	s.index[e.ID] = e
	s.mu.Unlock()
	return nil
}

// SweepResult is the result of ForgetSweep — Candidates always reflects
// who qualifies in this call (dry-run or not); Applied is only filled in a
// real call (dry_run=false).
type SweepResult struct {
	DryRun bool `json:"dryRun"`
	// Model is the retention model that scored this call (D39).
	Model                RetentionModel `json:"model"`
	Evaluated            int            `json:"evaluated"`
	SoftDeleteCandidates []string       `json:"softDeleteCandidates"`
	HardDeleteCandidates []string       `json:"hardDeleteCandidates"`
	// HardDeleteSpared counts soft-deleted entries past the grace window that
	// the per-kind model refused to hard-delete because it would not have
	// soft-deleted them (they stay soft-deleted and restorable). Always 0 in
	// the legacy model.
	HardDeleteSpared int      `json:"hardDeleteSpared"`
	SoftDeleted      []string `json:"softDeleted"`
	HardDeleted      []string `json:"hardDeleted"`
}

// sweepCandidates sweeps the index under RLock and classifies each entry
// into 1 of 3 buckets: already soft-deleted for more than
// SoftDeleteGraceDays (hard-delete candidate), eligible (D22) with a score
// below the floor (soft-delete candidate), or neither (ignored). evaluated
// counts only the eligible ones that took part in the score calculation —
// not the already soft-deleted ones, which are not "evaluated" again, only
// checked by the age of their own deletion.
//
// With the per-kind model (D39) a soft-deleted entry past the grace window is
// hard-deleted only if the model would also soft-delete it now: entries that
// the legacy score soft-deleted but the per-kind score keeps are spared (and
// counted), because the hard-delete is the irreversible step.
func (s *MemoryStore) sweepCandidates(now time.Time, model RetentionModel, p RetentionParams) (soft, hard []schema.Entry, evaluated, spared int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ev := newRetentionEval(s.index, model, p)
	for _, e := range s.index {
		if isSoftDeleted(e) {
			deletedAt, err := time.Parse(time.RFC3339, e.Metadata[metadataKeyDeletedAt])
			if err != nil {
				continue
			}
			if now.Sub(deletedAt) >= SoftDeleteGraceDays*24*time.Hour {
				if model == RetentionPerKind && (!ev.eligible(e) || !ev.cold(e, now)) {
					spared++
					continue
				}
				hard = append(hard, e)
			}
			continue
		}
		if !ev.eligible(e) {
			continue
		}
		evaluated++
		if ev.cold(e, now) {
			soft = append(soft, e)
		}
	}
	return soft, hard, evaluated, spared
}

// ForgetSweep is D22's "automatic forgetting" (closes A9): dryRun=true only
// reports who would qualify, changing nothing — meant for manual preview
// (WebUI/CLI/MCP) before trusting the scheduled job (cmd/m3m0s/serve.go).
// dryRun=false soft-deletes the cold candidates and hard-deletes (a real
// Delete, the same cascading-purge path as Phase 2) those that had already
// been soft-deleted for more than SoftDeleteGraceDays. An isolated failure
// (network, id gone midway) is logged and skipped — it never aborts the
// whole sweep, same spirit as RunRetroactiveExtraction.
//
// It scores with the configured retention model (D39, legacy by default).
func (s *MemoryStore) ForgetSweep(ctx context.Context, dryRun bool, now time.Time) (SweepResult, error) {
	model, _ := s.Retention()
	return s.ForgetSweepModel(ctx, dryRun, now, model)
}

// ForgetSweepModel is ForgetSweep with an explicit model, so a dry-run can
// preview what another model would do. A real sweep must use the configured
// model (ErrRetentionModelMismatch otherwise): flipping the setting is the
// only way to change what gets deleted.
func (s *MemoryStore) ForgetSweepModel(ctx context.Context, dryRun bool, now time.Time, model RetentionModel) (SweepResult, error) {
	model, err := ParseRetentionModel(string(model))
	if err != nil {
		return SweepResult{}, err
	}
	configured, params := s.Retention()
	if !dryRun && model != configured {
		return SweepResult{}, ErrRetentionModelMismatch
	}
	soft, hard, evaluated, spared := s.sweepCandidates(now, model, params)

	result := SweepResult{
		DryRun:               dryRun,
		Model:                model,
		Evaluated:            evaluated,
		SoftDeleteCandidates: idsOf(soft),
		HardDeleteCandidates: idsOf(hard),
		HardDeleteSpared:     spared,
	}
	if dryRun {
		return result, nil
	}

	for _, e := range soft {
		if err := s.softDeleteOne(ctx, e, now); err != nil {
			log.Printf("m3m0s: forget-sweep: soft-delete of %s failed: %v", e.ID, err)
			continue
		}
		result.SoftDeleted = append(result.SoftDeleted, e.ID)
	}
	for _, e := range hard {
		if _, err := s.Delete(ctx, e.ID); err != nil {
			log.Printf("m3m0s: forget-sweep: hard-delete of %s failed: %v", e.ID, err)
			continue
		}
		result.HardDeleted = append(result.HardDeleted, e.ID)
	}
	return result, nil
}

func idsOf(entries []schema.Entry) []string {
	if len(entries) == 0 {
		return nil
	}
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	return ids
}

// Restore reverts a soft-delete (D22) — it works at any moment before
// ForgetSweep runs the corresponding hard-delete (it does not impose its
// own "restorable only up to X days" ceiling, the real ceiling is
// SoftDeleteGraceDays, which ForgetSweep already honors). ErrNotFound
// covers both "id never existed" and "id exists but is not soft-deleted" —
// the same code in both cases, on purpose: whoever calls Restore only
// needs to know "there is nothing to restore here", not to tell the two
// reasons apart.
func (s *MemoryStore) Restore(ctx context.Context, id string, now time.Time) (schema.Entry, error) {
	s.mu.RLock()
	entry, ok := s.index[id]
	s.mu.RUnlock()
	if !ok || !isSoftDeleted(entry) {
		return schema.Entry{}, ErrNotFound
	}

	entry.Metadata = withoutDeletedAt(entry.Metadata)
	entry.UpdatedAt = now

	if err := s.db.UpdateMetadata(ctx, entry.ID, entry.Metadata, now); err != nil {
		return schema.Entry{}, err
	}

	s.mu.Lock()
	s.index[entry.ID] = entry
	s.mu.Unlock()
	return s.externalizeEntry(ctx, entry), nil
}

// SoftDeletedCount counts how many index entries are soft-deleted, waiting
// for the hard-delete of a future ForgetSweep — used by GET /stats
// (telemetry).
func (s *MemoryStore) SoftDeletedCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	n := 0
	for _, e := range s.index {
		if isSoftDeleted(e) {
			n++
		}
	}
	return n
}
