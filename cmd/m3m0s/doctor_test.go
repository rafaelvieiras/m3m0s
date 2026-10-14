package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Informational lines (successful injections) and untimestamped legacy lines
// must not count as failures; only real error lines inside the window do.
func TestRecentHookFailuresIgnoresInfoLines(t *testing.T) {
	now := time.Now().UTC()
	ts := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }
	log := strings.Join([]string{
		"m3m0s hook claude-code: legacy line without timestamp",
		ts(48*time.Hour) + " m3m0s hook claude-code: POST /api/v1/memories failed: old",
		ts(time.Hour) + " m3m0s hook claude-code: injected 15 memories, ~600 tokens (budget 1000) for code:x",
		ts(30*time.Minute) + " m3m0s hook claude-code: POST /api/v1/memories failed, chunk 1/1 spooled: boom",
		ts(time.Minute) + " m3m0s hook claude-code: injected 3 memories, ~100 tokens (budget 1000) for code:y",
	}, "\n") + "\n"
	path := filepath.Join(t.TempDir(), "claude-code.log")
	if err := os.WriteFile(path, []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	n, last := recentHookFailures(path, 24*time.Hour)
	if n != 1 || !strings.Contains(last, "boom") {
		t.Fatalf("got %d failures, last %q; want 1 ending in boom", n, last)
	}
}
