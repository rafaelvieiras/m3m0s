package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

func strPtr(s string) *string                        { return &s }
func tagsPtr(t []string) *[]string                   { return &t }
func metaPtr(m map[string]string) *map[string]string { return &m }

// mustAdd stores a raw entry (without extraction) and returns the stored
// Entry — a helper common to the Update tests below.
func mustAdd(t *testing.T, s *MemoryStore, in AddInput) schema.Entry {
	t.Helper()
	out, err := s.Add(context.Background(), in, time.Now())
	if err != nil {
		t.Fatalf("Add (setup): %v", err)
	}
	if len(out.Entries) != 1 {
		t.Fatalf("Add (setup): expected 1 entry, got %d", len(out.Entries))
	}
	return out.Entries[0].Entry
}

func TestUpdateNotFoundReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Update(context.Background(), "mem_nonexistent", UpdatePatch{Content: strPtr("x")}, time.Now())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateContentReembedsAndRedacts(t *testing.T) {
	s := newTestStore(t)
	entry := mustAdd(t, s, AddInput{Content: "original content", Domain: "code:m3m0s"})

	secret := "sk-abcdefghijklmnopqrstuvwx"
	updated, err := s.Update(context.Background(), entry.ID, UpdatePatch{
		Content: strPtr("new content with " + secret + " embedded"),
	}, time.Now())
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Content == "" || updated.Content == entry.Content {
		t.Errorf("content was not replaced: %q", updated.Content)
	}
	if containsSecret(updated.Content, secret) {
		t.Errorf("secret leaked into the updated content: %q", updated.Content)
	}

	// The re-embed did happen: searching for the new text must find the entry.
	results, err := s.Search(context.Background(), "new content embedded", 5, Filter{Domain: "code:m3m0s"}, time.Now())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var found bool
	for _, r := range results {
		if r.ID == entry.ID {
			found = true
		}
	}
	if !found {
		t.Error("expected to find the updated entry when searching by the new content (did the re-embed not happen?)")
	}
}

func TestUpdateEmptyContentReturnsValidationError(t *testing.T) {
	s := newTestStore(t)
	entry := mustAdd(t, s, AddInput{Content: "original content", Domain: "code:m3m0s"})

	_, err := s.Update(context.Background(), entry.ID, UpdatePatch{Content: strPtr("   ")}, time.Now())
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Code != "content_required" {
		t.Fatalf("expected ValidationError content_required, got %v", err)
	}
}

func TestUpdateDomainRecalculatesDomainCategory(t *testing.T) {
	s := newTestStore(t)
	entry := mustAdd(t, s, AddInput{Content: "content", Domain: "code:m3m0s"})
	if entry.DomainCategory != "code" {
		t.Fatalf("setup: unexpected domainCategory %q", entry.DomainCategory)
	}

	updated, err := s.Update(context.Background(), entry.ID, UpdatePatch{Domain: strPtr("writing:book-x")}, time.Now())
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Domain != "writing:book-x" {
		t.Errorf("domain not updated: %q", updated.Domain)
	}
	if updated.DomainCategory != "writing" {
		t.Errorf("domainCategory not recalculated: %q", updated.DomainCategory)
	}
}

func TestUpdateInvalidDomainReturnsValidationError(t *testing.T) {
	s := newTestStore(t)
	entry := mustAdd(t, s, AddInput{Content: "content", Domain: "code:m3m0s"})

	_, err := s.Update(context.Background(), entry.ID, UpdatePatch{Domain: strPtr("InvalidDomain")}, time.Now())
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Code != "invalid_domain" {
		t.Fatalf("expected ValidationError invalid_domain, got %v", err)
	}
}

func TestUpdateEmptySourceReturnsValidationError(t *testing.T) {
	s := newTestStore(t)
	entry := mustAdd(t, s, AddInput{Content: "content", Domain: "code:m3m0s", Source: "manual"})

	_, err := s.Update(context.Background(), entry.ID, UpdatePatch{Source: strPtr("")}, time.Now())
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Code != "source_required" {
		t.Fatalf("expected ValidationError source_required, got %v", err)
	}
}

func TestUpdateSourcePresentReplacesValue(t *testing.T) {
	s := newTestStore(t)
	entry := mustAdd(t, s, AddInput{Content: "content", Domain: "code:m3m0s", Source: "manual"})

	updated, err := s.Update(context.Background(), entry.ID, UpdatePatch{Source: strPtr("opencode")}, time.Now())
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Source != "opencode" {
		t.Errorf("source not replaced: %q", updated.Source)
	}
}

