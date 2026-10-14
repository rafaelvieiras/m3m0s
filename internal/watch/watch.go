// Package watch is the engine of `m3m0s watch <dir>` (Phase 2, D11): it
// watches a project root via fsnotify, chunks files matching the allowlist
// by Markdown header (internal/chunker) and keeps that in sync on
// `m3m0s serve` via internal/client — it never touches storage directly (D3:
// watch is always an HTTP client of the core).
package watch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/chunker"
	"github.com/rafaelvieiras/m3m0s/internal/client"
)

// Config is the resolved configuration of a watched root — resolving
// defaults (automatic domain, default allow/deny) is the responsibility of
// cmd/m3m0s, not of this package.
type Config struct {
	Root      string   // absolute watched directory
	Domain    string   // already resolved: code:<basename> or explicit --domain
	Allow     []string // globs relative to Root
	Deny      []string // globs relative to Root — always wins over Allow
	StatePath string   // path of the local state file
}

// chunkIDPrefix is the ID prefix watch requests on every Add — it makes it
// clear, just by looking at the ID, that the memory came from a reindexed
// file chunk (D11), not from a direct `add`.
const chunkIDPrefix = "chunk"

const watchSource = "watch"

func hashContent(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// ReconcileFile reads relPath (relative to cfg.Root) and, if the content
// changed since last time (hash diff), reindexes: it deletes the old chunks
// (if any) and inserts the new ones, chunked by Markdown header.
func ReconcileFile(ctx context.Context, c *client.Client, cfg Config, state *State, relPath string) error {
	absPath := filepath.Join(cfg.Root, filepath.FromSlash(relPath))
	content, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", relPath, err)
	}
	hash := hashContent(content)

	if existing, ok := state.Get(relPath); ok {
		if existing.Hash == hash {
			return nil // hash diff: content unchanged, nothing to do
		}
		if len(existing.ChunkIDs) > 0 {
			if _, err := c.DeleteByFilePath(ctx, cfg.Domain, relPath); err != nil {
				return fmt.Errorf("delete old chunks of %s: %w", relPath, err)
			}
		}
	}

	chunks := chunker.Split(string(content))
	var ids []string
	failed := false
	for _, chunk := range chunks {
		metadata := map[string]string{"file_path": relPath}
		if chunk.Heading != "" {
			metadata["heading"] = chunk.Heading
		}
		resp, err := c.Add(ctx, apitypes.AddRequest{
			Content:  chunk.Content,
			Domain:   cfg.Domain,
			Source:   watchSource,
			Metadata: metadata,
			IDPrefix: chunkIDPrefix,
		})
		if err != nil {
			// D24: a chunk that fails (timeout, provider down) does not
			// take down the rest of the file nor the whole watch process —
			// same per-chunk failure isolation principle as D18, one
			// layer up. Since `failed` is set, the file's hash is not
			// saved (below) and the next scan retries the whole file.
			log.Printf("m3m0s watch: error indexing chunk of %s: %v", relPath, err)
			failed = true
			continue
		}
		// Fact extraction (Phase 4, D12) can turn 1 chunk into N entries —
		// all IDs go into the same FileState.ChunkIDs, so the cascading
		// purge keeps deleting everything that came from this file.
		for _, e := range resp.Entries {
			ids = append(ids, e.Entry.ID)
		}
	}
	if failed {
		return fmt.Errorf("index %s: 1 or more chunks failed, hash not advanced (retry on the next scan)", relPath)
	}

	return state.Set(relPath, FileState{Hash: hash, ChunkIDs: ids})
}

// ReconcileDelete deletes all chunks derived from relPath (file removed, or
// no longer matching the allowlist) and clears the local state — cascading
// purge (D11).
func ReconcileDelete(ctx context.Context, c *client.Client, cfg Config, state *State, relPath string) error {
	if _, err := c.DeleteByFilePath(ctx, cfg.Domain, relPath); err != nil {
		return fmt.Errorf("purge %s: %w", relPath, err)
	}
	return state.Delete(relPath)
}

