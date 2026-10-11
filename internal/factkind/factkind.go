// Package factkind defines the closed set of natures ("kinds") a stored fact
// can have (D38): what the fact IS (a decision, a rule, a transient status,
// noise...), independent of how old or how often read it is. It holds the
// enum, the metadata keys and the conservative heuristic classifier; the
// LLM tag comes from internal/extract and the persistence from
// internal/store.
package factkind

import "strings"

// Kinds of a fact. The set is closed and easy to extend: add a constant, put
// it in All, and decide whether it is durable (Durable) or transient.
const (
	Decision   = "decision"   // a choice made and, ideally, why
	Rule       = "rule"       // a convention, constraint or standing instruction
	Preference = "preference" // a stated preference of the user
	Gotcha     = "gotcha"     // a root cause, pitfall or non-obvious behaviour
	Procedure  = "procedure"  // how to do something (steps, commands, configuration)
	State      = "state"      // a transient status (progress, current branch, a changing number)
	Noise      = "noise"      // meta-description, boilerplate, diff trivia
	Other      = "other"      // default when unsure; also what a missing kind reads as
)

// All lists every kind, in display order.
var All = []string{Decision, Rule, Preference, Gotcha, Procedure, State, Noise, Other}

// Sources of the tag, recorded next to the kind so a stopgap heuristic tag
// can be told from an LLM one, and a human correction is never overwritten.
const (
	SourceLLM       = "llm"
	SourceHeuristic = "heuristic"
	SourceManual    = "manual"
)

// Metadata keys holding the tag. They live in the free-form metadata map like
// the other reserved keys (pinned, valid_until...), so adding the feature
// rewrote no existing row.
const (
	MetadataKey       = "kind"
	MetadataSourceKey = "kind_source"
)

// Valid reports whether s is exactly one of the kinds.
func Valid(s string) bool {
	for _, k := range All {
		if s == k {
			return true
		}
	}
	return false
}

// ValidSource reports whether s is one of the tag sources.
func ValidSource(s string) bool {
	return s == SourceLLM || s == SourceHeuristic || s == SourceManual
}

// Normalize maps a model's free-text answer to the enum: trimmed and
// lowercased; anything outside the enum becomes Other.
func Normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if Valid(s) {
		return s
	}
	return Other
}

// Durable says whether a kind is expected to stay true for a long time.
// Transient kinds are State and Noise; Other is neither (unknown).
func Durable(kind string) bool {
	switch kind {
	case Decision, Rule, Preference, Gotcha, Procedure:
		return true
	}
	return false
}

// Transient says whether a kind is expected to go stale quickly.
func Transient(kind string) bool { return kind == State || kind == Noise }

// Fact is one extracted fact with the kind the model gave it. Kind is empty
// when the model did not give one (a provider that ignored the schema); a
// non-empty Kind is always a valid enum value.
type Fact struct {
	Text string
	Kind string
}
