package store

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/factkind"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

var retNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// synth builds a synthetic, extracted, unpinned fact of the given kind and
// source, ageDays old at retNow. Every fixture here is made up.
func synth(id, kind, source string, ageDays float64, extra map[string]string) schema.Entry {
	created := retNow.Add(-time.Duration(ageDays * 24 * float64(time.Hour)))
	md := map[string]string{metadataKeySourceContentRef: "ref-" + id}
	if kind != "" {
		md[factkind.MetadataKey] = kind
	}
	if source != "" {
		md[factkind.MetadataSourceKey] = source
	}
	for k, v := range extra {
		md[k] = v
	}
	return schema.Entry{ID: id, Content: "synthetic fact " + id, CreatedAt: created, UpdatedAt: created, Metadata: md}
}

func almost(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// ageAtCut is the age (days) at which an unread fact of half-life h crosses
// the cut: 2^(-a/h) = threshold.
func ageAtCut(h float64) float64 { return h * math.Log2(1/ColdRetentionThreshold) }

func TestDefaultRetentionParams(t *testing.T) {
	p := DefaultRetentionParams()
	want := map[string]float64{
		factkind.Decision: 365, factkind.Rule: 365, factkind.Preference: 365, factkind.Gotcha: 365,
		factkind.Procedure: 365, factkind.Other: 90, factkind.State: 21, factkind.Noise: 7,
	}
	for k, v := range want {
		if p.HalfLifeDays[k] != v {
			t.Errorf("half-life of %s = %v, want %v", k, p.HalfLifeDays[k], v)
		}
	}
	if len(p.HalfLifeDays) != len(factkind.All) {
		t.Errorf("half-lives for %d kinds, the enum has %d", len(p.HalfLifeDays), len(factkind.All))
	}
	if p.ExpiredCapEnabled {
		t.Error("the expired-valid_until cap must be off by default (pending audit)")
	}
	if p.ExpiredCapDays != 60 {
		t.Errorf("expired cap = %v days, want 60", p.ExpiredCapDays)
	}
	if err := p.Validate(); err != nil {
		t.Errorf("defaults must validate: %v", err)
	}
}

func TestRetentionParamsFromEnv(t *testing.T) {
	env := map[string]string{
		"M3M0S_RETENTION_HALFLIFE_STATE_DAYS":      "14",
		"M3M0S_RETENTION_HALFLIFE_DECISION_DAYS":   "730",
		"M3M0S_RETENTION_HUB_WEIGHT":               "1.5",
		"M3M0S_RETENTION_HUB_MAX_FACTOR":           "4",
		"M3M0S_RETENTION_RECURRENCE_WEIGHT":        "0.25",
		"M3M0S_RETENTION_RECURRENCE_MAX_FACTOR":    "3",
		"M3M0S_RETENTION_EXPIRED_CAP":              "true",
		"M3M0S_RETENTION_EXPIRED_CAP_DAYS":         "45",
		"M3M0S_RETENTION_ACCESS_WEIGHT":            "0.25",
		"M3M0S_RETENTION_ACCESS_HALFLIFE_DAYS":     "30",
		"M3M0S_RETENTION_LEGACY_ACCESS_DAYS_CAP":   "2",
		"M3M0S_RETENTION_HALFLIFE_OTHER_DAYS":      " 120 ",
		"UNRELATED_VARIABLE_THAT_MUST_BE_IGNORED":  "x",
		"M3M0S_RETENTION_HALFLIFE_NOISE_DAYS":      "",
		"M3M0S_RETENTION_HALFLIFE_PROCEDURE_DAYS":  "",
		"M3M0S_RETENTION_HALFLIFE_PREFERENCE_DAYS": "",
	}
	p, err := RetentionParamsFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if p.HalfLifeDays[factkind.State] != 14 || p.HalfLifeDays[factkind.Decision] != 730 || p.HalfLifeDays[factkind.Other] != 120 {
		t.Errorf("half-lives = %v", p.HalfLifeDays)
	}
	if p.HalfLifeDays[factkind.Noise] != 7 {
		t.Errorf("an empty variable must keep the default, noise = %v", p.HalfLifeDays[factkind.Noise])
	}
	if p.HubWeight != 1.5 || p.HubMaxFactor != 4 || p.RecurrenceWeight != 0.25 || p.RecurrenceMaxFactor != 3 {
		t.Errorf("bonus params = %+v", p)
	}
	if !p.ExpiredCapEnabled || p.ExpiredCapDays != 45 {
		t.Errorf("expired cap = %v / %v", p.ExpiredCapEnabled, p.ExpiredCapDays)
	}
	if p.AccessWeight != 0.25 || p.AccessHalfLifeDays != 30 || p.LegacyAccessDaysCap != 2 {
		t.Errorf("access params = %+v", p)
	}
}

func TestRetentionParamsFromEnvRejectsBadValues(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"not a number":     {"M3M0S_RETENTION_HALFLIFE_STATE_DAYS": "soon"},
		"zero half-life":   {"M3M0S_RETENTION_HALFLIFE_NOISE_DAYS": "0"},
		"negative":         {"M3M0S_RETENTION_HALFLIFE_RULE_DAYS": "-5"},
		"nan":              {"M3M0S_RETENTION_HALFLIFE_RULE_DAYS": "NaN"},
		"factor below one": {"M3M0S_RETENTION_HUB_MAX_FACTOR": "0.5"},
		"bad bool":         {"M3M0S_RETENTION_EXPIRED_CAP": "maybe"},
		"negative weight":  {"M3M0S_RETENTION_HUB_WEIGHT": "-1"},
		"zero cap days":    {"M3M0S_RETENTION_EXPIRED_CAP_DAYS": "0"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := RetentionParamsFromEnv(func(k string) string { return env[k] }); err == nil {
				t.Errorf("expected an error for %v", env)
			}
		})
	}
}

