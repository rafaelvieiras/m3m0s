package ingestfilter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultsCompileAndAreAnchored(t *testing.T) {
	e := Default()
	if len(e.rules) == 0 {
		t.Fatal("no default rules")
	}
	ids := map[string]bool{}
	for _, r := range e.rules {
		if ids[r.ID] {
			t.Errorf("duplicate id %s", r.ID)
		}
		ids[r.ID] = true
	}
	for _, id := range []string{
		"extractor-prompt-segment", "extractor-prompt-fact", "session-continuation-segment",
		"session-continuation-fact", "commit-prompt-segment", "commit-prompt-fact", "diff-trivia-fact",
	} {
		if !ids[id] {
			t.Errorf("missing default rule %s", id)
		}
	}
}

func TestAnchoringIsEnforced(t *testing.T) {
	for _, tc := range []struct {
		name  string
		scope Scope
		pat   string
		ok    bool
	}{
		{"bare word", ScopeFact, `(?i)compact`, false},
		{"fact without end anchor", ScopeFact, `(?i)^the session`, false},
		{"fact anchored", ScopeFact, `(?i)^the session$`, true},
		{"segment start anchor only", ScopeSegment, `(?is)^hello.*`, true},
		{"segment unanchored", ScopeSegment, `(?is)hello`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkAnchored(tc.pat, tc.scope)
			if (err == nil) != tc.ok {
				t.Fatalf("checkAnchored(%q) err=%v, want ok=%v", tc.pat, err, tc.ok)
			}
		})
	}
}

// factCases are synthetic. Positives are boilerplate; negatives are real
// facts a project could legitimately hold (including projects that document
// the extractor, compaction or commit tooling) and must survive.
var factCases = []struct {
	rule    string
	text    string
	wantHit bool
	comment string
}{
	// extractor-prompt-fact
	{"extractor-prompt-fact", `The response must be only a JSON object in the format {"facts": ["fact 1", "fact 2"]}, without markdown.`, true, "schema restated"},
	{"extractor-prompt-fact", `Each atomic fact must be self-contained and reusable in future searches.`, true, ""},
	{"extractor-prompt-fact", `If the text is already a single atomic fact, return it as is; do not invent artificial subdivisions.`, true, ""},
	{"extractor-prompt-fact", `The extractor parses the model reply as {"facts": [...]} and falls back to a bare array when the provider ignores response_format.`, false, "documents the extractor"},
	{"extractor-prompt-fact", `Atomic facts are stored with a source reference so the UI can show where each one came from.`, false, ""},
	{"extractor-prompt-fact", `Do not invent a new table for this; reuse the existing blobs table.`, false, "ordinary engineering advice"},

	// session-continuation-fact
	{"session-continuation-fact", `The session is a continuation of a previous conversation that ran out of context.`, true, ""},
	{"session-continuation-fact", `The summary below covers the earlier portion of the conversation.`, true, ""},
	{"session-continuation-fact", `The session continues from a previous conversation.`, true, ""},
	{"session-continuation-fact", `The importer resumes the session from the last checkpoint after a crash.`, false, "natural language about sessions"},
	{"session-continuation-fact", `Session continuation is implemented by replaying the stored transcript from the last offset.`, false, ""},
	{"session-continuation-fact", `The /compact command is documented in the CLI reference and takes optional instructions.`, false, ""},

	// commit-prompt-fact
	{"commit-prompt-fact", `Generate a commit message in English from a staged git diff.`, true, ""},
	{"commit-prompt-fact", `The prepare-commit-msg hook generates a commit message from the staged git diff.`, false, "project documents a tool"},
	{"commit-prompt-fact", `Commit messages follow Conventional Commits, with the scope in parentheses.`, false, ""},

	// diff-trivia-fact (tag)
	{"diff-trivia-fact", `The commit changed 9 files with 103 insertions and 4 deletions.`, true, ""},
	{"diff-trivia-fact", `The upload limit for avatars is 2 MB; larger images are rejected.`, false, ""},
	{"diff-trivia-fact", `Images are served from the CDN and resized to 512 pixels.`, false, ""},
}

func TestFactRules(t *testing.T) {
	for _, tc := range factCases {
		e := Load2(t, Config{})
		d := e.CheckFact(tc.text)
		hit := d.RuleID == tc.rule
		if tc.wantHit && !hit {
			t.Errorf("rule %s should match %q (got %q) %s", tc.rule, tc.text, d.RuleID, tc.comment)
		}
		if !tc.wantHit && d.Matched() && d.RuleID == tc.rule {
			t.Errorf("rule %s must not match %q %s", tc.rule, tc.text, tc.comment)
		}
		if !tc.wantHit && d.Matched() {
			t.Errorf("no rule may match legitimate fact %q (got %s)", tc.text, d.RuleID)
		}
	}
}