func TestUpdateAgentSetAndClear(t *testing.T) {
	s := newTestStore(t)
	entry := mustAdd(t, s, AddInput{Content: "content", Domain: "code:m3m0s"})
	if entry.Agent != nil {
		t.Fatalf("setup: expected agent nil, got %v", entry.Agent)
	}

	updated, err := s.Update(context.Background(), entry.ID, UpdatePatch{Agent: strPtr("test-agent")}, time.Now())
	if err != nil {
		t.Fatalf("Update (set): %v", err)
	}
	if updated.AgentValue() != "test-agent" {
		t.Fatalf("expected agent=test-agent, got %v", updated.Agent)
	}

	cleared, err := s.Update(context.Background(), entry.ID, UpdatePatch{Agent: strPtr("")}, time.Now())
	if err != nil {
		t.Fatalf("Update (clear): %v", err)
	}
	if cleared.Agent != nil {
		t.Errorf("expected agent cleared (nil), got %v", cleared.Agent)
	}
}

func TestUpdateTagsReplaceAndClear(t *testing.T) {
	s := newTestStore(t)
	entry := mustAdd(t, s, AddInput{Content: "content", Domain: "code:m3m0s", Tags: []string{"old"}})

	updated, err := s.Update(context.Background(), entry.ID, UpdatePatch{Tags: tagsPtr([]string{"new1", "new2"})}, time.Now())
	if err != nil {
		t.Fatalf("Update (replace): %v", err)
	}
	if len(updated.Tags) != 2 || !updated.HasTag("new1") || !updated.HasTag("new2") || updated.HasTag("old") {
		t.Fatalf("tags not replaced correctly: %v", updated.Tags)
	}

	cleared, err := s.Update(context.Background(), entry.ID, UpdatePatch{Tags: tagsPtr([]string{})}, time.Now())
	if err != nil {
		t.Fatalf("Update (clear): %v", err)
	}
	if len(cleared.Tags) != 0 {
		t.Errorf("expected empty tags, got %v", cleared.Tags)
	}
}

func TestUpdateMetadataReplaceAndClear(t *testing.T) {
	s := newTestStore(t)
	entry := mustAdd(t, s, AddInput{Content: "content", Domain: "code:m3m0s", Metadata: map[string]string{"old": "1"}})

	updated, err := s.Update(context.Background(), entry.ID, UpdatePatch{Metadata: metaPtr(map[string]string{"new": "2"})}, time.Now())
	if err != nil {
		t.Fatalf("Update (replace): %v", err)
	}
	if len(updated.Metadata) != 1 || updated.Metadata["new"] != "2" || updated.Metadata["old"] != "" {
		t.Fatalf("metadata not replaced correctly: %v", updated.Metadata)
	}

	cleared, err := s.Update(context.Background(), entry.ID, UpdatePatch{Metadata: metaPtr(map[string]string{})}, time.Now())
	if err != nil {
		t.Fatalf("Update (clear): %v", err)
	}
	if len(cleared.Metadata) != 0 {
		t.Errorf("expected empty metadata, got %v", cleared.Metadata)
	}

	// explicit "metadata": null in the PATCH — patch.Metadata points to a nil
	// map (present, but nil). The resulting Metadata must be {} (a non-nil
	// empty map), never nil: nil serializes as JSON `null`, which breaks
	// clients typed as object (e.g. the WebUI's Zod schema).
	nulled, err := s.Update(context.Background(), entry.ID, UpdatePatch{Metadata: metaPtr(nil)}, time.Now())
	if err != nil {
		t.Fatalf("Update (explicit metadata null): %v", err)
	}
	if nulled.Metadata == nil {
		t.Error("Metadata is nil after PATCH with explicit metadata null — should normalize to map[string]string{}")
	}
}

func TestUpdatePartialLeavesUntouchedFieldsIntact(t *testing.T) {
	s := newTestStore(t)
	entry := mustAdd(t, s, AddInput{
		Content: "original content", Domain: "code:m3m0s", Source: "manual",
		Agent: "test-agent", Tags: []string{"foo"}, Metadata: map[string]string{"k": "v"},
	})

	// Only domain is sent — everything else must stay the same.
	updated, err := s.Update(context.Background(), entry.ID, UpdatePatch{Domain: strPtr("code:other")}, time.Now())
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Content != entry.Content {
		t.Errorf("content changed without being sent: %q vs %q", updated.Content, entry.Content)
	}
	if updated.Source != entry.Source {
		t.Errorf("source changed without being sent: %q vs %q", updated.Source, entry.Source)
	}
	if updated.AgentValue() != entry.AgentValue() {
		t.Errorf("agent changed without being sent: %v vs %v", updated.Agent, entry.Agent)
	}
	if len(updated.Tags) != 1 || !updated.HasTag("foo") {
		t.Errorf("tags changed without being sent: %v", updated.Tags)
	}
	if updated.Metadata["k"] != "v" {
		t.Errorf("metadata changed without being sent: %v", updated.Metadata)
	}
	if updated.ID != entry.ID {
		t.Error("id should not change")
	}
	if !updated.CreatedAt.Equal(entry.CreatedAt) {
		t.Error("createdAt should not change")
	}
	if !updated.UpdatedAt.After(entry.UpdatedAt) && !updated.UpdatedAt.Equal(entry.UpdatedAt) {
		t.Error("updatedAt should advance (or at least not regress)")
	}
}

