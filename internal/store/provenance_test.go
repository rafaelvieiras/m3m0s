package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newKeywordStore(t *testing.T) *MemoryStore {
	t.Helper()
	s, err := New(t.TempDir(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.db.Close() })
	return s
}

func addProv(t *testing.T, s *MemoryStore, content, source, model, project string) AddedEntry {
	t.Helper()
	out, err := s.Add(context.Background(), AddInput{Content: content, Domain: "code:t", Source: source, Model: model, Project: project}, time.Now())
	if err != nil {
		t.Fatalf("Add(%q): %v", content, err)
	}
	return out.Entries[0]
}

func TestProvenanceIsStoredAndFilterable(t *testing.T) {
	s := newKeywordStore(t)
	a := addProv(t, s, "deploy notes for the living room server", "claude-code", "claude-sonnet-5-5", "m3m0s")
	b := addProv(t, s, "deploy notes for the desktop", "opencode", "", "")
	if a.Entry.ModelValue() != "claude-sonnet-5-5" || a.Entry.ProjectValue() != "m3m0s" {
		t.Fatalf("provenance not stored: %+v", a.Entry)
	}
	if b.Entry.Model != nil || b.Entry.Project != nil {
		t.Fatalf("absent provenance must stay nil (global, unknown model): %+v", b.Entry)
	}

	ids := func(f Filter) []string {
		f.Domain, f.Mode = "code:t", ModeKeyword
		res, err := s.Search(context.Background(), "deploy notes", 10, f, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, len(res))
		for i, e := range res {
			out[i] = e.ID
		}
		return out
	}
	if got := ids(Filter{}); len(got) != 2 {
		t.Fatalf("no filter: %v", got)
	}
	if got := ids(Filter{Project: "m3m0s", Scope: ScopeProjectOnly}); len(got) != 1 || got[0] != a.Entry.ID {
		t.Errorf("project filter: %v", got)
	}
	if got := ids(Filter{Model: "claude-sonnet-5-5"}); len(got) != 1 || got[0] != a.Entry.ID {
		t.Errorf("model filter: %v", got)
	}
	if got := ids(Filter{Source: "opencode"}); len(got) != 1 || got[0] != b.Entry.ID {
		t.Errorf("source filter: %v", got)
	}
	if got := s.Recent(10, Filter{Project: "m3m0s", Scope: ScopeProjectOnly}, time.Now()); len(got) != 1 || got[0].ID != a.Entry.ID {
		t.Errorf("recent project filter: %v", got)
	}
}

// The same text in two scopes must stay two entries: a project memory never
// merges into (or overwrites) the global one.
func TestDedupDoesNotCrossProjects(t *testing.T) {
	s := newKeywordStore(t)
	g := addProv(t, s, "the build uses make", "manual", "", "")
	p := addProv(t, s, "the build uses make", "manual", "", "proj-a")
	if p.Deduped || p.Entry.ID == g.Entry.ID {
		t.Fatalf("project add merged into global entry: %+v", p)
	}
	again := addProv(t, s, "the build uses make", "manual", "", "proj-a")
	if !again.Deduped || again.Entry.ID != p.Entry.ID {
		t.Fatalf("same project should dedup: %+v", again)
	}
}

func TestProvenanceValidationAndUpdate(t *testing.T) {
	s := newKeywordStore(t)
	ctx := context.Background()
	for _, in := range []AddInput{
		{Content: "x", Domain: "code:t", Project: "Bad Project"},
		{Content: "x", Domain: "code:t", Model: "has space"},
	} {
		var verr *ValidationError
		if _, err := s.Add(ctx, in, time.Now()); !errors.As(err, &verr) {
			t.Errorf("Add(%+v) error = %v, want ValidationError", in, err)
		}
	}

	e := addProv(t, s, "some fact", "manual", "m1", "p1")
	proj, model := "p2", ""
	up, err := s.Update(ctx, e.Entry.ID, UpdatePatch{Project: &proj, Model: &model}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if up.ProjectValue() != "p2" || up.Model != nil {
		t.Fatalf("update: %+v", up)
	}
	global := ""
	up, err = s.Update(ctx, e.Entry.ID, UpdatePatch{Project: &global}, time.Now())
	if err != nil || up.Project != nil {
		t.Fatalf("clearing project: %v %+v", err, up)
	}
	bad := "UPPER"
	var verr *ValidationError
	if _, err := s.Update(ctx, e.Entry.ID, UpdatePatch{Project: &bad}, time.Now()); !errors.As(err, &verr) || verr.Code != "invalid_project" {
		t.Errorf("update with invalid project: %v", err)
	}
}
