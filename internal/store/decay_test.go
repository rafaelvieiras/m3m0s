package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// mustAddExtracted stores 1 fact through a fakeExtractor (marking the
// entry as eligible for decay, D22 — it needs
// metadataKeySourceContentRef) and returns the Entry as it is in the
// internal index (schema.NewEntry-equivalent, with the reference key, not
// the resolved external "source_content") — a helper common to this
// file's tests.
func mustAddExtracted(t *testing.T, s *MemoryStore, content, domain string) schema.Entry {
	t.Helper()
	s.SetExtractor(&fakeExtractor{facts: []string{content}})
	out, err := s.Add(context.Background(), AddInput{Content: content, Domain: domain}, time.Now())
	s.SetExtractor(nil)
	if err != nil {
		t.Fatalf("Add (extracted setup): %v", err)
	}
	if len(out.Entries) != 1 {
		t.Fatalf("Add (extracted setup): expected 1 entry, got %d", len(out.Entries))
	}
	id := out.Entries[0].Entry.ID
	s.mu.RLock()
	internal := s.index[id]
	s.mu.RUnlock()
	if _, has := internal.Metadata[metadataKeySourceContentRef]; !has {
		t.Fatalf("setup: entry %s did not become eligible (no source_content_ref)", id)
	}
	return internal
}

// setIndexEntry overwrites s.index[e.ID] directly — used by the tests
// below to control CreatedAt/metadata without depending on a real sleep.
func setIndexEntry(s *MemoryStore, e schema.Entry) {
	s.mu.Lock()
	s.index[e.ID] = e
	s.mu.Unlock()
}

func getIndexEntry(t *testing.T, s *MemoryStore, id string) schema.Entry {
	t.Helper()
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.index[id]
	if !ok {
		t.Fatalf("entry %s disappeared from the index", id)
	}
	return e
}

// ── retentionScore / isDecayEligible (pure functions) ──

func TestRetentionScoreFreshUnaccessedEntryIsAtCeiling(t *testing.T) {
	now := time.Now()
	e := schema.Entry{CreatedAt: now, UpdatedAt: now, Metadata: map[string]string{}}
	score := retentionScore(e, now)
	if score < 0.99 || score > 1.01 {
		t.Errorf("freshly created entry, with no access, should have score ~1.0, got %f", score)
	}
}

func TestRetentionScoreDecaysBelowThresholdWithAgeAndNoAccess(t *testing.T) {
	now := time.Now()
	old := now.Add(-6 * DecayAgeHalfLifeDays * 24 * time.Hour) // 6 half-lives of age
	e := schema.Entry{CreatedAt: old, UpdatedAt: old, Metadata: map[string]string{}}
	score := retentionScore(e, now)
	if score >= ColdRetentionThreshold {
		t.Errorf("entry with 6 half-lives of age and no access should be below the floor (%f), score=%f", ColdRetentionThreshold, score)
	}
}

func TestRetentionScoreReinforcedByRecentFrequentAccessStaysAboveThreshold(t *testing.T) {
	now := time.Now()
	old := now.Add(-6 * DecayAgeHalfLifeDays * 24 * time.Hour)
	e := schema.Entry{
		CreatedAt: old,
		UpdatedAt: old,
		Metadata: map[string]string{
			metadataKeyAccessCount:  "50",
			metadataKeyLastAccessed: now.Add(-1 * time.Hour).Format(time.RFC3339),
		},
	}
	score := retentionScore(e, now)
	if score < ColdRetentionThreshold {
		t.Errorf("old entry but accessed 50x recently should stay above the floor (%f), score=%f", ColdRetentionThreshold, score)
	}
}

func TestIsDecayEligible(t *testing.T) {
	cases := []struct {
		name string
		md   map[string]string
		want bool
	}{
		{"extracted and not pinned: eligible", map[string]string{metadataKeySourceContentRef: "src_x"}, true},
		{"not extracted (raw/manual): exempt", map[string]string{}, false},
		{"extracted but pinned: exempt", map[string]string{metadataKeySourceContentRef: "src_x", metadataKeyPinned: "true"}, false},
		{"explicit pinned=false does not exempt", map[string]string{metadataKeySourceContentRef: "src_x", metadataKeyPinned: "false"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isDecayEligible(schema.Entry{Metadata: c.md})
			if got != c.want {
				t.Errorf("isDecayEligible = %v, expected %v", got, c.want)
			}
		})
	}
}

// ── ForgetSweep ──

