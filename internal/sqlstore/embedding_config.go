package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// DefaultEmbeddingModel is the model every vector stored before the
// embedding configuration existed came from (migration 00005 backfills it).
const DefaultEmbeddingModel = "text-embedding-3-small"

// EmbeddingConfigRow is the single row of embedding_config (#39, D36).
type EmbeddingConfigRow struct {
	Enabled    bool
	BaseURL    string
	APIKey     string // empty = fall back to the environment key
	Model      string
	Dimensions int // requested "dimensions" parameter, 0 = provider default
	VectorDims int // length the provider returned when the config was saved
	UpdatedAt  time.Time
}

// GetEmbeddingConfig returns the saved configuration; ok=false when the
// admin never saved one (the environment defaults apply).
func (db *DB) GetEmbeddingConfig(ctx context.Context) (cfg EmbeddingConfigRow, ok bool, err error) {
	var enabled int
	var updated string
	err = db.sql.QueryRowContext(ctx, `
		SELECT enabled, base_url, api_key, model, dimensions, vector_dims, updated_at
		FROM embedding_config WHERE id = 1`).Scan(&enabled, &cfg.BaseURL, &cfg.APIKey, &cfg.Model, &cfg.Dimensions, &cfg.VectorDims, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return EmbeddingConfigRow{}, false, nil
	}
	if err != nil {
		return EmbeddingConfigRow{}, false, fmt.Errorf("get embedding config: %w", err)
	}
	cfg.Enabled = enabled != 0
	cfg.UpdatedAt, _ = time.Parse(timeLayout, updated)
	return cfg, true, nil
}

// SetEmbeddingConfig stores (or replaces) the configuration.
func (db *DB) SetEmbeddingConfig(ctx context.Context, c EmbeddingConfigRow) error {
	enabled := 0
	if c.Enabled {
		enabled = 1
	}
	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO embedding_config (id, enabled, base_url, api_key, model, dimensions, vector_dims, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			enabled = excluded.enabled, base_url = excluded.base_url, api_key = excluded.api_key,
			model = excluded.model, dimensions = excluded.dimensions, vector_dims = excluded.vector_dims,
			updated_at = excluded.updated_at`,
		enabled, c.BaseURL, c.APIKey, c.Model, c.Dimensions, c.VectorDims, time.Now().UTC().Format(timeLayout))
	if err != nil {
		return fmt.Errorf("set embedding config: %w", err)
	}
	return nil
}

// EmbeddingSpace identifies a set of comparable vectors.
type EmbeddingSpace struct {
	Model string
	Dims  int
}

// EmbeddingSpaceCounts returns how many stored vectors each space holds.
func (db *DB) EmbeddingSpaceCounts(ctx context.Context) (map[EmbeddingSpace]int, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT `+effectiveModelSQL+`, `+effectiveDimsSQL+`, COUNT(*)
		FROM entries WHERE embedding IS NOT NULL GROUP BY 1, 2`)
	if err != nil {
		return nil, fmt.Errorf("count embedding spaces: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[EmbeddingSpace]int{}
	for rows.Next() {
		var sp EmbeddingSpace
		var n int
		if err := rows.Scan(&sp.Model, &sp.Dims, &n); err != nil {
			return nil, err
		}
		out[sp] = n
	}
	return out, rows.Err()
}

// outsideSpaceSQL selects the rows a re-embedding run still has to handle:
// everything that is not a vector of the active space, including rows with no
// vector at all (stored while keyword-only, or whose vector was dropped).
const outsideSpaceSQL = `NOT (` + inSpaceSQL + `)`

// PendingEntry is one row waiting for a vector of the active space.
type PendingEntry struct {
	ID      string
	Domain  string
	Content string
}

// PendingEmbeddings returns up to limit rows outside space sp with an id
// greater than afterID, in id order (keyset pagination: a row that keeps
// failing cannot make the caller loop on it).
func (db *DB) PendingEmbeddings(ctx context.Context, sp EmbeddingSpace, afterID string, limit int) ([]PendingEntry, error) {
	rows, err := db.sql.QueryContext(ctx,
		`SELECT id, domain, content FROM entries WHERE id > ? AND `+outsideSpaceSQL+` ORDER BY id LIMIT ?`,
		afterID, sp.Model, sp.Dims, sp.Dims, limit)
	if err != nil {
		return nil, fmt.Errorf("pending embeddings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []PendingEntry
	for rows.Next() {
		var p PendingEntry
		if err := rows.Scan(&p.ID, &p.Domain, &p.Content); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PendingStats counts the rows outside space sp and sums their content length
// in characters, each capped at maxChars (the text sent to the provider is
// truncated the same way).
func (db *DB) PendingStats(ctx context.Context, sp EmbeddingSpace, maxChars int) (count int, chars int64, err error) {
	err = db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(MIN(length(content), ?)), 0) FROM entries WHERE `+outsideSpaceSQL,
		maxChars, sp.Model, sp.Dims, sp.Dims).Scan(&count, &chars)
	if err != nil {
		return 0, 0, fmt.Errorf("pending stats: %w", err)
	}
	return count, chars, nil
}

// SetEmbeddingIfPending stores vec for id in space sp, but only when the row
// still has the content that was embedded and is still outside sp. A row that
// was edited (its new vector was written by Update) or already re-embedded by
// a concurrent write is left alone; ok=false then.
func (db *DB) SetEmbeddingIfPending(ctx context.Context, id, content string, sp EmbeddingSpace, vec []float32) (ok bool, err error) {
	res, err := db.sql.ExecContext(ctx,
		`UPDATE entries SET embedding = ?, embedding_model = ?, embedding_dims = ?
		 WHERE id = ? AND content = ? AND `+outsideSpaceSQL,
		encodeEmbedding(vec), sp.Model, len(vec), id, content, sp.Model, sp.Dims, sp.Dims)
	if err != nil {
		return false, fmt.Errorf("set embedding: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
