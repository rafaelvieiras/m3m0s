package sqlstore

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestFTSQueryBuildsSafeOrExpression(t *testing.T) {
	got := FTSQuery(`Décision: "résumé" AND hosting -- a x NEAR(foo) foo`)
	want := `"décision" OR "résumé" OR "and" OR "hosting" OR "near" OR "foo"`
	if got != want {
		t.Errorf("FTSQuery = %s, want %s", got, want)
	}
	if FTSQuery("  ! ? a ") != "" {
		t.Error("query without usable terms should be empty")
	}
}

func TestSearchFTSRanksFiltersByDomainAndFollowsWrites(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	now := time.Now().UTC()
	put := func(id, domain, content string) {
		t.Helper()
		if err := db.UpsertEntry(ctx, EntryRow{ID: id, Content: content, Domain: domain, DomainCategory: "code", Source: "t", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	put("a", "code:x", "The server runs on the living room notebook with Docker")
	put("b", "code:x", "Carrot cake recipe")
	put("c", "code:y", "résumé server in the living room")

	ids, err := db.SearchFTS(ctx, "code:x", "server room", 10)
	if err != nil || !reflect.DeepEqual(ids, []string{"a"}) {
		t.Fatalf("domain-filtered hits = %v (%v)", ids, err)
	}
	if ids, _ := db.SearchFTS(ctx, "", "RESUME", 10); !reflect.DeepEqual(ids, []string{"c"}) {
		t.Errorf("accent/case-insensitive across domains = %v", ids)
	}

	// Content update is reflected; metadata-only update and delete too.
	put("b", "code:x", "Now it talks about the server too")
	if ids, _ := db.SearchFTS(ctx, "code:x", "cake", 10); len(ids) != 0 {
		t.Errorf("old content still indexed: %v", ids)
	}
	if ids, _ := db.SearchFTS(ctx, "code:x", "server", 10); len(ids) != 2 {
		t.Errorf("updated content not indexed: %v", ids)
	}
	if err := db.DeleteEntry(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if ids, _ := db.SearchFTS(ctx, "code:x", "server", 10); !reflect.DeepEqual(ids, []string{"b"}) {
		t.Errorf("after delete = %v", ids)
	}
}
