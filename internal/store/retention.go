// Per-kind retention (D39, issue #52): the score that replaces D22's single
// 30-day half-life with a half-life chosen by the nature of the fact (D38).
//
// The shape of the score, the cut-off, the grace period and Restore are the
// ones of D22. Only the half-life of the age term changes, and the access term
// counts distinct days instead of raw reads. The legacy score (retentionScore
// in decay.go) is untouched and stays the default; the new one runs in shadow
// (Insights) until the operator switches M3M0S_RETENTION_MODEL.
//
// This code decides what is deleted irreversibly, so every rule that could
// make a fact be forgotten sooner is conservative:
//   - a kind only shortens a half-life when a model (llm) or a person
//     (manual) set it; a heuristic or unsourced tag never goes below the
//     half-life of "other";
//   - a missing or invalid kind reads as "other";
//   - a manual tag with a durable kind exempts the fact, like pinned does;
//   - the cap for an expired valid_until is off until the invalidations are
//     audited.
package store

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/factkind"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// RetentionModel selects the score the forget-sweep uses.
type RetentionModel string

const (
	// RetentionLegacy is D22's score: one 30-day half-life for every fact.
	RetentionLegacy RetentionModel = "legacy"
	// RetentionPerKind is D39's score: a half-life per nature of fact.
	RetentionPerKind RetentionModel = "per-kind"
)

// ParseRetentionModel parses the setting; an empty value is the default
// (legacy). Anything else unknown is an error, so a typo never silently picks
// a model.
func ParseRetentionModel(s string) (RetentionModel, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", string(RetentionLegacy):
		return RetentionLegacy, nil
	case string(RetentionPerKind), "perkind", "per_kind":
		return RetentionPerKind, nil
	}
	return "", fmt.Errorf("unknown retention model %q (want %q or %q)", s, RetentionLegacy, RetentionPerKind)
}

// Metadata keys read or written by the per-kind model.
const (
	// metadataKeyAccessDays counts the distinct UTC days on which the entry was
	// read (bumped by bumpAccessMetadata). It is what the new model reinforces
	// with, so a burst of reads in one day counts once.
	metadataKeyAccessDays = "access_days"
	// metadataKeyRecurrence is the number of EXTRA distinct sessions or projects
	// in which the same fact was extracted again. Nothing writes it yet (the
	// extraction pipeline does not carry a session id into the dedup merge); the
	// score reads it so the term works the day a writer exists, and an operator
	// or a backfill can already set it.
	metadataKeyRecurrence = "recurrence_count"
)

// RetentionParams is every tunable of the per-kind model, in one place.
// DefaultRetentionParams holds the starting values; RetentionParamsFromEnv
// overrides them. They are heuristics (issue #52): tune them with the
// Insights dashboard and the labelled sample, not by feel.
type RetentionParams struct {
	// HalfLifeDays is the half-life of the age term by kind. Every kind in
	// factkind.All must have a positive entry.
	HalfLifeDays map[string]float64

	// AccessHalfLifeDays and AccessWeight shape the reinforcement term, with
	// the same meaning as in D22 (DecayReinforcementHalfLifeDays and
	// DecayReinforcementWeight).
	AccessHalfLifeDays float64
	AccessWeight       float64
	// LegacyAccessDaysCap bounds the number of access days assumed for an entry
	// that has access_count but no access_days (read before D39): we cannot tell
	// a burst from a habit, so we credit at most this many days.
	LegacyAccessDaysCap int

	// Hub bonus: the half-life is multiplied by
	// min(HubMaxFactor, 1 + HubWeight*ln(1+incoming)), where incoming is the
	// number of other active entries that list this one in related_to.
	HubWeight    float64
	HubMaxFactor float64

	// Recurrence bonus, same form, over metadata.recurrence_count.
	RecurrenceWeight    float64
	RecurrenceMaxFactor float64

	// Superseded facts: when ExpiredCapEnabled and valid_until is in the past,
	// the half-life is capped at ExpiredCapDays. Disabled by default until the
	// existing invalidations are audited (about 13% of entries carry one and
	// some look like stable facts invalidated by mistake).
	ExpiredCapEnabled bool
	ExpiredCapDays    float64
}

