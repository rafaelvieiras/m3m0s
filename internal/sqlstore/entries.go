package sqlstore

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// EntryRow is the row format of the entries table — used by
// internal/store to convert from/to schema.Entry. Metadata carries the
// ENTIRE free-form map (including the reserved keys that already live
// inside metadata_json in chromem-go/index.json: source_content_ref,
// related_to, valid_until, pinned, access_count, last_accessed_at,
// deleted_at — D28 does not promote these keys to their own columns, it
// only changes where the map lives; the public contract of
// schema.Entry.Metadata stays identical, a pure round-trip).
type EntryRow struct {
	ID             string
	Content        string
	Domain         string
	DomainCategory string
	Source         string
	Agent          *string
	Model          *string
	Project        *string
	Tags           []string
	Metadata       map[string]string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Embedding      []float32
	// EmbeddingModel names the model that produced Embedding (empty means
	// DefaultEmbeddingModel); the dimension is len(Embedding).
	EmbeddingModel string
}

const timeLayout = time.RFC3339Nano

// Rows written before migration 00005 have NULL embedding_model/dims; they
// were all produced by DefaultEmbeddingModel at length(embedding)/4 floats.
const (
	effectiveModelSQL = `COALESCE(embedding_model, '` + DefaultEmbeddingModel + `')`
	effectiveDimsSQL  = `COALESCE(embedding_dims, length(embedding) / 4)`
	inSpaceSQL        = `embedding IS NOT NULL AND ` + effectiveModelSQL + ` = ? AND (? = 0 OR ` + effectiveDimsSQL + ` = ?)`
)

func encodeEmbedding(vec []float32) []byte {
	if len(vec) == 0 {
		return nil
	}
	buf := make([]byte, 4*len(vec))
	for i, f := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func decodeEmbedding(buf []byte) []float32 {
	if len(buf) == 0 {
		return nil
	}
	vec := make([]float32, len(buf)/4)
	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4:]))
	}
	return vec
}

