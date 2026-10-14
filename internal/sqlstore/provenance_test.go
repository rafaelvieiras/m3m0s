package sqlstore

import (
	"context"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
)

// Entries written before the provenance migration must survive it with NULL
// model/project (unknown model, global), and new columns must round-trip.
func TestProvenanceMigrationKeepsExistingRowsAndRoundTrips(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	if err := goose.Down(db.sql, "migrations"); err != nil { // back to 00002
		t.Fatalf("down: %v", err)
	}
	now := time.Now().UTC().Format(timeLayout)
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO entries (id, content, domain, domain_category, source, tags_json, metadata_json, created_at, updated_at)
		VALUES ('old', 'legacy entry', 'code:x', 'code', 'manual', '[]', '{}', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(db.sql, "migrations"); err != nil {
		t.Fatalf("up: %v", err)
	}

	rows, err := db.LoadAllEntries(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("LoadAllEntries: %v, %d rows", err, len(rows))
	}
	if rows[0].Model != nil || rows[0].Project != nil || rows[0].Content != "legacy entry" {
		t.Fatalf("legacy row after migration: %+v", rows[0])
	}

	model, project := "claude-sonnet-5-5", "m3m0s"
	t0 := time.Now().UTC()
	if err := db.UpsertEntry(ctx, EntryRow{ID: "new", Content: "c", Domain: "code:x", DomainCategory: "code", Source: "claude-code", Model: &model, Project: &project, CreatedAt: t0, UpdatedAt: t0}); err != nil {
		t.Fatal(err)
	}
	rows, err = db.LoadAllEntries(ctx)
	if err != nil || len(rows) != 2 {
		t.Fatalf("reload: %v, %d rows", err, len(rows))
	}
	var got EntryRow
	for _, r := range rows {
		if r.ID == "new" {
			got = r
		}
	}
	if got.Model == nil || *got.Model != model || got.Project == nil || *got.Project != project {
		t.Fatalf("round trip lost provenance: %+v", got)
	}
}
