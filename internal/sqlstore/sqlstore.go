// Package sqlstore is the only layer that speaks SQL directly (D28,
// docs/decisions/) — it replaces the 6 hand-rolled JSON files
// (index.json/sourceblobs.json/telemetry.json/jobs-config.json/
// apikeys.json) and chromem-go as the vector layer with a single SQLite
// database (dataDir/m3m0s.db), with the schema evolved by versioned
// migrations (goose, embedded via go:embed in migrations/*.sql) instead of
// ad-hoc scans like internal/store/migrate.go (D21).
//
// Driver: modernc.org/sqlite — pure Go, no cgo, a requirement for the
// static binary the Dockerfile builds with CGO_ENABLED=0 (D2/D4: small
// ARM host). That is why vector search (internal/store) does not use a
// native extension like sqlite-vec (it would require cgo) — embeddings
// live in a BLOB column and cosine similarity is computed in Go over the
// set already filtered by domain, the same brute-force strategy chromem-go
// already used (suited to m3m0s's personal-scale volume).
package sqlstore

import (
	"database/sql"
	"embed"
	"fmt"
	"path/filepath"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB wraps the already-migrated *sql.DB — internal/store, internal/auth
// and internal/jobs keep a reference to this struct instead of opening
// their own connections, in the same spirit of "only 1 component touches
// storage directly" that MemoryStore already had over chromem-go.
type DB struct {
	sql *sql.DB
}

// Open opens (or creates) dataDir/m3m0s.db and runs the pending
// migrations (goose.Up) before returning — a failing migration aborts boot
// (error propagated, never a silent log): unlike the previous discipline
// (D21's migrate.go, which only logged and carried on), a failure here
// means the on-disk schema does not match what the binary expects, and
// continuing to run would risk corrupting data.
func Open(dataDir string) (*DB, error) {
	path := filepath.Join(dataDir, "m3m0s.db")
	// busy_timeout avoids SQLITE_BUSY under concurrent writes (fact
	// extraction in goroutines, D18, and the async Add, D26, may write at
	// the same time as a scheduler job, D16); WAL allows concurrent readers
	// during a write.
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	sqlDB.SetMaxOpenConns(1) // modernc.org/sqlite: 1 writer at a time avoids lock contention beyond what busy_timeout already covers; reads are cheap enough at this scale not to need a pool.

	if err := goose.SetDialect("sqlite"); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("goose dialect: %w", err)
	}
	goose.SetBaseFS(migrationsFS)
	if err := goose.Up(sqlDB, "migrations"); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return &DB{sql: sqlDB}, nil
}

// Close closes the underlying connection.
func (db *DB) Close() error {
	return db.sql.Close()
}
