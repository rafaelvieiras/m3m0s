//go:build e2e

// End-to-end tests for M3m0s: they compile the real binary, start `m3m0s
// serve` and `m3m0s mcp` as real subprocesses and talk to them over real
// HTTP/stdio, exactly as a host (Claude Code, OpenCode, pi) or the CLI
// would. The only fake piece is the embeddings provider (there is no
// OPENAI_API_KEY in this environment and the tests cannot depend on the
// external network or on real API cost): a local OpenAI-compatible server
// (see fakeEmbedding in embed_test.go) plugged in via `-openai-base`.
//
// Run with: go test -tags=e2e ./e2e/...
// They are left out of the default `go test ./...` on purpose (the build
// tag above), because they start real processes and real TCP ports.
package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
	"time"
)

var binPath string

func TestMain(m *testing.M) {
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "repoRoot:", err)
		os.Exit(1)
	}

	tmpDir, err := os.MkdirTemp("", "m3m0s-e2e-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "mkdir temp:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	binPath = filepath.Join(tmpDir, "m3m0s")
	build := exec.Command("go", "build", "-o", binPath, "./cmd/m3m0s")
	build.Dir = root
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build m3m0s:", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// repoRoot finds the module root from the path of this very source file, so
// as not to depend on the cwd `go test` uses (which is the package
// directory, e2e/, not the root).
func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(file)), nil
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("close free-port listener: %v", err)
	}
	return port
}

func waitHealth(t *testing.T, baseURL string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for /health to respond: %v", lastErr)
}

// serveProc is a real `m3m0s serve` running as a subprocess, ready to
// receive requests.
type serveProc struct {
	URL    string
	APIKey string
	cmd    *exec.Cmd
}

var bootstrapKeyRe = regexp.MustCompile(`m3m0s_[0-9A-Za-z]+`)

// startServe starts a real `m3m0s serve` (subprocess), points the
// embedding at the fake server (embedURL) and waits for the full boot:
// /health answering and the bootstrap API key captured from stderr (see the
// message in cmd/m3m0s/serve.go, "it will not be shown again"). It kills the
// process automatically at the end of the test. extraArgs is optional — used
// by the fact extraction tests (Phase 4, D12) to pass
// -extract-key/-extract-base against a chat completions fake.
func startServe(t *testing.T, embedURL string, extraArgs ...string) *serveProc {
	t.Helper()

	dataDir := t.TempDir()
	port := freePort(t)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	args := []string{"serve",
		"-port", fmt.Sprintf("%d", port),
		"-data", dataDir,
		"-key", "test-key-not-validated-against-the-real-openai",
		"-openai-base", embedURL,
	}
	args = append(args, extraArgs...)
	cmd := exec.Command(binPath, args...)

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start m3m0s serve: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	})

	keyCh := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			line := scanner.Text()
			if m := bootstrapKeyRe.FindString(line); m != "" {
				select {
				case keyCh <- m:
				default:
				}
			}
		}
	}()

	var apiKey string
	select {
	case apiKey = <-keyCh:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for the bootstrap api key to appear on serve's stderr")
	}

	waitHealth(t, baseURL)

	return &serveProc{URL: baseURL, APIKey: apiKey, cmd: cmd}
}

func mustUnmarshal(t *testing.T, data []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("invalid json (%v): %s", err, data)
	}
}