// UpsertEntry stores e in full (INSERT OR REPLACE — same upsert semantics
// Collection.AddDocument had in chromem-go): it replaces the entire row,
// including the embedding. Used by Add/Update/dedup — every path that
// changes Content has to re-embed anyway (same behavior as today).
func (db *DB) UpsertEntry(ctx context.Context, e EntryRow) error {
	tagsJSON, err := json.Marshal(e.Tags)
	if err != nil {
		return fmt.Errorf("marshal tags: %w", err)
	}
	metadataJSON, err := json.Marshal(e.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	var embModel any
	var embDims any
	if len(e.Embedding) > 0 {
		embModel, embDims = e.EmbeddingModel, len(e.Embedding)
		if e.EmbeddingModel == "" {
			embModel = DefaultEmbeddingModel
		}
	}
	_, err = db.sql.ExecContext(ctx, `
		INSERT INTO entries (id, content, domain, domain_category, source, agent, model, project, tags_json, metadata_json, created_at, updated_at, embedding, embedding_model, embedding_dims)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			content = excluded.content,
			domain = excluded.domain,
			domain_category = excluded.domain_category,
			source = excluded.source,
			agent = excluded.agent,
			model = excluded.model,
			project = excluded.project,
			tags_json = excluded.tags_json,
			metadata_json = excluded.metadata_json,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at,
			embedding = excluded.embedding,
			embedding_model = excluded.embedding_model,
			embedding_dims = excluded.embedding_dims
	`, e.ID, e.Content, e.Domain, e.DomainCategory, e.Source, e.Agent, e.Model, e.Project, string(tagsJSON), string(metadataJSON),
		e.CreatedAt.UTC().Format(timeLayout), e.UpdatedAt.UTC().Format(timeLayout), encodeEmbedding(e.Embedding), embModel, embDims)
	if err != nil {
		return fmt.Errorf("upsert entry %s: %w", e.ID, err)
	}
	return nil
}

// UpdateMetadata rewrites only metadata_json/updated_at of an existing
// entry, without touching the embedding — equivalent to what
// rewriteChromemMetadata did via a GetByID+AddDocument just to preserve
// the already computed Embedding (touchAccess/softDeleteOne/Restore, D22;
// Restore does not change Content, so re-embedding would be wasteful). A
// partial UPDATE in SQL already leaves the other columns untouched,
// without needing to read the embedding back first.
func (db *DB) UpdateMetadata(ctx context.Context, id string, metadata map[string]string, updatedAt time.Time) error {
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	_, err = db.sql.ExecContext(ctx, `UPDATE entries SET metadata_json = ?, updated_at = ? WHERE id = ?`,
		string(metadataJSON), updatedAt.UTC().Format(timeLayout), id)
	if err != nil {
		return fmt.Errorf("update metadata %s: %w", id, err)
	}
	return nil
}

// DeleteEntry removes 1 row by ID. A missing ID is not an error (same
// semantics as chromem's Collection.Delete) — the caller decides the
// meaning of "0 rows affected".
func (db *DB) DeleteEntry(ctx context.Context, id string) error {
	_, err := db.sql.ExecContext(ctx, `DELETE FROM entries WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete entry %s: %w", id, err)
	}
	return nil
}

// DeleteEntries removes several rows by ID at once (DeleteByFilePath,
// D11 — cascading purge from m3m0s watch).
func (db *DB) DeleteEntries(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	_, err := db.sql.ExecContext(ctx, `DELETE FROM entries WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return fmt.Errorf("delete entries: %w", err)
	}
	return nil
}

func scanEntryRow(scan func(dest ...any) error, withEmbedding bool) (EntryRow, error) {
	var e EntryRow
	var agent, model, project sql.NullString
	var tagsJSON, metadataJSON, createdAt, updatedAt string
	var embedding []byte

	dest := []any{&e.ID, &e.Content, &e.Domain, &e.DomainCategory, &e.Source, &agent, &model, &project, &tagsJSON, &metadataJSON, &createdAt, &updatedAt}
	if withEmbedding {
		dest = append(dest, &embedding)
	}
	if err := scan(dest...); err != nil {
		return EntryRow{}, err
	}

	if agent.Valid {
		v := agent.String
		e.Agent = &v
	}
	if model.Valid {
		v := model.String
		e.Model = &v
	}
	if project.Valid {
		v := project.String
		e.Project = &v
	}
	if err := json.Unmarshal([]byte(tagsJSON), &e.Tags); err != nil {
		return EntryRow{}, fmt.Errorf("unmarshal tags of %s: %w", e.ID, err)
	}
	if err := json.Unmarshal([]byte(metadataJSON), &e.Metadata); err != nil {
		return EntryRow{}, fmt.Errorf("unmarshal metadata of %s: %w", e.ID, err)
	}
	t, err := time.Parse(timeLayout, createdAt)
	if err != nil {
		return EntryRow{}, fmt.Errorf("parse created_at of %s: %w", e.ID, err)
	}
	e.CreatedAt = t
	t, err = time.Parse(timeLayout, updatedAt)
	if err != nil {
		return EntryRow{}, fmt.Errorf("parse updated_at of %s: %w", e.ID, err)
	}
	e.UpdatedAt = t
	if withEmbedding {
		e.Embedding = decodeEmbedding(embedding)
	}
	return e, nil
}

// LoadAllEntries returns all entries (without embedding) ordered by
// created_at ascending. Boot uses ForEachEntry instead, which does not hold
// every row twice; this is kept for callers that need the whole slice.
func (db *DB) LoadAllEntries(ctx context.Context) ([]EntryRow, error) {
	var out []EntryRow
	err := db.ForEachEntry(ctx, func(e EntryRow) { out = append(out, e) })
	return out, err
}

// ForEachEntry streams every entry (without embedding) to fn, ordered by
// created_at ascending, one row at a time. It exists so building the
// in-memory index at boot does not materialize a []EntryRow next to the
// index itself (the peak behind #37).
func (db *DB) ForEachEntry(ctx context.Context, fn func(EntryRow)) error {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT id, content, domain, domain_category, source, agent, model, project, tags_json, metadata_json, created_at, updated_at
		FROM entries ORDER BY created_at ASC
	`)
	if err != nil {
		return fmt.Errorf("load all entries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		e, err := scanEntryRow(rows.Scan, false)
		if err != nil {
			return err
		}
		fn(e)
	}
	return rows.Err()
}

// CountEntries returns how many rows exist in the entries table — used by
// the legacy data import (D28) to decide whether it already ran (the
// import only runs when the table is empty).
func (db *DB) CountEntries(ctx context.Context) (int, error) {
	var n int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count entries: %w", err)
	}
	return n, nil
}

// EmbeddingsByIDs returns the embedding of each ID found (IDs with no
// matching row are omitted, not an error — same best-effort spirit as
// externalizeEntry for a missing blob). Used by findDuplicate/Search to
// fetch only the vectors of candidates already filtered by domain in Go
// (via the in-memory index), without loading all embeddings from the
// database at once (a relevant RAM saving for a small ARM host, D4).
func (db *DB) EmbeddingsByIDs(ctx context.Context, ids []string) (map[string][]float32, error) {
	out := make(map[string][]float32, len(ids))
	// SQLite caps bound variables per statement (32766 in this driver):
	// query in chunks so large candidate sets never fail.
	const chunk = 500
	for start := 0; start < len(ids); start += chunk {
		end := min(start+chunk, len(ids))
		part := ids[start:end]
		placeholders := make([]string, len(part))
		args := make([]any, len(part))
		for i, id := range part {
			placeholders[i] = "?"
			args[i] = id
		}
		rows, err := db.sql.QueryContext(ctx, `SELECT id, embedding FROM entries WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("embeddings by ids: %w", err)
		}
		for rows.Next() {
			var id string
			var embedding []byte
			if err := rows.Scan(&id, &embedding); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out[id] = decodeEmbedding(embedding)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// CountEmbeddings returns how many rows carry an embedding of space sp (Dims 0 matches any dimension), so the vector
// index can be sized once at boot instead of growing by append.
func (db *DB) CountEmbeddings(ctx context.Context, sp EmbeddingSpace) (int, error) {
	var n int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE `+inSpaceSQL, sp.Model, sp.Dims, sp.Dims).Scan(&n); err != nil {
		return 0, fmt.Errorf("count embeddings: %w", err)
	}
	return n, nil
}

// ForEachEmbedding streams every stored embedding (id, domain, vector) to fn
// one row at a time, so building the in-memory vector index at boot never
// holds all float32 vectors at once. Rows without an embedding, or from
// another embedding space than sp (Dims 0 matches any dimension), are skipped.
// The blob is read without a copy and decoded into one reused buffer: fn
// must not retain vec after it returns.
func (db *DB) ForEachEmbedding(ctx context.Context, sp EmbeddingSpace, fn func(id, domain string, vec []float32)) error {
	rows, err := db.sql.QueryContext(ctx, `SELECT id, domain, embedding FROM entries WHERE `+inSpaceSQL, sp.Model, sp.Dims, sp.Dims)
	if err != nil {
		return fmt.Errorf("stream embeddings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var scratch []float32
	for rows.Next() {
		var id, domain string
		var embedding sql.RawBytes
		if err := rows.Scan(&id, &domain, &embedding); err != nil {
			return err
		}
		n := len(embedding) / 4
		if n == 0 {
			continue
		}
		if cap(scratch) < n {
			scratch = make([]float32, n)
		}
		vec := scratch[:n]
		for i := range vec {
			vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(embedding[i*4:]))
		}
		fn(id, domain, vec)
	}
	return rows.Err()
}
