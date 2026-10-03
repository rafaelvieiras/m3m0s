package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Checkpoint is the per-session local state that avoids reprocessing the same
// transcript lines when PreCompact and SessionEnd fire in the same session
// (D14). LastLine is the total number of JSONL lines processed in the last
// run (not the index of the last line, the total: lines [0, LastLine) have
// already been seen).
type Checkpoint struct {
	LastLine int `json:"last_line"`
}

// CheckpointPath is ~/.m3m0s/hooks/claude-code/<sessionID>.json (D14); it
// creates the directory (0700) if it does not exist yet.
func CheckpointPath(homeDir, sessionID string) (string, error) {
	dir := filepath.Join(homeDir, ".m3m0s", "hooks", "claude-code")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, sessionID+".json"), nil
}

// LoadCheckpoint reads the checkpoint at path (or returns the zero value if
// the file does not exist yet).
func LoadCheckpoint(path string) (Checkpoint, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Checkpoint{}, nil
	}
	if err != nil {
		return Checkpoint{}, err
	}
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return Checkpoint{}, err
	}
	return cp, nil
}

// SaveCheckpoint writes atomically (temp file + rename), the same write
// pattern already used in internal/store/store.go and
// internal/watch/state.go.
func SaveCheckpoint(path string, cp Checkpoint) error {
	data, err := json.Marshal(cp)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
