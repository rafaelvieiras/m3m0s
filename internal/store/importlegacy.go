// One-time import of the 6 hand-rolled JSON files + chromem-go (the
// pre-D28 architecture, docs/decisions/) into SQLite — it generalizes the
// effect internal/store/migrate.go (D21) had, without scanning the index
// on every boot forever: it only runs when the entries table is empty
// (ImportLegacyData is called by cmd/m3m0s/serve.go right after store.New,
// before anything else touches the database).
//
// The old files (index.json/sourceblobs.json/apikeys.json/
// jobs-config.json/telemetry.json and chromem-go's internal directory) are
// NOT deleted by this import — they stay as harmless dead weight until
// manual confirmation in production that SQLite is the source of truth.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	chromem "github.com/philippgille/chromem-go"

	"github.com/rafaelvieiras/m3m0s/internal/schema"
	"github.com/rafaelvieiras/m3m0s/internal/sqlstore"
)

const legacyCollectionName = "memories"

// ImportLegacyData checks whether the entries table already has any row —
// if so, it is a no-op (it already ran before, or this is a fresh
// install with no legacy data at all). Otherwise, it imports whatever it
// finds in dataDir: a failure reading any individual file (should not
// happen outside corrupted data) is logged and skipped, never aborting
// boot — same best-effort spirit as migrate.go (D21), which this import
// replaces.
func (s *MemoryStore) ImportLegacyData(ctx context.Context) error {
	count, err := s.db.CountEntries(ctx)
	if err != nil {
		return fmt.Errorf("check whether entries already exist: %w", err)
	}
	if count > 0 {
		return nil
	}

	entries, err := readLegacyIndex(filepath.Join(s.dataDir, "index.json"))
	if err != nil {
		log.Printf("m3m0s: legacy import: index.json: %v", err)
	}
	if len(entries) == 0 {
		return nil // nothing to import (fresh install)
	}

	blobs, err := readLegacyBlobs(filepath.Join(s.dataDir, "sourceblobs.json"))
	if err != nil {
		log.Printf("m3m0s: legacy import: sourceblobs.json: %v", err)
	}
	for hash, text := range blobs {
		if err := s.db.PutSourceBlob(ctx, hash, text); err != nil {
			log.Printf("m3m0s: legacy import: source blob %s: %v", hash, err)
		}
	}

	legacyDB, legacyErr := chromem.NewPersistentDB(s.dataDir, false)
	var legacyCollection *chromem.Collection
	if legacyErr != nil {
		log.Printf("m3m0s: legacy import: open chromem-go: %v", legacyErr)
	} else {
		legacyCollection, legacyErr = legacyDB.GetOrCreateCollection(legacyCollectionName, nil, nil)
		if legacyErr != nil {
			log.Printf("m3m0s: legacy import: chromem-go collection: %v", legacyErr)
		}
	}

	imported := 0
	for _, e := range entries {
		// Accepts both forms of metadata.source_content (D28: it covers both
		// installs that already ran D21 — only source_content_ref — and an
		// install that never got to run D21 — raw source_content).
		if text, has := e.Metadata[metadataKeySourceContent]; has {
			hash := hashSourceContent(text)
			if err := s.db.PutSourceBlob(ctx, hash, text); err != nil {
				log.Printf("m3m0s: legacy import: source blob of %s: %v", e.ID, err)
			}
			md := make(map[string]string, len(e.Metadata))
			for k, v := range e.Metadata {
				if k == metadataKeySourceContent {
					continue
				}
				md[k] = v
			}
			md[metadataKeySourceContentRef] = hash
			e.Metadata = md
		}

		var embedding []float32
		if legacyCollection != nil {
			if doc, err := legacyCollection.GetByID(ctx, e.ID); err == nil {
				embedding = doc.Embedding
			}
		}
		if len(embedding) == 0 {
			// No legacy chromem-go available (partial install, or its persistence
			// directory never existed) — recompute the embedding instead of
			// discarding the entry. It costs 1 extra API call per orphan entry, but
			// that only happens in this rare case; never silently losing data is more
			// important than saving that call in an import that runs only once.
			vec, err := s.embedFunc(ctx, truncateForEmbedding(e.Content))
			if err != nil {
				log.Printf("m3m0s: legacy import: %s has no legacy embedding and recomputing failed, skipping: %v", e.ID, err)
				continue
			}
			embedding = vec
		}

		if err := s.db.UpsertEntry(ctx, entryToRow(e, embedding)); err != nil {
			log.Printf("m3m0s: legacy import: entry %s: %v", e.ID, err)
			continue
		}
		imported++
	}
	log.Printf("m3m0s: legacy import: %d/%d entries imported from chromem-go+index.json into SQLite", imported, len(entries))

	if err := s.importLegacyAPIKeys(ctx); err != nil {
		log.Printf("m3m0s: legacy import: apikeys.json: %v", err)
	}
	if err := s.importLegacyJobIntervals(ctx); err != nil {
		log.Printf("m3m0s: legacy import: jobs-config.json: %v", err)
	}
	if err := s.importLegacyTelemetry(ctx); err != nil {
		log.Printf("m3m0s: legacy import: telemetry.json: %v", err)
	}

	return s.loadIndex() // reloads the in-memory s.index with what was just imported
}

func readLegacyIndex(path string) ([]schema.Entry, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []schema.Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func readLegacyBlobs(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var blobs map[string]string
	if err := json.Unmarshal(data, &blobs); err != nil {
		return nil, err
	}
	return blobs, nil
}

type legacyAPIKey struct {
	Prefix    string    `json:"prefix"`
	Hash      string    `json:"hash"`
	Label     string    `json:"label,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

func (s *MemoryStore) importLegacyAPIKeys(ctx context.Context) error {
	data, err := os.ReadFile(filepath.Join(s.dataDir, "apikeys.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var keys []legacyAPIKey
	if err := json.Unmarshal(data, &keys); err != nil {
		return err
	}
	for _, k := range keys {
		if err := s.db.InsertAPIKey(ctx, sqlstore.APIKeyRow{Prefix: k.Prefix, Hash: k.Hash, Label: k.Label, CreatedAt: k.CreatedAt}); err != nil {
			log.Printf("m3m0s: legacy import: api key %s: %v", k.Prefix, err)
		}
	}
	return nil
}

func (s *MemoryStore) importLegacyJobIntervals(ctx context.Context) error {
	data, err := os.ReadFile(filepath.Join(s.dataDir, "jobs-config.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var intervals map[string]int64
	if err := json.Unmarshal(data, &intervals); err != nil {
		return err
	}
	for name, seconds := range intervals {
		if err := s.db.SaveJobInterval(ctx, name, seconds); err != nil {
			log.Printf("m3m0s: legacy import: interval of job %s: %v", name, err)
		}
	}
	return nil
}

func (s *MemoryStore) importLegacyTelemetry(ctx context.Context) error {
	data, err := os.ReadFile(filepath.Join(s.dataDir, "telemetry.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var t struct {
		TokensSavedByCompaction int64 `json:"tokensSavedByCompaction"`
	}
	if err := json.Unmarshal(data, &t); err != nil {
		return err
	}
	return s.db.SetTelemetryInt64(ctx, telemetryKeyTokensSaved, t.TokensSavedByCompaction)
}
