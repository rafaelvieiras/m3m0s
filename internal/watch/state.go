package watch

import (
	"encoding/json"
	"os"
	"sync"
)

// FileState holds, per watched file (key = path relative to the root), the
// hash of the last indexed content and the IDs of the chunks generated from
// it — this is what allows skipping reindexing when nothing really changed
// (hash diff) and deleting exactly the right chunks when the file changes or
// is removed (cascading purge, D11).
type FileState struct {
	Hash     string   `json:"hash"`
	ChunkIDs []string `json:"chunkIds"`
}

// State is the local state of m3m0s watch for a watched root — it is never
// seen by m3m0s serve, it exists only in the watch process (D3: watch is an
// HTTP client of serve, it does not share storage).
type State struct {
	mu   sync.Mutex
	path string
	data map[string]FileState
}

// LoadState reads the state at path (or starts empty if the file does not
// exist yet).
func LoadState(path string) (*State, error) {
	s := &State{path: path, data: make(map[string]FileState)}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *State) Get(relPath string) (FileState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fs, ok := s.data[relPath]
	return fs, ok
}

// Set records the state of relPath and persists it (atomic tmp+rename
// write, same pattern as internal/store/internal/auth).
func (s *State) Set(relPath string, fs FileState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[relPath] = fs
	return s.saveLocked()
}

// Delete removes relPath from the state and persists it.
func (s *State) Delete(relPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, relPath)
	return s.saveLocked()
}

// Paths returns all relative paths currently tracked — used by the initial
// Sync to find files that vanished since the last run (watch was not running
// when they were deleted).
func (s *State) Paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	paths := make([]string, 0, len(s.data))
	for p := range s.data {
		paths = append(paths, p)
	}
	return paths
}

func (s *State) saveLocked() error {
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
