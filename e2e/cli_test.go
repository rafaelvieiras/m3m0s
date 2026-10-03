//go:build e2e

package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// runCLI runs the real m3m0s binary as a subprocess (not the lib
// in-process) — it is the same binary the user would call in the terminal.
func runCLI(t *testing.T, env []string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Env = env
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return outBuf.String(), errBuf.String(), exitErr.ExitCode()
		}
		t.Fatalf("unexpected error running cli %v: %v", args, err)
	}
	return outBuf.String(), errBuf.String(), 0
}

func TestEndToEndCLI(t *testing.T) {
	embed := newFakeEmbedServer(t)
	srv := startServe(t, embed.URL)
	env := append(os.Environ(), "M3M0S_URL="+srv.URL, "M3M0S_API_KEY="+srv.APIKey)

	const domain = "codigo:test-e2e-cli"
	const content = "Content created via CLI e2e about automated end-to-end tests"

	t.Run("add without --json", func(t *testing.T) {
		out, errOut, code := runCLI(t, env, "add", content, "--domain", domain, "--tag", "foo", "--meta", "k=v")
		if code != 0 {
			t.Fatalf("exit %d, stderr=%s", code, errOut)
		}
		if !strings.Contains(out, "id=") || !strings.Contains(out, domain) {
			t.Errorf("unexpected stdout: %q", out)
		}
	})

	t.Run("add with --json decodes AddResponse", func(t *testing.T) {
		out, errOut, code := runCLI(t, env, "add", content, "--domain", domain, "--json")
		if code != 0 {
			t.Fatalf("exit %d, stderr=%s", code, errOut)
		}
		var added apitypes.AddResponse
		mustUnmarshal(t, []byte(out), &added)
		if len(added.Entries) != 1 || added.Entries[0].Entry.ID == "" {
			t.Errorf("expected 1 entry with a non-empty id in --json, got %#v", added)
		}
	})

	t.Run("search without --json prints contextBlock", func(t *testing.T) {
		out, errOut, code := runCLI(t, env, "search", "Content created via CLI about tests", "--domain", domain)
		if code != 0 {
			t.Fatalf("exit %d, stderr=%s", code, errOut)
		}
		if strings.TrimSpace(out) == "" {
			t.Error("expected non-empty contextBlock")
		}
	})

	t.Run("search with --json decodes []Entry", func(t *testing.T) {
		out, errOut, code := runCLI(t, env, "search", "Content created via CLI about tests", "--domain", domain, "--json")
		if code != 0 {
			t.Fatalf("exit %d, stderr=%s", code, errOut)
		}
		var results []schema.Entry
		mustUnmarshal(t, []byte(out), &results)
		if len(results) == 0 {
			t.Error("expected at least one result")
		}
	})

	t.Run("recent with --json includes the created entry", func(t *testing.T) {
		out, errOut, code := runCLI(t, env, "recent", "--domain", domain, "--json")
		if code != 0 {
			t.Fatalf("exit %d, stderr=%s", code, errOut)
		}
		var results []schema.Entry
		mustUnmarshal(t, []byte(out), &results)
		if len(results) == 0 {
			t.Error("recent is empty")
		}
	})

	t.Run("stats decodes StatsResponse", func(t *testing.T) {
		out, errOut, code := runCLI(t, env, "stats")
		if code != 0 {
			t.Fatalf("exit %d, stderr=%s", code, errOut)
		}
		var stats apitypes.StatsResponse
		mustUnmarshal(t, []byte(out), &stats)
		if stats.Count < 1 {
			t.Errorf("expected count >= 1, got %d", stats.Count)
		}
	})

	t.Run("add without --domain fails with exit != 0", func(t *testing.T) {
		_, errOut, code := runCLI(t, env, "add", "any content without domain")
		if code == 0 {
			t.Fatal("expected exit != 0")
		}
		if strings.TrimSpace(errOut) == "" {
			t.Error("expected an error message on stderr")
		}
	})

	t.Run("wrong api key fails with exit != 0", func(t *testing.T) {
		badEnv := append(os.Environ(), "M3M0S_URL="+srv.URL, "M3M0S_API_KEY=truly-invalid-key")
		_, errOut, code := runCLI(t, badEnv, "stats")
		if code == 0 {
			t.Fatal("expected exit != 0 with a wrong api key")
		}
		if strings.TrimSpace(errOut) == "" {
			t.Error("expected an error message on stderr")
		}
	})

	t.Run("update partially edits and --json decodes UpdateResponse", func(t *testing.T) {
		out, errOut, code := runCLI(t, env, "add", "original content to edit via cli", "--domain", domain, "--json")
		if code != 0 {
			t.Fatalf("add setup: exit %d, stderr=%s", code, errOut)
		}
		var added apitypes.AddResponse
		mustUnmarshal(t, []byte(out), &added)
		id := added.Entries[0].Entry.ID

		out, errOut, code = runCLI(t, env, "update", id, "--content", "content edited via m3m0s update", "--json")
		if code != 0 {
			t.Fatalf("update: exit %d, stderr=%s", code, errOut)
		}
		var updated schema.Entry
		mustUnmarshal(t, []byte(out), &updated)
		if updated.ID != id {
			t.Errorf("unexpected id: %q", updated.ID)
		}
		if updated.Content != "content edited via m3m0s update" {
			t.Errorf("content not updated: %q", updated.Content)
		}
	})

	t.Run("update --clear-tags clears the tags", func(t *testing.T) {
		out, errOut, code := runCLI(t, env, "add", "content with tags to clear", "--domain", domain, "--tag", "a", "--tag", "b", "--json")
		if code != 0 {
			t.Fatalf("add setup: exit %d, stderr=%s", code, errOut)
		}
		var added apitypes.AddResponse
		mustUnmarshal(t, []byte(out), &added)
		id := added.Entries[0].Entry.ID

		out, errOut, code = runCLI(t, env, "update", id, "--clear-tags", "--json")
		if code != 0 {
			t.Fatalf("update: exit %d, stderr=%s", code, errOut)
		}
		var updated schema.Entry
		mustUnmarshal(t, []byte(out), &updated)
		if len(updated.Tags) != 0 {
			t.Errorf("expected empty tags after --clear-tags, got %v", updated.Tags)
		}
	})

	t.Run("update --clear-tags and --tag together fails with exit != 0", func(t *testing.T) {
		_, errOut, code := runCLI(t, env, "update", "mem_any", "--clear-tags", "--tag", "foo")
		if code == 0 {
			t.Fatal("expected exit != 0 with --clear-tags and --tag together")
		}
		if !strings.Contains(errOut, "mutually exclusive") {
			t.Errorf("expected a message about mutually exclusive flags, got: %q", errOut)
		}
	})

	t.Run("update --clear-metadata and --meta together fails with exit != 0", func(t *testing.T) {
		_, errOut, code := runCLI(t, env, "update", "mem_any", "--clear-metadata", "--meta", "k=v")
		if code == 0 {
			t.Fatal("expected exit != 0 with --clear-metadata and --meta together")
		}
		if !strings.Contains(errOut, "mutually exclusive") {
			t.Errorf("expected a message about mutually exclusive flags, got: %q", errOut)
		}
	})

	t.Run("update without any flag fails with exit != 0", func(t *testing.T) {
		_, errOut, code := runCLI(t, env, "update", "mem_any")
		if code == 0 {
			t.Fatal("expected exit != 0 without any edit flag")
		}
		if strings.TrimSpace(errOut) == "" {
			t.Error("expected an error message on stderr")
		}
	})

	t.Run("update nonexistent id fails with exit != 0", func(t *testing.T) {
		_, errOut, code := runCLI(t, env, "update", "mem_definitely_does_not_exist", "--content", "x")
		if code == 0 {
			t.Fatal("expected exit != 0 for a nonexistent id")
		}
		if strings.TrimSpace(errOut) == "" {
			t.Error("expected an error message on stderr")
		}
	})

	t.Run("get --json decodes GetResponse", func(t *testing.T) {
		out, errOut, code := runCLI(t, env, "add", "original content to read via m3m0s get", "--domain", domain, "--json")
		if code != 0 {
			t.Fatalf("add setup: exit %d, stderr=%s", code, errOut)
		}
		var added apitypes.AddResponse
		mustUnmarshal(t, []byte(out), &added)
		id := added.Entries[0].Entry.ID

		out, errOut, code = runCLI(t, env, "get", id, "--json")
		if code != 0 {
			t.Fatalf("get: exit %d, stderr=%s", code, errOut)
		}
		var got schema.Entry
		mustUnmarshal(t, []byte(out), &got)
		if got.ID != id {
			t.Errorf("unexpected id: %q", got.ID)
		}
		if got.Content != "original content to read via m3m0s get" {
			t.Errorf("unexpected content: %q", got.Content)
		}
	})

	t.Run("get without --json prints a readable summary", func(t *testing.T) {
		out, errOut, code := runCLI(t, env, "add", "content for a readable get summary", "--domain", domain, "--json")
		if code != 0 {
			t.Fatalf("add setup: exit %d, stderr=%s", code, errOut)
		}
		var added apitypes.AddResponse
		mustUnmarshal(t, []byte(out), &added)
		id := added.Entries[0].Entry.ID

		out, errOut, code = runCLI(t, env, "get", id)
		if code != 0 {
			t.Fatalf("get: exit %d, stderr=%s", code, errOut)
		}
		if !strings.Contains(out, "id="+id) || !strings.Contains(out, domain) {
			t.Errorf("unexpected stdout: %q", out)
		}
	})

	t.Run("get nonexistent id fails with exit != 0", func(t *testing.T) {
		_, errOut, code := runCLI(t, env, "get", "mem_definitely_does_not_exist")
		if code == 0 {
			t.Fatal("expected exit != 0 for a nonexistent id")
		}
		if strings.TrimSpace(errOut) == "" {
			t.Error("expected an error message on stderr")
		}
	})
}
