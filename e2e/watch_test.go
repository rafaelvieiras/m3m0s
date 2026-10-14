//go:build e2e

package e2e

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
)

// startWatch starts a real `m3m0s watch <dir>` as a subprocess, pointed at
// the already running test `serve`. It kills the process at the end of the
// test (closing stdin is not enough here, unlike `mcp` — watch does not
// read stdin, it runs until the context/process dies).
func startWatch(t *testing.T, srv *serveProc, dir, domain string) {
	t.Helper()
	cmd := exec.Command(binPath, "watch", dir, "--domain", domain)
	cmd.Env = append(os.Environ(), "M3M0S_URL="+srv.URL, "M3M0S_API_KEY="+srv.APIKey)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start m3m0s watch: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	})
}

// searchDomain returns the search results for a query in a domain — used
// for polling (watch reindexes asynchronously).
func searchDomain(t *testing.T, srv *serveProc, query, domain string) apitypes.SearchResponse {
	t.Helper()
	resp, data := doJSON(t, http.MethodPost, srv.URL+"/api/v1/search", srv.APIKey, apitypes.SearchRequest{
		Query: query, Domain: domain, Limit: 20,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search: status %d: %s", resp.StatusCode, data)
	}
	var out apitypes.SearchResponse
	mustUnmarshal(t, data, &out)
	return out
}

func statsFor(t *testing.T, srv *serveProc) apitypes.StatsResponse {
	t.Helper()
	resp, data := doJSON(t, http.MethodGet, srv.URL+"/api/v1/stats", srv.APIKey, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats: status %d: %s", resp.StatusCode, data)
	}
	var out apitypes.StatsResponse
	mustUnmarshal(t, data, &out)
	return out
}

// waitUntil polls cond until it is true or the timeout expires — never a
// long fixed sleep.
func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !cond() {
		t.Fatal("timeout waiting for the condition to become true")
	}
}

func TestEndToEndWatch(t *testing.T) {
	embed := newFakeEmbedServer(t)
	srv := startServe(t, embed.URL)

	root := t.TempDir()
	const domain = "code:test-e2e-watch"

	agentsContent := "# Test AGENTS.md\n\n" +
		"## First section watched by watch\n" +
		strings.Repeat("original content of the first section reindexed by watch. ", 8) + "\n\n" +
		"## Second section watched by watch\n" +
		strings.Repeat("original content of the second section reindexed by watch. ", 8) + "\n"
	mustWriteFile(t, filepath.Join(root, "AGENTS.md"), agentsContent)
	mustWriteFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")
	mustWriteFile(t, filepath.Join(root, "node_modules", "package", "README.md"), "should never be indexed by watch")

	startWatch(t, srv, root, domain)

	t.Run("indexes AGENTS.md into multiple chunks and ignores files outside the allowlist", func(t *testing.T) {
		waitUntil(t, 15*time.Second, func() bool {
			out := searchDomain(t, srv, "first section reindexed", domain)
			return len(out.Results) >= 1
		})

		out := searchDomain(t, srv, "first section reindexed", domain)
		if len(out.Results) < 1 {
			t.Fatalf("expected at least 1 result for 'first section', got %d", len(out.Results))
		}

		stats := statsFor(t, srv)
		if stats.CountByDomainCategory["code"] < 2 {
			t.Errorf("AGENTS.md has 2 ## headers — expected at least 2 indexed chunks, stats: %#v", stats)
		}

		mainGoResults := searchDomain(t, srv, "package main func main", domain)
		for _, r := range mainGoResults.Results {
			if r.Metadata["file_path"] == "main.go" {
				t.Errorf("main.go should not have been indexed (outside the default allowlist): %#v", r)
			}
		}
		nodeModulesResults := searchDomain(t, srv, "should never be indexed", domain)
		for _, r := range nodeModulesResults.Results {
			if r.Metadata["file_path"] == "node_modules/package/README.md" {
				t.Errorf("file in node_modules should not have been indexed (default denylist): %#v", r)
			}
		}
	})

	t.Run("edits AGENTS.md and watch reindexes the new content", func(t *testing.T) {
		newContent := "# Test AGENTS.md\n\n" +
			"## First section watched by watch\n" +
			strings.Repeat("COMPLETELY_NEW_CONTENT_AFTER_WATCH_EDIT replaced text. ", 8) + "\n\n" +
			"## Second section watched by watch\n" +
			strings.Repeat("original content of the second section reindexed by watch. ", 8) + "\n"
		mustWriteFile(t, filepath.Join(root, "AGENTS.md"), newContent)

		waitUntil(t, 15*time.Second, func() bool {
			out := searchDomain(t, srv, "COMPLETELY_NEW_CONTENT_AFTER_WATCH_EDIT", domain)
			return len(out.Results) >= 1
		})

		waitUntil(t, 15*time.Second, func() bool {
			out := searchDomain(t, srv, "original content of the first section", domain)
			for _, r := range out.Results {
				if r.Metadata["file_path"] == "AGENTS.md" && strings.Contains(r.Content, "original content of the first section") {
					return false
				}
			}
			return true
		})
	})

	t.Run("deletes AGENTS.md and watch purges the chunks in cascade", func(t *testing.T) {
		if err := os.Remove(filepath.Join(root, "AGENTS.md")); err != nil {
			t.Fatalf("remove AGENTS.md: %v", err)
		}

		waitUntil(t, 15*time.Second, func() bool {
			out := searchDomain(t, srv, "COMPLETELY_NEW_CONTENT_AFTER_WATCH_EDIT", domain)
			for _, r := range out.Results {
				if r.Metadata["file_path"] == "AGENTS.md" {
					return false
				}
			}
			return true
		})
	})
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
