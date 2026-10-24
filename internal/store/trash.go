// Manual delete with a trash (D53, builds on D22): the delete a person asks
// for is a soft-delete, so it can be undone until the forget-sweep hard-deletes
// it after SoftDeleteGraceDays.
package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

const (
	// metadataKeyDeletedBy marks a soft delete a person asked for. The
	// per-kind retention model (D39) spares from hard-delete the entries it
	// would not have soft-deleted itself; a manual delete is not one of them,
	// or it would sit in the trash forever.
	metadataKeyDeletedBy = "deleted_by"
	deletedByUserValue   = "user"
)

func deletedByUser(e schema.Entry) bool {
	return e.Metadata[metadataKeyDeletedBy] == deletedByUserValue
}

// SoftDelete is the manual delete: it marks the entry as soft-deleted (the
// same metadata.deleted_at the forget-sweep writes), so Restore can revert it
// until the sweep hard-deletes it after SoftDeleteGraceDays (D22). It returns
// (false, nil) when the id does not exist or is already in the trash: in both
// cases there is nothing live to delete. updatedAt is left untouched, as in
// the sweep's soft-delete.
func (s *MemoryStore) SoftDelete(ctx context.Context, id string, now time.Time) (bool, error) {
	s.mu.RLock()
	e, ok := s.index[id]
	s.mu.RUnlock()
	if !ok || isSoftDeleted(e) {
		return false, nil
	}
	md := make(map[string]string, len(e.Metadata)+1)
	for k, v := range e.Metadata {
		md[k] = v
	}
	md[metadataKeyDeletedBy] = deletedByUserValue
	e.Metadata = md
	if err := s.softDeleteOne(ctx, e, now); err != nil {
		return false, fmt.Errorf("soft-delete entry: %w", err)
	}
	return true, nil
}

// TrashItem is one soft-deleted entry as listed by Trash.
type TrashItem struct {
	ID      string
	Snippet string
	Kind    string
	Domain  string
	// DeletedAt is zero when metadata.deleted_at cannot be parsed.
	DeletedAt time.Time
	// PurgeAt is when the forget-sweep becomes allowed to hard-delete the
	// entry (DeletedAt plus SoftDeleteGraceDays). The sweep runs on an
	// interval, so the real removal can happen a little later.
	PurgeAt time.Time
}

const trashSnippetRunes = 200

// Trash lists the soft-deleted entries, most recently deleted first. An entry
// whose deleted_at cannot be parsed is listed with zero dates so it can still
// be restored or purged by hand.
func (s *MemoryStore) Trash() []TrashItem {
	s.mu.RLock()
	items := make([]TrashItem, 0)
	for _, e := range s.index {
		if !isSoftDeleted(e) {
			continue
		}
		item := TrashItem{ID: e.ID, Snippet: trashSnippet(e.Content), Kind: e.KindValue(), Domain: e.Domain}
		if t, err := time.Parse(time.RFC3339, e.Metadata[metadataKeyDeletedAt]); err == nil {
			item.DeletedAt = t
			item.PurgeAt = t.Add(SoftDeleteGraceDays * 24 * time.Hour)
		}
		items = append(items, item)
	}
	s.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool {
		if !items[i].DeletedAt.Equal(items[j].DeletedAt) {
			return items[i].DeletedAt.After(items[j].DeletedAt)
		}
		return items[i].ID < items[j].ID
	})
	return items
}

func trashSnippet(content string) string {
	content = strings.Join(strings.Fields(content), " ")
	r := []rune(content)
	if len(r) <= trashSnippetRunes {
		return content
	}
	return string(r[:trashSnippetRunes]) + "…"
}

// PurgeTrashed hard-deletes one entry, but only if it is in the trash, so a
// "delete forever" from a stale screen can never remove a memory that was
// restored in the meantime. (false, nil) means nothing in the trash has that
// id.
func (s *MemoryStore) PurgeTrashed(ctx context.Context, id string) (bool, error) {
	s.mu.RLock()
	e, ok := s.index[id]
	s.mu.RUnlock()
	if !ok || !isSoftDeleted(e) {
		return false, nil
	}
	return s.Delete(ctx, id)
}

// EmptyTrash hard-deletes every soft-deleted entry and returns how many were
// removed. A failure on one entry does not stop the others; the first error
// is returned at the end.
func (s *MemoryStore) EmptyTrash(ctx context.Context) (int, error) {
	n := 0
	var firstErr error
	for _, item := range s.Trash() {
		ok, err := s.PurgeTrashed(ctx, item.ID)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if ok {
			n++
		}
	}
	return n, firstErr
}