func TestDefaultActions(t *testing.T) {
	e := Default()
	if d := e.CheckFact("Each atomic fact must be self-contained and reusable in future searches."); d.Action != ActionDrop {
		t.Errorf("leak should drop, got %v", d)
	}
	if d := e.CheckFact("The commit has 12 insertions and 3 deletions."); d.Action != ActionTag || d.RuleID != "diff-trivia-fact" {
		t.Errorf("diff trivia should tag, got %v", d)
	}
}

const extractorPrompt = `You extract atomic facts from a text for a semantic memory system.
Rules:
- Each fact must be self-contained (makes sense on its own, without the original text) and reusable in future searches.
- Do not invent information that is not in the text.
- Reply ONLY with a JSON object in the format {"facts": ["fact 1", "fact 2"]}, nothing else: no markdown.`

func TestSegmentRules(t *testing.T) {
	cont := "This session is being continued from a previous conversation that ran out of context. The summary below covers the earlier portion of the conversation.\n\nSummary:\n1. Primary request: build a widget."
	digest := strings.Join([]string{
		"[user]\nPlease add a retry to the uploader.",
		"[assistant]\nDone, the uploader now retries three times.",
		"[user]\n" + cont,
		"[user]\n" + extractorPrompt,
		"[user]\nWrite a commit message from this git diff:\n+x",
		"[assistant]\nfeat: add retry to uploader",
	}, "\n\n")

	e := Default()
	kept, hits := e.FilterSegments(digest)
	if len(hits) != 3 {
		t.Fatalf("want 3 hits, got %v", hits)
	}
	for _, gone := range []string{"being continued", "You extract atomic", "Write a commit message"} {
		if strings.Contains(kept, gone) {
			t.Errorf("boilerplate %q survived: %s", gone, kept)
		}
	}
	for _, stay := range []string{"add a retry to the uploader", "retries three times", "feat: add retry"} {
		if !strings.Contains(kept, stay) {
			t.Errorf("real message %q was lost: %s", stay, kept)
		}
	}
}

func TestSegmentFalsePositiveGuards(t *testing.T) {
	e := Default()
	for _, body := range []string{
		// natural language about continuing a session
		"[user]\nThis session is a continuation of the design work we did yesterday; let's pick up the retry logic.",
		"[user]\nI ran out of context last time, so please keep answers short.",
		// documenting the extractor: quotes the prompt but is not the prompt
		"[user]\nIn the extractor the system prompt says: You extract atomic facts from a text for a semantic memory system. Should we add a rule about dates?",
		// asks about commit messages without being the generator prompt
		"[user]\nWrite a commit message convention doc that explains how the git diff review step works.",
		"[assistant]\nHere is the summary below the table covering the earlier portion of the rollout.",
	} {
		kept, hits := e.FilterSegments(body)
		if len(hits) != 0 || kept != body {
			t.Errorf("legitimate message was filtered: %q (%v)", body, hits)
		}
	}
}

func TestSplitSegmentsWithoutHeaders(t *testing.T) {
	segs := SplitSegments("plain text with no role header")
	if len(segs) != 1 || segs[0].Body != "plain text with no role header" {
		t.Fatalf("got %+v", segs)
	}
}

func TestModes(t *testing.T) {
	const drop = "Each atomic fact must be self-contained and reusable in future searches."
	const trivia = "The commit changed 9 files with 103 insertions and 4 deletions."

	cases := []struct {
		name       string
		cfg        Config
		wantDrop   Action
		wantTrivia Action
	}{
		{"default", Config{}, ActionDrop, ActionTag},
		{"global off", Config{Mode: "off"}, "", ""},
		{"global tag", Config{Mode: "tag"}, ActionTag, ActionTag},
		{"global drop", Config{Mode: "drop"}, ActionDrop, ActionDrop},
		{"per-rule beats global", Config{Mode: "drop", RuleModes: map[string]Action{"diff-trivia-fact": ActionOff}}, ActionDrop, ""},
		{"per-rule tag", Config{RuleModes: map[string]Action{"extractor-prompt-fact": ActionTag}}, ActionTag, ActionTag},
		{"prefix glob", Config{RuleModes: map[string]Action{"extractor-*": ActionOff}}, "", ActionTag},
		{"exact beats glob", Config{RuleModes: map[string]Action{"extractor-*": ActionOff, "extractor-prompt-fact": ActionDrop}}, ActionDrop, ActionTag},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := Load2(t, tc.cfg)
			if got := e.CheckFact(drop).Action; got != tc.wantDrop {
				t.Errorf("leak: got %q want %q", got, tc.wantDrop)
			}
			if got := e.CheckFact(trivia).Action; got != tc.wantTrivia {
				t.Errorf("trivia: got %q want %q", got, tc.wantTrivia)
			}
		})
	}
}