func TestForgetSweepDryRunReportsCandidatesWithoutMutating(t *testing.T) {
	s := newTestStore(t)
	e := mustAddExtracted(t, s, "cold fact candidate for forgetting", "code:sweep-dry")

	old := time.Now().Add(-6 * DecayAgeHalfLifeDays * 24 * time.Hour)
	e.CreatedAt = old
	e.UpdatedAt = old
	setIndexEntry(s, e)

	now := time.Now()
	result, err := s.ForgetSweep(context.Background(), true, now)
	if err != nil {
		t.Fatalf("ForgetSweep: %v", err)
	}
	if !containsID(result.SoftDeleteCandidates, e.ID) {
		t.Fatalf("expected %s in SoftDeleteCandidates, got %v", e.ID, result.SoftDeleteCandidates)
	}
	if len(result.SoftDeleted) != 0 || len(result.HardDeleted) != 0 {
		t.Errorf("dry-run should not fill SoftDeleted/HardDeleted: %+v", result)
	}

	// Nothing really changed: Get still sees the entry.
	if _, err := s.Get(e.ID, now); err != nil {
		t.Errorf("Get after dry-run: %v (entry should not have been soft-deleted)", err)
	}
}

func TestForgetSweepSoftDeletesColdEligibleEntry(t *testing.T) {
	s := newTestStore(t)
	e := mustAddExtracted(t, s, "real cold fact", "code:sweep-real")

	old := time.Now().Add(-6 * DecayAgeHalfLifeDays * 24 * time.Hour)
	e.CreatedAt = old
	e.UpdatedAt = old
	setIndexEntry(s, e)

	now := time.Now()
	result, err := s.ForgetSweep(context.Background(), false, now)
	if err != nil {
		t.Fatalf("ForgetSweep: %v", err)
	}
	if !containsID(result.SoftDeleted, e.ID) {
		t.Fatalf("expected %s in SoftDeleted, got %v", e.ID, result.SoftDeleted)
	}

	if _, err := s.Get(e.ID, now); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after the sweep should return ErrNotFound (soft-deleted), got %v", err)
	}

	stored := getIndexEntry(t, s, e.ID)
	if stored.Metadata[metadataKeyDeletedAt] == "" {
		t.Error("metadata.deleted_at was not written by the sweep")
	}
}

func TestForgetSweepSkipsPinnedEntry(t *testing.T) {
	s := newTestStore(t)
	e := mustAddExtracted(t, s, "cold but pinned fact", "code:sweep-pinned")

	old := time.Now().Add(-6 * DecayAgeHalfLifeDays * 24 * time.Hour)
	e.CreatedAt = old
	e.UpdatedAt = old
	e.Metadata[metadataKeyPinned] = "true"
	setIndexEntry(s, e)

	now := time.Now()
	result, err := s.ForgetSweep(context.Background(), false, now)
	if err != nil {
		t.Fatalf("ForgetSweep: %v", err)
	}
	if containsID(result.SoftDeleteCandidates, e.ID) {
		t.Errorf("pinned entry should not be a candidate: %v", result.SoftDeleteCandidates)
	}
	if _, err := s.Get(e.ID, now); err != nil {
		t.Errorf("Get after the sweep: %v (pinned entry should never disappear)", err)
	}
}

func TestForgetSweepSkipsNonExtractedRawEntry(t *testing.T) {
	s := newTestStore(t)
	e := mustAdd(t, s, AddInput{Content: "old raw fact, never extracted", Domain: "code:sweep-raw"})

	old := time.Now().Add(-6 * DecayAgeHalfLifeDays * 24 * time.Hour)
	e.CreatedAt = old
	e.UpdatedAt = old
	setIndexEntry(s, e)

	now := time.Now()
	result, err := s.ForgetSweep(context.Background(), false, now)
	if err != nil {
		t.Fatalf("ForgetSweep: %v", err)
	}
	if containsID(result.SoftDeleteCandidates, e.ID) {
		t.Errorf("raw (non-extracted) entry should not be a candidate by default: %v", result.SoftDeleteCandidates)
	}
	if _, err := s.Get(e.ID, now); err != nil {
		t.Errorf("Get after the sweep: %v (raw entry should never disappear by default)", err)
	}
}

