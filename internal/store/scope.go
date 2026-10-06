package store

import (
	"context"
	"strings"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// Scope values of Filter.Scope. With no scope and a project set, recall is
// layered: the global memory plus that project's entries (docs/memory-schema.md,
// D34). With no scope and no project nothing is filtered by project.
const (
	// ScopeGlobal keeps only entries that belong to no project.
	ScopeGlobal = "global"
	// ScopeProjectOnly keeps only the entries of Filter.Project.
	ScopeProjectOnly = "project-only"
)

// metadataKeyOverrides lists the IDs of global entries a project entry
// supersedes (comma-separated, like related_to). Layered recall drops an
// overridden global entry when the project entry that overrides it is in
// scope, so "the project wins on conflict" without invalidating the global
// fact for every other project.
const metadataKeyOverrides = "overrides"

// Validate checks the scope/project combination of a filter.
func (f Filter) Validate() error {
	switch f.Scope {
	case "":
	case ScopeGlobal:
		if f.Project != "" {
			return &ValidationError{Code: "invalid_scope", Message: `scope "global" cannot be combined with a project`}
		}
	case ScopeProjectOnly:
		if f.Project == "" {
			return &ValidationError{Code: "invalid_scope", Message: `scope "project-only" requires a project`}
		}
	default:
		return &ValidationError{Code: "invalid_scope", Message: `invalid scope: ` + f.Scope + ` (expected "global" or "project-only")`}
	}
	if f.Project != "" {
		if err := schema.ValidateProject(f.Project); err != nil {
			return &ValidationError{Code: "invalid_project", Message: err.Error()}
		}
	}
	return nil
}

// matchesProject applies the project scope of the filter to an entry.
func (f Filter) matchesProject(e schema.Entry) bool {
	switch f.Scope {
	case ScopeGlobal:
		return e.Project == nil
	case ScopeProjectOnly:
		return e.ProjectValue() == f.Project
	}
	if f.Project == "" {
		return true
	}
	p := e.ProjectValue()
	return p == "" || p == f.Project
}

// layered reports whether the filter asks for global + one project.
func (f Filter) layered() bool { return f.Scope == "" && f.Project != "" }

// dropOverridden removes, from layered results, the global entries that a
// project entry in the same results overrides.
func dropOverridden(entries []schema.Entry) []schema.Entry {
	overridden := map[string]struct{}{}
	for _, e := range entries {
		if e.Project == nil {
			continue
		}
		for _, id := range splitRelatedTo(e.Metadata[metadataKeyOverrides]) {
			overridden[id] = struct{}{}
		}
	}
	if len(overridden) == 0 {
		return entries
	}
	out := make([]schema.Entry, 0, len(entries))
	for _, e := range entries {
		if _, drop := overridden[e.ID]; drop && e.Project == nil {
			continue
		}
		out = append(out, e)
	}
	return out
}

// withOverrides returns a copy of base with id added to metadataKeyOverrides.
func withOverrides(base map[string]string, id string) map[string]string {
	md := make(map[string]string, len(base)+1)
	for k, v := range base {
		md[k] = v
	}
	for _, existing := range splitRelatedTo(md[metadataKeyOverrides]) {
		if existing == id {
			return md
		}
	}
	md[metadataKeyOverrides] = strings.Join(append(splitRelatedTo(md[metadataKeyOverrides]), id), ",")
	return md
}

// findOverridden decides whether the new project entry supersedes a global
// one: the nearest global entry of the domain counts when it is a
// near-duplicate, or in the related band and the Classifier says the new
// fact contradicts it. Without embeddings or a Classifier only the
// near-duplicate case applies. Nothing is invalidated: the global fact stays
// valid for every other scope.
func (s *MemoryStore) findOverridden(ctx context.Context, entry schema.Entry, vec []float32, now time.Time) (string, bool) {
	if entry.Project == nil || len(vec) == 0 {
		return "", false
	}
	scores, err := s.collectCandidates(ctx, entry.Domain, "", vec, 1, ModeVector, func(e schema.Entry) bool {
		return e.Project == nil && !isSoftDeleted(e) && !isExpired(e, now)
	})
	if err != nil || len(scores) == 0 {
		return "", false
	}
	cand := scores[0]
	switch similarityBand(cand.similarity) {
	case "dedup":
		return cand.entry.ID, true
	case "related":
		if cl, ok := s.extractor.(Classifier); ok {
			if r, err := cl.Classify(ctx, entry.Content, cand.entry.Content); err == nil && r == relationContradicts {
				return cand.entry.ID, true
			}
		}
	}
	return "", false
}