func TestInvalidConfig(t *testing.T) {
	if _, err := Load(Config{Mode: "maybe"}); err == nil {
		t.Error("invalid mode accepted")
	}
	if _, err := ParseModes("a=explode"); err == nil {
		t.Error("invalid rule mode accepted")
	}
	if _, err := ParseModes("noequals"); err == nil {
		t.Error("malformed rule mode accepted")
	}
	if m, err := ParseModes(" a = drop , b-*=TAG ,"); err != nil || m["a"] != ActionDrop || m["b-*"] != ActionTag {
		t.Errorf("ParseModes: %v %v", m, err)
	}
}

func TestRulesFileOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	write := func(s string) {
		if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Replace a default, disable another, add a new one.
	write(`{"version": 2, "rules": [
	  {"id": "diff-trivia-fact", "scope": "fact", "action": "off", "patterns": ["(?i)^x$"]},
	  {"id": "extractor-prompt-fact", "scope": "fact", "action": "tag", "patterns": ["(?i)^each atomic fact must be self-contained.*$"]},
	  {"id": "team-banner", "scope": "fact", "action": "drop", "patterns": ["(?i)^generated by the build bot\\.?$"]}
	]}`)
	e := Load2(t, Config{RulesFile: path})
	if st := e.Stats(); st.Version != 2 {
		t.Errorf("version = %d", st.Version)
	}
	if d := e.CheckFact("The commit changed 9 files with 103 insertions."); d.Matched() {
		t.Errorf("disabled rule fired: %v", d)
	}
	if d := e.CheckFact("Each atomic fact must be self-contained and reusable in future searches."); d.Action != ActionTag {
		t.Errorf("overridden rule: %v", d)
	}
	if d := e.CheckFact("Generated by the build bot."); d.Action != ActionDrop || d.RuleID != "team-banner" {
		t.Errorf("added rule: %v", d)
	}

	// An unanchored operator pattern is rejected.
	write(`{"version": 1, "rules": [{"id": "greedy", "scope": "fact", "action": "drop", "patterns": ["compact"]}]}`)
	if _, err := Load(Config{RulesFile: path}); err == nil {
		t.Error("unanchored operator rule accepted")
	}
	write(`{"version": 1, "rules": [{"id": "bad", "scope": "nope", "action": "drop", "patterns": ["^x$"]}]}`)
	if _, err := Load(Config{RulesFile: path}); err == nil {
		t.Error("invalid scope accepted")
	}
	if _, err := Load(Config{RulesFile: filepath.Join(dir, "missing.json")}); err == nil {
		t.Error("missing rules file accepted")
	}
}

func TestCounters(t *testing.T) {
	e := Default()
	e.CheckFact("Each atomic fact must be self-contained and reusable in future searches.") // drop
	e.CheckFact("The commit changed 9 files with 103 insertions.")                          // tag
	e.CheckFact("A perfectly normal fact about the cache layer.")                           // none
	e.FilterSegments("[user]\nhello")                                                       // segment scope only

	by := map[string]RuleStats{}
	for _, r := range e.Stats().Rules {
		by[r.ID] = r
	}
	if r := by["extractor-prompt-fact"]; r.Seen != 3 || r.Dropped != 1 || r.Tagged != 0 {
		t.Errorf("leak counters: %+v", r)
	}
	if r := by["diff-trivia-fact"]; r.Seen != 3 || r.Dropped != 0 || r.Tagged != 1 {
		t.Errorf("trivia counters: %+v", r)
	}
	if r := by["session-continuation-segment"]; r.Seen != 1 || r.Dropped != 0 {
		t.Errorf("segment counters: %+v", r)
	}

	// Disabled rules do not count items as seen.
	off := Load2(t, Config{Mode: "off"})
	off.CheckFact("anything")
	for _, r := range off.Stats().Rules {
		if r.Seen != 0 {
			t.Errorf("%s saw items while off", r.ID)
		}
	}
}

func TestSegmentTagKeepsTheSegment(t *testing.T) {
	e := Load2(t, Config{Mode: "tag"})
	in := "[user]\nWrite a commit message from this git diff:\n+x"
	kept, hits := e.FilterSegments(in)
	if kept != in || len(hits) != 1 || hits[0].Action != ActionTag {
		t.Errorf("kept=%q hits=%v", kept, hits)
	}
}

// Load2 is a test helper.
func Load2(t *testing.T, cfg Config) *Engine {
	t.Helper()
	e, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
