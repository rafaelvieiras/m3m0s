package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSoftDeleteHidesEntryAndRestoreBringsItBack(t *testing.T) {
	s := newTestStore(t)
	e := mustAdd(t, s, AddInput{Content: "manual delete target", Domain: "code:trash-soft"})
	now := time.Now()

	found, err := s.SoftDelete(context.Background(), e.ID, now)
	if err != nil || !found {
		t.Fatalf("SoftDelete = (%v, %v), want (true, nil)", found, err)
	}
	if _, err := s.Get(e.ID, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after SoftDelete: want ErrNotFound, got %v", err)
	}
	if got := getIndexEntry(t, s, e.ID); got.Metadata[metadataKeyDeletedAt] == "" {
		t.Error("metadata.deleted_at not set")
	}
	if n := s.SoftDeletedCount(); n != 1 {
		t.Errorf("SoftDeletedCount = %d, want 1", n)
	}
	if total, _ := s.Stats(); total != 0 {
		t.Errorf("Stats count = %d, want 0 (trash is not counted)", total)
	}

	if _, err := s.Restore(context.Background(), e.ID, now); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if _, err := s.Get(e.ID, now); err != nil {
		t.Errorf("Get after Restore: %v", err)
	}
}

func TestSoftDeleteMissingOrAlreadyTrashedIsNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if found, err := s.SoftDelete(ctx, "mem_nope", time.Now()); err != nil || found {
		t.Fatalf("SoftDelete of unknown id = (%v, %v), want (false, nil)", found, err)
	}
	e := mustAdd(t, s, AddInput{Content: "twice", Domain: "code:trash-twice"})
	if found, _ := s.SoftDelete(ctx, e.ID, time.Now()); !found {
		t.Fatal("first SoftDelete should find the entry")
	}
	if found, err := s.SoftDelete(ctx, e.ID, time.Now()); err != nil || found {
		t.Fatalf("second SoftDelete = (%v, %v), want (false, nil)", found, err)
	}
}

func TestSoftDeletedEntryIsHardDeletedBySweepAfterGrace(t *testing.T) {
	s := newTestStore(t)
	e := mustAddExtracted(t, s, "manually deleted fact", "code:trash-sweep")
	deletedAt := time.Now().Add(-(SoftDeleteGraceDays + 1) * 24 * time.Hour)
	if found, err := s.SoftDelete(context.Background(), e.ID, deletedAt); err != nil || !found {
		t.Fatalf("SoftDelete: (%v, %v)", found, err)
	}
	if _, err := s.ForgetSweep(context.Background(), false, time.Now()); err != nil {
		t.Fatalf("ForgetSweep: %v", err)
	}
	s.mu.RLock()
	_, still := s.index[e.ID]
	s.mu.RUnlock()
	if still {
		t.Error("entry still in the index after the grace window")
	}
}

func TestTrashListsNewestFirstWithPurgeDate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	older := mustAdd(t, s, AddInput{Content: "older", Domain: "code:trash-list-a"})
	newer := mustAdd(t, s, AddInput{Content: strings.Repeat("long text ", 60), Domain: "code:trash-list-b"})
	live := mustAdd(t, s, AddInput{Content: "still live", Domain: "code:trash-list-c"})

	base := time.Now().UTC().Truncate(time.Second)
	if _, err := s.SoftDelete(ctx, older.ID, base.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SoftDelete(ctx, newer.ID, base); err != nil {
		t.Fatal(err)
	}

	items := s.Trash()
	if len(items) != 2 {
		t.Fatalf("Trash has %d items, want 2", len(items))
	}
	if items[0].ID != newer.ID || items[1].ID != older.ID {
		t.Errorf("order = %s, %s; want newest first", items[0].ID, items[1].ID)
	}
	for _, it := range items {
		if it.ID == live.ID {
			t.Error("live entry listed in the trash")
		}
	}
	if want := base.Add(SoftDeleteGraceDays * 24 * time.Hour); !items[0].PurgeAt.Equal(want) {
		t.Errorf("PurgeAt = %v, want %v", items[0].PurgeAt, want)
	}
	if n := len([]rune(items[0].Snippet)); n > trashSnippetRunes+1 {
		t.Errorf("snippet has %d runes, want at most %d", n, trashSnippetRunes+1)
	}
	if items[0].Kind == "" || items[0].Domain != "code:trash-list-b" {
		t.Errorf("kind/domain not filled: %+v", items[0])
	}
}