func TestUpdateNoFieldsStillTouchesUpdatedAt(t *testing.T) {
	s := newTestStore(t)
	entry := mustAdd(t, s, AddInput{Content: "content", Domain: "code:m3m0s"})

	later := entry.UpdatedAt.Add(time.Hour)
	updated, err := s.Update(context.Background(), entry.ID, UpdatePatch{}, later)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !updated.UpdatedAt.Equal(later) {
		t.Errorf("updatedAt should become %v, got %v", later, updated.UpdatedAt)
	}
	if updated.Content != entry.Content {
		t.Error("content should not change on an empty patch")
	}
}

func TestUpdateDoesNotRunExtractionOrDedup(t *testing.T) {
	s := newTestStore(t)
	ext := &fakeExtractor{facts: []string{"fact that should not be generated"}}
	s.SetExtractor(ext)

	entry := mustAdd(t, s, AddInput{Content: "original content", Domain: "code:m3m0s"})

	// The Add above called the extractor normally (it was set before the Add)
	// — we reset the count to measure only what Update does next.
	ext.calls.Store(0)

	updated, err := s.Update(context.Background(), entry.ID, UpdatePatch{Content: strPtr("completely new content")}, time.Now())
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if ext.calls.Load() != 0 {
		t.Errorf("Update should not call the extractor, but it called it %d time(s)", ext.calls.Load())
	}
	if updated.Content != "completely new content" {
		t.Errorf("expected the raw content (no extraction) in the updated entry, got %q", updated.Content)
	}

	// Check that no extra entry was created (Update does not generate N
	// entries).
	count, _ := s.Stats()
	if count != 1 {
		t.Errorf("expected 1 entry in total (no dedup or extraction), got %d", count)
	}
}

func TestUpdateDoesNotDedupAgainstSimilarEntry(t *testing.T) {
	s := newTestStore(t)
	// Very distinct content — it needs to stay far enough apart in this
	// package's deterministic fakeVector (raw sum of character codes, barely
	// discriminative for long natural-language texts, which tend to converge
	// to high similarity just from the distribution of common letters) so as
	// not to collide already at Add, otherwise the test would not isolate
	// Update's behavior.
	a := mustAdd(t, s, AddInput{Content: "xyz", Domain: "code:m3m0s"})
	b := mustAdd(t, s, AddInput{Content: "Traditional Italian cooking recipes with fresh pasta", Domain: "code:m3m0s"})
	if count, _ := s.Stats(); count != 2 {
		t.Fatalf("setup: expected 2 distinct entries before Update, got %d", count)
	}

	// After editing B to have the same content as A, Update must NOT merge B
	// into A (dedup is exclusive to the Add pipeline) — b.ID must keep
	// existing as its own entry, and A must not be touched.
	updated, err := s.Update(context.Background(), b.ID, UpdatePatch{Content: strPtr(a.Content)}, time.Now())
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.ID != b.ID {
		t.Fatalf("Update changed the PATCH's target id — it never should: expected %s, got %s", b.ID, updated.ID)
	}

	count, _ := s.Stats()
	if count != 2 {
		t.Errorf("expected 2 entries (no dedup in Update), got %d", count)
	}
}

// Clients that still send the pre-D48 Portuguese category names keep working:
// writes and filters are rewritten to the English names.
func TestLegacyDomainCategoryIsAcceptedOnInput(t *testing.T) {
	s := newTestStore(t)
	out, err := s.Add(context.Background(), AddInput{Content: "legacy client fact about deploys", Domain: "codigo:legacy", Source: "manual"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) == 0 {
		t.Fatal("no entry added")
	}
	got := s.Recent(10, Filter{Domain: "codigo:legacy"}, time.Now())
	if len(got) != 1 || got[0].Domain != "code:legacy" || got[0].DomainCategory != "code" {
		t.Fatalf("recent by legacy domain = %+v", got)
	}
	if got := s.Recent(10, Filter{DomainCategory: "codigo"}, time.Now()); len(got) != 1 {
		t.Fatalf("recent by legacy category = %d entries", len(got))
	}
}
