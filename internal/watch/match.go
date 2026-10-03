package watch

import (
	"path"
	"strings"
)

// matchGlob checks whether relPath (always with "/" as separator, relative
// to the watched root) matches pattern. It supports "**" as a whole segment
// that consumes zero or more path segments (gitignore/bash globstar
// semantics: "docs/**/*.md" matches both "docs/x.md" and "docs/a/b/x.md").
// Each segment that is not "**" is compared with path.Match (supports *, ?,
// [classes]). Patterns without "/" (e.g. "AGENTS.md") only match at the root
// — they do not descend into subdirectories, on purpose: the A4 default
// lists top-level project files, not "any AGENTS.md at any depth".
func matchGlob(pattern, relPath string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(relPath, "/"))
}

func matchSegments(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		if matchSegments(pat[1:], name) {
			return true
		}
		if len(name) == 0 {
			return false
		}
		return matchSegments(pat, name[1:])
	}
	if len(name) == 0 {
		return false
	}
	ok, err := path.Match(pat[0], name[0])
	if err != nil || !ok {
		return false
	}
	return matchSegments(pat[1:], name[1:])
}

// matchAny checks whether relPath matches any of the patterns.
func matchAny(patterns []string, relPath string) bool {
	for _, p := range patterns {
		if matchGlob(p, relPath) {
			return true
		}
	}
	return false
}

// shouldIndex applies the "deny always wins over allow" rule (D11/docs/api-contract.md).
func shouldIndex(allow, deny []string, relPath string) bool {
	if matchAny(deny, relPath) {
		return false
	}
	return matchAny(allow, relPath)
}
