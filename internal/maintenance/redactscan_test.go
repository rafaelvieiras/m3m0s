package maintenance

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/sqlstore"
)

// Synthetic fixtures only.
const (
	fakePw     = "Zx9-fake-password-1"
	dirtyBlob  = "staging env: ADMIN_PASSWORD=" + fakePw + " and the rest"
	cleanBlob  = "nothing sensitive here, just notes about buildSessionSummaryFromTranscript"
	dirtyEntry = "the password is '" + fakePw + "'"
)

func seed(t *testing.T) *sqlstore.DB {
	t.Helper()
	db, err := sqlstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	for hash, text := range map[string]string{"src_dirty": dirtyBlob, "src_clean": cleanBlob} {
		if err := db.PutSourceBlob(ctx, hash, text); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	for id, content := range map[string]string{"mem_dirty": dirtyEntry, "mem_clean": "plain fact"} {
		e := sqlstore.EntryRow{
			ID: id, Content: content, Domain: "code:test", DomainCategory: "code", Source: "manual",
			Metadata: map[string]string{"source_content_ref": "src_dirty"}, CreatedAt: now, UpdatedAt: now,
			Embedding: []float32{0.1, 0.2, 0.3},
		}
		if err := db.UpsertEntry(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestRedactScanDryRunReportsAndChangesNothing(t *testing.T) {
	db := seed(t)
	ctx := context.Background()
	rep, err := RedactScan(ctx, db, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Blobs) != 1 || rep.Blobs[0].ID != "src_dirty" || rep.Blobs[0].Rules[0] != "kv-secret" {
		t.Fatalf("blob findings: %+v", rep.Blobs)
	}
	if len(rep.Entries) != 1 || rep.Entries[0].ID != "mem_dirty" || rep.Entries[0].Rules[0] != "narrative-secret" {
		t.Fatalf("entry findings: %+v", rep.Entries)
	}
	if rep.BlobsRewritten != 0 {
		t.Fatalf("dry run rewrote blobs: %d", rep.BlobsRewritten)
	}
	if got, _, _ := db.GetSourceBlob(ctx, "src_dirty"); got != dirtyBlob {
		t.Fatal("dry run changed a blob")
	}
	assertNoValue(t, rep.Format())
}

func TestRedactScanApplyRewritesBlobsOnly(t *testing.T) {
	db := seed(t)
	ctx := context.Background()
	rep, err := RedactScan(ctx, db, true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.BlobsRewritten != 1 {
		t.Fatalf("BlobsRewritten = %d", rep.BlobsRewritten)
	}
	got, ok, _ := db.GetSourceBlob(ctx, "src_dirty")
	if !ok || strings.Contains(got, "fake-password") || !strings.Contains(got, "ADMIN_PASSWORD=[REDACTED]") {
		t.Fatalf("blob not re-redacted: %q", got)
	}
	if clean, _, _ := db.GetSourceBlob(ctx, "src_clean"); clean != cleanBlob {
		t.Fatalf("clean blob changed: %q", clean)
	}
	if n, _ := db.CountSourceBlobs(ctx); n != 2 {
		t.Fatalf("blob count = %d, want 2 (keys must be kept)", n)
	}
	// Entries are never rewritten (their embeddings would go stale).
	all, _ := db.LoadAllEntries(ctx)
	for _, e := range all {
		if e.ID == "mem_dirty" && e.Content != dirtyEntry {
			t.Fatalf("entry text was rewritten: %q", e.Content)
		}
		if e.Metadata["source_content_ref"] != "src_dirty" {
			t.Fatalf("entry reference changed: %v", e.Metadata)
		}
	}
	assertNoValue(t, rep.Format())

	// Idempotent: a second pass finds no blob left.
	again, err := RedactScan(ctx, db, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Blobs) != 0 || again.BlobsRewritten != 0 {
		t.Fatalf("second pass still finds blobs: %+v", again.Blobs)
	}
	if len(again.Entries) != 1 {
		t.Fatalf("entry should still be reported for manual review: %+v", again.Entries)
	}
}

func assertNoValue(t *testing.T, out string) {
	t.Helper()
	if strings.Contains(out, "fake-password") || strings.Contains(out, "Zx9") {
		t.Fatalf("report leaks a value: %q", out)
	}
}
