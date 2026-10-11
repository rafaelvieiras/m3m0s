package factkind

import (
	"strings"
	"testing"
)

func TestNormalizeAndValid(t *testing.T) {
	for _, k := range All {
		if !Valid(k) || Normalize(k) != k {
			t.Errorf("%s should be valid and stable", k)
		}
	}
	for in, want := range map[string]string{"  RULE ": Rule, "Gotcha": Gotcha, "": Other, "bogus": Other, "decisions": Other} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
	if Valid("Rule") || Valid("") {
		t.Error("Valid is exact: no casing or empty")
	}
	if !ValidSource(SourceLLM) || !ValidSource(SourceHeuristic) || !ValidSource(SourceManual) || ValidSource("auto") {
		t.Error("ValidSource")
	}
}

func TestDurableTransientPartition(t *testing.T) {
	for _, k := range []string{Decision, Rule, Preference, Gotcha, Procedure} {
		if !Durable(k) || Transient(k) {
			t.Errorf("%s should be durable only", k)
		}
	}
	for _, k := range []string{State, Noise} {
		if Durable(k) || !Transient(k) {
			t.Errorf("%s should be transient only", k)
		}
	}
	if Durable(Other) || Transient(Other) {
		t.Error("other is neither")
	}
}

// Positives: marker texts the heuristic must classify. All synthetic.
func TestClassifyPositives(t *testing.T) {
	cases := []struct{ text, want string }{
		{"We decided to use SQLite instead of Postgres for the embedded mode.", Decision},
		{"The team opted for a monorepo because releases are simpler.", Decision},
		{"Switched to a job queue after we moved from cron.", Decision},
		{"Always run the linter before pushing.", Rule},
		{"Never commit generated files to the repository.", Rule},
		{"The deploy token must never be logged.", Rule},
		{"Convention: one migration per pull request.", Rule},
		{"The user prefers short commit messages over long ones.", Preference},
		{"I prefer tabs to spaces in this project.", Preference},
		{"The root cause was a stale lockfile left by a crashed run.", Gotcha},
		{"The build fails silently ignores unknown flags, a real pitfall.", Gotcha},
		{"Turns out the proxy strips the Authorization header.", Gotcha},
		{"To deploy the staging stack, run make deploy-staging.", Procedure},
		{"Steps: 1) build the image 2) push it 3) restart the service.", Procedure},
		{"Status: migration in progress, 4 of 9 tasks done.", State},
		{"The worker is currently waiting for review on the open branch.", State},
		{"PR #42 is open and waiting for CI.", State},
		{"12 files changed, 340 insertions(+), 8 deletions(-)", Noise},
		{"The text describes a refactor of the parser.", Noise},
	}
	for _, c := range cases {
		got, decided := Classify(c.text)
		if got != c.want || !decided {
			t.Errorf("Classify(%q) = %s (decided=%v), want %s", c.text, got, decided, c.want)
		}
	}
}

// Negatives: the heuristic must stay silent (other, not decided).
func TestClassifyNegatives(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"The parser reads tokens from a buffered reader.",
		"Latency is measured in milliseconds at the edge.",
		"The server never started in the sandbox image.",         // "never" without a rule verb
		"They decided nothing yet; the meeting was moved.",       // "decided" without a complement
		"A user guide for the preferences page is available.",    // "preferences" is not a stated preference
		"The run command takes a path argument.",                 // "run" alone is not a procedure
		"This document explains the architecture of the module.", // not a meta-description marker
		"Order of keys in the config file does not matter.",
	}
	for _, text := range cases {
		if got, decided := Classify(text); decided || got != Other {
			t.Errorf("Classify(%q) = %s (decided=%v), want undecided other", text, got, decided)
		}
	}
}

// Two families matching means the heuristic abstains rather than guessing.
func TestClassifyAbstainsWhenFamiliesDisagree(t *testing.T) {
	text := "We decided to use Redis, and the root cause of the outage was a missing index."
	if got, decided := Classify(text); decided || got != Other {
		t.Errorf("got %s decided=%v", got, decided)
	}
}

func TestClassifyOnlyLooksAtTheStart(t *testing.T) {
	long := strings.Repeat("filler words go here. ", 200) + "The root cause was a stale lockfile."
	if got, decided := Classify(long); decided {
		t.Errorf("marker beyond the window must be ignored, got %s", got)
	}
}

func TestEvaluateAgreement(t *testing.T) {
	items := []SampleItem{
		{ID: "1", Content: "Always run the linter before pushing.", Kind: Rule, KindSource: SourceLLM, Label: Rule},
		{ID: "2", Content: "Status: waiting for review.", Kind: State, KindSource: SourceLLM, Label: Noise}, // both transient
		{ID: "3", Content: "We decided to cache results because reads dominate.", Kind: State, KindSource: SourceLLM, Label: Decision},
		{ID: "4", Content: "Plain words.", Kind: Other, KindSource: SourceHeuristic, Label: Gotcha},
		{ID: "5", Content: "Unlabelled.", Kind: Rule, KindSource: SourceLLM},
		{ID: "6", Content: "Bad label.", Kind: Rule, KindSource: SourceLLM, Label: "nonsense"},
	}
	r := Evaluate(items, "")
	if r.Items != 6 || r.Labelled != 4 || r.Invalid != 1 {
		t.Fatalf("counts: %+v", r)
	}
	if r.Stored.Exact != 1 || r.Stored.Labelled != 4 {
		t.Errorf("stored exact = %d/%d", r.Stored.Exact, r.Stored.Labelled)
	}
	// compared where both sides decided: items 1,2,3 (4 predicts other)
	if r.Stored.DurableTransientCompared != 3 || r.Stored.DurableTransientAgreed != 2 {
		t.Errorf("durable/transient: %d/%d", r.Stored.DurableTransientAgreed, r.Stored.DurableTransientCompared)
	}
	// the heuristic is recomputed from content: items 1 (rule), 2 (state), 3 (decision) correct
	if r.Heuristic.Exact != 2 {
		t.Errorf("heuristic exact = %d, want 2", r.Heuristic.Exact)
	}
	// restricting the stored predictor to a source
	llmOnly := Evaluate(items, SourceLLM)
	if llmOnly.Stored.Labelled != 3 || llmOnly.Heuristic.Labelled != 4 {
		t.Errorf("source filter: stored %d, heuristic %d", llmOnly.Stored.Labelled, llmOnly.Heuristic.Labelled)
	}
	if !strings.Contains(r.String(), "durable-vs-transient") {
		t.Error("String() should render the agreement")
	}
}

func TestEvaluateEmpty(t *testing.T) {
	r := Evaluate(nil, "")
	if r.Labelled != 0 || r.Stored.ExactPct != 0 {
		t.Errorf("%+v", r)
	}
}
