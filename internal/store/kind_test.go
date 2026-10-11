package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/factkind"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// kindExtractor is a fake provider that returns scripted facts with kinds on
// extraction and scripted kinds on classification.
type kindExtractor struct {
	facts    []factkind.Fact
	classify func(texts []string) ([]string, error)
	calls    atomic.Int32 // ClassifyKinds calls
}

func (k *kindExtractor) Extract(ctx context.Context, content, domain string) ([]string, error) {
	f, err := k.ExtractFacts(ctx, content, domain)
	out := make([]string, len(f))
	for i := range f {
		out[i] = f[i].Text
	}
	return out, err
}

func (k *kindExtractor) ExtractFacts(context.Context, string, string) ([]factkind.Fact, error) {
	return k.facts, nil
}

func (k *kindExtractor) ClassifyKinds(_ context.Context, texts []string) ([]string, error) {
	k.calls.Add(1)
	if k.classify != nil {
		return k.classify(texts)
	}
	out := make([]string, len(texts))
	for i := range out {
		out[i] = factkind.Rule
	}
	return out, nil
}

// stripKind turns an entry into a legacy row: no kind keys, in the index and
// in SQLite (what every row written before D38 looks like).
func stripKind(t *testing.T, s *MemoryStore, id string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.index[id]
	e.Metadata = withoutKey(withoutKey(e.Metadata, factkind.MetadataKey), factkind.MetadataSourceKey)
	if err := s.db.UpdateMetadata(context.Background(), id, e.Metadata, e.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	s.index[id] = e
}

func entryKind(t *testing.T, s *MemoryStore, id string) (string, string) {
	t.Helper()
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.index[id]
	if !ok {
		t.Fatalf("entry %s not found", id)
	}
	return e.KindValue(), e.KindSourceValue()
}

func TestAddTagsKindFromLLM(t *testing.T) {
	s := newTestStore(t)
	s.SetExtractor(&kindExtractor{facts: []factkind.Fact{
		{Text: "m|!x(*u|", Kind: factkind.Gotcha},
		{Text: "8'z'^x)%", Kind: factkind.State},
	}})
	out, err := s.Add(context.Background(), AddInput{Content: "original note", Domain: "code:demo"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 2 {
		t.Fatalf("got %d entries", len(out.Entries))
	}
	want := []string{factkind.Gotcha, factkind.State}
	for i, a := range out.Entries {
		k, src := entryKind(t, s, a.Entry.ID)
		if k != want[i] || src != factkind.SourceLLM {
			t.Errorf("entry %d: kind=%s source=%s, want %s/llm", i, k, src, want[i])
		}
		if a.Entry.KindValue() != want[i] {
			t.Errorf("returned entry kind = %s", a.Entry.KindValue())
		}
	}
}

func TestAddWithPlainExtractorFallsBackToHeuristic(t *testing.T) {
	s := newTestStore(t)
	// fakeExtractor only implements Extract: no kinds come back.
	s.SetExtractor(&fakeExtractor{facts: []string{"We decided to use SQLite instead of Postgres because it is embedded."}})
	out, err := s.Add(context.Background(), AddInput{Content: "x", Domain: "code:demo"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	k, src := entryKind(t, s, out.Entries[0].Entry.ID)
	if k != factkind.Decision || src != factkind.SourceHeuristic {
		t.Fatalf("kind=%s source=%s, want decision/heuristic", k, src)
	}
}

func TestAddWithoutExtractorUsesHeuristic(t *testing.T) {
	s := newTestStore(t)
	for i, c := range []struct{ text, kind string }{
		{"The root cause was a stale lockfile left by the previous run.", factkind.Gotcha},
		{"Status: migration in progress, 3 of 9 tasks done.", factkind.State},
		{"Plain sentence with no markers at all.", factkind.Other},
	} {
		out, err := s.Add(context.Background(), AddInput{Content: c.text, Domain: fmt.Sprintf("code:case%d", i)}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		k, src := entryKind(t, s, out.Entries[0].Entry.ID)
		if k != c.kind || src != factkind.SourceHeuristic {
			t.Errorf("%q: kind=%s source=%s, want %s/heuristic", c.text, k, src, c.kind)
		}
	}
}

func TestRawFallbackAfterExtractionFailureUsesHeuristic(t *testing.T) {
	s := newTestStore(t)
	s.SetExtractor(&fakeExtractor{err: errors.New("provider down")})
	out, err := s.Add(context.Background(), AddInput{Content: "Never commit generated files to the repo.", Domain: "code:demo"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if out.Extracted {
		t.Fatal("expected the raw fallback")
	}
	k, src := entryKind(t, s, out.Entries[0].Entry.ID)
	if k != factkind.Rule || src != factkind.SourceHeuristic {
		t.Fatalf("kind=%s source=%s, want rule/heuristic", k, src)
	}
}

func TestLegacyRowReadsAsOtherAndIsNotRewrittenAtBoot(t *testing.T) {
	dir := t.TempDir()
	embed := fakeEmbedServer(t)
	s, err := New(dir, "fake-key", embed.URL)
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.Add(context.Background(), AddInput{Content: "Legacy row from before the tag existed.", Domain: "code:demo"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	id := out.Entries[0].Entry.ID
	stripKind(t, s, id)
	before, _ := s.db.LoadAllEntries(context.Background())
	_ = s.db.Close()

	s2, err := New(dir, "fake-key", embed.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s2.db.Close() })
	got, err := s2.Get(id, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.KindValue() != factkind.Other || got.KindSourceValue() != "" {
		t.Fatalf("legacy row reads as %s/%q", got.KindValue(), got.KindSourceValue())
	}
	after, _ := s2.db.LoadAllEntries(context.Background())
	if len(before) != len(after) {
		t.Fatal("row count changed")
	}
	for i := range before {
		if _, has := after[i].Metadata[factkind.MetadataKey]; has {
			t.Errorf("opening the store tagged a legacy row: %v", after[i].Metadata)
		}
		if !before[i].UpdatedAt.Equal(after[i].UpdatedAt) {
			t.Errorf("opening the store touched updated_at of %s", before[i].ID)
		}
	}
}

func TestFilterByKind(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	rule, _ := s.Add(ctx, AddInput{Content: "Always run the linter before pushing.", Domain: "code:one"}, time.Now())
	state, _ := s.Add(ctx, AddInput{Content: "Status: waiting for review on branch alpha.", Domain: "code:two"}, time.Now())
	legacy, _ := s.Add(ctx, AddInput{Content: "Zzz qqq xxx unrelated words here.", Domain: "code:three"}, time.Now())
	stripKind(t, s, legacy.Entries[0].Entry.ID)

	ids := func(es []schema.Entry) string {
		var out []string
		for _, e := range es {
			out = append(out, e.ID)
		}
		return strings.Join(out, ",")
	}
	if got := ids(s.Recent(10, Filter{Kind: factkind.Rule}, time.Now())); got != rule.Entries[0].Entry.ID {
		t.Errorf("recent kind=rule: %s", got)
	}
	if got := ids(s.Recent(10, Filter{Kind: factkind.State}, time.Now())); got != state.Entries[0].Entry.ID {
		t.Errorf("recent kind=state: %s", got)
	}
	if got := ids(s.Recent(10, Filter{Kind: factkind.Other}, time.Now())); got != legacy.Entries[0].Entry.ID {
		t.Errorf("recent kind=other must include unclassified rows: %s", got)
	}
	res, err := s.Search(ctx, "waiting for review", 10, Filter{Domain: "code:two", Kind: factkind.State}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].ID != state.Entries[0].Entry.ID {
		t.Errorf("search with kind=state: %v", res)
	}
	res, err = s.Search(ctx, "waiting for review", 10, Filter{Domain: "code:two", Kind: factkind.Rule}, time.Now())
	if err != nil || len(res) != 0 {
		t.Errorf("search with a non-matching kind must be empty: %v %v", res, err)
	}
	var verr *ValidationError
	if err := (Filter{Kind: "bogus"}).Validate(); !errors.As(err, &verr) || verr.Code != "invalid_kind" {
		t.Errorf("Validate(bogus) = %v", err)
	}
}

func TestUpdateKindSetsManualAndSurvivesMetadataReplace(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	e := mustAdd(t, s, AddInput{Content: "Some fact worth keeping.", Domain: "code:demo"})

	if _, err := s.Update(ctx, e.ID, UpdatePatch{Kind: strPtr("bogus")}, time.Now()); err == nil {
		t.Fatal("expected invalid_kind")
	} else {
		var verr *ValidationError
		if !errors.As(err, &verr) || verr.Code != "invalid_kind" {
			t.Fatalf("got %v", err)
		}
	}
	got, err := s.Update(ctx, e.ID, UpdatePatch{Kind: strPtr(factkind.Preference)}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.KindValue() != factkind.Preference || got.KindSourceValue() != factkind.SourceManual {
		t.Fatalf("got %s/%s", got.KindValue(), got.KindSourceValue())
	}
	// A metadata replacement keeps the human correction and cannot forge one.
	got, err = s.Update(ctx, e.ID, UpdatePatch{Metadata: metaPtr(map[string]string{"k": "v", "kind": "state", "kind_source": "llm"})}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.KindValue() != factkind.Preference || got.KindSourceValue() != factkind.SourceManual || got.Metadata["k"] != "v" {
		t.Fatalf("after metadata replace: %s/%s %v", got.KindValue(), got.KindSourceValue(), got.Metadata)
	}
}

func TestMetadataReplaceDropsMachineTagButKindAndMetadataTogetherWork(t *testing.T) {
	s := newTestStore(t)
	e := mustAdd(t, s, AddInput{Content: "Never push to main directly.", Domain: "code:demo"})
	if e.KindSourceValue() != factkind.SourceHeuristic {
		t.Fatalf("setup: %s", e.KindSourceValue())
	}
	got, err := s.Update(context.Background(), e.ID, UpdatePatch{Metadata: metaPtr(map[string]string{}), Kind: strPtr(factkind.Rule)}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.KindValue() != factkind.Rule || got.KindSourceValue() != factkind.SourceManual {
		t.Fatalf("got %s/%s", got.KindValue(), got.KindSourceValue())
	}
}

func TestDedupKeepsManualKind(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	first := mustAdd(t, s, AddInput{Content: "Always rebase before merging.", Domain: "code:demo"})
	if _, err := s.Update(ctx, first.ID, UpdatePatch{Kind: strPtr(factkind.Preference)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	out, err := s.Add(ctx, AddInput{Content: "Always rebase before merging.", Domain: "code:demo"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !out.Entries[0].Deduped || out.Entries[0].Entry.ID != first.ID {
		t.Fatalf("expected a dedup merge into %s", first.ID)
	}
	if k, src := entryKind(t, s, first.ID); k != factkind.Preference || src != factkind.SourceManual {
		t.Fatalf("manual tag lost on dedup: %s/%s", k, src)
	}
}

func TestDedupAdoptsLLMKindOverHeuristicAndLegacy(t *testing.T) {
	merged := map[string]string{factkind.MetadataKey: factkind.Other, factkind.MetadataSourceKey: factkind.SourceHeuristic}
	llm := map[string]string{factkind.MetadataKey: factkind.Gotcha, factkind.MetadataSourceKey: factkind.SourceLLM}
	got := adoptKind(merged, llm)
	if got[factkind.MetadataKey] != factkind.Gotcha || got[factkind.MetadataSourceKey] != factkind.SourceLLM {
		t.Errorf("heuristic should yield to llm: %v", got)
	}
	// legacy (no tag) adopts a heuristic one
	h := map[string]string{factkind.MetadataKey: factkind.Rule, factkind.MetadataSourceKey: factkind.SourceHeuristic}
	if got := adoptKind(map[string]string{}, h); got[factkind.MetadataKey] != factkind.Rule {
		t.Errorf("legacy should adopt: %v", got)
	}
	// llm is never replaced, manual neither
	if got := adoptKind(llm, h); got[factkind.MetadataKey] != factkind.Gotcha {
		t.Errorf("llm replaced: %v", got)
	}
	man := map[string]string{factkind.MetadataKey: factkind.State, factkind.MetadataSourceKey: factkind.SourceManual}
	if got := adoptKind(man, llm); got[factkind.MetadataKey] != factkind.State {
		t.Errorf("manual replaced: %v", got)
	}
}

func TestRetroactiveExtractionTagsFacts(t *testing.T) {
	s := newTestStore(t)
	out, err := s.Add(context.Background(), AddInput{Content: "Raw digest stored before extraction was enabled.", Domain: "code:demo"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s.SetExtractor(&kindExtractor{facts: []factkind.Fact{{Text: "m|!x(*u|", Kind: factkind.Procedure}}})
	n, err := s.RunRetroactiveExtraction(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("processed=%d err=%v", n, err)
	}
	if _, err := s.Get(out.Entries[0].Entry.ID, time.Now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("original should be replaced: %v", err)
	}
	found := false
	for _, e := range s.Recent(10, Filter{Kind: factkind.Procedure}, time.Now()) {
		found = e.KindSourceValue() == factkind.SourceLLM
	}
	if !found {
		t.Fatal("extracted fact is not tagged procedure/llm")
	}
}

// ── backfill ──

// seedLegacy adds n distinct entries with the given contents and strips their
// tags, returning the ids.
func seedLegacy(t *testing.T, s *MemoryStore, contents ...string) []string {
	t.Helper()
	var ids []string
	for i, c := range contents {
		// one domain per entry: dedup only compares inside a domain, and the fake
		// embedding makes unrelated sentences look alike
		out, err := s.Add(context.Background(), AddInput{Content: c, Domain: fmt.Sprintf("code:seed%d", i)}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		id := out.Entries[0].Entry.ID
		stripKind(t, s, id)
		ids = append(ids, id)
	}
	return ids
}

func TestBackfillRequiresConfirmationAndNeverOverwritesManual(t *testing.T) {
	s := newTestStore(t)
	ids := seedLegacy(t, s, "m|!x(*u|", "8'z'^x)%", "q#w$e%r^")
	ke := &kindExtractor{}
	s.SetExtractor(ke)
	ctx := context.Background()

	// A human corrects one; the backfill must not touch it.
	if _, err := s.Update(ctx, ids[0], UpdatePatch{Kind: strPtr(factkind.Decision)}, time.Now()); err != nil {
		t.Fatal(err)
	}

	if _, err := s.RunKindBackfill(ctx); !errors.Is(err, ErrKindBackfillNotConfirmed) {
		t.Fatalf("unconfirmed run: %v", err)
	}
	if ke.calls.Load() != 0 {
		t.Fatal("provider called without confirmation")
	}

	s.ConfirmKindBackfill()
	if _, err := s.RunKindBackfill(ctx); err != nil {
		t.Fatal(err)
	}
	if k, src := entryKind(t, s, ids[0]); k != factkind.Decision || src != factkind.SourceManual {
		t.Errorf("manual overwritten: %s/%s", k, src)
	}
	for _, id := range ids[1:] {
		if k, src := entryKind(t, s, id); k != factkind.Rule || src != factkind.SourceLLM {
			t.Errorf("%s: %s/%s, want rule/llm", id, k, src)
		}
	}
	p := s.KindBackfillStatus()
	if p.Done != 2 || p.Total != 2 || p.Running {
		t.Errorf("progress = %+v", p)
	}
	// The confirmation was consumed: a second LLM run needs a new one.
	if _, err := s.RunKindBackfill(ctx); !errors.Is(err, ErrKindBackfillNotConfirmed) {
		t.Errorf("confirmation should be single-use: %v", err)
	}
}

func TestBackfillDoesNotTouchContentOrUpdatedAt(t *testing.T) {
	s := newTestStore(t)
	ids := seedLegacy(t, s, "m|!x(*u|")
	s.SetExtractor(&kindExtractor{})
	before := s.index[ids[0]]
	s.ConfirmKindBackfill()
	if _, err := s.RunKindBackfill(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := s.index[ids[0]]
	if after.Content != before.Content || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatal("backfill changed content or updated_at")
	}
	rows, _ := s.db.LoadAllEntries(context.Background())
	if rows[0].Metadata[factkind.MetadataKey] != factkind.Rule {
		t.Fatalf("tag not persisted: %v", rows[0].Metadata)
	}
}

func TestBackfillIsResumableAndRateLimited(t *testing.T) {
	s := newTestStore(t)
	ids := seedLegacy(t, s, "m|!x(*u|", "8'z'^x)%", "q#w$e%r^", "p0o9i8u7", "l;k:j.h,")
	s.SetKindBackfillOptions(KindBackfillOptions{BatchSize: 2, RatePerSec: 1000})
	var failNext atomic.Bool
	failNext.Store(true)
	ke := &kindExtractor{classify: func(texts []string) ([]string, error) {
		if len(texts) == 2 && failNext.CompareAndSwap(true, false) {
			return nil, errors.New("provider hiccup")
		}
		out := make([]string, len(texts))
		for i := range out {
			out[i] = factkind.Noise
		}
		return out, nil
	}}
	s.SetExtractor(ke)
	ctx := context.Background()

	s.ConfirmKindBackfill()
	if _, err := s.RunKindBackfill(ctx); err != nil {
		t.Fatal(err)
	}
	p := s.KindBackfillStatus()
	if p.Errors != 2 || p.Done != 3 {
		t.Fatalf("first run: %+v", p)
	}
	// The failed batch stayed pending; a new run finishes exactly that.
	s.ConfirmKindBackfill()
	if _, err := s.RunKindBackfill(ctx); err != nil {
		t.Fatal(err)
	}
	p = s.KindBackfillStatus()
	if p.Total != 2 || p.Done != 2 || p.Errors != 0 {
		t.Fatalf("second run: %+v", p)
	}
	for _, id := range ids {
		if k, src := entryKind(t, s, id); k != factkind.Noise || src != factkind.SourceLLM {
			t.Errorf("%s: %s/%s", id, k, src)
		}
	}
	s.ConfirmKindBackfill()
	if _, err := s.RunKindBackfill(ctx); err != nil {
		t.Fatal(err)
	}
	if p := s.KindBackfillStatus(); p.Total != 0 {
		t.Errorf("nothing should be pending: %+v", p)
	}
}

func TestBackfillUpgradesHeuristicTagsAndAbortsOnOutage(t *testing.T) {
	s := newTestStore(t)
	// created with no extractor: heuristic tags
	out, _ := s.Add(context.Background(), AddInput{Content: "m|!x(*u|", Domain: "code:demo"}, time.Now())
	id := out.Entries[0].Entry.ID
	if _, src := entryKind(t, s, id); src != factkind.SourceHeuristic {
		t.Fatalf("setup source %s", src)
	}
	s.SetKindBackfillOptions(KindBackfillOptions{BatchSize: 1, RatePerSec: 1000})
	s.SetExtractor(&kindExtractor{classify: func(texts []string) ([]string, error) { return []string{factkind.Gotcha}, nil }})
	s.ConfirmKindBackfill()
	if _, err := s.RunKindBackfill(context.Background()); err != nil {
		t.Fatal(err)
	}
	if k, src := entryKind(t, s, id); k != factkind.Gotcha || src != factkind.SourceLLM {
		t.Fatalf("heuristic tag not upgraded: %s/%s", k, src)
	}

	// an outage stops the run after a few consecutive failed batches
	s2 := newTestStore(t)
	seedLegacy(t, s2, "a1", "b2", "c3", "d4", "e5", "f6", "g7")
	s2.SetKindBackfillOptions(KindBackfillOptions{BatchSize: 1, RatePerSec: 1000})
	var calls atomic.Int32
	s2.SetExtractor(&kindExtractor{classify: func([]string) ([]string, error) {
		calls.Add(1)
		return nil, errors.New("down")
	}})
	s2.ConfirmKindBackfill()
	if _, err := s2.RunKindBackfill(context.Background()); err == nil {
		t.Fatal("expected the run to give up")
	}
	if calls.Load() != kindAbortAfterFailures {
		t.Errorf("calls = %d, want %d", calls.Load(), kindAbortAfterFailures)
	}
}

func TestBackfillWithoutClassifierUsesFreeHeuristic(t *testing.T) {
	s := newTestStore(t)
	ids := seedLegacy(t, s, "The root cause was a stale socket file.", "Plain words only here.")
	est := s.EstimateKindBackfill()
	if est.Mode != "heuristic" || est.Pending != 2 || est.CostUSD != nil {
		t.Fatalf("estimate = %+v", est)
	}
	// No confirmation needed: it costs nothing.
	if _, err := s.RunKindBackfill(context.Background()); err != nil {
		t.Fatal(err)
	}
	if k, src := entryKind(t, s, ids[0]); k != factkind.Gotcha || src != factkind.SourceHeuristic {
		t.Errorf("%s/%s", k, src)
	}
	if k, src := entryKind(t, s, ids[1]); k != factkind.Other || src != factkind.SourceHeuristic {
		t.Errorf("%s/%s", k, src)
	}
	// Heuristic tags are final in this mode: a rerun has nothing to do.
	if _, err := s.RunKindBackfill(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p := s.KindBackfillStatus(); p.Total != 0 {
		t.Errorf("rerun total = %d", p.Total)
	}
}

func TestBackfillSkipsEntryEditedMeanwhile(t *testing.T) {
	s := newTestStore(t)
	ids := seedLegacy(t, s, "m|!x(*u|")
	ke := &kindExtractor{}
	ke.classify = func(texts []string) ([]string, error) {
		// the entry is edited while the provider call is in flight
		if _, err := s.Update(context.Background(), ids[0], UpdatePatch{Content: strPtr("8'z'^x)% new text")}, time.Now()); err != nil {
			t.Error(err)
		}
		return []string{factkind.State}, nil
	}
	s.SetExtractor(ke)
	s.ConfirmKindBackfill()
	if _, err := s.RunKindBackfill(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p := s.KindBackfillStatus(); p.Skipped != 1 || p.Done != 0 {
		t.Fatalf("progress = %+v", p)
	}
	if _, src := entryKind(t, s, ids[0]); src == factkind.SourceLLM {
		t.Fatal("stale classification was applied to edited content")
	}
}

func TestEstimateKindBackfillLLM(t *testing.T) {
	t.Setenv("M3M0S_EXTRACT_PRICE_IN_PER_MTOK", "0.5")
	t.Setenv("M3M0S_EXTRACT_PRICE_OUT_PER_MTOK", "2")
	s := newTestStore(t)
	seedLegacy(t, s, "m|!x(*u|", "8'z'^x)%", "q#w$e%r^")
	s.SetKindBackfillOptions(KindBackfillOptions{BatchSize: 2})
	s.SetExtractor(&kindExtractor{})
	est := s.EstimateKindBackfill()
	if est.Mode != "llm" || est.Pending != 3 || est.Requests != 2 || est.InputTokens <= 0 || est.OutputTokens <= 0 {
		t.Fatalf("estimate = %+v", est)
	}
	if est.CostUSD == nil || *est.CostUSD <= 0 {
		t.Fatalf("cost = %v", est.CostUSD)
	}
	t.Setenv("M3M0S_EXTRACT_PRICE_IN_PER_MTOK", "")
	if est := s.EstimateKindBackfill(); est.CostUSD != nil {
		t.Errorf("cost without prices should be unknown, got %v", *est.CostUSD)
	}
}

// ── distribution and sample ──

func TestKindDistributionAndSample(t *testing.T) {
	s := newTestStore(t)
	seedLegacy(t, s, "m|!x(*u|")
	ctx := context.Background()
	for i, c := range []string{"Always run the formatter before committing.", "Status: waiting for review on branch beta."} {
		if _, err := s.Add(ctx, AddInput{Content: c, Domain: fmt.Sprintf("code:dist%d", i)}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Add(ctx, AddInput{Content: fmt.Sprintf("Release notes live at the wiki, key %s.", "sk-abcdefghijklmnopqrstuvwx"), Domain: "docs:demo"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	by, bySrc := s.KindDistribution()
	counts := map[string]int{}
	for _, c := range by {
		counts[c.Key] = c.Count
	}
	if counts[factkind.Rule] != 1 || counts[factkind.State] != 1 || counts[factkind.Other] != 2 {
		t.Errorf("by kind = %v", counts)
	}
	if len(by) != len(factkind.All) || by[0].Key != factkind.Decision {
		t.Errorf("distribution must list every kind in enum order: %v", by)
	}
	srcs := map[string]int{}
	for _, c := range bySrc {
		srcs[c.Key] = c.Count
	}
	if srcs["none"] != 1 || srcs[factkind.SourceHeuristic] != 3 {
		t.Errorf("by source = %v", srcs)
	}

	a := s.KindSample(3, 7, time.Now())
	b := s.KindSample(3, 7, time.Now())
	if len(a) != 3 {
		t.Fatalf("sample size %d", len(a))
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Fatal("same seed must give the same sample")
		}
		if a[i].Label != "" {
			t.Error("labels must start empty")
		}
		if strings.Contains(a[i].Content, "sk-abcdefghijklmnopqrstuvwx") {
			t.Error("secret leaked into the sample")
		}
	}
	domains := map[string]bool{}
	for _, it := range s.KindSample(2, 1, time.Now()) {
		domains[it.Domain] = true
	}
	if len(domains) != 2 {
		t.Errorf("a 2-item sample should span both domain categories, got %v", domains)
	}
	if len(s.KindSample(100, 1, time.Now())) != 4 {
		t.Error("sample larger than the data should return everything")
	}
}
