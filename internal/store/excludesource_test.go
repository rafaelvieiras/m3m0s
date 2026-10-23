package store

import (
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

func TestRecentExcludeSourceAndSourceCounts(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	mine, err := schema.NewEntry("my own memory", "code:m3m0s", "manual", "", nil, nil, now, "")
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	doc, err := schema.NewEntry("built-in documentation", "code:m3m0s", "selfdocs", "", nil, nil, now, "")
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	s.mu.Lock()
	s.index[mine.ID] = mine
	s.index[doc.ID] = doc
	s.mu.Unlock()

	all := s.Recent(10, Filter{}, now)
	if len(all) != 2 {
		t.Fatalf("without exclusion expected 2 results, got %d", len(all))
	}
	got := s.Recent(10, Filter{ExcludeSource: "selfdocs"}, now)
	if len(got) != 1 || got[0].ID != mine.ID {
		t.Fatalf("expected only the user's memory, got %v", got)
	}

	total, byCategory := s.StatsExcluding("selfdocs")
	if total != 1 || byCategory["code"] != 1 {
		t.Fatalf("StatsExcluding: total=%d byCategory=%v", total, byCategory)
	}
	if all, _ := s.Stats(); all != 2 {
		t.Fatalf("Stats must keep counting everything, got %d", all)
	}

	counts := s.SourceCounts()
	if counts["manual"] != 1 || counts["selfdocs"] != 1 {
		t.Fatalf("unexpected source counts: %v", counts)
	}
}
