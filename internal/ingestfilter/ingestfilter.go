// Package ingestfilter drops or tags tooling boilerplate at the automatic
// capture paths, before it becomes memory (issue #53, D12 addendum).
//
// What it filters: text that the tooling itself produced and that the fact
// extractor then turned into "facts" - the extractor's own prompt and JSON
// schema, session-continuation and compaction boilerplate, commit-message
// generator prompts, and (review-only) diff arithmetic.
//
// How: a versioned set of named rules, shipped as data (defaults.json) and
// overridable by the operator. Each rule has a scope (a transcript "segment"
// before extraction, or an extracted "fact" after it), one or more regular
// expressions anchored on a full template (a pattern must start with ^, and a
// fact pattern must also end with $, so a single keyword never matches), and
// an action (drop, tag or off). Global and per-rule modes can override the
// shipped actions. Per-rule counters (seen, dropped, tagged) are kept in
// memory for the Insights overview and the server log.
//
// What it never does: it has no opinion on explicit writes or pinned
// entries; the callers only consult an Engine on automatic capture paths.
package ingestfilter

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
)

// Scope says what a rule is evaluated against.
type Scope string

const (
	// ScopeSegment rules run on the messages of a captured transcript, before
	// fact extraction (they also save extraction tokens).
	ScopeSegment Scope = "segment"
	// ScopeFact rules run on each extracted fact, before it is stored.
	ScopeFact Scope = "fact"
)

// Action is what happens on a match. The same values double as modes.
type Action string

const (
	ActionOff  Action = "off"  // the rule is disabled
	ActionTag  Action = "tag"  // keep the entry, mark metadata.filtered_by (segments: count and log only)
	ActionDrop Action = "drop" // discard before storing
)

// MetadataKey is the entry metadata key that carries the id of the rule that
// tagged the entry.
const MetadataKey = "filtered_by"

// Rule is one named, versioned-in-data filter.
type Rule struct {
	ID          string   `json:"id"`
	Description string   `json:"description,omitempty"`
	Scope       Scope    `json:"scope"`
	Action      Action   `json:"action"`
	Patterns    []string `json:"patterns"`
}

// RuleFile is the on-disk (and embedded) format of a rule set.
type RuleFile struct {
	Version int    `json:"version"`
	Rules   []Rule `json:"rules"`
}

//go:embed defaults.json
var defaultsJSON []byte

// Config is the operator-facing configuration.
type Config struct {
	// RulesFile is an optional JSON file in the RuleFile format. A rule with
	// the id of a default replaces it; a new id adds a rule; action "off"
	// disables one.
	RulesFile string
	// Mode is the global mode: "" or "default" keeps each rule's own action;
	// "off" disables everything; "tag" and "drop" force that action on every
	// rule that matches.
	Mode string
	// RuleModes overrides single rules: id (or a prefix ending in "*") to
	// off|tag|drop. It wins over Mode.
	RuleModes map[string]Action
}

type compiled struct {
	Rule
	res     []*regexp.Regexp
	seen    atomic.Int64
	dropped atomic.Int64
	tagged  atomic.Int64
}

// Engine evaluates a resolved rule set. Safe for concurrent use.
type Engine struct {
	version int
	mode    string
	rules   []*compiled
	modes   map[string]Action
}

// Decision is the outcome for one segment or fact.
type Decision struct {
	RuleID string
	Action Action // ActionDrop, ActionTag, or "" when nothing matched
}

// Matched reports whether a rule fired.
func (d Decision) Matched() bool { return d.Action != "" }

// Default returns an Engine with the shipped rules and their own actions.
func Default() *Engine {
	e, err := Load(Config{})
	if err != nil {
		panic("ingestfilter: invalid embedded defaults: " + err.Error()) // covered by tests
	}
	return e
}

