package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func recallIDs(t *testing.T, s *MemoryStore, f Filter) map[string]bool {
	t.Helper()
	f.Domain, f.Mode = "codigo:t", ModeKeyword
	res, err := s.Search(context.Background(), "build tooling notes", 20, f, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, e := range res {
		out[e.ID] = true
	}
	for _, e := range s.Recent(20, f, time.Now()) {
		if !out[e.ID] {
			t.Errorf("Recent returned %s that Search did not", e.ID)
		}
	}
	return out
}

// echoClassifier extracts the content itself as the single fact and answers
// every Classify with a fixed relation.
type echoClassifier struct{ relation string }

func (echoClassifier) Extract(_ context.Context, content, _ string) ([]string, error) {
	return []string{content}, nil
}

func (e echoClassifier) Classify(context.Context, string, string) (string, error) {
	return e.relation, nil
}

func TestRecallScopes(t *testing.T) {
	s := newKeywordStore(t)
	g := addProv(t, s, "build tooling notes global", "manual", "", "")
	a := addProv(t, s, "build tooling notes for project a", "manual", "", "proj-a")
	b := addProv(t, s, "build tooling notes for project b", "manual", "", "proj-b")

	check := func(name string, f Filter, want ...string) {
		t.Helper()
		got := recallIDs(t, s, f)
		if len(got) != len(want) {
			t.Errorf("%s: got %d entries %v, want %v", name, len(got), got, want)
		}
		for _, id := range want {
			if !got[id] {
				t.Errorf("%s: missing %s", name, id)
			}
		}
	}
	gid, aid, bid := g.Entry.ID, a.Entry.ID, b.Entry.ID
	check("no filter", Filter{}, gid, aid, bid)
	check("layered a", Filter{Project: "proj-a"}, gid, aid)
	check("project-only a", Filter{Project: "proj-a", Scope: ScopeProjectOnly}, aid)
	check("global", Filter{Scope: ScopeGlobal}, gid)
}

func TestFilterValidate(t *testing.T) {
	bad := []Filter{
		{Scope: "bogus"},
		{Scope: ScopeGlobal, Project: "p"},
		{Scope: ScopeProjectOnly},
		{Project: "Not A Slug"},
	}
	for _, f := range bad {
		var verr *ValidationError
		if err := f.Validate(); !errors.As(err, &verr) {
			t.Errorf("Validate(%+v) = %v, want ValidationError", f, err)
		}
	}
	for _, f := range []Filter{{}, {Project: "p"}, {Scope: ScopeGlobal}, {Scope: ScopeProjectOnly, Project: "p"}} {
		if err := f.Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v", f, err)
		}
	}
}

// 1-hop expansion follows related_to links, which must never smuggle another
// project's memory into a scoped recall.
func TestRelatedExpansionRespectsScope(t *testing.T) {
	s := newKeywordStore(t)
	ctx := context.Background()
	a := addProv(t, s, "alpha topic in project a", "manual", "", "proj-a")
	b := addProv(t, s, "unrelated words in project b", "manual", "", "proj-b")
	link := func(from, to string) {
		s.mu.Lock()
		e := s.index[from]
		e.Metadata = withRelatedTo(e.Metadata, to)
		s.index[from] = e
		s.mu.Unlock()
	}
	link(a.Entry.ID, b.Entry.ID)

	res, err := s.Search(ctx, "alpha topic", 5, Filter{Domain: "codigo:t", Mode: ModeKeyword, Project: "proj-a"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range res {
		if e.ID == b.Entry.ID {
			t.Fatalf("project b entry leaked through related_to into proj-a recall: %+v", res)
		}
	}
	res, _ = s.Search(ctx, "alpha topic", 5, Filter{Domain: "codigo:t", Mode: ModeKeyword}, time.Now())
	found := false
	for _, e := range res {
		found = found || e.ID == b.Entry.ID
	}
	if !found {
		t.Error("unfiltered recall should still expand related entries")
	}
}

// A project fact that contradicts a global one overrides it inside that
// project only: layered recall drops the global entry, every other scope
// still sees it, and nothing is invalidated.
func TestProjectEntryOverridesContradictedGlobalEntry(t *testing.T) {
	embed := vectorEmbedServer(t, map[string][]float32{
		"the build uses make":  {1, 0, 0},
		"the build uses bazel": {0.8, 0.6, 0}, // cosine 0.8: related band
		"unrelated cooking":    {0, 0, 1},
	})
	s, err := New(t.TempDir(), "fake-key", embed.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.db.Close() })
	ctx := context.Background()
	s.SetExtractor(echoClassifier{relation: relationContradicts})

	add := func(content, project string) AddedEntry {
		t.Helper()
		out, err := s.Add(ctx, AddInput{Content: content, Domain: "codigo:t", Source: "manual", Project: project}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return out.Entries[0]
	}
	g := add("the build uses make", "")
	p := add("the build uses bazel", "proj-a")
	if p.Entry.Metadata[metadataKeyOverrides] != g.Entry.ID {
		t.Fatalf("project entry should override the global one: %+v", p.Entry.Metadata)
	}
	// An unrelated project entry overrides nothing.
	if o := add("unrelated cooking", "proj-a"); o.Entry.Metadata[metadataKeyOverrides] != "" {
		t.Errorf("unrelated entry overrides: %+v", o.Entry.Metadata)
	}

	has := func(f Filter, id string) bool {
		f.Domain, f.Mode = "codigo:t", ModeKeyword
		res, err := s.Search(ctx, "the build uses", 10, f, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range res {
			if e.ID == id {
				return true
			}
		}
		return false
	}
	if has(Filter{Project: "proj-a"}, g.Entry.ID) || !has(Filter{Project: "proj-a"}, p.Entry.ID) {
		t.Error("layered proj-a recall must show the project entry and hide the overridden global one")
	}
	if !has(Filter{Project: "proj-b"}, g.Entry.ID) || !has(Filter{Scope: ScopeGlobal}, g.Entry.ID) {
		t.Error("other scopes must still see the global entry")
	}
	if got, _ := s.Get(g.Entry.ID, time.Now()); got.Metadata[metadataKeyValidUntil] != "" {
		t.Error("the global entry must not be invalidated")
	}

	// Promoting the project entry to global clears the stale override.
	none := ""
	up, err := s.Update(ctx, p.Entry.ID, UpdatePatch{Project: &none}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if up.Project != nil || up.Metadata[metadataKeyOverrides] != "" {
		t.Fatalf("promotion should make the entry global and drop overrides: %+v", up)
	}
	if !has(Filter{Project: "proj-a"}, g.Entry.ID) {
		t.Error("after promotion the global entry is no longer overridden")
	}
}
