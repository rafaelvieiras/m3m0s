package hook

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/rafaelvieiras/m3m0s/internal/client"
)

// ResolveCredentials resolves M3M0S_URL/M3M0S_API_KEY in 2 steps (D14): 1)
// process environment: if M3M0S_API_KEY is set there, M3M0S_URL is also read
// from the environment (or client.DefaultURL if absent), without mixing with
// the file; 2) if M3M0S_API_KEY is not in the environment, fall back to the
// ~/.m3m0s/hook.env file (KEY=value format, one per line, blank lines and
// "#" lines ignored). It exists so the API key is not embedded in plain text
// inside ~/.claude/settings.json, which is a global config with a broader
// purpose. ok=false when neither source has M3M0S_API_KEY; the caller
// decides what to do (D14: never block the hook for lack of config, just
// don't send).
//
// getenv and homeDir are injectable for tests (os.Getenv/expandHome in
// production).
func ResolveCredentials(getenv func(string) string, homeDir string) (baseURL, apiKey string, ok bool) {
	if v := getenv("M3M0S_API_KEY"); v != "" {
		baseURL = getenv("M3M0S_URL")
		if baseURL == "" {
			baseURL = client.DefaultURL
		}
		return baseURL, v, true
	}

	fileVars, err := readEnvFile(filepath.Join(homeDir, ".m3m0s", "hook.env"))
	if err != nil || fileVars["M3M0S_API_KEY"] == "" {
		return "", "", false
	}
	baseURL = fileVars["M3M0S_URL"]
	if baseURL == "" {
		baseURL = client.DefaultURL
	}
	return baseURL, fileVars["M3M0S_API_KEY"], true
}

// readEnvFile parses a simple KEY=value file (1 per line, "#" starts a
// comment, blank lines ignored). Returns (nil, nil) if the file does not
// exist: that is not an error, it just means "no credentials here".
func readEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	out := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out, nil
}
