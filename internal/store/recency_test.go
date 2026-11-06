package store

import (
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

func sc(id string, score float64, ageDays int) schema.Entry {
	s := score
	return schema.Entry{ID: id, Score: &s, CreatedAt: time.Now().Add(-time.Duration(ageDays) * 24 * time.Hour)}
}

// D59: near-equal scores are ordered newest first; a real gap is preserved.
func TestOrderWithRecencyTieBreakPrefersNewerInsideTheBand(t *testing.T) {
	in := []schema.Entry{
		sc("old", 0.70, 120),
		sc("new", 0.69, 1),
		sc("mid", 0.68, 40),
		sc("far-old", 0.50, 200),
		sc("far-new", 0.49, 2),
		sc("much-older", 0.20, 300),
	}
	got := OrderWithRecencyTieBreak(in, RecencyWindow)
	// old 0.70 @120d, new 0.69 @1d and mid 0.68 @40d fall inside one band:
	// newest first, even though old has the best cosine.
	want := []string{"new", "mid", "old", "far-new", "far-old", "much-older"}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("position %d = %q, want %q (full order: %v)", i, got[i].ID, id, ids(got))
		}
	}
}

func ids(in []schema.Entry) []string {
	out := make([]string, len(in))
	for i, e := range in {
		out[i] = e.ID
	}
	return out
}

// A zero window and a single entry are no-ops.
func TestOrderWithRecencyTieBreakNoopCases(t *testing.T) {
	in := []schema.Entry{sc("a", 0.60, 10), sc("b", 0.59, 1)}
	if got := OrderWithRecencyTieBreak(in, 0); got[0].ID != "a" {
		t.Fatalf("window 0 must not reorder: %v", ids(got))
	}
	one := []schema.Entry{sc("only", 0.6, 1)}
	if got := OrderWithRecencyTieBreak(one, RecencyWindow); len(got) != 1 || got[0].ID != "only" {
		t.Fatalf("single entry changed: %v", ids(got))
	}
}

// Unscored entries (keyword-only hits) keep their position.
func TestOrderWithRecencyTieBreakLeavesUnscoredInPlace(t *testing.T) {
	in := []schema.Entry{
		sc("old", 0.70, 90),
		sc("new", 0.69, 1),
		{ID: "keyword", CreatedAt: time.Now()},
		sc("old2", 0.60, 60),
		sc("new2", 0.59, 2),
	}
	got := OrderWithRecencyTieBreak(in, RecencyWindow)
	if got[2].ID != "keyword" {
		t.Fatalf("unscored entry moved: %v", ids(got))
	}
	if got[0].ID != "new" || got[1].ID != "old" {
		t.Fatalf("scored run before the unscored entry not reordered: %v", ids(got))
	}
	if got[3].ID != "new2" || got[4].ID != "old2" {
		t.Fatalf("scored run after the unscored entry not reordered: %v", ids(got))
	}
}
