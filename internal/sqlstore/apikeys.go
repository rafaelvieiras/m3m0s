package sqlstore

import (
	"context"
	"fmt"
	"time"
)

// APIKeyRow mirrors auth.Key (never stores the raw key, only the hash).
type APIKeyRow struct {
	Prefix    string
	Hash      string
	Label     string
	CreatedAt time.Time
}

// InsertAPIKey stores a new key.
func (db *DB) InsertAPIKey(ctx context.Context, k APIKeyRow) error {
	_, err := db.sql.ExecContext(ctx, `INSERT INTO api_keys (prefix, hash, label, created_at) VALUES (?, ?, ?, ?)`,
		k.Prefix, k.Hash, k.Label, k.CreatedAt.UTC().Format(timeLayout))
	if err != nil {
		return fmt.Errorf("insert api key: %w", err)
	}
	return nil
}

// ListAPIKeys returns all registered keys — used by Verify (scans and
// compares the hash in constant time) and Count.
func (db *DB) ListAPIKeys(ctx context.Context) ([]APIKeyRow, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT prefix, hash, label, created_at FROM api_keys`)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var out []APIKeyRow
	for rows.Next() {
		var k APIKeyRow
		var createdAt string
		if err := rows.Scan(&k.Prefix, &k.Hash, &k.Label, &createdAt); err != nil {
			return nil, err
		}
		t, err := time.Parse(timeLayout, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		k.CreatedAt = t
		out = append(out, k)
	}
	return out, rows.Err()
}