func TestParseRetentionModel(t *testing.T) {
	for in, want := range map[string]RetentionModel{"": RetentionLegacy, "legacy": RetentionLegacy, " Per-Kind ": RetentionPerKind, "per_kind": RetentionPerKind} {
		got, err := ParseRetentionModel(in)
		if err != nil || got != want {
			t.Errorf("ParseRetentionModel(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseRetentionModel("fast"); err == nil {
		t.Error("an unknown model must be an error, never a silent default")
	}
}

// ── term 1: half-life by kind ──

func TestPerKindHalfLifeByKind(t *testing.T) {
	p := DefaultRetentionParams()
	cases := map[string]float64{
		factkind.Decision: 365, factkind.Rule: 365, factkind.Preference: 365, factkind.Gotcha: 365,
		factkind.Procedure: 365, factkind.Other: 90, factkind.State: 21, factkind.Noise: 7,
	}
	for kind, want := range cases {
		e := synth("k", kind, factkind.SourceLLM, 10, nil)
		if got := p.effectiveHalfLife(e, retNow, 0); got != want {
			t.Errorf("%s half-life = %v, want %v", kind, got, want)
		}
		// The age term is exactly 2^(-age/halfLife) with no access.
		wantScore := math.Exp(-ln2 * 10 / want)
		if got := perKindScore(e, retNow, p, 0); !almost(got, wantScore) {
			t.Errorf("%s score = %v, want %v", kind, got, wantScore)
		}
	}
}

func TestPerKindDurableOutlivesTransient(t *testing.T) {
	p := DefaultRetentionParams()
	age := 100.0
	durable := perKindScore(synth("d", factkind.Decision, factkind.SourceLLM, age, nil), retNow, p, 0)
	other := perKindScore(synth("o", factkind.Other, factkind.SourceLLM, age, nil), retNow, p, 0)
	state := perKindScore(synth("s", factkind.State, factkind.SourceLLM, age, nil), retNow, p, 0)
	noise := perKindScore(synth("n", factkind.Noise, factkind.SourceLLM, age, nil), retNow, p, 0)
	if !(durable > other && other > state && state > noise) {
		t.Errorf("expected durable > other > state > noise, got %v %v %v %v", durable, other, state, noise)
	}
	if durable < ColdRetentionThreshold || other < ColdRetentionThreshold {
		t.Errorf("a 100-day durable/other fact must survive: %v %v", durable, other)
	}
	if state >= ColdRetentionThreshold || noise >= ColdRetentionThreshold {
		t.Errorf("a 100-day state/noise fact must be cold: %v %v", state, noise)
	}
}

// ── safety rules ──

func TestMissingOrInvalidKindReadsAsOther(t *testing.T) {
	p := DefaultRetentionParams()
	for name, e := range map[string]schema.Entry{
		"no kind":       synth("a", "", "", 10, nil),
		"invalid kind":  synth("b", "banana", factkind.SourceLLM, 10, nil),
		"source only":   synth("c", "", factkind.SourceLLM, 10, nil),
		"explicit kind": synth("d", factkind.Other, factkind.SourceLLM, 10, nil),
	} {
		if got := p.effectiveHalfLife(e, retNow, 0); got != 90 {
			t.Errorf("%s: half-life = %v, want the one of other (90)", name, got)
		}
	}
}

func TestHeuristicKindNeverAcceleratesForgetting(t *testing.T) {
	p := DefaultRetentionParams()
	for _, kind := range []string{factkind.State, factkind.Noise} {
		for _, src := range []string{factkind.SourceHeuristic, ""} {
			e := synth("h", kind, src, 30, nil)
			if got := p.effectiveHalfLife(e, retNow, 0); got < p.HalfLifeDays[factkind.Other] {
				t.Errorf("kind %s from source %q got half-life %v, below other's %v", kind, src, got, p.HalfLifeDays[factkind.Other])
			}
			llm := synth("l", kind, factkind.SourceLLM, 30, nil)
			if perKindScore(e, retNow, p, 0) <= perKindScore(llm, retNow, p, 0) {
				t.Errorf("a heuristic %s must score higher than the LLM one", kind)
			}
		}
	}
	// A heuristic tag may still lengthen the half-life.
	if got := p.effectiveHalfLife(synth("h", factkind.Decision, factkind.SourceHeuristic, 1, nil), retNow, 0); got != 365 {
		t.Errorf("heuristic decision half-life = %v, want 365", got)
	}
}

func TestHeuristicFloorFollowsConfiguredOther(t *testing.T) {
	p := DefaultRetentionParams()
	p.HalfLifeDays[factkind.Other] = 200
	if got := p.effectiveHalfLife(synth("h", factkind.Noise, factkind.SourceHeuristic, 1, nil), retNow, 0); got != 200 {
		t.Errorf("floor = %v, want other's 200", got)
	}
	if got := p.effectiveHalfLife(synth("l", factkind.Noise, factkind.SourceLLM, 1, nil), retNow, 0); got != 7 {
		t.Errorf("llm noise = %v, want 7", got)
	}
}

func TestPinnedAndManualDurableAreExempt(t *testing.T) {
	ev := newRetentionEval(nil, RetentionPerKind, DefaultRetentionParams())
	ancient := 5000.0
	cases := []struct {
		name string
		e    schema.Entry
		want bool
	}{
		{"plain llm decision", synth("a", factkind.Decision, factkind.SourceLLM, ancient, nil), true},
		{"pinned", synth("b", factkind.State, factkind.SourceLLM, ancient, map[string]string{metadataKeyPinned: "true"}), false},
		{"manual durable", synth("c", factkind.Rule, factkind.SourceManual, ancient, nil), false},
		{"manual gotcha", synth("d", factkind.Gotcha, factkind.SourceManual, ancient, nil), false},
		{"manual state decays", synth("e", factkind.State, factkind.SourceManual, ancient, nil), true},
		{"manual other decays", synth("f", factkind.Other, factkind.SourceManual, ancient, nil), true},
		{"heuristic durable decays", synth("g", factkind.Decision, factkind.SourceHeuristic, ancient, nil), true},
		{"raw (not extracted)", schema.Entry{ID: "h", Metadata: map[string]string{}}, false},
	}
	for _, c := range cases {
		if got := ev.eligible(c.e); got != c.want {
			t.Errorf("%s: eligible = %v, want %v", c.name, got, c.want)
		}
	}
	// The legacy model keeps D22's eligibility (a manual tag does not exempt).
	legacy := newRetentionEval(nil, RetentionLegacy, DefaultRetentionParams())
	if !legacy.eligible(synth("c", factkind.Rule, factkind.SourceManual, ancient, nil)) {
		t.Error("legacy eligibility must not change")
	}
}

// ── term 2: hub bonus ──

func TestHubBonusStretchesHalfLife(t *testing.T) {
	p := DefaultRetentionParams()
	e := synth("hub", factkind.State, factkind.SourceLLM, 40, nil)
	base := p.effectiveHalfLife(e, retNow, 0)
	if base != 21 {
		t.Fatalf("base = %v", base)
	}
	one := p.effectiveHalfLife(e, retNow, 1)
	ten := p.effectiveHalfLife(e, retNow, 10)
	if !(one > base && ten > one) {
		t.Errorf("half-life must grow with incoming links: %v %v %v", base, one, ten)
	}
	if want := 21 * (1 + 0.5*math.Log1p(1)); !almost(one, want) {
		t.Errorf("one link = %v, want %v", one, want)
	}
	if got := p.effectiveHalfLife(e, retNow, 100000); !almost(got, 21*p.HubMaxFactor) {
		t.Errorf("the bonus must be capped at %vx, got %v", p.HubMaxFactor, got)
	}
	if perKindScore(e, retNow, p, 10) <= perKindScore(e, retNow, p, 0) {
		t.Error("a hub must score higher than an isolated fact of the same age")
	}
}

func TestIncomingLinksCountOnlyActiveDistinctSources(t *testing.T) {
	target := synth("target", factkind.Rule, factkind.SourceLLM, 5, nil)
	a := synth("a", "", "", 5, map[string]string{metadataKeyRelatedTo: "target,target,a,ghost"}) // dup + self + dangling
	b := synth("b", "", "", 5, map[string]string{metadataKeyRelatedTo: "target"})
	gone := synth("gone", "", "", 5, map[string]string{metadataKeyRelatedTo: "target", metadataKeyDeletedAt: retNow.Format(time.RFC3339)})
	index := map[string]schema.Entry{"target": target, "a": a, "b": b, "gone": gone}
	got := buildIncomingLinks(index)
	if got["target"] != 2 {
		t.Errorf("incoming(target) = %d, want 2 (a once, b; not the soft-deleted one)", got["target"])
	}
	if got["a"] != 0 {
		t.Errorf("a self-link must not count, got %d", got["a"])
	}
}

func TestHubBonusUsedBySweepEvaluator(t *testing.T) {
	// 70 days old state fact: cold alone (21 d half-life, cut at ~57 d), saved
	// by three linking facts (half-life x1.69).
	target := synth("target", factkind.State, factkind.SourceLLM, 70, nil)
	l1 := synth("l1", factkind.Rule, factkind.SourceLLM, 1, map[string]string{metadataKeyRelatedTo: "target"})
	l2 := synth("l2", factkind.Rule, factkind.SourceLLM, 1, map[string]string{metadataKeyRelatedTo: "target"})
	l3 := synth("l3", factkind.Rule, factkind.SourceLLM, 1, map[string]string{metadataKeyRelatedTo: "target"})
	alone := newRetentionEval(map[string]schema.Entry{"target": target}, RetentionPerKind, DefaultRetentionParams())
	linked := newRetentionEval(map[string]schema.Entry{"target": target, "l1": l1, "l2": l2, "l3": l3}, RetentionPerKind, DefaultRetentionParams())
	if !alone.cold(target, retNow) {
		t.Fatal("the isolated state fact should be cold")
	}
	if linked.cold(target, retNow) {
		t.Errorf("with 3 incoming links it should survive, score %v", linked.score(target, retNow))
	}
}

// ── term 3: recurrence bonus ──

func TestRecurrenceBonusStretchesHalfLife(t *testing.T) {
	p := DefaultRetentionParams()
	plain := synth("r0", factkind.Other, factkind.SourceLLM, 100, nil)
	recur := synth("r3", factkind.Other, factkind.SourceLLM, 100, map[string]string{metadataKeyRecurrence: "3"})
	if got, want := p.effectiveHalfLife(recur, retNow, 0), 90*(1+0.5*math.Log1p(3)); !almost(got, want) {
		t.Errorf("half-life = %v, want %v", got, want)
	}
	if perKindScore(recur, retNow, p, 0) <= perKindScore(plain, retNow, p, 0) {
		t.Error("a recurring fact must score higher")
	}
	huge := synth("rh", factkind.Other, factkind.SourceLLM, 100, map[string]string{metadataKeyRecurrence: "100000"})
	if got := p.effectiveHalfLife(huge, retNow, 0); !almost(got, 90*p.RecurrenceMaxFactor) {
		t.Errorf("recurrence bonus must be capped, got %v", got)
	}
	// Garbage in the key is ignored (reads as 0), never a crash or a penalty.
	junk := synth("rj", factkind.Other, factkind.SourceLLM, 100, map[string]string{metadataKeyRecurrence: "-4x"})
	if got := p.effectiveHalfLife(junk, retNow, 0); got != 90 {
		t.Errorf("junk recurrence changed the half-life to %v", got)
	}
}

func TestBonusesNeverShortenAHalfLife(t *testing.T) {
	p := DefaultRetentionParams()
	for kind, base := range p.HalfLifeDays {
		e := synth("b", kind, factkind.SourceLLM, 3, nil)
		for incoming := 0; incoming < 5; incoming++ {
			if got := p.effectiveHalfLife(e, retNow, incoming); got < base {
				t.Errorf("%s with %d links: %v < base %v", kind, incoming, got, base)
			}
		}
	}
}

// ── term 4: cap for expired valid_until ──

func TestExpiredValidUntilCapOffByDefault(t *testing.T) {
	p := DefaultRetentionParams()
	past := retNow.Add(-24 * time.Hour).Format(time.RFC3339)
	e := synth("x", factkind.Decision, factkind.SourceLLM, 100, map[string]string{metadataKeyValidUntil: past})
	if got := p.effectiveHalfLife(e, retNow, 0); got != 365 {
		t.Errorf("with the cap off an expired fact keeps its half-life, got %v", got)
	}
}

func TestExpiredValidUntilCapWhenEnabled(t *testing.T) {
	p := DefaultRetentionParams()
	p.ExpiredCapEnabled = true
	past := retNow.Add(-24 * time.Hour).Format(time.RFC3339)
	future := retNow.Add(24 * time.Hour).Format(time.RFC3339)

	expired := synth("x", factkind.Decision, factkind.SourceLLM, 100, map[string]string{metadataKeyValidUntil: past})
	if got := p.effectiveHalfLife(expired, retNow, 0); got != 60 {
		t.Errorf("expired decision half-life = %v, want 60", got)
	}
	if perKindScore(expired, retNow, p, 0) >= perKindScore(synth("y", factkind.Decision, factkind.SourceLLM, 100, nil), retNow, p, 0) {
		t.Error("the cap must lower the score of an expired fact")
	}
	valid := synth("v", factkind.Decision, factkind.SourceLLM, 100, map[string]string{metadataKeyValidUntil: future})
	if got := p.effectiveHalfLife(valid, retNow, 0); got != 365 {
		t.Errorf("a future valid_until must not cap, got %v", got)
	}
	// A cap never lengthens a half-life that is already shorter.
	short := synth("n", factkind.Noise, factkind.SourceLLM, 100, map[string]string{metadataKeyValidUntil: past})
	if got := p.effectiveHalfLife(short, retNow, 0); got != 7 {
		t.Errorf("noise half-life = %v, want 7 (cap must not lengthen)", got)
	}
	// The cap applies after the bonuses.
	hub := synth("h", factkind.Decision, factkind.SourceLLM, 100, map[string]string{metadataKeyValidUntil: past})
	if got := p.effectiveHalfLife(hub, retNow, 50); got != 60 {
		t.Errorf("capped hub = %v, want 60", got)
	}
	// A malformed valid_until does not cap (same leniency as isExpired).
	bad := synth("b", factkind.Decision, factkind.SourceLLM, 100, map[string]string{metadataKeyValidUntil: "someday"})
	if got := p.effectiveHalfLife(bad, retNow, 0); got != 365 {
		t.Errorf("malformed valid_until capped to %v", got)
	}
}

// ── term 5: access reinforcement per day ──

func TestBumpAccessCountsDistinctDays(t *testing.T) {
	d1 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	md := map[string]string{}
	for i := 0; i < 50; i++ { // a burst inside one day
		md = bumpAccessMetadata(md, d1.Add(time.Duration(i)*time.Minute))
	}
	if md[metadataKeyAccessCount] != "50" || md[metadataKeyAccessDays] != "1" {
		t.Fatalf("burst: count=%s days=%s, want 50 and 1", md[metadataKeyAccessCount], md[metadataKeyAccessDays])
	}
	md = bumpAccessMetadata(md, d1.Add(24*time.Hour))
	md = bumpAccessMetadata(md, d1.Add(48*time.Hour))
	if md[metadataKeyAccessDays] != "3" || md[metadataKeyAccessCount] != "52" {
		t.Errorf("after two more days: count=%s days=%s, want 52 and 3", md[metadataKeyAccessCount], md[metadataKeyAccessDays])
	}
}

func TestBumpAccessOnLegacyMetadata(t *testing.T) {
	// Read before D39: access_count and last_accessed_at but no access_days.
	d1 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	md := map[string]string{metadataKeyAccessCount: "7", metadataKeyLastAccessed: d1.Format(time.RFC3339)}
	same := bumpAccessMetadata(md, d1.Add(time.Hour))
	if same[metadataKeyAccessDays] != "1" || same[metadataKeyAccessCount] != "8" {
		t.Errorf("same day: %v", same)
	}
	next := bumpAccessMetadata(md, d1.Add(26*time.Hour))
	if next[metadataKeyAccessDays] != "1" || next[metadataKeyAccessCount] != "8" {
		t.Errorf("next day on legacy data: %v", next)
	}
	if md[metadataKeyAccessCount] != "7" || md[metadataKeyAccessDays] != "" {
		t.Error("bumpAccessMetadata must not mutate its input")
	}
}

func TestBurstOfReadsDoesNotInflatePerKindScore(t *testing.T) {
	p := DefaultRetentionParams()
	last := retNow.Add(-time.Hour).Format(time.RFC3339)
	burst := synth("burst", factkind.Other, factkind.SourceLLM, 400, map[string]string{
		metadataKeyAccessCount: "500", metadataKeyAccessDays: "1", metadataKeyLastAccessed: last})
	habit := synth("habit", factkind.Other, factkind.SourceLLM, 400, map[string]string{
		metadataKeyAccessCount: "500", metadataKeyAccessDays: "40", metadataKeyLastAccessed: last})
	if got, want := p.accessDays(burst), 1; got != want {
		t.Errorf("burst access days = %d, want %d", got, want)
	}
	sb, sh := perKindScore(burst, retNow, p, 0), perKindScore(habit, retNow, p, 0)
	if sb >= sh {
		t.Errorf("a one-day burst (%v) must score below a 40-day habit (%v)", sb, sh)
	}
	// The legacy score is inflated by the same burst, which is the point.
	if retentionScore(burst, retNow) <= sb {
		t.Error("expected the legacy score to be inflated by the burst")
	}
	// One read on one day: reinforcement is w*ln(2).
	one := synth("one", factkind.Other, factkind.SourceLLM, 400, map[string]string{
		metadataKeyAccessCount: "1", metadataKeyAccessDays: "1", metadataKeyLastAccessed: retNow.Format(time.RFC3339)})
	ageTerm := math.Exp(-ln2 * 400 / 90)
	if got, want := perKindScore(one, retNow, p, 0), ageTerm+0.5*math.Log(2); !almost(got, want) {
		t.Errorf("score = %v, want %v", got, want)
	}
}

func TestAccessDaysFallbackForEntriesWithoutIt(t *testing.T) {
	p := DefaultRetentionParams()
	if got := p.accessDays(synth("a", "", "", 1, map[string]string{metadataKeyAccessCount: "40"})); got != 3 {
		t.Errorf("legacy entry with 40 reads: %d days, want the cap of 3", got)
	}
	if got := p.accessDays(synth("b", "", "", 1, map[string]string{metadataKeyAccessCount: "2"})); got != 2 {
		t.Errorf("legacy entry with 2 reads: %d days, want 2", got)
	}
	if got := p.accessDays(synth("c", "", "", 1, nil)); got != 0 {
		t.Errorf("never read: %d days", got)
	}
	// access_days can never exceed access_count (inconsistent metadata).
	if got := p.accessDays(synth("d", "", "", 1, map[string]string{metadataKeyAccessCount: "2", metadataKeyAccessDays: "9"})); got != 2 {
		t.Errorf("days above count: %d, want 2", got)
	}
}

func TestPerKindAccessDecaysWithTimeSinceLastRead(t *testing.T) {
	p := DefaultRetentionParams()
	mk := func(daysAgo float64) schema.Entry {
		return synth("a", factkind.Other, factkind.SourceLLM, 300, map[string]string{
			metadataKeyAccessCount: "5", metadataKeyAccessDays: "5",
			metadataKeyLastAccessed: retNow.Add(-time.Duration(daysAgo * 24 * float64(time.Hour))).Format(time.RFC3339)})
	}
	if perKindScore(mk(1), retNow, p, 0) <= perKindScore(mk(90), retNow, p, 0) {
		t.Error("a recent read must reinforce more than an old one")
	}
}

// ── model equivalence: legacy is unchanged ──

// legacyReference is D22's sweep written out independently of retentionEval.
func legacyReference(index map[string]schema.Entry, now time.Time) (soft, hard []string, evaluated int) {
	for id, e := range index {
		if e.Metadata[metadataKeyDeletedAt] != "" {
			t, err := time.Parse(time.RFC3339, e.Metadata[metadataKeyDeletedAt])
			if err == nil && now.Sub(t) >= SoftDeleteGraceDays*24*time.Hour {
				hard = append(hard, id)
			}
			continue
		}
		if e.Metadata[metadataKeyPinned] == "true" {
			continue
		}
		if _, ok := e.Metadata[metadataKeySourceContentRef]; !ok {
			continue
		}
		evaluated++
		age := now.Sub(e.CreatedAt).Hours() / 24
		last := e.UpdatedAt
		if raw := e.Metadata[metadataKeyLastAccessed]; raw != "" {
			if t, err := time.Parse(time.RFC3339, raw); err == nil {
				last = t
			}
		}
		since := now.Sub(last).Hours() / 24
		n := 0
		_, _ = fmt.Sscanf(e.Metadata[metadataKeyAccessCount], "%d", &n)
		score := math.Exp(-math.Ln2*age/30) + 0.5*math.Log1p(float64(n))*math.Exp(-math.Ln2*since/45)
		if score < 0.15 {
			soft = append(soft, id)
		}
	}
	sort.Strings(soft)
	sort.Strings(hard)
	return soft, hard, evaluated
}

// mixedPopulation is a deterministic synthetic corpus covering every kind and
// source, pinned and raw entries, reads, links, expired facts and soft-deleted
// ones.
func mixedPopulation(n int) map[string]schema.Entry {
	rng := rand.New(rand.NewSource(7))
	kinds := append([]string{""}, factkind.All...)
	sources := []string{"", factkind.SourceLLM, factkind.SourceHeuristic, factkind.SourceManual}
	index := make(map[string]schema.Entry, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("e%04d", i)
		extra := map[string]string{}
		switch rng.Intn(10) {
		case 0:
			extra[metadataKeyPinned] = "true"
		case 1:
			extra[metadataKeyAccessCount] = fmt.Sprint(1 + rng.Intn(60))
			extra[metadataKeyLastAccessed] = retNow.Add(-time.Duration(rng.Intn(120)) * 24 * time.Hour).Format(time.RFC3339)
		case 2:
			extra[metadataKeyValidUntil] = retNow.Add(-time.Duration(rng.Intn(90)) * 24 * time.Hour).Format(time.RFC3339)
		case 3:
			extra[metadataKeyDeletedAt] = retNow.Add(-time.Duration(rng.Intn(60)) * 24 * time.Hour).Format(time.RFC3339)
		case 4:
			extra[metadataKeyRelatedTo] = fmt.Sprintf("e%04d,e%04d", rng.Intn(n), rng.Intn(n))
		}
		e := synth(id, kinds[rng.Intn(len(kinds))], sources[rng.Intn(len(sources))], rng.Float64()*400, extra)
		if rng.Intn(12) == 0 {
			delete(e.Metadata, metadataKeySourceContentRef) // raw entry
		}
		index[id] = e
	}
	return index
}

func TestLegacyModelMatchesD22Exactly(t *testing.T) {
	index := mixedPopulation(3000)
	wantSoft, wantHard, wantEval := legacyReference(index, retNow)
	if len(wantSoft) == 0 || len(wantHard) == 0 {
		t.Fatalf("the fixture must exercise both outcomes, got %d soft %d hard", len(wantSoft), len(wantHard))
	}
	// Whatever the per-kind parameters are, legacy must ignore them.
	weird := DefaultRetentionParams()
	weird.HalfLifeDays[factkind.Decision] = 0.5
	weird.ExpiredCapEnabled = true
	weird.HubWeight = 9

	for name, p := range map[string]RetentionParams{"default": DefaultRetentionParams(), "weird": weird} {
		s := newTestStore(t)
		s.mu.Lock()
		s.index = index
		s.mu.Unlock()
		if err := s.SetRetention(RetentionLegacy, p); err != nil {
			t.Fatal(err)
		}
		res, err := s.ForgetSweep(context.Background(), true, retNow)
		if err != nil {
			t.Fatal(err)
		}
		gotSoft, gotHard := append([]string(nil), res.SoftDeleteCandidates...), append([]string(nil), res.HardDeleteCandidates...)
		sort.Strings(gotSoft)
		sort.Strings(gotHard)
		if strings.Join(gotSoft, ",") != strings.Join(wantSoft, ",") {
			t.Errorf("%s: soft candidates differ from the D22 reference (%d vs %d)", name, len(gotSoft), len(wantSoft))
		}
		if strings.Join(gotHard, ",") != strings.Join(wantHard, ",") {
			t.Errorf("%s: hard candidates differ from the D22 reference (%d vs %d)", name, len(gotHard), len(wantHard))
		}
		if res.Evaluated != wantEval || res.Model != RetentionLegacy || res.HardDeleteSpared != 0 {
			t.Errorf("%s: evaluated=%d model=%s spared=%d", name, res.Evaluated, res.Model, res.HardDeleteSpared)
		}
		// The Insights outlook agrees too.
		f := s.ForgettingStats(retNow)
		if f.ColdNow != len(wantSoft) || f.HardDeleteDue != len(wantHard) || f.Eligible != wantEval || f.Model != "legacy" {
			t.Errorf("%s: forgetting stats %+v disagree with the reference", name, f)
		}
	}
}

func TestLegacyScoreUnchangedByNewMetadata(t *testing.T) {
	// access_days, recurrence and kind keys must not move the legacy score.
	base := synth("a", "", "", 40, map[string]string{metadataKeyAccessCount: "4", metadataKeyLastAccessed: retNow.Add(-72 * time.Hour).Format(time.RFC3339)})
	with := synth("a", factkind.Noise, factkind.SourceLLM, 40, map[string]string{
		metadataKeyAccessCount: "4", metadataKeyLastAccessed: retNow.Add(-72 * time.Hour).Format(time.RFC3339),
		metadataKeyAccessDays: "1", metadataKeyRecurrence: "5"})
	if retentionScore(base, retNow) != retentionScore(with, retNow) {
		t.Error("the legacy score must ignore kind, access_days and recurrence")
	}
}

func TestDefaultStoreIsLegacy(t *testing.T) {
	s := newTestStore(t)
	m, p := s.Retention()
	if m != RetentionLegacy {
		t.Errorf("default model = %s", m)
	}
	if p.HalfLifeDays[factkind.State] != 21 {
		t.Errorf("default params not loaded: %v", p.HalfLifeDays)
	}
	// SetRetention validates and normalizes.
	if err := s.SetRetention("nope", p); err == nil {
		t.Error("an unknown model must be rejected")
	}
	bad := p.Clone()
	bad.HalfLifeDays[factkind.State] = 0
	if err := s.SetRetention(RetentionPerKind, bad); err == nil {
		t.Error("invalid params must be rejected")
	}
	if m, _ := s.Retention(); m != RetentionLegacy {
		t.Error("a rejected SetRetention must leave the model untouched")
	}
	// The returned params are a copy.
	_, cp := s.Retention()
	cp.HalfLifeDays[factkind.State] = 1
	if _, again := s.Retention(); again.HalfLifeDays[factkind.State] != 21 {
		t.Error("Retention must return a copy of the parameters")
	}
}

// ── dry-run with a chosen model ──

func TestDryRunCanPreviewAnotherModelWithoutChangingAnything(t *testing.T) {
	s := newTestStore(t)
	// 100-day-old decision (llm): cold under legacy (cut at ~82 d), kept per-kind.
	dec := synth("dec", factkind.Decision, factkind.SourceLLM, 100, nil)
	// 100-day-old noise (llm): cold under both.
	noise := synth("noise", factkind.Noise, factkind.SourceLLM, 100, nil)
	setIndexEntry(s, dec)
	setIndexEntry(s, noise)

	legacy, err := s.ForgetSweepModel(context.Background(), true, retNow, RetentionLegacy)
	if err != nil {
		t.Fatal(err)
	}
	perKind, err := s.ForgetSweepModel(context.Background(), true, retNow, RetentionPerKind)
	if err != nil {
		t.Fatal(err)
	}
	if !containsID(legacy.SoftDeleteCandidates, "dec") || !containsID(legacy.SoftDeleteCandidates, "noise") {
		t.Errorf("legacy candidates = %v", legacy.SoftDeleteCandidates)
	}
	if containsID(perKind.SoftDeleteCandidates, "dec") || !containsID(perKind.SoftDeleteCandidates, "noise") {
		t.Errorf("per-kind candidates = %v (want only noise)", perKind.SoftDeleteCandidates)
	}
	if legacy.Model != RetentionLegacy || perKind.Model != RetentionPerKind || !perKind.DryRun {
		t.Errorf("models reported: %s / %s", legacy.Model, perKind.Model)
	}
	if got := getIndexEntry(t, s, "dec"); isSoftDeleted(got) {
		t.Error("a dry-run must not change the entry")
	}
	// The configured model is still legacy.
	if m, _ := s.Retention(); m != RetentionLegacy {
		t.Errorf("configured model changed to %s", m)
	}
}

func TestRealSweepRefusesAModelOtherThanTheConfiguredOne(t *testing.T) {
	s := newTestStore(t)
	setIndexEntry(s, synth("noise", factkind.Noise, factkind.SourceLLM, 90, nil))
	if _, err := s.ForgetSweepModel(context.Background(), false, retNow, RetentionPerKind); err != ErrRetentionModelMismatch {
		t.Errorf("err = %v, want ErrRetentionModelMismatch", err)
	}
	if isSoftDeleted(getIndexEntry(t, s, "noise")) {
		t.Error("nothing may be deleted when the call is refused")
	}
	if _, err := s.ForgetSweepModel(context.Background(), true, retNow, "bogus"); err == nil {
		t.Error("an unknown model must be an error")
	}
}

func TestPerKindSweepSoftDeletesOnlyWhenConfigured(t *testing.T) {
	s := newTestStore(t)
	e := mustAddExtracted(t, s, "an ephemeral status line", "code:retention-flip")
	old := time.Now().Add(-40 * 24 * time.Hour) // cold under both once tagged noise
	e.CreatedAt, e.UpdatedAt = old, old
	e.Metadata[factkind.MetadataKey], e.Metadata[factkind.MetadataSourceKey] = factkind.Noise, factkind.SourceLLM
	setIndexEntry(s, e)

	// Flip the setting: the same job now uses the per-kind score.
	if err := s.SetRetention(RetentionPerKind, DefaultRetentionParams()); err != nil {
		t.Fatal(err)
	}
	res, err := s.ForgetSweep(context.Background(), false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if res.Model != RetentionPerKind || !containsID(res.SoftDeleted, e.ID) {
		t.Errorf("per-kind sweep result = %+v", res)
	}
}

func TestPerKindSweepKeepsDurableThatLegacyWouldForget(t *testing.T) {
	s := newTestStore(t)
	e := mustAddExtracted(t, s, "a root cause worth keeping", "code:retention-durable")
	old := time.Now().Add(-120 * 24 * time.Hour)
	e.CreatedAt, e.UpdatedAt = old, old
	e.Metadata[factkind.MetadataKey], e.Metadata[factkind.MetadataSourceKey] = factkind.Gotcha, factkind.SourceLLM
	setIndexEntry(s, e)

	if err := s.SetRetention(RetentionPerKind, DefaultRetentionParams()); err != nil {
		t.Fatal(err)
	}
	res, err := s.ForgetSweep(context.Background(), false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.SoftDeleted) != 0 || isSoftDeleted(getIndexEntry(t, s, e.ID)) {
		t.Errorf("a 120-day gotcha must survive the per-kind sweep: %+v", res)
	}
}

func TestPerKindSparesHardDeleteOfWhatItWouldKeep(t *testing.T) {
	s := newTestStore(t)
	longAgo := retNow.Add(-(SoftDeleteGraceDays + 5) * 24 * time.Hour).Format(time.RFC3339)
	// Soft-deleted earlier by the legacy score, but a 100-day-old rule is
	// durable: per-kind must not hard-delete it.
	spared := synth("spared", factkind.Rule, factkind.SourceLLM, 100, map[string]string{metadataKeyDeletedAt: longAgo})
	// A noise fact past grace is still cold per-kind: hard-delete proceeds.
	gone := synth("gone", factkind.Noise, factkind.SourceLLM, 100, map[string]string{metadataKeyDeletedAt: longAgo})
	// Manual-durable (exempt) soft-deleted earlier: also spared.
	manual := synth("manual", factkind.Decision, factkind.SourceManual, 900, map[string]string{metadataKeyDeletedAt: longAgo})
	for _, e := range []schema.Entry{spared, gone, manual} {
		setIndexEntry(s, e)
	}

	legacy, _ := s.ForgetSweepModel(context.Background(), true, retNow, RetentionLegacy)
	if len(legacy.HardDeleteCandidates) != 3 || legacy.HardDeleteSpared != 0 {
		t.Errorf("legacy hard candidates = %v (spared %d), want all 3 as in D22", legacy.HardDeleteCandidates, legacy.HardDeleteSpared)
	}
	pk, _ := s.ForgetSweepModel(context.Background(), true, retNow, RetentionPerKind)
	if len(pk.HardDeleteCandidates) != 1 || pk.HardDeleteCandidates[0] != "gone" || pk.HardDeleteSpared != 2 {
		t.Errorf("per-kind hard candidates = %v spared %d", pk.HardDeleteCandidates, pk.HardDeleteSpared)
	}

	// The Insights count agrees once the setting is flipped.
	if err := s.SetRetention(RetentionPerKind, DefaultRetentionParams()); err != nil {
		t.Fatal(err)
	}
	if f := s.ForgettingStats(retNow); f.HardDeleteDue != 1 {
		t.Errorf("hard_delete_due = %d, want 1", f.HardDeleteDue)
	}
}

// ── shadow mode in Insights ──

func TestShadowStatsByKindAndHorizon(t *testing.T) {
	s := newTestStore(t)
	for _, e := range []schema.Entry{
		synth("dec", factkind.Decision, factkind.SourceLLM, 70, nil), // legacy cold now; per-kind never in 90 d
		synth("st", factkind.State, factkind.SourceLLM, 10, nil),     // per-kind cold at +60/+90 (age 70 > 57.5), legacy cold from +60
		synth("nz", factkind.Noise, factkind.SourceLLM, 10, nil),     // per-kind cold at +30 (40 > 19), legacy not until +60
		synth("man", factkind.Rule, factkind.SourceManual, 70, nil),  // protected under per-kind
		synth("old", "", "", 70, nil),                                // other
		synth("pin", factkind.Noise, factkind.SourceLLM, 500, map[string]string{metadataKeyPinned: "true"}),
		{ID: "raw", CreatedAt: retNow.Add(-1000 * time.Hour), UpdatedAt: retNow, Metadata: map[string]string{}},
	} {
		setIndexEntry(s, e)
	}
	f := s.ForgettingStats(retNow)
	sh := f.Shadow
	if f.Model != "legacy" || sh.Active != "legacy" {
		t.Errorf("model = %s / %s", f.Model, sh.Active)
	}
	if fmt.Sprint(sh.HorizonsDays) != "[0 30 60 90]" {
		t.Errorf("horizons = %v", sh.HorizonsDays)
	}
	if sh.Eligible != 5 || sh.Protected != 1 {
		t.Errorf("eligible=%d protected=%d, want 5 and 1", sh.Eligible, sh.Protected)
	}
	byKind := map[string][]int{}
	for _, k := range sh.ByKind {
		byKind[k.Kind] = append(append([]int{}, k.Legacy...), k.PerKind...)
	}
	// [legacy now,+30,+60,+90, perKind now,+30,+60,+90]
	if got := fmt.Sprint(byKind[factkind.Decision]); got != "[0 1 1 1 0 0 0 0]" {
		t.Errorf("decision = %s", got)
	}
	if got := fmt.Sprint(byKind[factkind.State]); got != "[0 0 0 1 0 0 1 1]" {
		t.Errorf("state = %s", got)
	}
	if got := fmt.Sprint(byKind[factkind.Noise]); got != "[0 0 0 1 0 1 1 1]" {
		t.Errorf("noise = %s", got)
	}
	if got := fmt.Sprint(byKind[factkind.Rule]); got != "[0 1 1 1 0 0 0 0]" {
		t.Errorf("manual rule (protected per-kind) = %s", got)
	}
	// Totals are the sum of the rows, and legacy ColdNow == legacy@0.
	if sh.Legacy[0] != f.ColdNow {
		t.Errorf("legacy@0 = %d, cold now = %d", sh.Legacy[0], f.ColdNow)
	}
	for i := range sh.HorizonsDays {
		var l, p int
		for _, k := range sh.ByKind {
			l += k.Legacy[i]
			p += k.PerKind[i]
		}
		if l != sh.Legacy[i] || p != sh.PerKind[i] {
			t.Errorf("horizon %d: totals %d/%d disagree with rows %d/%d", sh.HorizonsDays[i], sh.Legacy[i], sh.PerKind[i], l, p)
		}
	}
	if sh.HalfLifeDays[factkind.State] != 21 {
		t.Errorf("params not reported: %v", sh.HalfLifeDays)
	}
}

func TestShadowDoesNotChangeStatsOfTheActiveModel(t *testing.T) {
	s := newTestStore(t)
	for _, e := range mixedPopulation(500) {
		setIndexEntry(s, e)
	}
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, model := range []RetentionModel{RetentionLegacy, RetentionPerKind} {
		if err := s.SetRetention(model, DefaultRetentionParams()); err != nil {
			t.Fatal(err)
		}
		f := s.ForgettingStats(now)
		sweep, err := s.ForgetSweep(context.Background(), true, now)
		if err != nil {
			t.Fatal(err)
		}
		if f.ColdNow != len(sweep.SoftDeleteCandidates) || f.Eligible != sweep.Evaluated || f.HardDeleteDue != len(sweep.HardDeleteCandidates) {
			t.Errorf("%s: stats %+v disagree with the dry-run %+v", model, f, sweep)
		}
	}
}

// ── simulation: the numbers of issue #52 ──

// simulate returns, per horizon, the fraction of a synthetic corpus that
// survives under each model, and the durable facts forgotten at +30 d. The
// corpus mimics the snapshot of the issue: nothing older than 83 days, mostly
// recent, a mix of kinds, never read.
func simulate(model RetentionModel, p RetentionParams, horizon int) (survival float64, durableForgotten int) {
	rng := rand.New(rand.NewSource(11))
	kinds := []struct {
		kind  string
		share float64
	}{
		{factkind.Decision, 0.10}, {factkind.Rule, 0.06}, {factkind.Preference, 0.02}, {factkind.Gotcha, 0.12},
		{factkind.Procedure, 0.08}, {factkind.Other, 0.50}, {factkind.State, 0.08}, {factkind.Noise, 0.04},
	}
	const n = 20000
	index := map[string]schema.Entry{}
	for i := 0; i < n; i++ {
		x, kind := rng.Float64(), factkind.Other
		for _, k := range kinds {
			if x < k.share {
				kind = k.kind
				break
			}
			x -= k.share
		}
		age := 83 * rng.Float64() // nothing older than 83 days, like the snapshot
		index[fmt.Sprintf("s%05d", i)] = synth(fmt.Sprintf("s%05d", i), kind, factkind.SourceLLM, age, nil)
	}
	ev := newRetentionEval(index, model, p)
	at := retNow.Add(time.Duration(horizon) * 24 * time.Hour)
	kept := 0
	for _, e := range index {
		if ev.cold(e, at) {
			if factkind.Durable(e.KindValue()) {
				durableForgotten++
			}
			continue
		}
		kept++
	}
	return float64(kept) / n, durableForgotten
}

func TestSimulationReproducesTheIssueNumbers(t *testing.T) {
	p := DefaultRetentionParams()
	var leg, per [4]float64
	for i, h := range []int{30, 60, 90, 180} {
		leg[i], _ = simulate(RetentionLegacy, p, h)
		per[i], _ = simulate(RetentionPerKind, p, h)
		t.Logf("+%3d d: legacy %5.1f%%  per-kind %5.1f%%", h, leg[i]*100, per[i]*100)
	}
	// Legacy collapses like a fixed TTL.
	if leg[0] > 0.70 || leg[1] > 0.40 || leg[2] > 0.05 || leg[3] > 0.01 {
		t.Errorf("legacy should be nearly empty by +90/+180 d: %v", leg)
	}
	// Per-kind keeps most of the corpus at 30 d and a large core at 180 d.
	if per[0] < 0.88 || per[1] < 0.80 || per[2] < 0.80 || per[3] < 0.70 {
		t.Errorf("per-kind survival too low: %v", per)
	}
	for i := range leg {
		if per[i] <= leg[i] {
			t.Errorf("horizon %d: per-kind %v must survive more than legacy %v", i, per[i], leg[i])
		}
	}
	// Monotone: the corpus never grows back.
	for i := 1; i < len(per); i++ {
		if per[i] > per[i-1] {
			t.Errorf("survival is not monotone: %v", per)
		}
	}
	// The acceptance figure: no durable fact forgotten at +30 d.
	if _, d := simulate(RetentionPerKind, p, 30); d != 0 {
		t.Errorf("per-kind forgot %d durable facts at +30 d", d)
	}
	if _, d := simulate(RetentionLegacy, p, 30); d == 0 {
		t.Error("the fixture should show the legacy model forgetting durable facts at +30 d")
	}
}

func TestAgeAtCutPerKind(t *testing.T) {
	// Sanity of the closed form used in the docs: when does an unread fact go cold.
	p := DefaultRetentionParams()
	for kind, want := range map[string]float64{factkind.Noise: 19.2, factkind.State: 57.5, factkind.Other: 246.3, factkind.Rule: 999.0} {
		cut := ageAtCut(p.HalfLifeDays[kind])
		if math.Abs(cut-want) > 0.2 {
			t.Errorf("%s crosses the cut at %.1f d, expected ~%.1f", kind, cut, want)
		}
		before := synth("b", kind, factkind.SourceLLM, cut-1, nil)
		after := synth("a", kind, factkind.SourceLLM, cut+1, nil)
		if perKindScore(before, retNow, p, 0) < ColdRetentionThreshold || perKindScore(after, retNow, p, 0) >= ColdRetentionThreshold {
			t.Errorf("%s: wrong side of the cut around %.1f d", kind, cut)
		}
	}
}

// ── offline evaluation against a labelled sample ──

func sampleItem(id, label, kind, source string, ageDays int) factkind.SampleItem {
	return factkind.SampleItem{
		ID: id, Domain: "code:synthetic", Content: "synthetic " + id, Label: label, Kind: kind, KindSource: source,
		CreatedAt: retNow.Add(-time.Duration(ageDays) * 24 * time.Hour).Format(time.RFC3339),
	}
}

func TestEvaluateRetentionSample(t *testing.T) {
	items := []factkind.SampleItem{
		sampleItem("d1", "decision", "decision", "llm", 70),  // legacy forgets at +30, per-kind keeps
		sampleItem("d2", "gotcha", "other", "heuristic", 80), // durable mislabelled as other: still 90 d half-life
		sampleItem("d3", "rule", "", "", 60),                 // untagged durable: reads as other
		sampleItem("t1", "state", "state", "llm", 40),        // transient forgotten by both
		sampleItem("t2", "noise", "noise", "heuristic", 40),  // heuristic noise: floored to other, kept by per-kind
		sampleItem("u1", "", "", "", 10),                     // unlabelled: skipped
		sampleItem("u2", "banana", "", "", 10),               // invalid label: skipped
		{ID: "u3", Label: "state", CreatedAt: "not a date"},  // bad date: skipped
	}
	rep := EvaluateRetentionSample(items, DefaultRetentionParams(), RetentionSampleOptions{HorizonDays: 30, Now: retNow})
	if rep.Labelled != 5 || rep.Skipped != 3 || rep.Items != 8 {
		t.Fatalf("report = %+v", rep)
	}
	leg, per := rep.Results[0], rep.Results[1]
	if leg.Model != RetentionLegacy || per.Model != RetentionPerKind {
		t.Fatalf("results order: %+v", rep.Results)
	}
	if leg.DurableLabelled != 3 || leg.DurableForgotten != 3 || leg.TransientForgotten != 0 {
		t.Errorf("legacy = %+v", leg)
	}
	if per.DurableForgotten != 0 {
		t.Errorf("per-kind forgot durable facts: %+v", per)
	}
	if per.TransientLabelled != 2 || per.TransientForgotten != 1 {
		t.Errorf("per-kind transient = %+v (heuristic noise must not be forgotten faster than other)", per)
	}
	out := rep.String()
	if !strings.Contains(out, "per-kind") || !strings.Contains(out, "durable forgotten 0 of 3") {
		t.Errorf("rendering:\n%s", out)
	}

	// With the labels as a perfect classifier, the heuristic noise goes cold too.
	perfect := EvaluateRetentionSample(items, DefaultRetentionParams(), RetentionSampleOptions{HorizonDays: 30, Now: retNow, Kinds: SampleKindsLabel})
	if perfect.Results[1].TransientForgotten != 2 || perfect.Results[1].DurableForgotten != 0 {
		t.Errorf("label-kinds per-kind = %+v", perfect.Results[1])
	}
}

func TestEvaluateRetentionSampleListsDurableForgotten(t *testing.T) {
	// A durable fact tagged as noise by the LLM is the failure the acceptance
	// criterion exists to catch: it must be listed by id.
	items := []factkind.SampleItem{sampleItem("bad", "decision", "noise", "llm", 40)}
	rep := EvaluateRetentionSample(items, DefaultRetentionParams(), RetentionSampleOptions{HorizonDays: 30, Now: retNow})
	per := rep.Results[1]
	if per.DurableForgotten != 1 || len(per.DurableForgottenID) != 1 || per.DurableForgottenID[0] != "bad" {
		t.Errorf("per-kind = %+v", per)
	}
	if !strings.Contains(rep.String(), "durable ids: bad") {
		t.Errorf("the id must be printed:\n%s", rep.String())
	}
}
