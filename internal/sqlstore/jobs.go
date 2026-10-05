package sqlstore

import (
	"context"
	"fmt"
)

// LoadJobIntervals returns all interval overrides (D27) — job name ->
// seconds.
func (db *DB) LoadJobIntervals(ctx context.Context) (map[string]int64, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT name, interval_seconds FROM job_intervals`)
	if err != nil {
		return nil, fmt.Errorf("load job intervals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]int64{}
	for rows.Next() {
		var name string
		var seconds int64
		if err := rows.Scan(&name, &seconds); err != nil {
			return nil, err
		}
		out[name] = seconds
	}
	return out, rows.Err()
}

// SaveJobInterval stores (or replaces) the override of 1 job — unlike the
// old jobs-config.json (always rewritten in full), this is a single row,
// the other jobs are not touched.
func (db *DB) SaveJobInterval(ctx context.Context, name string, seconds int64) error {
	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO job_intervals (name, interval_seconds) VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET interval_seconds = excluded.interval_seconds
	`, name, seconds)
	if err != nil {
		return fmt.Errorf("save job interval %s: %w", name, err)
	}
	return nil
}
