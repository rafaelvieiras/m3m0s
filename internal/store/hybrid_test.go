package store

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func addText(t *testing.T, s *MemoryStore, domain, content string) AddOutput {
	t.Helper()
	out, err := s.Add(context.Background(), AddInput{Content: content, Domain: domain, Source: "test"}, time.Now())
	if err != nil {
		t.Fatalf("Add(%q): %v", content, err)
	}
	return out
}

func searchIDs(t *testing.T, s *MemoryStore, query, mode string) []string {
	t.Helper()
	res, err := s.Search(context.Background(), query, 5, Filter{Domain: "codigo:t", Mode: mode}, time.Now())
	if err != nil {
		t.Fatalf("Search(%q, %s): %v", query, mode, err)
	}
	ids := make([]string, len(res))
	for i, e := range res {
		ids[i] = e.ID
	}
	return ids
}

func contains(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// Keyword-only: no embedding key. Add stores without a vector, exact
// duplicates are still merged, search answers from FTS5 and results carry no
// cosine score.
func TestKeywordOnlyServerAddsDedupsAndSearches(t *testing.T) {
	s, err := New(t.TempDir(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.db.Close() })
	if s.HasEmbeddings() {
		t.Fatal("no key must mean keyword-only")
	}

	a := addText(t, s, "codigo:t", "The deploy runs on the living room notebook")
	addText(t, s, "codigo:t", "Carrot cake recipe with chocolate")
	again := addText(t, s, "codigo:t", "The deploy runs on the living room notebook")
	if len(a.Entries) != 1 || len(again.Entries) != 1 || !again.Entries[0].Deduped || again.Entries[0].Entry.ID != a.Entries[0].Entry.ID {
		t.Fatalf("exact duplicate should dedup to the same entry: %+v / %+v", a, again)
	}
	if n, _ := s.Stats(); n != 2 {
		t.Fatalf("entries = %d, want 2", n)
	}

	res, err := s.Search(context.Background(), "notebook deploy", 5, Filter{Domain: "codigo:t"}, time.Now())
	if err != nil || len(res) != 1 || res[0].ID != a.Entries[0].Entry.ID {
		t.Fatalf("keyword search = %+v (%v)", res, err)
	}
	if res[0].Score != nil {
		t.Errorf("keyword-only results must not carry a cosine score, got %v", *res[0].Score)
	}
}

// The embedding provider failing must not break search: hybrid degrades to
// keyword; explicit vector mode still reports the error.
func TestHybridFallsBackToKeywordWhenEmbeddingFails(t *testing.T) {
	var failing atomic.Bool
	embedSrv := fakeEmbedServer(t)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		embedSrv.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(proxy.Close)
	s, err := New(t.TempDir(), "k", proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.db.Close() })
	e := addText(t, s, "codigo:t", "Authelia protects the web interface")

	failing.Store(true)
	if ids := searchIDs(t, s, "authelia interface", ""); !contains(ids, e.Entries[0].Entry.ID) {
		t.Errorf("hybrid should fall back to keyword, got %v", ids)
	}
	if _, err := s.Search(context.Background(), "authelia", 5, Filter{Domain: "codigo:t", Mode: ModeVector}, time.Now()); err == nil {
		t.Error("explicit vector mode should surface the embedding error")
	}
}

// With the 8-dimension fake embedding, vector ranking is nearly meaningless;
// the keyword side must still bring the right entry in hybrid mode.
func TestHybridFindsExactTermsThatVectorsMiss(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 40; i++ {
		addText(t, s, "codigo:t", "filler note number "+string(rune('a'+i%26))+string(rune('A'+i/26))+" about nothing in particular")
	}
	target := addText(t, s, "codigo:t", "zebracorn is the codename of the migration")

	if ids := searchIDs(t, s, "zebracorn", ModeKeyword); len(ids) == 0 || ids[0] != target.Entries[0].Entry.ID {
		t.Fatalf("keyword mode = %v", ids)
	}
	if ids := searchIDs(t, s, "zebracorn", ModeHybrid); len(ids) == 0 || ids[0] != target.Entries[0].Entry.ID {
		t.Errorf("hybrid should rank the keyword hit first, got %v", ids)
	}
}

// Add embeds the content once: findDuplicate's vector is reused for storage.
func TestAddEmbedsContentOnlyOnce(t *testing.T) {
	var calls atomic.Int32
	embedSrv := fakeEmbedServer(t)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		embedSrv.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(proxy.Close)
	s, err := New(t.TempDir(), "k", proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.db.Close() })
	addText(t, s, "codigo:t", "a single fact")
	if got := calls.Load(); got != 1 {
		t.Errorf("embedding calls for one Add = %d, want 1", got)
	}
}

func TestFuseRewardsAgreementBetweenRankers(t *testing.T) {
	got := fuse([]string{"a", "b", "c"}, []string{"c", "b", "d"})
	// b is 2nd+2nd, c is 3rd+1st: both beat a (1st only) and d (3rd only).
	if got[0] != "b" && got[0] != "c" {
		t.Errorf("fused order = %v", got)
	}
	if len(got) != 4 {
		t.Errorf("union size = %d, want 4", len(got))
	}
}