// Load resolves defaults, the optional rules file and the modes.
func Load(cfg Config) (*Engine, error) {
	var base RuleFile
	if err := json.Unmarshal(defaultsJSON, &base); err != nil {
		return nil, fmt.Errorf("parse embedded rules: %w", err)
	}
	rules := append([]Rule(nil), base.Rules...)
	version := base.Version

	if cfg.RulesFile != "" {
		data, err := os.ReadFile(cfg.RulesFile)
		if err != nil {
			return nil, fmt.Errorf("read rules file: %w", err)
		}
		var over RuleFile
		if err := json.Unmarshal(data, &over); err != nil {
			return nil, fmt.Errorf("parse rules file: %w", err)
		}
		if over.Version > version {
			version = over.Version
		}
		for _, r := range over.Rules {
			replaced := false
			for i := range rules {
				if rules[i].ID == r.ID {
					rules[i], replaced = r, true
					break
				}
			}
			if !replaced {
				rules = append(rules, r)
			}
		}
	}

	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	switch mode {
	case "", "default":
		mode = "default"
	case string(ActionOff), string(ActionTag), string(ActionDrop):
	default:
		return nil, fmt.Errorf("invalid filter mode %q (want off, tag, drop or default)", cfg.Mode)
	}
	for k, a := range cfg.RuleModes {
		if !validAction(a) {
			return nil, fmt.Errorf("invalid mode %q for rule %q", a, k)
		}
	}

	e := &Engine{version: version, mode: mode, modes: cfg.RuleModes}
	seen := map[string]bool{}
	for _, r := range rules {
		c, err := compile(r)
		if err != nil {
			return nil, err
		}
		if seen[r.ID] {
			return nil, fmt.Errorf("duplicate rule id %q", r.ID)
		}
		seen[r.ID] = true
		e.rules = append(e.rules, c)
	}
	return e, nil
}

func validAction(a Action) bool { return a == ActionOff || a == ActionTag || a == ActionDrop }

func compile(r Rule) (*compiled, error) {
	if r.ID == "" {
		return nil, fmt.Errorf("rule without id")
	}
	if r.Scope != ScopeSegment && r.Scope != ScopeFact {
		return nil, fmt.Errorf("rule %q: invalid scope %q", r.ID, r.Scope)
	}
	if !validAction(r.Action) {
		return nil, fmt.Errorf("rule %q: invalid action %q", r.ID, r.Action)
	}
	if len(r.Patterns) == 0 {
		return nil, fmt.Errorf("rule %q: no patterns", r.ID)
	}
	c := &compiled{Rule: r}
	for _, p := range r.Patterns {
		if err := checkAnchored(p, r.Scope); err != nil {
			return nil, fmt.Errorf("rule %q: %w", r.ID, err)
		}
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", r.ID, err)
		}
		c.res = append(c.res, re)
	}
	return c, nil
}

var leadingFlags = regexp.MustCompile(`^(\(\?[a-z]+\))*`)

// checkAnchored enforces "anchor on the full template, not on a word": every
// pattern starts with ^ (after optional inline flags) and a fact pattern also
// ends with $. Segment patterns anchor the start of the message only, because
// the tail of such a message (a summary, a diff) is part of the boilerplate.
func checkAnchored(p string, s Scope) error {
	rest := p[len(leadingFlags.FindString(p)):]
	if !strings.HasPrefix(rest, "^") {
		return fmt.Errorf("pattern %q is not anchored with ^", p)
	}
	if s == ScopeFact && !strings.HasSuffix(p, "$") {
		return fmt.Errorf("fact pattern %q is not anchored with $", p)
	}
	return nil
}

// ParseModes parses "id=mode,prefix-*=mode" (the M3M0S_FILTER_MODES format).
func ParseModes(s string) (map[string]Action, error) {
	out := map[string]Action{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		a := Action(strings.ToLower(strings.TrimSpace(v)))
		if !ok || strings.TrimSpace(k) == "" || !validAction(a) {
			return nil, fmt.Errorf("invalid rule mode %q (want id=off|tag|drop)", part)
		}
		out[strings.TrimSpace(k)] = a
	}
	return out, nil
}

// effective resolves the action for r: per-rule override, then global mode,
// then the rule's own action.
func (e *Engine) effective(r *compiled) Action {
	if a, ok := e.ruleMode(r.ID); ok {
		return a
	}
	if e.mode != "default" {
		return Action(e.mode)
	}
	return r.Action
}

func (e *Engine) ruleMode(id string) (Action, bool) {
	if a, ok := e.modes[id]; ok { // exact id wins over a prefix
		return a, true
	}
	best, bestLen := Action(""), -1
	for k, a := range e.modes {
		if p, ok := strings.CutSuffix(k, "*"); ok && strings.HasPrefix(id, p) && len(p) > bestLen {
			best, bestLen = a, len(p)
		}
	}
	return best, bestLen >= 0
}

