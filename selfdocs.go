// Package selfdocs embeds m3m0s's own documentation (AGENTS.md, docs/*.md,
// docs/decisions/*.md) into the binary via go:embed — the same pattern
// already used by webui/embed.go for the WebUI (dist/) — so that any
// deployment (standalone Docker, a small ARM host, someone else's clone)
// has the WebUI chat's "self-RAG" (D23) working without depending on the
// host having the files or on an external `m3m0s watch` running on the
// side (D24 was the development-only version of this; this one is the
// version that works in production). See D25 in docs/decisions/.
//
// The embed directive does not accept ".." in the pattern — patterns are always
// relative to the directory of the .go file that carries the directive.
// Since AGENTS.md and docs/ live at the repo ROOT, this file must also be
// at the root (a path that only exists above a subdirectory cannot be
// embedded from inside internal/* or from a subdirectory) — which is why
// this package's import path is the module root itself, with no suffix,
// even though the package is named "selfdocs".
package selfdocs

import (
	"embed"
	"io/fs"
	"sort"
)

// CLAUDE.md is left out on purpose: it is just a 1-line pointer to
// AGENTS.md ("Read @AGENTS.md"), with no content of its own to index.
//
//go:embed AGENTS.md docs/*.md docs/decisions/*.md
var embedded embed.FS

// File is an embedded documentation file, already read.
type File struct {
	Path    string // relative to the repo root, e.g. "AGENTS.md", "docs/decisions/D25-embedded-docs-self-indexing.md"
	Content string
}

// Files returns every embedded file, sorted by Path (deterministic —
// important for selfindex idempotence: same order on every boot).
func Files() ([]File, error) {
	var files []File
	err := fs.WalkDir(embedded, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		content, err := fs.ReadFile(embedded, path)
		if err != nil {
			return err
		}
		files = append(files, File{Path: path, Content: string(content)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}
