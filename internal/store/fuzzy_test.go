package store

import (
	"context"
	"testing"
	"time"
)

// A misspelled project name is repaired against the store vocabulary (D51):
// the search still finds the memories and reports what it actually searched.
func TestSearchRepairsMisspelledTerm(t *testing.T) {
	s := newKeywordStore(t)
	want := map[string]bool{}
	for _, c := range []string{
		"The orchestrator schedules every nightly batch",
		"The orchestrator retries a failed batch twice",
		"Upgrading the orchestrator requires a restart",
	} {
		want[addText(t, s, "code:t", c).Entries[0].Entry.ID] = true
	}
	addText(t, s, "code:t", "Carrot cake recipe with chocolate")

	res, corrected, err := s.SearchCorrected(context.Background(), "what does the orchestrater do", 5, Filter{Domain: "code:t"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if corrected != "what does the orchestrator do" {
		t.Errorf("corrected = %q", corrected)
	}
	if len(res) != 3 {
		t.Fatalf("results = %d, want 3", len(res))
	}
	for _, e := range res {
		if !want[e.ID] {
			t.Errorf("unexpected result %q", e.Content)
		}
	}
}

// Correctly spelled queries are searched as typed and report no correction,
// even when the word is rare or close to a more common one.
func TestSearchLeavesCorrectTermsAlone(t *testing.T) {
	s := newKeywordStore(t)
	for _, c := range []string{
		"The scheduler picks the next job",
		"The scheduler runs every minute",
		"The scheduler stores its state on disk",
		"A scheduled report is emailed on Mondays",
	} {
		addText(t, s, "code:t", c)
	}
	for _, q := range []string{"scheduled report", "scheduler state", "zebra crossing"} {
		_, corrected, err := s.SearchCorrected(context.Background(), q, 5, Filter{Domain: "code:t"}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if corrected != "" {
			t.Errorf("%q was corrected to %q", q, corrected)
		}
	}
}

// Domain names count as vocabulary, so a project whose name never appears in
// its memories' text can still be found through a misspelling.
func TestSearchRepairsMisspelledDomainName(t *testing.T) {
	s := newKeywordStore(t)
	for _, c := range []string{"Builds run nightly", "Releases are tagged by hand", "The cache is warmed at boot"} {
		addText(t, s, "code:harborlight", c)
	}
	_, corrected, err := s.SearchCorrected(context.Background(), "harborligth", 5, Filter{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if corrected != "harborlight" {
		t.Errorf("corrected = %q, want harborlight", corrected)
	}
}
