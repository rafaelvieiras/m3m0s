// Package project resolves which project a client session belongs to
// (D34): an explicit M3M0S_PROJECT, or the longest directory prefix of the
// working directory listed in ~/.m3m0s/projects.json. Nothing is written into
// the repositories themselves.
package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// EnvVar names the explicit override.
const EnvVar = "M3M0S_PROJECT"

// FileName is the mapping file inside ~/.m3m0s.
const FileName = "projects.json"

// Path returns the mapping file path for a home directory.
func Path(home string) string { return filepath.Join(home, ".m3m0s", FileName) }

// Map is directory -> project slug.
type Map map[string]string

// Load reads the mapping; a missing file is an empty mapping.
func Load(home string) (Map, error) {
	data, err := os.ReadFile(Path(home))
	if errors.Is(err, os.ErrNotExist) {
		return Map{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := Map{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", Path(home), err)
	}
	return m, nil
}

// Save writes the mapping (mode 600, directories created).
func (m Map) Save(home string) error {
	if err := os.MkdirAll(filepath.Dir(Path(home)), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(Path(home), append(data, '\n'), 0o600)
}

// Set maps dir to slug after validating both.
func (m Map) Set(dir, slug string) error {
	if err := schema.ValidateProject(slug); err != nil {
		return err
	}
	abs, err := normalize(dir)
	if err != nil {
		return err
	}
	m[abs] = slug
	return nil
}

// Unset removes a directory; it reports whether it was mapped.
func (m Map) Unset(dir string) (bool, error) {
	abs, err := normalize(dir)
	if err != nil {
		return false, err
	}
	_, ok := m[abs]
	delete(m, abs)
	return ok, nil
}

// Dirs returns the mapped directories, sorted.
func (m Map) Dirs() []string {
	out := make([]string, 0, len(m))
	for d := range m {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

func normalize(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

// Lookup returns the slug of the longest mapped directory that contains cwd
// (at a path boundary), skipping entries whose slug is invalid.
func (m Map) Lookup(cwd string) string {
	if cwd == "" {
		return ""
	}
	cwd = filepath.Clean(cwd)
	best, bestLen := "", -1
	for dir, slug := range m {
		dir = filepath.Clean(dir)
		if schema.ValidateProject(slug) != nil {
			continue
		}
		if cwd == dir || strings.HasPrefix(cwd, strings.TrimRight(dir, "/")+"/") {
			if len(dir) > bestLen {
				best, bestLen = slug, len(dir)
			}
		}
	}
	return best
}

// Resolve returns the project for a session: M3M0S_PROJECT when set and
// valid (an invalid value yields "" instead of falling through, so a typo is
// never silently replaced by a different project), else the mapping for cwd.
// An empty result means global.
func Resolve(getenv func(string) string, home, cwd string) string {
	if v := strings.TrimSpace(getenv(EnvVar)); v != "" {
		if schema.ValidateProject(v) != nil {
			return ""
		}
		return v
	}
	m, err := Load(home)
	if err != nil {
		return ""
	}
	return m.Lookup(cwd)
}
