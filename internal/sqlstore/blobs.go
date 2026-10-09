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

// ForEachSourceBlob streams every blob to fn, ordered by hash. fn must not
// write to the database (the connection is shared); collect what to change
// and apply it afterwards.
func (db *DB) ForEachSourceBlob(ctx context.Context, fn func(hash, content string)) error {
	rows, err := db.sql.QueryContext(ctx, `SELECT hash, content FROM source_blobs ORDER BY hash`)
	if err != nil {
		return fmt.Errorf("list source blobs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var hash, content string
		if err := rows.Scan(&hash, &content); err != nil {
			return fmt.Errorf("scan source blob: %w", err)
		}
		fn(hash, content)
	}
	return rows.Err()
}

// ReplaceSourceBlobContent rewrites the text of an existing blob while
// keeping its key. Entries reference blobs by hash (D21), so keeping the key
// leaves every reference valid, including the ones cached by a running
// server; the hash is an opaque key, not a checksum anything verifies.
func (db *DB) ReplaceSourceBlobContent(ctx context.Context, hash, content string) error {
	if _, err := db.sql.ExecContext(ctx, `UPDATE source_blobs SET content = ? WHERE hash = ?`, content, hash); err != nil {
		return fmt.Errorf("replace source blob %s: %w", hash, err)
	}
	return nil
}