func TestForgetSweepHardDeletesAfterGracePeriod(t *testing.T) {
	s := newTestStore(t)
	e := mustAddExtracted(t, s, "fact soft-deleted a long time ago", "code:sweep-hard")

	longAgo := time.Now().Add(-(SoftDeleteGraceDays + 1) * 24 * time.Hour)
	e.Metadata[metadataKeyDeletedAt] = longAgo.UTC().Format(time.RFC3339)
	setIndexEntry(s, e)

	now := time.Now()
	result, err := s.ForgetSweep(context.Background(), false, now)
	if err != nil {
		t.Fatalf("ForgetSweep: %v", err)
	}
	if !containsID(result.HardDeleted, e.ID) {
		t.Fatalf("expected %s in HardDeleted, got %v", e.ID, result.HardDeleted)
	}

	s.mu.RLock()
	_, stillIndexed := s.index[e.ID]
	s.mu.RUnlock()
	if stillIndexed {
		t.Error("entry should have been removed from the index by the hard-delete")
	}
}

func TestForgetSweepDoesNotHardDeleteWithinGracePeriod(t *testing.T) {
	s := newTestStore(t)
	e := mustAddExtracted(t, s, "recently soft-deleted fact", "code:sweep-grace")

	recent := time.Now().Add(-1 * 24 * time.Hour) // well within SoftDeleteGraceDays' grace period
	e.Metadata[metadataKeyDeletedAt] = recent.UTC().Format(time.RFC3339)
	setIndexEntry(s, e)

	now := time.Now()
	result, err := s.ForgetSweep(context.Background(), false, now)
	if err != nil {
		t.Fatalf("ForgetSweep: %v", err)
	}
	if containsID(result.HardDeleted, e.ID) {
		t.Errorf("entry still within the grace period should not be hard-deleted: %v", result.HardDeleted)
	}
	s.mu.RLock()
	_, stillIndexed := s.index[e.ID]
	s.mu.RUnlock()
	if !stillIndexed {
		t.Error("entry within the grace period should not have been removed from the index")
	}
}

// ── Restore ──

func TestRestoreRevertsSoftDelete(t *testing.T) {
	s := newTestStore(t)
	e := mustAddExtracted(t, s, "fact to restore", "code:restore")

	old := time.Now().Add(-6 * DecayAgeHalfLifeDays * 24 * time.Hour)
	e.CreatedAt = old
	e.UpdatedAt = old
	setIndexEntry(s, e)

	now := time.Now()
	if _, err := s.ForgetSweep(context.Background(), false, now); err != nil {
		t.Fatalf("ForgetSweep: %v", err)
	}
	if _, err := s.Get(e.ID, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound before the restore, got %v", err)
	}

	restored, err := s.Restore(context.Background(), e.ID, now)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if restored.ID != e.ID {
		t.Errorf("Restore returned an unexpected ID: %q", restored.ID)
	}

	if _, err := s.Get(e.ID, now); err != nil {
		t.Errorf("Get after Restore: %v (should be visible again)", err)
	}
	stored := getIndexEntry(t, s, e.ID)
	if stored.Metadata[metadataKeyDeletedAt] != "" {
		t.Error("metadata.deleted_at still present after Restore")
	}
}

func TestRestoreNotFoundWhenIDDoesNotExist(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Restore(context.Background(), "mem_never_existed", time.Now())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRestoreNotFoundWhenEntryNotSoftDeleted(t *testing.T) {
	s := newTestStore(t)
	e := mustAdd(t, s, AddInput{Content: "live entry, never deleted", Domain: "code:restore-alive"})

	_, err := s.Restore(context.Background(), e.ID, time.Now())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for a non-soft-deleted entry, got %v", err)
	}
}

// ── Soft-deleted excluded from Search/Recent/Get, and from dedup ──

func TestSoftDeletedEntryExcludedFromSearchRecentGet(t *testing.T) {
	s := newTestStore(t)
	e := mustAdd(t, s, AddInput{Content: "very specific content for the soft-delete search", Domain: "code:soft-visibility"})

	now := time.Now()
	if err := s.softDeleteOne(context.Background(), e, now); err != nil {
		t.Fatalf("softDeleteOne: %v", err)
	}

	if _, err := s.Get(e.ID, now); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get should return ErrNotFound for soft-deleted, got %v", err)
	}

	results, err := s.Search(context.Background(), "very specific content for the soft-delete search", 5, Filter{}, now)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range results {
		if r.ID == e.ID {
			t.Errorf("Search returned a soft-deleted entry: %s", r.ID)
		}
	}

	recent := s.Recent(50, Filter{Domain: "code:soft-visibility"}, now)
	for _, r := range recent {
		if r.ID == e.ID {
			t.Errorf("Recent returned a soft-deleted entry: %s", r.ID)
		}
	}
}

