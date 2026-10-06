package project

import (
	"path/filepath"
	"testing"
)

func TestLookupLongestPrefixAtPathBoundary(t *testing.T) {
	m := Map{"/work/a": "proj-a", "/work/a/sub": "proj-sub", "/work/bad": "Not Valid"}
	cases := map[string]string{
		"/work/a":       "proj-a",
		"/work/a/x/y":   "proj-a",
		"/work/a/sub/z": "proj-sub",
		"/work/ab":      "", // not a path-boundary match
		"/work/bad/x":   "", // invalid slug is ignored
		"/elsewhere":    "",
		"":              "",
	}
	for cwd, want := range cases {
		if got := m.Lookup(cwd); got != want {
			t.Errorf("Lookup(%q) = %q, want %q", cwd, got, want)
		}
	}
}

func TestResolveEnvBeatsMappingAndInvalidEnvMeansGlobal(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "repo")
	m := Map{}
	if err := m.Set(dir, "from-file"); err != nil {
		t.Fatal(err)
	}
	if err := m.Save(home); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{}
	get := func(k string) string { return env[k] }

	if got := Resolve(get, home, dir); got != "from-file" {
		t.Errorf("mapping: %q", got)
	}
	env[EnvVar] = "from-env"
	if got := Resolve(get, home, dir); got != "from-env" {
		t.Errorf("env override: %q", got)
	}
	env[EnvVar] = "Bad Slug"
	if got := Resolve(get, home, dir); got != "" {
		t.Errorf("invalid env must resolve to global, got %q", got)
	}
}

func TestSetUnsetRoundTrip(t *testing.T) {
	home := t.TempDir()
	m, err := Load(home)
	if err != nil || len(m) != 0 {
		t.Fatalf("empty load: %v %v", m, err)
	}
	if err := m.Set("/some/dir", "Bad Slug"); err == nil {
		t.Error("invalid slug accepted")
	}
	if err := m.Set("/some/dir", "ok-slug"); err != nil {
		t.Fatal(err)
	}
	if err := m.Save(home); err != nil {
		t.Fatal(err)
	}
	m2, _ := Load(home)
	if m2["/some/dir"] != "ok-slug" {
		t.Fatalf("reload: %v", m2)
	}
	if removed, _ := m2.Unset("/some/dir"); !removed {
		t.Error("Unset reported not mapped")
	}
}
