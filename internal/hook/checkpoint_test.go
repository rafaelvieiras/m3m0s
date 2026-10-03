package hook

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckpointPathCreatesDirWithRestrictedPerms(t *testing.T) {
	home := t.TempDir()
	path, err := CheckpointPath(home, "sess123")
	if err != nil {
		t.Fatalf("CheckpointPath: %v", err)
	}
	want := filepath.Join(home, ".m3m0s", "hooks", "claude-code", "sess123.json")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}

	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat of checkpoint directory: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("directory perm = %o, want %o", perm, 0o700)
	}
}

func TestLoadCheckpointMissingFileReturnsZeroValue(t *testing.T) {
	cp, err := LoadCheckpoint(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}
	if cp.LastLine != 0 {
		t.Errorf("LastLine = %d, want 0", cp.LastLine)
	}
}

func TestSaveThenLoadCheckpointRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess.json")
	if err := SaveCheckpoint(path, Checkpoint{LastLine: 42}); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}
	cp, err := LoadCheckpoint(path)
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}
	if cp.LastLine != 42 {
		t.Errorf("LastLine = %d, want 42", cp.LastLine)
	}
}
