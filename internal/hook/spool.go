package hook

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/client"
)

const (
	// maxSpoolFiles caps the spool so a long outage cannot fill the disk;
	// beyond it the oldest payloads are dropped (and reported).
	maxSpoolFiles = 500
	// spoolRetention drops payloads that stayed unsent for too long: stale
	// session digests are not worth extracting weeks later.
	spoolRetention = 30 * 24 * time.Hour
	// maxSpoolAttempts moves a payload the server keeps rejecting (not an
	// outage: the server answered) to the dead-letter directory.
	maxSpoolAttempts = 5
)

// SpoolItem is one pending POST /api/v1/memories saved on disk because the
// server did not accept it.
type SpoolItem struct {
	CreatedAt time.Time           `json:"created_at"`
	Attempts  int                 `json:"attempts"`
	LastError string              `json:"last_error,omitempty"`
	Request   apitypes.AddRequest `json:"request"`
}

// SpoolDir is ~/.m3m0s/spool (payloads) — failed payloads wait here.
func SpoolDir(homeDir string) string { return filepath.Join(homeDir, ".m3m0s", "spool") }

func deadDir(homeDir string) string { return filepath.Join(SpoolDir(homeDir), "dead") }

// spoolName is <unix-nanos>-<hash>.json. The hash covers domain+content, so
// spooling the same payload twice is a no-op (idempotent enqueue) and the
// order of names is the order of capture.
func spoolName(req apitypes.AddRequest, now time.Time) string {
	sum := sha256.Sum256([]byte(req.Domain + "\x00" + req.Content))
	return fmt.Sprintf("%020d-%s.json", now.UnixNano(), hex.EncodeToString(sum[:6]))
}

func spoolHash(name string) string {
	base := strings.TrimSuffix(name, ".json")
	if i := strings.LastIndex(base, "-"); i >= 0 {
		return base[i+1:]
	}
	return base
}

// Enqueue writes req to the spool (atomic, mode 0600). If a payload with the
// same hash is already pending it returns that file instead of duplicating.
func Enqueue(homeDir string, req apitypes.AddRequest, cause error, now time.Time) (string, error) {
	dir := SpoolDir(homeDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := spoolName(req, now)
	names, _ := spoolFiles(homeDir)
	for _, n := range names {
		if spoolHash(n) == spoolHash(name) {
			return filepath.Join(dir, n), nil
		}
	}
	item := SpoolItem{CreatedAt: now.UTC(), Request: req}
	if cause != nil {
		item.LastError = cause.Error()
	}
	if err := writeItem(filepath.Join(dir, name), item); err != nil {
		return "", err
	}
	// Enforce the cap: drop the oldest beyond maxSpoolFiles.
	if names, err := spoolFiles(homeDir); err == nil && len(names) > maxSpoolFiles {
		for _, n := range names[:len(names)-maxSpoolFiles] {
			_ = os.Remove(filepath.Join(dir, n))
		}
	}
	return filepath.Join(dir, name), nil
}

func writeItem(path string, item SpoolItem) error {
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// spoolFiles lists pending payload names, oldest first.
func spoolFiles(homeDir string) ([]string, error) {
	entries, err := os.ReadDir(SpoolDir(homeDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// SpoolStats summarizes the spool for `m3m0s doctor`.
type SpoolStats struct {
	Pending int
	Dead    int
	Bytes   int64
	Oldest  time.Time // zero when empty
}

func Stats(homeDir string) SpoolStats {
	var s SpoolStats
	names, _ := spoolFiles(homeDir)
	s.Pending = len(names)
	for i, n := range names {
		if info, err := os.Stat(filepath.Join(SpoolDir(homeDir), n)); err == nil {
			s.Bytes += info.Size()
		}
		if i == 0 {
			if item, err := readItem(filepath.Join(SpoolDir(homeDir), n)); err == nil {
				s.Oldest = item.CreatedAt
			}
		}
	}
	if dead, err := os.ReadDir(deadDir(homeDir)); err == nil {
		s.Dead = len(dead)
	}
	return s
}

func readItem(path string) (SpoolItem, error) {
	var item SpoolItem
	data, err := os.ReadFile(path)
	if err != nil {
		return item, err
	}
	err = json.Unmarshal(data, &item)
	return item, err
}

// ReplayResult reports one Replay pass.
type ReplayResult struct {
	Sent, Failed, Dead, Expired int
	Unreachable                 bool // stopped early: the server did not answer
}

// isTransport reports whether err means the server did not answer (outage),
// as opposed to the server answering with an error.
func isTransport(err error) bool {
	var ue *url.Error
	return errors.As(err, &ue) || errors.Is(err, context.DeadlineExceeded)
}

// Replay sends pending payloads oldest first, at most limit per pass
// (limit<=0: all). A payload is deleted only after the server accepted it; a
// crash between accept and delete re-sends it later, which the server's
// dedup absorbs (idempotent replay). It stops at the first transport error so
// an outage costs one timeout, not one per file.
func Replay(ctx context.Context, homeDir string, c *client.Client, limit int, now time.Time) ReplayResult {
	var res ReplayResult
	names, _ := spoolFiles(homeDir)
	dir := SpoolDir(homeDir)
	for _, n := range names {
		if limit > 0 && res.Sent+res.Failed >= limit {
			break
		}
		path := filepath.Join(dir, n)
		item, err := readItem(path)
		if err != nil { // corrupt file: quarantine
			quarantine(homeDir, n)
			res.Dead++
			continue
		}
		if now.Sub(item.CreatedAt) > spoolRetention {
			_ = os.Remove(path)
			res.Expired++
			continue
		}
		if _, err := c.Add(ctx, item.Request); err != nil {
			if isTransport(err) {
				res.Unreachable = true
				return res
			}
			item.Attempts++
			item.LastError = err.Error()
			if item.Attempts >= maxSpoolAttempts {
				quarantine(homeDir, n)
				res.Dead++
			} else {
				_ = writeItem(path, item)
				res.Failed++
			}
			continue
		}
		_ = os.Remove(path)
		res.Sent++
	}
	return res
}

func quarantine(homeDir, name string) {
	_ = os.MkdirAll(deadDir(homeDir), 0o700)
	_ = os.Rename(filepath.Join(SpoolDir(homeDir), name), filepath.Join(deadDir(homeDir), name))
}