// DefaultRetentionParams returns the starting parameters of D39.
func DefaultRetentionParams() RetentionParams {
	return RetentionParams{
		HalfLifeDays: map[string]float64{
			factkind.Decision:   365,
			factkind.Rule:       365,
			factkind.Preference: 365,
			factkind.Gotcha:     365,
			factkind.Procedure:  365,
			factkind.Other:      90,
			factkind.State:      21,
			factkind.Noise:      7,
		},
		AccessHalfLifeDays:  DecayReinforcementHalfLifeDays,
		AccessWeight:        DecayReinforcementWeight,
		LegacyAccessDaysCap: 3,
		HubWeight:           0.5,
		HubMaxFactor:        2,
		RecurrenceWeight:    0.5,
		RecurrenceMaxFactor: 2,
		ExpiredCapEnabled:   false,
		ExpiredCapDays:      60,
	}
}

// Clone returns a deep copy (the half-life map is not shared).
func (p RetentionParams) Clone() RetentionParams {
	out := p
	out.HalfLifeDays = make(map[string]float64, len(p.HalfLifeDays))
	for k, v := range p.HalfLifeDays {
		out.HalfLifeDays[k] = v
	}
	return out
}

// Validate rejects values that would make the score meaningless or silently
// aggressive (zero or negative half-lives, factors below 1).
func (p RetentionParams) Validate() error {
	for _, k := range factkind.All {
		if v, ok := p.HalfLifeDays[k]; !ok || !(v > 0) || math.IsInf(v, 0) {
			return fmt.Errorf("retention: half-life of %q must be a positive number of days", k)
		}
	}
	if !(p.AccessHalfLifeDays > 0) || math.IsInf(p.AccessHalfLifeDays, 0) {
		return fmt.Errorf("retention: access half-life must be positive")
	}
	if p.AccessWeight < 0 || math.IsNaN(p.AccessWeight) {
		return fmt.Errorf("retention: access weight must not be negative")
	}
	if p.LegacyAccessDaysCap < 0 {
		return fmt.Errorf("retention: legacy access-days cap must not be negative")
	}
	if p.HubWeight < 0 || p.RecurrenceWeight < 0 || math.IsNaN(p.HubWeight) || math.IsNaN(p.RecurrenceWeight) {
		return fmt.Errorf("retention: bonus weights must not be negative")
	}
	if !(p.HubMaxFactor >= 1) || !(p.RecurrenceMaxFactor >= 1) {
		return fmt.Errorf("retention: bonus max factors must be >= 1 (a bonus never shortens a half-life)")
	}
	if !(p.ExpiredCapDays > 0) || math.IsInf(p.ExpiredCapDays, 0) {
		return fmt.Errorf("retention: expired cap must be a positive number of days")
	}
	return nil
}

