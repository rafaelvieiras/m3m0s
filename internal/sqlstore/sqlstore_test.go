package sqlstore

import (
	"context"
	"testing"
	"time"
)

func TestOpenRunsMigrations(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
}

func TestUpsertEntryRoundTrip(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	agent := "test-agent"

	in := EntryRow{
		ID: "mem_test1", Content: "hello world", Domain: "code:test", DomainCategory: "code",
		Source: "manual", Agent: &agent, Tags: []string{"a", "b"}, Metadata: map[string]string{"k": "v"},
		CreatedAt: now, UpdatedAt: now, Embedding: []float32{0.1, 0.2, 0.3},
	}
	if err := db.UpsertEntry(ctx, in); err != nil {
		t.Fatalf("UpsertEntry: %v", err)
	}

	all, err := db.LoadAllEntries(ctx)
	if err != nil {
		t.Fatalf("LoadAllEntries: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("len(all) = %d, want 1", len(all))
	}
	got := all[0]
	if got.ID != in.ID || got.Content != in.Content || got.Domain != in.Domain || got.Source != in.Source {
		t.Errorf("basic round trip diverged: %+v", got)
	}
	if got.Agent == nil || *got.Agent != agent {
		t.Errorf("Agent = %v, want %q", got.Agent, agent)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "a" || got.Tags[1] != "b" {
		t.Errorf("Tags = %v", got.Tags)
	}
	if got.Metadata["k"] != "v" {
		t.Errorf("Metadata = %v", got.Metadata)
	}
	if !got.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, now)
	}
	if got.Embedding != nil {
		t.Errorf("LoadAllEntries should not return an embedding, got %v", got.Embedding)
	}

	embs, err := db.EmbeddingsByIDs(ctx, []string{"mem_test1", "does-not-exist"})
	if err != nil {
		t.Fatalf("EmbeddingsByIDs: %v", err)
	}
	if len(embs) != 1 {
		t.Fatalf("len(embs) = %d, want 1", len(embs))
	}
	vec := embs["mem_test1"]
	if len(vec) != 3 || vec[0] != 0.1 || vec[1] != 0.2 || vec[2] != 0.3 {
		t.Errorf("embedding = %v", vec)
	}
}

func TestUpsertEntryOverwritesExisting(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	base := EntryRow{
		ID: "mem_test1", Content: "v1", Domain: "code:test", DomainCategory: "code",
		Source: "manual", Tags: []string{}, Metadata: map[string]string{},
		CreatedAt: now, UpdatedAt: now, Embedding: []float32{1, 0},
	}
	if err := db.UpsertEntry(ctx, base); err != nil {
		t.Fatalf("UpsertEntry 1: %v", err)
	}
	base.Content = "v2"
	base.Embedding = []float32{0, 1}
	if err := db.UpsertEntry(ctx, base); err != nil {
		t.Fatalf("UpsertEntry 2: %v", err)
	}

	all, err := db.LoadAllEntries(ctx)
	if err != nil {
		t.Fatalf("LoadAllEntries: %v", err)
	}
	if len(all) != 1 || all[0].Content != "v2" {
		t.Fatalf("expected 1 entry with content v2, got %+v", all)
	}
	embs, err := db.EmbeddingsByIDs(ctx, []string{"mem_test1"})
	if err != nil {
		t.Fatalf("EmbeddingsByIDs: %v", err)
	}
	if vec := embs["mem_test1"]; len(vec) != 2 || vec[0] != 0 || vec[1] != 1 {
		t.Errorf("embedding was not updated: %v", vec)
	}
}

func TestUpdateMetadataKeepsEmbedding(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	if err := db.UpsertEntry(ctx, EntryRow{
		ID: "mem_test1", Content: "v1", Domain: "code:test", DomainCategory: "code",
		Source: "manual", Tags: []string{}, Metadata: map[string]string{},
		CreatedAt: now, UpdatedAt: now, Embedding: []float32{1, 0},
	}); err != nil {
		t.Fatalf("UpsertEntry: %v", err)
	}

	later := now.Add(time.Hour)
	if err := db.UpdateMetadata(ctx, "mem_test1", map[string]string{"access_count": "1"}, later); err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}

	all, err := db.LoadAllEntries(ctx)
	if err != nil {
		t.Fatalf("LoadAllEntries: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("len(all) = %d", len(all))
	}
	if all[0].Metadata["access_count"] != "1" {
		t.Errorf("metadata was not updated: %v", all[0].Metadata)
	}
	if all[0].Content != "v1" {
		t.Errorf("content changed unexpectedly: %v", all[0].Content)
	}
	embs, err := db.EmbeddingsByIDs(ctx, []string{"mem_test1"})
	if err != nil {
		t.Fatalf("EmbeddingsByIDs: %v", err)
	}
	if vec := embs["mem_test1"]; len(vec) != 2 || vec[0] != 1 || vec[1] != 0 {
		t.Errorf("embedding should not change: %v", vec)
	}
}

