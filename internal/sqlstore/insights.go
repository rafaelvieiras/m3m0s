package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SaveInsightReport stores (or replaces) the latest report under name.
func (db *DB) SaveInsightReport(ctx context.Context, name string, computedAt time.Time, reportJSON []byte) error {
	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO insight_reports (name, computed_at, report_json) VALUES (?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET computed_at = excluded.computed_at, report_json = excluded.report_json
	`, name, computedAt.UTC().Format(time.RFC3339), string(reportJSON))
	if err != nil {
		return fmt.Errorf("save insight report %s: %w", name, err)
	}
	return nil
}

// LoadInsightReport returns the latest report stored under name; ok is false
// when none was ever saved.
func (db *DB) LoadInsightReport(ctx context.Context, name string) (reportJSON []byte, ok bool, err error) {
	var raw string
	err = db.sql.QueryRowContext(ctx, `SELECT report_json FROM insight_reports WHERE name = ?`, name).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load insight report %s: %w", name, err)
	}
	return []byte(raw), true, nil
}