// Sync walks the whole tree from cfg.Root, reconciles every file that
// matches the allowlist (and not the denylist) and purges from the index any
// file the local state still remembers but that vanished from the tree
// (deleted while watch was not running).
func Sync(ctx context.Context, c *client.Client, cfg Config, state *State) error {
	seen := make(map[string]bool)

	err := filepath.WalkDir(cfg.Root, func(fpath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, relErr := filepath.Rel(cfg.Root, fpath)
		if relErr != nil {
			return relErr
		}
		relPath = filepath.ToSlash(relPath)
		if relPath == "." {
			return nil
		}
		if d.IsDir() {
			if matchAny(cfg.Deny, relPath) {
				return filepath.SkipDir
			}
			return nil
		}
		if !shouldIndex(cfg.Allow, cfg.Deny, relPath) {
			return nil
		}
		seen[relPath] = true
		// D24: an error reconciling ONE file (already logged inside
		// ReconcileFile) must not abort the whole scan — other files
		// must keep being indexed.
		if err := ReconcileFile(ctx, c, cfg, state, relPath); err != nil {
			log.Printf("m3m0s watch: %v", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("scan %s: %w", cfg.Root, err)
	}

	for _, relPath := range state.Paths() {
		if !seen[relPath] {
			if err := ReconcileDelete(ctx, c, cfg, state, relPath); err != nil {
				log.Printf("m3m0s watch: error purging %s: %v", relPath, err)
			}
		}
	}
	return nil
}

// Run does the initial Sync and then enters the fsnotify event loop until
// ctx is canceled. fsnotify does not watch subdirectories recursively on its
// own — each directory must be added explicitly to the watcher, and new
// directories (Create event) must be added right away.
func Run(ctx context.Context, c *client.Client, cfg Config, state *State) error {
	if err := Sync(ctx, c, cfg, state); err != nil {
		return fmt.Errorf("initial sync: %w", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create fsnotify watcher: %w", err)
	}
	defer func() { _ = watcher.Close() }()

	if err := addDirsRecursive(watcher, cfg, cfg.Root); err != nil {
		return fmt.Errorf("add directories to watcher: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if err := handleEvent(ctx, c, cfg, state, watcher, event); err != nil {
				log.Printf("m3m0s watch: error processing %s: %v", event.Name, err)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			log.Printf("m3m0s watch: fsnotify error: %v", err)
		}
	}
}

// addDirsRecursive adds root and every subdirectory that does not match the
// denylist to the watcher.
func addDirsRecursive(watcher *fsnotify.Watcher, cfg Config, root string) error {
	return filepath.WalkDir(root, func(fpath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		relPath, relErr := filepath.Rel(cfg.Root, fpath)
		if relErr != nil {
			return relErr
		}
		relPath = filepath.ToSlash(relPath)
		if relPath != "." && matchAny(cfg.Deny, relPath) {
			return filepath.SkipDir
		}
		return watcher.Add(fpath)
	})
}

func handleEvent(ctx context.Context, c *client.Client, cfg Config, state *State, watcher *fsnotify.Watcher, event fsnotify.Event) error {
	relPath, relErr := filepath.Rel(cfg.Root, event.Name)
	if relErr != nil {
		return relErr
	}
	relPath = filepath.ToSlash(relPath)

	switch {
	case event.Has(fsnotify.Remove), event.Has(fsnotify.Rename):
		if _, tracked := state.Get(relPath); tracked {
			return ReconcileDelete(ctx, c, cfg, state, relPath)
		}
		return nil

	case event.Has(fsnotify.Create):
		info, err := os.Stat(event.Name)
		if err != nil {
			// The file may have vanished between the event and the Stat (e.g.
			// an editor that creates and renames quickly) — not a real
			// watch error.
			return nil
		}
		if info.IsDir() {
			return addDirsRecursive(watcher, cfg, event.Name)
		}
		if !shouldIndex(cfg.Allow, cfg.Deny, relPath) {
			return nil
		}
		return ReconcileFile(ctx, c, cfg, state, relPath)

	case event.Has(fsnotify.Write):
		if !shouldIndex(cfg.Allow, cfg.Deny, relPath) {
			return nil
		}
		return ReconcileFile(ctx, c, cfg, state, relPath)
	}
	return nil
}
