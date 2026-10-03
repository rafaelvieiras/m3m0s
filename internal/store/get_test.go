package store

import (
	"errors"
	"testing"
	"time"
)

// TestGetExistingEntry covers the happy path of memory_get (elaboration
// after Phase 5/D13, docs/api-contract.md): Get returns exactly the entry
// stored by Add. Since D22, Get is no longer 100% free of side effects
// (it records access reinforcement in metadata), but it still does not
// change UpdatedAt.
func TestGetExistingEntry(t *testing.T) {
	s := newTestStore(t)
	added := mustAdd(t, s, AddInput{
		Content: "content for the get test", Domain: "code:test-get",
		Source: "manual", Agent: "test-agent", Tags: []string{"foo"}, Metadata: map[string]string{"k": "v"},
	})

	got, err := s.Get(added.ID, time.Now())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != added.ID {
		t.Errorf("unexpected ID: %q", got.ID)
	}
	if got.Content != added.Content {
		t.Errorf("unexpected Content: %q", got.Content)
	}
	if got.Domain != added.Domain {
		t.Errorf("unexpected Domain: %q", got.Domain)
	}
	if !got.UpdatedAt.Equal(added.UpdatedAt) {
		t.Errorf("Get should not change UpdatedAt: before=%v after=%v", added.UpdatedAt, got.UpdatedAt)
	}
}

// TestGetNotFoundReturnsErrNotFound covers the 404 case (the same error
// Update already uses, mapped to 404 not_found in the HTTP layer).
func TestGetNotFoundReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Get("mem_nonexistent", time.Now())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestGetDoesNotMutateStore guarantees that calling Get repeatedly does
// not change Content/UpdatedAt between calls (access reinforcement, D22,
// only touches metadata.access_count/last_accessed_at, never the content).
func TestGetDoesNotMutateStore(t *testing.T) {
	s := newTestStore(t)
	added := mustAdd(t, s, AddInput{Content: "stable content", Domain: "code:test-get-immutable"})

	_, err := s.Get(added.ID, time.Now())
	if err != nil {
		t.Fatalf("Get (1st call): %v", err)
	}
	got, err := s.Get(added.ID, time.Now())
	if err != nil {
		t.Fatalf("Get (2nd call): %v", err)
	}
	if got.Content != added.Content || !got.UpdatedAt.Equal(added.UpdatedAt) {
		t.Errorf("Get mutated the entry state between calls: %+v", got)
	}
}
