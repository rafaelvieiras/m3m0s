// Package maintenance holds offline repair jobs that operate directly on the
// data directory (as opposed to the HTTP-backed CLI commands).
package maintenance

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/rafaelvieiras/m3m0s/internal/redact"
	"github.com/rafaelvieiras/m3m0s/internal/sqlstore"
)

// Finding is one affected row. It carries ids and rule names only — never any
// part of the value that matched.
type Finding struct {
	Kind  string   // "blob" or "entry"
	ID    string   // blob hash or entry id
	Field string   // entry field: "content" or "metadata.<key>"
	Rules []string // ids of the redaction rules that matched
}

// RedactScanReport is the outcome of RedactScan.
type RedactScanReport struct {
	Applied          bool
	BlobsScanned     int
	EntriesScanned   int
	Blobs            []Finding
	Entries          []Finding
	BlobsRewritten   int
	EntriesUntouched int // entries reported but, by design, never rewritten
}

const sourceContentRefKey = "source_content_ref"

// RedactScan runs the current redactor over every stored source blob and
// every entry. With apply=false (dry run) it only reports. With apply=true it
// re-redacts the blobs in place (keeping their hash keys, so entry references
// and the D21 dedup bookkeeping stay valid); it never rewrites entries,
// because changing an entry's text would invalidate its embedding — affected
// entries are listed for manual review.
func RedactScan(ctx context.Context, db *sqlstore.DB, apply bool) (RedactScanReport, error) {
	rep := RedactScanReport{Applied: apply}

	type fix struct{ hash, text string }
	var fixes []fix
	err := db.ForEachSourceBlob(ctx, func(hash, content string) {
		rep.BlobsScanned++
		res := redact.Redact(content)
		if len(res.Redacted) == 0 {
			return
		}
		rep.Blobs = append(rep.Blobs, Finding{Kind: "blob", ID: hash, Rules: res.Redacted})
		fixes = append(fixes, fix{hash, res.Text})
	})
	if err != nil {
		return rep, err
	}

	err = db.ForEachEntry(ctx, func(e sqlstore.EntryRow) {
		rep.EntriesScanned++
		if rules := redact.Scan(e.Content); len(rules) > 0 {
			rep.Entries = append(rep.Entries, Finding{Kind: "entry", ID: e.ID, Field: "content", Rules: rules})
		}
		keys := make([]string, 0, len(e.Metadata))
		for k := range e.Metadata {
			if k != sourceContentRefKey {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			if rules := redact.Scan(e.Metadata[k]); len(rules) > 0 {
				rep.Entries = append(rep.Entries, Finding{Kind: "entry", ID: e.ID, Field: "metadata." + k, Rules: rules})
			}
		}
	})
	if err != nil {
		return rep, err
	}
	rep.EntriesUntouched = len(rep.Entries)

	if apply {
		for _, f := range fixes {
			if err := db.ReplaceSourceBlobContent(ctx, f.hash, f.text); err != nil {
				return rep, err
			}
			rep.BlobsRewritten++
		}
	}
	return rep, nil
}

// Format renders the report as plain text: ids, hashes and rule ids only.
func (r RedactScanReport) Format() string {
	var b strings.Builder
	mode := "dry run (nothing was changed; use --apply to re-redact blobs)"
	if r.Applied {
		mode = "apply"
	}
	fmt.Fprintf(&b, "redact-scan: %s\n", mode)
	fmt.Fprintf(&b, "scanned: %d blobs, %d entries\n", r.BlobsScanned, r.EntriesScanned)
	for _, f := range r.Blobs {
		fmt.Fprintf(&b, "blob  %s  rules=%s\n", f.ID, strings.Join(f.Rules, ","))
	}
	for _, f := range r.Entries {
		fmt.Fprintf(&b, "entry %s  field=%s  rules=%s\n", f.ID, f.Field, strings.Join(f.Rules, ","))
	}
	fmt.Fprintf(&b, "affected: %d blobs, %d entry fields", len(r.Blobs), len(r.Entries))
	if r.Applied {
		fmt.Fprintf(&b, "; %d blobs re-redacted", r.BlobsRewritten)
	}
	b.WriteString("\n")
	if len(r.Entries) > 0 {
		b.WriteString("entries are never rewritten (that would invalidate their embeddings): review and fix the ids above by hand\n")
	}
	return b.String()
}
