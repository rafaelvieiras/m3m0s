package sqlstore

import (
	"context"
	"fmt"
	"strings"
	"unicode"
)

// maxFTSTerms bounds the OR query built from free text (a long prompt would
// otherwise become hundreds of terms).
const maxFTSTerms = 24

// FTSQuery turns free text into a safe FTS5 MATCH expression: lowercase
// words of 2+ characters, deduplicated, each quoted and OR-ed (bm25 already
// down-weights common words). Returns "" when no usable term exists.
func FTSQuery(text string) string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seen := make(map[string]struct{}, len(words))
	terms := make([]string, 0, len(words))
	for _, w := range words {
		if len([]rune(w)) < 2 {
			continue
		}
		if _, dup := seen[w]; dup {
			continue
		}
		seen[w] = struct{}{}
		terms = append(terms, `"`+w+`"`)
		if len(terms) == maxFTSTerms {
			break
		}
	}
	return strings.Join(terms, " OR ")
}

// SearchFTS returns up to limit entry ids matching text, best bm25 first,
// restricted to domain when it is not "". An empty or term-less query
// returns no ids and no error.
func (db *DB) SearchFTS(ctx context.Context, domain, text string, limit int) ([]string, error) {
	match := FTSQuery(text)
	if match == "" || limit <= 0 {
		return nil, nil
	}
	rows, err := db.sql.QueryContext(ctx, `
		SELECT f.id FROM entries_fts f JOIN entries e ON e.id = f.id
		WHERE entries_fts MATCH ? AND (? = '' OR e.domain = ?)
		ORDER BY bm25(entries_fts) LIMIT ?`, match, domain, domain, limit)
	if err != nil {
		return nil, fmt.Errorf("fts search: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