// RetentionParamsFromEnv returns the defaults overridden by environment
// variables (getenv is os.Getenv in production). Unset or empty variables keep
// the default; a malformed or invalid value is an error so the server refuses
// to start with a policy the operator did not mean.
//
//	M3M0S_RETENTION_HALFLIFE_<KIND>_DAYS   e.g. M3M0S_RETENTION_HALFLIFE_STATE_DAYS=14
//	M3M0S_RETENTION_ACCESS_HALFLIFE_DAYS, M3M0S_RETENTION_ACCESS_WEIGHT,
//	M3M0S_RETENTION_LEGACY_ACCESS_DAYS_CAP
//	M3M0S_RETENTION_HUB_WEIGHT, M3M0S_RETENTION_HUB_MAX_FACTOR
//	M3M0S_RETENTION_RECURRENCE_WEIGHT, M3M0S_RETENTION_RECURRENCE_MAX_FACTOR
//	M3M0S_RETENTION_EXPIRED_CAP (true|false), M3M0S_RETENTION_EXPIRED_CAP_DAYS
func RetentionParamsFromEnv(getenv func(string) string) (RetentionParams, error) {
	p := DefaultRetentionParams()
	var firstErr error
	num := func(name string, dst *float64) {
		raw := strings.TrimSpace(getenv(name))
		if raw == "" {
			return
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s: %q is not a number", name, raw)
			return
		}
		*dst = v
	}
	for _, k := range factkind.All {
		v := p.HalfLifeDays[k]
		num("M3M0S_RETENTION_HALFLIFE_"+strings.ToUpper(k)+"_DAYS", &v)
		p.HalfLifeDays[k] = v
	}
	num("M3M0S_RETENTION_ACCESS_HALFLIFE_DAYS", &p.AccessHalfLifeDays)
	num("M3M0S_RETENTION_ACCESS_WEIGHT", &p.AccessWeight)
	capDays := float64(p.LegacyAccessDaysCap)
	num("M3M0S_RETENTION_LEGACY_ACCESS_DAYS_CAP", &capDays)
	p.LegacyAccessDaysCap = int(capDays)
	num("M3M0S_RETENTION_HUB_WEIGHT", &p.HubWeight)
	num("M3M0S_RETENTION_HUB_MAX_FACTOR", &p.HubMaxFactor)
	num("M3M0S_RETENTION_RECURRENCE_WEIGHT", &p.RecurrenceWeight)
	num("M3M0S_RETENTION_RECURRENCE_MAX_FACTOR", &p.RecurrenceMaxFactor)
	num("M3M0S_RETENTION_EXPIRED_CAP_DAYS", &p.ExpiredCapDays)
	if raw := strings.TrimSpace(getenv("M3M0S_RETENTION_EXPIRED_CAP")); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("M3M0S_RETENTION_EXPIRED_CAP: %q is not a boolean", raw)
		}
		p.ExpiredCapEnabled = b
	}
	if firstErr != nil {
		return RetentionParams{}, firstErr
	}
	if err := p.Validate(); err != nil {
		return RetentionParams{}, err
	}
	return p, nil
}

// kindHalfLife is the base half-life (days) of e's kind. A kind only counts
// when an LLM or a person set it: anything else (heuristic, or a tag with no
// recorded source) is floored at the half-life of "other", so a stopgap
// regex can slow forgetting down but never speed it up.
func (p RetentionParams) kindHalfLife(e schema.Entry) float64 {
	other := p.HalfLifeDays[factkind.Other]
	kind := e.KindValue()
	hl, ok := p.HalfLifeDays[kind]
	if !ok {
		return other
	}
	switch e.KindSourceValue() {
	case factkind.SourceLLM, factkind.SourceManual:
		return hl
	}
	return math.Max(hl, other)
}

// hubFactor is the half-life multiplier for an entry with n incoming links.
func (p RetentionParams) hubFactor(incoming int) float64 {
	if incoming <= 0 {
		return 1
	}
	return math.Min(p.HubMaxFactor, 1+p.HubWeight*math.Log1p(float64(incoming)))
}

// recurrenceFactor is the half-life multiplier for an entry re-extracted in n
// extra sessions.
func (p RetentionParams) recurrenceFactor(n int) float64 {
	if n <= 0 {
		return 1
	}
	return math.Min(p.RecurrenceMaxFactor, 1+p.RecurrenceWeight*math.Log1p(float64(n)))
}

// effectiveHalfLife is the half-life of the age term for e: the kind's base,
// stretched by the hub and recurrence bonuses, then capped when its
// valid_until has passed and the cap is enabled.
func (p RetentionParams) effectiveHalfLife(e schema.Entry, now time.Time, incoming int) float64 {
	hl := p.kindHalfLife(e) * p.hubFactor(incoming) * p.recurrenceFactor(parseAccessCount(e.Metadata[metadataKeyRecurrence]))
	if p.ExpiredCapEnabled && isExpired(e, now) {
		hl = math.Min(hl, p.ExpiredCapDays)
	}
	return hl
}

// accessDays is the number of distinct days of reads credited to e. With
// access_days recorded it is that (never more than access_count); without
// it (entries read before D39) it is access_count capped by
// LegacyAccessDaysCap.
func (p RetentionParams) accessDays(e schema.Entry) int {
	count := parseAccessCount(e.Metadata[metadataKeyAccessCount])
	if count == 0 {
		return 0
	}
	if raw := e.Metadata[metadataKeyAccessDays]; raw != "" {
		if d := parseAccessCount(raw); d > 0 {
			return min(d, count)
		}
	}
	return min(count, p.LegacyAccessDaysCap)
}

