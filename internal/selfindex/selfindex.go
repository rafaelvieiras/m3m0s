// Package selfindex runs the self-indexing of m3m0s's own embedded
// documentation (selfdocs) at `m3m0s serve` startup — it works on any
// deploy (standalone Docker, any clone) for what previously (D24) only
// worked with an external `m3m0s watch` running on the development host.
// See D25, docs/decisions/.
//
// Unlike internal/watch (D3: watch is always an HTTP client of the core,
// never touching storage directly), here we are inside the `m3m0s serve`
// process itself — it calls store.MemoryStore.Add/DeleteByFilePath
// directly, with no HTTP round-trip.
package selfindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	selfdocs "github.com/rafaelvieiras/m3m0s"
	"github.com/rafaelvieiras/m3m0s/internal/chunker"
	"github.com/rafaelvieiras/m3m0s/internal/store"
)

// Domain is fixed and reserved: m3m0s's own knowledge about itself — the
// same domain already used by `m3m0s watch` on the dev host (D24), on
// purpose: if both mechanisms run at the same time (e.g. the maintainer
// still has the D24 systemd unit enabled), they compete for the same
// file_path/domain consistently instead of duplicating content under
// different domains.
//
// Contract string kept in Portuguese ("codigo" = "code"): it is the
// category of the domain already stored in existing databases and used by
// `m3m0s watch`; changing it would orphan previously indexed chunks.
const Domain = "codigo:m3m0s"

// source distinguishes provenance: this came from the documentation
// embedded in the binary, not from user content (unlike "watch",
// "webui-chat", "claude-code").
const source = "selfdocs"

const chunkIDPrefix = "chunk"

const stateFileName = "selfdocs-state.json"

// fileState is the hash of a file's last indexed content — it allows
// skipping reindexing (and not spending extraction tokens again) when the
// embedded content has not changed since the previous boot.
type fileState struct {
	Hash string `json:"hash"`
}

// Result summarizes what Run did, for the boot log.
type Result struct {
	FilesTotal     int
	FilesIndexed   int // hash changed (or first time) and was reindexed successfully
	FilesUnchanged int // hash equal to the already indexed one, skipped
	FilesFailed    int // 1+ chunk failed, hash did not advance (retry on next boot)
	ChunksFailed   int
}

func statePath(dataDir string) string {
	return filepath.Join(dataDir, stateFileName)
}

func loadState(path string) (map[string]fileState, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]fileState{}, nil
	}
	if err != nil {
		return nil, err
	}
	var st map[string]fileState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return st, nil
}

// saveState persists with an atomic tmp+rename write — same pattern
// already used by internal/watch/state.go, internal/store, internal/auth.
func saveState(path string, st map[string]fileState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func hashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// Run indexes every file embedded in selfdocs.Files() whose hash changed
// since the previous boot (state persisted in <dataDir>/selfdocs-state.json).
// It is a thin shell over RunFiles — it only resolves selfdocs.Files()
// (the real docs embedded in the binary) and delegates the logic.
func Run(ctx context.Context, s *store.MemoryStore, dataDir string) (Result, error) {
	files, err := selfdocs.Files()
	if err != nil {
		return Result{}, fmt.Errorf("read embedded docs: %w", err)
	}
	return RunFiles(ctx, s, dataDir, files)
}

// RunFiles is Run's engine, receiving the list of files explicitly
// (instead of reading selfdocs.Files() directly) — it exists to be
// testable with fake content, since the real docs embedded in the binary
// cannot be altered in tests. It indexes every file whose hash changed
// since the last time (state persisted in <dataDir>/selfdocs-state.json).
// Idempotent across calls. It isolates failures per chunk/file (same
// rationale as D24 in internal/watch): a chunk that fails (timeout,
// provider error) is logged and skipped, without taking down the rest of
// the file or the other files — only that specific file's hash does not
// advance, so the next call tries again. An error is returned only if the
// local state cannot be read/written (structural failure, not a content
// one).
func RunFiles(ctx context.Context, s *store.MemoryStore, dataDir string, files []selfdocs.File) (Result, error) {
	path := statePath(dataDir)
	st, err := loadState(path)
	if err != nil {
		return Result{}, fmt.Errorf("load state from %s: %w", path, err)
	}

	result := Result{FilesTotal: len(files)}

	for _, f := range files {
		hash := hashContent(f.Content)
		if existing, ok := st[f.Path]; ok && existing.Hash == hash {
			result.FilesUnchanged++
			continue
		}

		if _, err := s.DeleteByFilePath(ctx, Domain, f.Path); err != nil {
			log.Printf("selfdocs: error deleting old chunks of %s: %v", f.Path, err)
		}

		chunks := chunker.Split(f.Content)
		failed := false
		for _, chunk := range chunks {
			metadata := map[string]string{"file_path": f.Path}
			if chunk.Heading != "" {
				metadata["heading"] = chunk.Heading
			}
			_, err := s.Add(ctx, store.AddInput{
				Content:  chunk.Content,
				Domain:   Domain,
				Source:   source,
				Metadata: metadata,
				IDPrefix: chunkIDPrefix,
			}, time.Now())
			if err != nil {
				log.Printf("selfdocs: error indexing chunk of %s: %v", f.Path, err)
				failed = true
				result.ChunksFailed++
				continue
			}
		}

		if failed {
			result.FilesFailed++
			continue // hash does not advance — the next Run retries the whole file
		}
		st[f.Path] = fileState{Hash: hash}
		result.FilesIndexed++
	}

	if err := saveState(path, st); err != nil {
		return result, fmt.Errorf("save state to %s: %w", path, err)
	}

	return result, nil
}
