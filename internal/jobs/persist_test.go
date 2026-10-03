package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/sqlstore"
)

func openTestDB(t *testing.T) *sqlstore.DB {
	t.Helper()
	db, err := sqlstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("sqlstore.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestLoadIntervalsEmptyDB(t *testing.T) {
	db := openTestDB(t)
	got, err := LoadIntervals(context.Background(), db)
	if err != nil {
		t.Fatalf("LoadIntervals: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("LoadIntervals(empty) = %v, want empty map", got)
	}
}

func TestSaveThenLoadIntervalsRoundTrips(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	want := map[string]time.Duration{
		"forget-sweep":           30 * time.Minute,
		"retroactive-extraction": 90 * time.Second,
	}

	for name, interval := range want {
		if err := SaveInterval(ctx, db, name, interval); err != nil {
			t.Fatalf("SaveInterval(%s): %v", name, err)
		}
	}

	got, err := LoadIntervals(ctx, db)
	if err != nil {
		t.Fatalf("LoadIntervals: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("LoadIntervals() = %d entries, want %d", len(got), len(want))
	}
	for name, wantInterval := range want {
		if got[name] != wantInterval {
			t.Errorf("LoadIntervals()[%q] = %s, want %s", name, got[name], wantInterval)
		}
	}
}

func TestSaveIntervalOverwritesOnlyItsOwnJob(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := SaveInterval(ctx, db, "a", time.Minute); err != nil {
		t.Fatalf("SaveInterval(a): %v", err)
	}
	if err := SaveInterval(ctx, db, "b", time.Hour); err != nil {
		t.Fatalf("SaveInterval(b): %v", err)
	}
	// Unlike the old SaveIntervals (JSON, which always rewrote the whole
	// map), SaveInterval (D28) only touches its own row: "b" stays intact
	// after a 2nd call on "a" only.
	if err := SaveInterval(ctx, db, "a", 2*time.Minute); err != nil {
		t.Fatalf("SaveInterval(a, 2nd time): %v", err)
	}

	got, err := LoadIntervals(ctx, db)
	if err != nil {
		t.Fatalf("LoadIntervals: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("LoadIntervals() = %d entries, want 2", len(got))
	}
	if got["a"] != 2*time.Minute {
		t.Errorf("got[a] = %s, want 2m", got["a"])
	}
	if got["b"] != time.Hour {
		t.Errorf("got[b] = %s, want 1h (should not have been touched)", got["b"])
	}
}

func TestSaveIntervalTruncatesToWholeSeconds(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := SaveInterval(ctx, db, "fractional", 1500*time.Millisecond); err != nil {
		t.Fatalf("SaveInterval: %v", err)
	}
	got, err := LoadIntervals(ctx, db)
	if err != nil {
		t.Fatalf("LoadIntervals: %v", err)
	}
	// 1500ms truncates to 1s: the interval_seconds column only stores whole
	// seconds (the same format the WebUI uses with PATCH /api/v1/jobs/:name).
	if got["fractional"] != time.Second {
		t.Errorf("got[fractional] = %s, want 1s (truncated)", got["fractional"])
	}
}
