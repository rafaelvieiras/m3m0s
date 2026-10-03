package hook

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/m3m0s/internal/client"
)

func TestResolveCredentialsPrefersEnv(t *testing.T) {
	env := map[string]string{"M3M0S_URL": "http://env.example:1234", "M3M0S_API_KEY": "env-key"}
	getenv := func(k string) string { return env[k] }

	baseURL, apiKey, ok := ResolveCredentials(getenv, t.TempDir())
	if !ok {
		t.Fatal("expected ok=true with full env")
	}
	if baseURL != "http://env.example:1234" || apiKey != "env-key" {
		t.Errorf("baseURL=%q apiKey=%q, want env values", baseURL, apiKey)
	}
}

func TestResolveCredentialsEnvAPIKeyOnlyDefaultsURL(t *testing.T) {
	env := map[string]string{"M3M0S_API_KEY": "env-key"}
	getenv := func(k string) string { return env[k] }

	baseURL, apiKey, ok := ResolveCredentials(getenv, t.TempDir())
	if !ok {
		t.Fatal("expected ok=true")
	}
	if baseURL != client.DefaultURL {
		t.Errorf("baseURL = %q, want DefaultURL %q", baseURL, client.DefaultURL)
	}
	if apiKey != "env-key" {
		t.Errorf("apiKey = %q, want env-key", apiKey)
	}
}

func TestResolveCredentialsFallsBackToHookEnvFile(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".m3m0s")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "# comment, ignored\n\nM3M0S_URL=http://file.example:9999\nM3M0S_API_KEY=file-key\n"
	if err := os.WriteFile(filepath.Join(dir, "hook.env"), []byte(content), 0o600); err != nil {
		t.Fatalf("writing hook.env: %v", err)
	}

	getenv := func(string) string { return "" }
	baseURL, apiKey, ok := ResolveCredentials(getenv, home)
	if !ok {
		t.Fatal("expected ok=true reading from hook.env")
	}
	if baseURL != "http://file.example:9999" || apiKey != "file-key" {
		t.Errorf("baseURL=%q apiKey=%q, want file values", baseURL, apiKey)
	}
}

func TestResolveCredentialsHookEnvFileMissingURLDefaults(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".m3m0s")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hook.env"), []byte("M3M0S_API_KEY=file-key\n"), 0o600); err != nil {
		t.Fatalf("writing hook.env: %v", err)
	}

	getenv := func(string) string { return "" }
	baseURL, apiKey, ok := ResolveCredentials(getenv, home)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if baseURL != client.DefaultURL {
		t.Errorf("baseURL = %q, want DefaultURL", baseURL)
	}
	if apiKey != "file-key" {
		t.Errorf("apiKey = %q, want file-key", apiKey)
	}
}

func TestResolveCredentialsNoSourceReturnsNotOK(t *testing.T) {
	getenv := func(string) string { return "" }
	_, _, ok := ResolveCredentials(getenv, t.TempDir())
	if ok {
		t.Error("expected ok=false with neither env nor hook.env")
	}
}

func TestResolveCredentialsHookEnvFileWithoutAPIKeyIsNotOK(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".m3m0s")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hook.env"), []byte("M3M0S_URL=http://url-only.example\n"), 0o600); err != nil {
		t.Fatalf("writing hook.env: %v", err)
	}

	getenv := func(string) string { return "" }
	_, _, ok := ResolveCredentials(getenv, home)
	if ok {
		t.Error("expected ok=false when hook.env has no M3M0S_API_KEY")
	}
}
