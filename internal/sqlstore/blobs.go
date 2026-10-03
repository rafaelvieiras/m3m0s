package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// PutSourceBlob stores content under hash only if it does not exist yet
// (INSERT OR IGNORE — real dedup, D21: N facts extracted from the same
// piece of text share 1 blob).
func (db *DB) PutSourceBlob(ctx context.Context, hash, content string) error {
	_, err := db.sql.ExecContext(ctx, `INSERT OR IGNORE INTO source_blobs (hash, content) VALUES (?, ?)`, hash, content)
	if err != nil {
		return fmt.Errorf("put source blob %s: %w", hash, err)
	}
	return nil
}

// CountSourceBlobs returns how many distinct blobs exist — used in tests
// to confirm deduplication (D21: N facts from the same source text share
// just 1 blob).
func (db *DB) CountSourceBlobs(ctx context.Context) (int, error) {
	var n int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM source_blobs`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count source blobs: %w", err)
	}
	return n, nil
}

// GetSourceBlob returns the text for the hash and whether it was found.
func (db *DB) GetSourceBlob(ctx context.Context, hash string) (string, bool, error) {
	var content string
	err := db.sql.QueryRowContext(ctx, `SELECT content FROM source_blobs WHERE hash = ?`, hash).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get source blob %s: %w", hash, err)
	}
	return content, true, nil
}
