package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// GetTelemetryInt64 returns the value stored under key, or 0 if the key
// was never set (first boot).
func (db *DB) GetTelemetryInt64(ctx context.Context, key string) (int64, error) {
	var v int64
	err := db.sql.QueryRowContext(ctx, `SELECT value FROM telemetry WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get telemetry %s: %w", key, err)
	}
	return v, nil
}

// SetTelemetryInt64 stores (or replaces) the value under key.
func (db *DB) SetTelemetryInt64(ctx context.Context, key string, value int64) error {
	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO telemetry (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	if err != nil {
		return fmt.Errorf("set telemetry %s: %w", key, err)
	}
	return nil
}
