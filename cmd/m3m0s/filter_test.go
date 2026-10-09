package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/store"
)

func replayJSON(t *testing.T, args ...string) replayReport {
	t.Helper()
	var out, errOut bytes.Buffer
	if err := filterMain(context.Background(), &out, &errOut, append([]string{"replay", "-json"}, args...)); err != nil {
		t.Fatalf("filterMain: %v (%s)", err, errOut.String())
	}
	var rep replayReport
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out.String())
	}
	return rep
}

func counts(rep replayReport) map[string][3]int64 {
	m := map[string][3]int64{}
	for _, r := range rep.Rules {
		m[r.ID] = [3]int64{r.Seen, r.Dropped, r.Tagged}
	}
	return m
}

func TestReplayFactsAndSegmentsFiles(t *testing.T) {
	rep := replayJSON(t, "-facts", "testdata/replay-facts.txt", "-segments", "testdata/replay-digest.txt")
	if rep.Facts != 10 || rep.Segments != 4 {
		t.Fatalf("examined facts=%d segments=%d", rep.Facts, rep.Segments)
	}
	c := counts(rep)
	want := map[string][2]int64{ // dropped, tagged
		"extractor-prompt-fact":        {2, 0},
		"session-continuation-fact":    {2, 0},
		"commit-prompt-fact":           {1, 0},
		"diff-trivia-fact":             {0, 1},
		"session-continuation-segment": {1, 0},
		"commit-prompt-segment":        {1, 0},
		"extractor-prompt-segment":     {0, 0},
	}
	for id, w := range want {
		if c[id][1] != w[0] || c[id][2] != w[1] {
			t.Errorf("%s: dropped/tagged = %d/%d, want %d/%d", id, c[id][1], c[id][2], w[0], w[1])
		}
	}
}

func TestReplayModeOverrideAndExamples(t *testing.T) {
	var out, errOut bytes.Buffer
	err := filterMain(context.Background(), &out, &errOut, []string{"replay", "-facts", "testdata/replay-facts.txt", "-mode", "off", "-examples", "1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "nothing was written") || strings.Contains(out.String(), "examples:") {
		t.Errorf("unexpected output:\n%s", out.String())
	}

	out.Reset()
	if err := filterMain(context.Background(), &out, &errOut, []string{"replay", "-facts", "testdata/replay-facts.txt", "-modes", "diff-trivia-fact=drop", "-examples", "1"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "diff-trivia-fact examples:") {
		t.Errorf("missing examples:\n%s", out.String())
	}
}

func TestReplayUsageErrors(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := filterMain(context.Background(), &out, &errOut, nil); err == nil {
		t.Error("no subcommand accepted")
	}
	if err := filterMain(context.Background(), &out, &errOut, []string{"replay"}); err == nil {
		t.Error("replay without input accepted")
	}
	if err := filterMain(context.Background(), &out, &errOut, []string{"replay", "-facts", "testdata/replay-facts.txt", "-modes", "x=bogus"}); err == nil {
		t.Error("bad modes accepted")
	}
}

func TestReplayDatabaseIsReadOnlyAndSkipsPinned(t *testing.T) {
	dir := t.TempDir()
	s, err := store.New(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	add := func(content string, md map[string]string) {
		if _, err := s.Add(context.Background(), store.AddInput{Content: content, Domain: "code:demo", Metadata: md}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	add("Each atomic fact must be self-contained and reusable in future searches.", nil)
	add("The commit changed 9 files with 103 insertions and 4 deletions.", map[string]string{"pinned": "true"})
	add("The cache evicts the least recently used entry when it is full.", nil)
	_ = s.DB().Close()

	rep := replayJSON(t, "-data", dir)
	c := counts(rep)
	if rep.Facts != 2 {
		t.Errorf("facts examined = %d, want 2 (pinned skipped)", rep.Facts)
	}
	if c["extractor-prompt-fact"][1] != 1 || c["diff-trivia-fact"][2] != 0 {
		t.Errorf("counts: %v", c)
	}
}