// perKindScore has the shape of D22's score with the per-kind half-life:
//
//	exp(-ln2 * age / halfLife) + w * ln(1 + accessDays) * exp(-ln2 * daysSinceAccess / accessHalfLife)
//
// incoming is the number of active entries linking to e through related_to.
func perKindScore(e schema.Entry, now time.Time, p RetentionParams, incoming int) float64 {
	ageDays := now.Sub(e.CreatedAt).Hours() / 24
	if ageDays < 0 {
		ageDays = 0
	}
	ageTerm := math.Exp(-ln2 * ageDays / p.effectiveHalfLife(e, now, incoming))

	lastAccess := e.UpdatedAt
	if raw := e.Metadata[metadataKeyLastAccessed]; raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			lastAccess = t
		}
	}
	sinceAccess := now.Sub(lastAccess).Hours() / 24
	if sinceAccess < 0 {
		sinceAccess = 0
	}
	reinforcement := math.Log1p(float64(p.accessDays(e))) * math.Exp(-ln2*sinceAccess/p.AccessHalfLifeDays)
	return ageTerm + p.AccessWeight*reinforcement
}

// isManualDurable reports whether a person tagged e with a durable kind: such
// a fact is protected from the per-kind model like a pinned one.
func isManualDurable(e schema.Entry) bool {
	return e.KindSourceValue() == factkind.SourceManual && factkind.Durable(e.KindValue())
}

// buildIncomingLinks counts, for every id, how many OTHER active entries list
// it in related_to (each source counted once). index must be read-locked by
// the caller.
func buildIncomingLinks(index map[string]schema.Entry) map[string]int {
	incoming := make(map[string]int)
	for _, e := range index {
		if isSoftDeleted(e) {
			continue
		}
		seen := map[string]struct{}{}
		for _, id := range splitRelatedTo(e.Metadata[metadataKeyRelatedTo]) {
			if id == e.ID {
				continue
			}
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			incoming[id]++
		}
	}
	return incoming
}

// retentionEval scores and classifies entries under one model. Build it with
// newRetentionEval while holding the index read lock.
type retentionEval struct {
	model    RetentionModel
	params   RetentionParams
	incoming map[string]int
}

func newRetentionEval(index map[string]schema.Entry, model RetentionModel, p RetentionParams) retentionEval {
	r := retentionEval{model: model, params: p}
	if model == RetentionPerKind {
		r.incoming = buildIncomingLinks(index)
	}
	return r
}

// eligible says whether e decays under this model: D22's rule, plus (per-kind
// only) a manual durable tag exempts it.
func (r retentionEval) eligible(e schema.Entry) bool {
	if !isDecayEligible(e) {
		return false
	}
	return r.model != RetentionPerKind || !isManualDurable(e)
}

func (r retentionEval) score(e schema.Entry, now time.Time) float64 {
	if r.model == RetentionPerKind {
		return perKindScore(e, now, r.params, r.incoming[e.ID])
	}
	return retentionScore(e, now)
}

// cold reports whether an eligible e is below the cut at time at.
func (r retentionEval) cold(e schema.Entry, at time.Time) bool {
	return r.score(e, at) < ColdRetentionThreshold
}

// SetRetention sets the model and parameters used by ForgetSweep and the
// forgetting statistics. Call it at boot (cmd/m3m0s/serve.go) before serving;
// the default is the legacy model with DefaultRetentionParams.
func (s *MemoryStore) SetRetention(model RetentionModel, p RetentionParams) error {
	model, err := ParseRetentionModel(string(model))
	if err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	s.retModel, s.retParams = model, p.Clone()
	s.mu.Unlock()
	return nil
}

// Retention returns the configured model and a copy of its parameters.
func (s *MemoryStore) Retention() (RetentionModel, RetentionParams) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.retModel, s.retParams.Clone()
}
