package extract

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/rafaelvieiras/m3m0s/internal/factkind"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// TestSystemPromptGolden pins the extraction prompt: any change to it must be
// a visible diff of testdata/system_prompt.golden.txt (go test ./internal/extract -update).
func TestSystemPromptGolden(t *testing.T) {
	const path = "testdata/system_prompt.golden.txt"
	if *update {
		if err := os.WriteFile(path, []byte(systemPrompt+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := systemPrompt + "\n"; got != string(want) {
		t.Fatalf("system prompt changed; review the diff and run `go test ./internal/extract -update`\n--- golden\n%s\n--- got\n%s", want, got)
	}
}

type goldenCase struct {
	Name     string `json:"name"`
	Domain   string `json:"domain"`
	Input    string `json:"input"`
	Reply    string `json:"reply"`
	Expected []struct {
		Text string `json:"text"`
		Kind string `json:"kind"`
	} `json:"expected"`
}

func loadGolden(t *testing.T) []goldenCase {
	t.Helper()
	b, err := os.ReadFile("testdata/golden_extraction.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Cases []goldenCase `json:"cases"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f.Cases
}

// TestGoldenExtractionOffline feeds each canned reply through a fake provider
// and checks the parsed facts and kinds against the reference output.
func TestGoldenExtractionOffline(t *testing.T) {
	for _, c := range loadGolden(t) {
		t.Run(c.Name, func(t *testing.T) {
			srv := newFakeChatServer(t, func(string) (int, string) { return http.StatusOK, c.Reply })
			got, err := New("fake-key", srv.URL, "fake-model").ExtractFacts(context.Background(), c.Input, c.Domain)
			if err != nil {
				t.Fatalf("ExtractFacts: %v", err)
			}
			if len(got) != len(c.Expected) {
				t.Fatalf("got %d facts, want %d: %#v", len(got), len(c.Expected), got)
			}
			for i, w := range c.Expected {
				if got[i].Text != w.Text || got[i].Kind != w.Kind {
					t.Errorf("fact %d: got %+v, want %+v", i, got[i], w)
				}
			}
		})
	}
}

// TestGoldenLive runs the golden inputs against a real model, to compare a
// prompt or model change. It spends tokens, so it is opt-in:
//
//	M3M0S_GOLDEN_LIVE=1 M3M0S_EXTRACT_API_KEY=... [M3M0S_EXTRACT_BASE_URL=... M3M0S_EXTRACT_MODEL=...] \
//	  go test ./internal/extract -run TestGoldenLive -v
//
// It logs, per case, the kinds the model gave versus the reference and the
// overall kind agreement; it fails only below 60% so ordinary model variance
// does not break it (the numbers in the log are the point).
func TestGoldenLive(t *testing.T) {
	if os.Getenv("M3M0S_GOLDEN_LIVE") != "1" {
		t.Skip("set M3M0S_GOLDEN_LIVE=1 (and M3M0S_EXTRACT_API_KEY) to run against a real model")
	}
	key := os.Getenv("M3M0S_EXTRACT_API_KEY")
	if key == "" {
		t.Fatal("M3M0S_EXTRACT_API_KEY is required")
	}
	e := New(key, os.Getenv("M3M0S_EXTRACT_BASE_URL"), os.Getenv("M3M0S_EXTRACT_MODEL"))
	match, total := 0, 0
	for _, c := range loadGolden(t) {
		got, err := e.ExtractFacts(context.Background(), c.Input, c.Domain)
		if err != nil {
			t.Errorf("%s: %v", c.Name, err)
			continue
		}
		var gk, wk []string
		for _, f := range got {
			gk = append(gk, orNone(f.Kind))
		}
		for _, w := range c.Expected {
			wk = append(wk, orNone(w.Kind))
			total++
		}
		for i := range wk {
			if i < len(gk) && gk[i] == wk[i] {
				match++
			}
		}
		t.Logf("%s: model=%s reference=%s", c.Name, strings.Join(gk, ","), strings.Join(wk, ","))
	}
	if total > 0 {
		t.Logf("kind agreement with the reference: %d/%d", match, total)
		if float64(match)/float64(total) < 0.6 {
			t.Fatalf("kind agreement %d/%d below 60%%", match, total)
		}
	}
}

func orNone(k string) string {
	if k == "" {
		return "(none)"
	}
	return k
}

func TestExtractFactsKinds(t *testing.T) {
	srv := newFakeChatServer(t, func(string) (int, string) {
		return http.StatusOK, `{"facts":[{"text":" a ","kind":"RULE"},{"text":"b","kind":"bogus"},{"text":"c"},{"text":"  "}]}`
	})
	got, err := New("k", srv.URL, "m").ExtractFacts(context.Background(), "x", "code:x")
	if err != nil {
		t.Fatal(err)
	}
	want := []factkind.Fact{{Text: "a", Kind: "rule"}, {Text: "b", Kind: "other"}, {Text: "c", Kind: ""}}
	if len(got) != len(want) {
		t.Fatalf("got %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("fact %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestExtractStillReturnsPlainTexts(t *testing.T) {
	srv := newFakeChatServer(t, func(string) (int, string) {
		return http.StatusOK, `{"facts":[{"text":"a","kind":"state"},"b"]}`
	})
	got, err := New("k", srv.URL, "m").Extract(context.Background(), "x", "code:x")
	if err != nil || len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %#v, %v", got, err)
	}
}

func TestClassifyKinds(t *testing.T) {
	srv := newFakeChatServer(t, func(user string) (int, string) {
		if !strings.Contains(user, "1. \"first fact\"") || !strings.Contains(user, "2. \"second fact\"") {
			t.Errorf("facts not numbered in the prompt: %q", user)
		}
		return http.StatusOK, `{"kinds":["Gotcha","nonsense"]}`
	})
	got, err := New("k", srv.URL, "m").ClassifyKinds(context.Background(), []string{"first fact", "second fact"})
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != "gotcha" || got[1] != "other" {
		t.Fatalf("got %#v", got)
	}
}

func TestClassifyKindsRejectsWrongLength(t *testing.T) {
	srv := newFakeChatServer(t, func(string) (int, string) { return http.StatusOK, `{"kinds":["rule"]}` })
	if _, err := New("k", srv.URL, "m").ClassifyKinds(context.Background(), []string{"a", "b"}); err == nil {
		t.Fatal("expected an error for a short answer")
	}
	srv2 := newFakeChatServer(t, func(string) (int, string) { return http.StatusInternalServerError, "boom" })
	if _, err := New("k", srv2.URL, "m").ClassifyKinds(context.Background(), []string{"a"}); err == nil {
		t.Fatal("expected an HTTP error")
	}
}