func TestFindDuplicateSkipsSoftDeletedCandidate(t *testing.T) {
	s := newTestStore(t)
	content := "identical repeated text to test dedup against soft-delete"
	e := mustAdd(t, s, AddInput{Content: content, Domain: "code:dedup-soft"})

	now := time.Now()
	if err := s.softDeleteOne(context.Background(), e, now); err != nil {
		t.Fatalf("softDeleteOne: %v", err)
	}

	out, err := s.Add(context.Background(), AddInput{Content: content, Domain: "code:dedup-soft"}, now)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(out.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(out.Entries))
	}
	if out.Entries[0].Deduped {
		t.Error("Add should not have merged/deduped against a soft-deleted candidate")
	}
	if out.Entries[0].Entry.ID == e.ID {
		t.Error("Add revived the soft-deleted entry instead of creating a new one")
	}
}

// ── Access reinforcement (Get/Search) ──

func TestGetReinforcesAccessCountAndLastAccessed(t *testing.T) {
	s := newTestStore(t)
	e := mustAdd(t, s, AddInput{Content: "entry to measure reinforcement via Get", Domain: "code:reinforcement-get"})

	before := getIndexEntry(t, s, e.ID)
	if before.Metadata[metadataKeyAccessCount] != "" {
		t.Fatalf("access_count should start empty, got %q", before.Metadata[metadataKeyAccessCount])
	}

	now := time.Now()
	if _, err := s.Get(e.ID, now); err != nil {
		t.Fatalf("Get (1st time): %v", err)
	}
	if _, err := s.Get(e.ID, now.Add(time.Second)); err != nil {
		t.Fatalf("Get (2nd time): %v", err)
	}

	after := getIndexEntry(t, s, e.ID)
	if after.Metadata[metadataKeyAccessCount] != "2" {
		t.Errorf("access_count after 2 Gets: expected %q, got %q", "2", after.Metadata[metadataKeyAccessCount])
	}
	if after.Metadata[metadataKeyLastAccessed] == "" {
		t.Error("last_accessed_at was not written")
	}
}

func TestSearchReinforcesOnlyPrimaryHitsNotExpandedRelated(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	primary := mustAdd(t, s, AddInput{Content: "primary about golang concurrency", Domain: "code:reinforcement-search"})

	// Create a related one manually (same pattern as related_test.go): it
	// does not need to go through the real similarity pipeline, it only needs
	// to be linked via metadata.related_to from primary. Domain deliberately
	// DIFFERENT from primary's: expandRelated (Phase 7/D15) does not filter by
	// domain (only by tags/expired/soft-deleted), but scoreDomainCandidates in
	// Search does (D28) — this guarantees STRUCTURALLY (not by similarity
	// ranking, fragile with this test file's crude fakeVector) that related
	// can only show up in the result via 1-hop expansion, never as a primary
	// hit.
	related := mustAdd(t, s, AddInput{Content: "something totally unrelated", Domain: "code:reinforcement-search-other-domain"})
	primaryEntry := getIndexEntry(t, s, primary.ID)
	primaryEntry.Metadata = withRelatedTo(primaryEntry.Metadata, related.ID)
	// Since D28, Search reads the primary hit (metadata included) straight
	// from the in-memory index — only the embedding comes from SQLite, and
	// this test does not change the embedding, only metadata.related_to —
	// setIndexEntry is already enough, with no need to sync a separate second
	// store (unlike before D28, when chromem and the in-memory index were 2
	// independent representations that had to be kept in sync by hand).
	setIndexEntry(s, primaryEntry)

	now := time.Now()
	results, err := s.Search(ctx, "primary about golang concurrency", 5, Filter{Domain: "code:reinforcement-search"}, now)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	foundPrimary, foundRelated := false, false
	for _, r := range results {
		if r.ID == primary.ID {
			foundPrimary = true
		}
		if r.ID == related.ID {
			foundRelated = true
		}
	}
	if !foundPrimary || !foundRelated {
		t.Fatalf("expected to see primary and related in the result (1-hop expansion), got %+v", results)
	}

	primaryAfter := getIndexEntry(t, s, primary.ID)
	relatedAfter := getIndexEntry(t, s, related.ID)
	if primaryAfter.Metadata[metadataKeyAccessCount] != "1" {
		t.Errorf("primary should have access_count=1 (primary hit), got %q", primaryAfter.Metadata[metadataKeyAccessCount])
	}
	if relatedAfter.Metadata[metadataKeyAccessCount] != "" {
		t.Errorf("related (only brought in by 1-hop expansion) should not gain reinforcement, got %q", relatedAfter.Metadata[metadataKeyAccessCount])
	}
}

func containsID(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}