func TestDeleteEntry(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	now := time.Now()
	if err := db.UpsertEntry(ctx, EntryRow{ID: "mem_1", Content: "c", Domain: "code:test", DomainCategory: "code", Source: "manual", Tags: []string{}, Metadata: map[string]string{}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("UpsertEntry: %v", err)
	}
	if err := db.DeleteEntry(ctx, "mem_1"); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	all, err := db.LoadAllEntries(ctx)
	if err != nil {
		t.Fatalf("LoadAllEntries: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("expected 0 entries after delete, got %d", len(all))
	}
	// deleting again is not an error
	if err := db.DeleteEntry(ctx, "mem_1"); err != nil {
		t.Fatalf("DeleteEntry (2nd time): %v", err)
	}
}

func TestDeleteEntries(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	now := time.Now()
	for _, id := range []string{"mem_1", "mem_2", "mem_3"} {
		if err := db.UpsertEntry(ctx, EntryRow{ID: id, Content: "c", Domain: "code:test", DomainCategory: "code", Source: "manual", Tags: []string{}, Metadata: map[string]string{}, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("UpsertEntry %s: %v", id, err)
		}
	}
	if err := db.DeleteEntries(ctx, []string{"mem_1", "mem_3"}); err != nil {
		t.Fatalf("DeleteEntries: %v", err)
	}
	all, err := db.LoadAllEntries(ctx)
	if err != nil {
		t.Fatalf("LoadAllEntries: %v", err)
	}
	if len(all) != 1 || all[0].ID != "mem_2" {
		t.Fatalf("expected only mem_2 left, got %+v", all)
	}
}

func TestSourceBlobs(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	if err := db.PutSourceBlob(ctx, "hash1", "original text"); err != nil {
		t.Fatalf("PutSourceBlob: %v", err)
	}
	// idempotent
	if err := db.PutSourceBlob(ctx, "hash1", "different text, ignored"); err != nil {
		t.Fatalf("PutSourceBlob (2nd time): %v", err)
	}
	got, ok, err := db.GetSourceBlob(ctx, "hash1")
	if err != nil {
		t.Fatalf("GetSourceBlob: %v", err)
	}
	if !ok || got != "original text" {
		t.Errorf("got=%q ok=%v, want %q true", got, ok, "original text")
	}

	_, ok, err = db.GetSourceBlob(ctx, "does-not-exist")
	if err != nil {
		t.Fatalf("GetSourceBlob (miss): %v", err)
	}
	if ok {
		t.Error("expected ok=false for nonexistent hash")
	}
}

func TestAPIKeys(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	if err := db.InsertAPIKey(ctx, APIKeyRow{Prefix: "m3m0s_ab", Hash: "h1", Label: "test", CreatedAt: now}); err != nil {
		t.Fatalf("InsertAPIKey: %v", err)
	}
	keys, err := db.ListAPIKeys(ctx)
	if err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	if len(keys) != 1 || keys[0].Hash != "h1" || keys[0].Label != "test" {
		t.Fatalf("keys = %+v", keys)
	}
}

func TestJobIntervals(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	empty, err := db.LoadJobIntervals(ctx)
	if err != nil {
		t.Fatalf("LoadJobIntervals: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty on first boot, got %v", empty)
	}

	if err := db.SaveJobInterval(ctx, "forget-sweep", 3600); err != nil {
		t.Fatalf("SaveJobInterval: %v", err)
	}
	if err := db.SaveJobInterval(ctx, "forget-sweep", 7200); err != nil {
		t.Fatalf("SaveJobInterval (overwrite): %v", err)
	}
	got, err := db.LoadJobIntervals(ctx)
	if err != nil {
		t.Fatalf("LoadJobIntervals: %v", err)
	}
	if got["forget-sweep"] != 7200 {
		t.Errorf("forget-sweep = %d, want 7200", got["forget-sweep"])
	}
}

func TestTelemetry(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	v, err := db.GetTelemetryInt64(ctx, "tokens_saved_by_compaction")
	if err != nil {
		t.Fatalf("GetTelemetryInt64: %v", err)
	}
	if v != 0 {
		t.Errorf("v = %d, want 0 on first boot", v)
	}

	if err := db.SetTelemetryInt64(ctx, "tokens_saved_by_compaction", 42); err != nil {
		t.Fatalf("SetTelemetryInt64: %v", err)
	}
	if err := db.SetTelemetryInt64(ctx, "tokens_saved_by_compaction", 100); err != nil {
		t.Fatalf("SetTelemetryInt64 (overwrite): %v", err)
	}
	v, err = db.GetTelemetryInt64(ctx, "tokens_saved_by_compaction")
	if err != nil {
		t.Fatalf("GetTelemetryInt64: %v", err)
	}
	if v != 100 {
		t.Errorf("v = %d, want 100", v)
	}
}
