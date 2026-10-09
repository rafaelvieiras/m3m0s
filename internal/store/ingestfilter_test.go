package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/ingestfilter"
)

// Synthetic facts. The "noise" strings keep the fake embedding from treating
// unrelated facts as duplicates (see the comment in retroextract_test.go).
const (
	realFact1   = "m|!x(*u|"
	realFact2   = "8'z'^x)%"
	leakFact    = "Each atomic fact must be self-contained and reusable in future searches."
	triviaFact  = "The commit changed 9 files with 103 insertions and 4 deletions."
	continuity  = "This session is being continued from a previous conversation that ran out of context. The summary below covers the earlier portion of the conversation.\n\nSummary: nothing real."
	realMessage = "[user]\nPlease add retries to the uploader."
)

func filterContents(out AddOutput) []string {
	var cs []string
	for _, e := range out.Entries {
		cs = append(cs, e.Entry.Content)
	}
	return cs
}

func TestCaptureDropsBoilerplateFactsAndKeepsRealOnes(t *testing.T) {
	s := newTestStore(t)
	s.SetExtractor(&fakeExtractor{facts: []string{realFact1, leakFact, realFact2, triviaFact}})

	out, err := s.Add(context.Background(), AddInput{Content: realMessage, Domain: "code:demo", Capture: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(filterContents(out), "|")
	if strings.Contains(got, "self-contained") {
		t.Errorf("leak survived: %s", got)
	}
	if !strings.Contains(got, realFact1) || !strings.Contains(got, realFact2) {
		t.Errorf("real facts lost: %s", got)
	}
	if out.Filtered != 1 {
		t.Errorf("Filtered = %d, want 1", out.Filtered)
	}
	// diff trivia is tag-only by default: stored, marked.
	var tagged int
	for _, e := range out.Entries {
		if e.Entry.Content == triviaFact {
			tagged++
			if e.Entry.Metadata[ingestfilter.MetadataKey] != "diff-trivia-fact" {
				t.Errorf("trivia not tagged: %v", e.Entry.Metadata)
			}
		} else if _, has := e.Entry.Metadata[ingestfilter.MetadataKey]; has {
			t.Errorf("unexpected tag on %q", e.Entry.Content)
		}
	}
	if tagged != 1 {
		t.Errorf("trivia entries = %d", tagged)
	}
}

func TestExplicitWritesAreNeverFiltered(t *testing.T) {
	s := newTestStore(t)
	s.SetExtractor(&fakeExtractor{facts: []string{leakFact, realFact1}})

	// Same facts and content, but not a capture path: nothing is filtered.
	out, err := s.Add(context.Background(), AddInput{Content: continuity, Domain: "code:demo"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 2 || out.Filtered != 0 {
		t.Fatalf("explicit write was filtered: %v (filtered=%d)", filterContents(out), out.Filtered)
	}
	for _, st := range s.FilterStats().Rules {
		if st.Seen != 0 {
			t.Errorf("rule %s evaluated an explicit write", st.ID)
		}
	}
}

func TestPinnedContentIsNeverFiltered(t *testing.T) {
	s := newTestStore(t)
	s.SetExtractor(&fakeExtractor{facts: []string{leakFact}})

	out, err := s.Add(context.Background(), AddInput{
		Content: continuity, Domain: "code:demo", Capture: true,
		Metadata: map[string]string{metadataKeyPinned: "true"},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 1 || out.Filtered != 0 {
		t.Fatalf("pinned content was filtered: %v", filterContents(out))
	}
}

func TestCaptureDropsBoilerplateSegmentsBeforeExtraction(t *testing.T) {
	s := newTestStore(t)
	ext := &fakeExtractor{facts: []string{realFact1}}
	s.SetExtractor(ext)

	// Everything is boilerplate: the extractor is never called.
	out, err := s.Add(context.Background(), AddInput{Content: "[user]\n" + continuity, Domain: "code:demo", Capture: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 0 || out.Filtered != 1 || ext.calls.Load() != 0 {
		t.Fatalf("entries=%d filtered=%d extractor calls=%d", len(out.Entries), out.Filtered, ext.calls.Load())
	}

	// Mixed: only the real message reaches the extractor.
	var sent string
	s.SetExtractor(extractorFunc(func(content string) []string { sent = content; return []string{realFact1} }))
	out, err = s.Add(context.Background(), AddInput{Content: realMessage + "\n\n[user]\n" + continuity, Domain: "code:demo", Capture: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sent, "being continued") || !strings.Contains(sent, "add retries") {
		t.Errorf("extractor input = %q", sent)
	}
	if len(out.Entries) != 1 {
		t.Errorf("entries = %v", filterContents(out))
	}
}

type extractorFunc func(string) []string

func (f extractorFunc) Extract(_ context.Context, content, _ string) ([]string, error) {
	return f(content), nil
}

func TestFilterModesAndCounters(t *testing.T) {
	s := newTestStore(t)
	s.SetExtractor(&fakeExtractor{facts: []string{leakFact, realFact1}})

	tag, _ := ingestfilter.Load(ingestfilter.Config{Mode: "tag"})
	s.SetFilter(tag)
	out, _ := s.Add(context.Background(), AddInput{Content: realMessage, Domain: "code:demo", Capture: true}, time.Now())
	if len(out.Entries) != 2 || out.Filtered != 0 {
		t.Fatalf("tag mode must keep both: %v", filterContents(out))
	}
	for _, e := range out.Entries {
		if e.Entry.Content == leakFact && e.Entry.Metadata[ingestfilter.MetadataKey] != "extractor-prompt-fact" {
			t.Errorf("leak not tagged: %v", e.Entry.Metadata)
		}
	}

	off, _ := ingestfilter.Load(ingestfilter.Config{Mode: "off"})
	s.SetFilter(off)
	out, _ = s.Add(context.Background(), AddInput{Content: realMessage + " again", Domain: "code:other", Capture: true}, time.Now())
	if len(out.Entries) != 2 {
		t.Fatalf("off mode must keep both: %v", filterContents(out))
	}

	s.SetFilter(nil)
	if st := s.FilterStats(); st.Mode != "off" {
		t.Errorf("nil filter stats mode = %q", st.Mode)
	}

	s.SetFilter(ingestfilter.Default())
	_, _ = s.Add(context.Background(), AddInput{Content: realMessage + " third", Domain: "code:third", Capture: true}, time.Now())
	ov := s.InsightsOverview(time.Now())
	var leak *apitypes.FilterRuleStats
	for i := range ov.Filter.Rules {
		if ov.Filter.Rules[i].ID == "extractor-prompt-fact" {
			leak = &ov.Filter.Rules[i]
		}
	}
	if leak == nil || leak.Dropped != 1 || leak.Seen != 2 || leak.Action != "drop" {
		t.Fatalf("overview counters: %+v", leak)
	}
	if ov.Filter.Mode != "default" || ov.Filter.RulesVersion < 1 {
		t.Errorf("overview filter header: %+v", ov.Filter)
	}
}

func TestRetroactiveExtractionFiltersOnlyCapturedRawEntries(t *testing.T) {
	s := newTestStore(t)

	// Captured raw entry (extraction was off) and an explicit raw entry.
	_, err := s.Add(context.Background(), AddInput{Content: realMessage + " captured", Domain: "code:demo", Capture: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(context.Background(), AddInput{Content: realMessage + " explicit", Domain: "code:demo2"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	s.SetExtractor(&fakeExtractor{facts: []string{realFact1, leakFact}})

	if _, err := s.RunRetroactiveExtraction(context.Background()); err != nil {
		t.Fatal(err)
	}
	var capturedLeak, explicitLeak int
	for _, e := range s.Recent(100, Filter{}, time.Now()) {
		if e.Content != leakFact {
			continue
		}
		if e.Domain == "code:demo" {
			capturedLeak++
		} else {
			explicitLeak++
		}
	}
	if capturedLeak != 0 {
		t.Errorf("captured raw entry kept the leak after re-extraction")
	}
	if explicitLeak != 1 {
		t.Errorf("explicit raw entry was filtered (leak count %d)", explicitLeak)
	}
}

func TestDedupDoesNotMarkExistingEntryAsFiltered(t *testing.T) {
	s := newTestStore(t)
	s.SetExtractor(&fakeExtractor{facts: []string{triviaFact}})

	// First an explicit write of the very same text, then a capture of it.
	first, err := s.Add(context.Background(), AddInput{Content: "[user]\nx", Domain: "code:demo"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Add(context.Background(), AddInput{Content: realMessage, Domain: "code:demo", Capture: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !second.Entries[0].Deduped || second.Entries[0].Entry.ID != first.Entries[0].Entry.ID {
		t.Fatalf("expected a dedup merge, got %+v", second.Entries[0])
	}
	if _, has := second.Entries[0].Entry.Metadata[ingestfilter.MetadataKey]; has {
		t.Errorf("existing entry was tagged by a merge: %v", second.Entries[0].Entry.Metadata)
	}
}
