package factkind

import (
	"regexp"
	"strings"
)

// The heuristic is a stopgap for servers without an extractor and for the
// raw-fallback paths, not a replacement for the LLM tag. It is deliberately
// conservative: a kind is returned only when exactly one family of markers
// matches; two families, or none, mean "undecided" (Other). A prototype on a
// hand-judged sample got 27/29 right when it decided but left almost half of
// the "neutral" bucket actually transient, so silence is the common outcome.

type marker struct {
	kind string
	re   *regexp.Regexp
}

func rx(p string) *regexp.Regexp { return regexp.MustCompile(`(?i)` + p) }

var markers = []marker{
	{Noise, rx(`\b\d+ files? changed\b|\b\d+ insertions?\(\+\)|\b\d+ deletions?\(-\)|^the (text|passage|message|document) (describes|discusses|explains|is about)\b|\bdiff --stat\b`)},
	{State, rx(`^\s*(status|progress|current (branch|status|task))\s*:|\bis (currently|now) (on|in|at|running|blocked|waiting)\b|\b(work|task|migration|build) (is )?in progress\b|\b\d+ of \d+ (tests?|tasks?|items?|files?) (pass|passed|done|complete|completed)\b|\bpr #\d+ (is )?(open|merged|pending)\b|\bwaiting (for|on) (review|ci|the)\b|\bcurrently (on|at|using) branch\b`)},
	{Gotcha, rx(`\broot cause\b|\bcaused by\b|\bturns out\b|\bpitfall\b|\bgotcha\b|\bworkaround\b|\bsilently (fails?|ignores?|drops?|truncates?)\b|\bdoes(n't| not) work (when|because|if)\b|\bnon-obvious\b`)},
	{Decision, rx(`\b(we |i )?(decided|chose|opted|settled on|agreed) (to|on|for|that)\b|\bwe (switched|moved) (to|from)\b|\b(going|went) with\b|\bdecision:`)},
	{Rule, rx(`\b(always|never) (use|run|commit|push|add|call|set|write|store|keep|prefer|avoid|pass|check|validate|rebase|bump|log|expose|hard-?code)\b|\bmust (not )?be\b|\bmust never\b|\bis (forbidden|prohibited|required)\b|\bconvention:|\bstanding rule\b|\bthe rule is\b`)},
	{Preference, rx(`\b(the )?user (prefers|likes|wants|dislikes)\b|\bi (prefer|like|dislike)\b|\bprefers? .{1,60} (over|to|instead of)\b`)},
	{Procedure, rx(`\bto (deploy|build|release|install|configure|reset|restore|migrate|run|set up|start|stop)\b.{0,80}\b(run|use|execute|set|edit|open)\b|\bsteps?:|\bhow to\b|(^|\n)\s*1[.)] \S`)},
}

// Classify returns the kind the markers support and whether it decided. When
// it did not decide, kind is Other. Only the first 2000 characters are
// looked at: markers carry their meaning near the start of a fact, and a
// long raw chunk would otherwise match anything.
func Classify(text string) (kind string, decided bool) {
	if len(text) > 2000 {
		text = text[:2000]
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Other, false
	}
	found := ""
	for _, m := range markers {
		if !m.re.MatchString(text) {
			continue
		}
		if found != "" {
			return Other, false // two families disagree: stay silent
		}
		found = m.kind
	}
	if found == "" {
		return Other, false
	}
	return found, true
}
