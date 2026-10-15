package sqlstore

import (
	"context"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
)

// Migration 00008 renames the legacy Portuguese domain categories of stored
// entries (D48) and leaves everything else alone.
func TestDomainCategoryMigrationRenamesLegacyCategories(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	if err := goose.DownTo(db.sql, "migrations", 7); err != nil { // back before 00008
		t.Fatalf("down: %v", err)
	}
	now := time.Now().UTC().Format(timeLayout)
	for id, d := range map[string][2]string{
		"a": {"codigo:repo-one", "codigo"},
		"b": {"pessoal:diary", "pessoal"},
		"c": {"escrita:book", "escrita"},
		"d": {"cliente:acme", "cliente"},
		"e": {"infra:homelab", "infra"},
	} {
		if _, err := db.sql.ExecContext(ctx, `INSERT INTO entries (id, content, domain, domain_category, source, tags_json, metadata_json, created_at, updated_at)
			VALUES (?, 'x', ?, ?, 'manual', '[]', '{}', ?, ?)`, id, d[0], d[1], now, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := goose.Up(db.sql, "migrations"); err != nil {
		t.Fatalf("up: %v", err)
	}

	rows, err := db.LoadAllEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]string{
		"a": {"code:repo-one", "code"},
		"b": {"personal:diary", "personal"},
		"c": {"writing:book", "writing"},
		"d": {"client:acme", "client"},
		"e": {"infra:homelab", "infra"},
	}
	for _, r := range rows {
		w := want[r.ID]
		if r.Domain != w[0] || r.DomainCategory != w[1] {
			t.Errorf("%s: got %s/%s, want %s/%s", r.ID, r.Domain, r.DomainCategory, w[0], w[1])
		}
	}
}
