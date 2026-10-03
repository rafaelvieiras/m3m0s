package jobs

import (
	"context"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/sqlstore"
)

// LoadIntervals reads the custom interval overrides persisted by
// internal/httpapi after a successful call to Scheduler.SetInterval (D27,
// docs/decisions/). Since D28, the SQLite job_intervals table
// (internal/sqlstore) replaces the old `<dataDir>/jobs-config.json` sidecar;
// with no overrides yet, it returns an empty map, not an error.
func LoadIntervals(ctx context.Context, db *sqlstore.DB) (map[string]time.Duration, error) {
	raw, err := db.LoadJobIntervals(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]time.Duration, len(raw))
	for name, seconds := range raw {
		out[name] = time.Duration(seconds) * time.Second
	}
	return out, nil
}

// SaveInterval writes (or replaces) the override of a single job. Unlike the
// old SaveIntervals (JSON, which always rewrote the whole file), SQLite (D28)
// does a 1-row upsert, so the caller no longer needs to build the full map
// (already loaded overrides + the new change) before calling.
func SaveInterval(ctx context.Context, db *sqlstore.DB, name string, interval time.Duration) error {
	return db.SaveJobInterval(ctx, name, int64(interval/time.Second))
}
