package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/sqlstore"
)

func seedMaintenanceDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	if err := db.PutSourceBlob(ctx, "src_dirty", "CLIENT_SECRET=Zx9-fake-password-1"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.UpsertEntry(ctx, sqlstore.EntryRow{
		ID: "mem_dirty", Content: "access_token=Zx9-fake-password-1", Domain: "code:test", DomainCategory: "code",
		Source: "manual", CreatedAt: now, UpdatedAt: now, Embedding: []float32{0.1, 0.2},
	}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestMaintenanceRedactScanDryRunByDefault(t *testing.T) {
	dir := seedMaintenanceDir(t)
	var out, errOut bytes.Buffer
	if err := maintenanceMain(context.Background(), &out, &errOut, []string{"redact-scan", "--data", dir}); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"dry run", "blob  src_dirty", "entry mem_dirty", "rules=kv-secret"} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s+errOut.String(), "fake-password") {
		t.Fatalf("value leaked:\n%s", s)
	}
	db, _ := sqlstore.Open(dir)
	defer func() { _ = db.Close() }()
	if got, _, _ := db.GetSourceBlob(context.Background(), "src_dirty"); !strings.Contains(got, "fake-password") {
		t.Fatal("dry run must not change the blob")
	}
}

func TestMaintenanceRedactScanApply(t *testing.T) {
	dir := seedMaintenanceDir(t)
	var out, errOut bytes.Buffer
	if err := maintenanceMain(context.Background(), &out, &errOut, []string{"redact-scan", "--data", dir, "--apply"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String()+errOut.String(), "fake-password") || !strings.Contains(out.String(), "1 blobs re-redacted") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
	db, _ := sqlstore.Open(dir)
	defer func() { _ = db.Close() }()
	if got, _, _ := db.GetSourceBlob(context.Background(), "src_dirty"); strings.Contains(got, "fake-password") {
		t.Fatalf("blob not re-redacted: %q", got)
	}
	rows, _ := db.LoadAllEntries(context.Background())
	if len(rows) != 1 || rows[0].Content != "access_token=Zx9-fake-password-1" {
		t.Fatalf("entry must stay intact: %+v", rows)
	}
}

func TestMaintenanceRejectsConflictingFlagsAndUnknownJobs(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := maintenanceMain(context.Background(), &out, &errOut, []string{"redact-scan", "--apply", "--dry-run"}); err == nil {
		t.Fatal("expected --apply with --dry-run to fail")
	}
	if err := maintenanceMain(context.Background(), &out, &errOut, []string{"nope"}); err == nil {
		t.Fatal("expected usage error")
	}
}