// Check evaluates the rules of the given scope against text. Drop wins over
// tag; among equals the first rule in file order wins. Only the winning rule's
// dropped/tagged counter moves; every enabled rule of the scope counts the
// item as seen.
func (e *Engine) Check(scope Scope, text string) Decision {
	var best Decision
	var bestRule *compiled
	for _, r := range e.rules {
		if r.Scope != scope {
			continue
		}
		act := e.effective(r)
		if act == ActionOff {
			continue
		}
		r.seen.Add(1)
		if bestRule != nil && best.Action == ActionDrop {
			continue
		}
		if !matchAny(r.res, text) {
			continue
		}
		if bestRule == nil || (act == ActionDrop && best.Action != ActionDrop) {
			best, bestRule = Decision{RuleID: r.ID, Action: act}, r
		}
	}
	if bestRule != nil {
		if best.Action == ActionDrop {
			bestRule.dropped.Add(1)
		} else {
			bestRule.tagged.Add(1)
		}
	}
	return best
}

func matchAny(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// CheckFact evaluates a single extracted fact.
func (e *Engine) CheckFact(fact string) Decision {
	return e.Check(ScopeFact, strings.TrimSpace(fact))
}

// FilterSegments splits a captured digest into its messages, removes the ones
// a segment rule drops and returns the remaining text plus every hit. A tag
// hit keeps the segment (it is counted and reported, nothing is marked: a
// segment does not map to a single stored entry).
func (e *Engine) FilterSegments(content string) (string, []Decision) {
	segs := SplitSegments(content)
	var hits []Decision
	kept := make([]string, 0, len(segs))
	for _, sg := range segs {
		d := e.Check(ScopeSegment, strings.TrimSpace(sg.Body))
		if d.Matched() {
			hits = append(hits, d)
			if d.Action == ActionDrop {
				continue
			}
		}
		kept = append(kept, sg.Raw)
	}
	if len(hits) == 0 {
		return content, nil
	}
	return strings.TrimSpace(strings.Join(kept, "\n\n")), hits
}

// Segment is one message of a digest: the raw text including its "[role]"
// header, and the body without it.
type Segment struct {
	Raw  string
	Body string
}

var segmentHeader = regexp.MustCompile(`(?m)^\[(user|assistant)\]\n`)

// SplitSegments splits the digest format produced by the hook ("[role]\ntext",
// one blank line between messages). Content without headers is one segment.
func SplitSegments(content string) []Segment {
	idx := segmentHeader.FindAllStringIndex(content, -1)
	if len(idx) == 0 {
		return []Segment{{Raw: content, Body: content}}
	}
	var out []Segment
	if pre := strings.TrimSpace(content[:idx[0][0]]); pre != "" {
		out = append(out, Segment{Raw: pre, Body: pre})
	}
	for i, loc := range idx {
		end := len(content)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		out = append(out, Segment{
			Raw:  strings.TrimSpace(content[loc[0]:end]),
			Body: content[loc[1]:end],
		})
	}
	return out
}

// RuleStats is one rule's resolved state and counters.
type RuleStats struct {
	ID          string `json:"id"`
	Scope       string `json:"scope"`
	Description string `json:"description,omitempty"`
	Action      string `json:"action"` // effective action after modes
	Seen        int64  `json:"seen"`
	Dropped     int64  `json:"dropped"`
	Tagged      int64  `json:"tagged"`
}

// Stats is a snapshot of the engine for the Insights overview.
type Stats struct {
	Version int
	Mode    string
	Rules   []RuleStats
}

// Stats returns the rules (sorted by id) with their counters.
func (e *Engine) Stats() Stats {
	out := Stats{Version: e.version, Mode: e.mode}
	for _, r := range e.rules {
		out.Rules = append(out.Rules, RuleStats{
			ID: r.ID, Scope: string(r.Scope), Description: r.Description,
			Action: string(e.effective(r)),
			Seen:   r.seen.Load(), Dropped: r.dropped.Load(), Tagged: r.tagged.Load(),
		})
	}
	sort.Slice(out.Rules, func(i, j int) bool { return out.Rules[i].ID < out.Rules[j].ID })
	return out
}