func TestPurgeTrashedOnlyTouchesTrashedEntries(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	e := mustAdd(t, s, AddInput{Content: "purge me", Domain: "code:trash-purge"})

	if found, err := s.PurgeTrashed(ctx, e.ID); err != nil || found {
		t.Fatalf("PurgeTrashed of a live entry = (%v, %v), want (false, nil)", found, err)
	}
	if _, err := s.Get(e.ID, time.Now()); err != nil {
		t.Fatalf("live entry must survive PurgeTrashed: %v", err)
	}

	if _, err := s.SoftDelete(ctx, e.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if found, err := s.PurgeTrashed(ctx, e.ID); err != nil || !found {
		t.Fatalf("PurgeTrashed = (%v, %v), want (true, nil)", found, err)
	}
	if _, err := s.Restore(ctx, e.ID, time.Now()); !errors.Is(err, ErrNotFound) {
		t.Errorf("Restore after purge: want ErrNotFound, got %v", err)
	}
	s.mu.RLock()
	_, still := s.index[e.ID]
	s.mu.RUnlock()
	if still {
		t.Error("purged entry still in the index")
	}
}

func TestEmptyTrashRemovesOnlyTrashedEntries(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a := mustAdd(t, s, AddInput{Content: "a", Domain: "code:trash-empty-a"})
	b := mustAdd(t, s, AddInput{Content: "b", Domain: "code:trash-empty-b"})
	live := mustAdd(t, s, AddInput{Content: "c", Domain: "code:trash-empty-c"})
	for _, id := range []string{a.ID, b.ID} {
		if _, err := s.SoftDelete(ctx, id, time.Now()); err != nil {
			t.Fatal(err)
		}
	}

	n, err := s.EmptyTrash(ctx)
	if err != nil || n != 2 {
		t.Fatalf("EmptyTrash = (%d, %v), want (2, nil)", n, err)
	}
	if len(s.Trash()) != 0 {
		t.Error("trash not empty")
	}
	if _, err := s.Get(live.ID, time.Now()); err != nil {
		t.Errorf("live entry removed by EmptyTrash: %v", err)
	}
}

func TestDeleteByFilePathStaysHardDelete(t *testing.T) {
	s := newTestStore(t)
	e := mustAdd(t, s, AddInput{
		Content:  "chunk of a watched file",
		Domain:   "code:trash-watch",
		Metadata: map[string]string{"file_path": "docs/a.md"},
	})
	n, err := s.DeleteByFilePath(context.Background(), "code:trash-watch", "docs/a.md")
	if err != nil || n != 1 {
		t.Fatalf("DeleteByFilePath = (%d, %v), want (1, nil)", n, err)
	}
	if len(s.Trash()) != 0 {
		t.Error("watch cascade must not leave anything in the trash")
	}
	if _, err := s.Restore(context.Background(), e.ID, time.Now()); !errors.Is(err, ErrNotFound) {
		t.Errorf("Restore after DeleteByFilePath: want ErrNotFound, got %v", err)
	}
}

func TestManualDeleteIsNotSparedByPerKindSweep(t *testing.T) {
	s := newTestStore(t)
	longAgo := time.Now().Add(-(SoftDeleteGraceDays + 5) * 24 * time.Hour)
	e := mustAdd(t, s, AddInput{Content: "durable rule deleted by hand", Domain: "code:trash-perkind"})
	if found, err := s.SoftDelete(context.Background(), e.ID, longAgo); err != nil || !found {
		t.Fatalf("SoftDelete: (%v, %v)", found, err)
	}
	res, err := s.ForgetSweepModel(context.Background(), true, time.Now(), RetentionPerKind)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.HardDeleteCandidates) != 1 || res.HardDeleteCandidates[0] != e.ID {
		t.Errorf("hard candidates = %v, want the manually deleted entry", res.HardDeleteCandidates)
	}

	// Restoring drops the marker, so a later sweep soft-delete is not mistaken for a manual one.
	if _, err := s.Restore(context.Background(), e.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := getIndexEntry(t, s, e.ID); got.Metadata[metadataKeyDeletedBy] != "" {
		t.Error("deleted_by still present after Restore")
	}
}
