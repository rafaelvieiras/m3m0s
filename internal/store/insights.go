package store

import (
	"sort"
	"time"
	"unicode/utf8"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/factkind"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// Memory health overview (D37). Everything here is computed in one pass over
// the in-memory index (which already holds every entry without its embedding),
// so it costs a few milliseconds and no extra memory worth mentioning.

const (
	overviewTopDomains    = 15
	overviewTopSources    = 12
	softDeletedDaysShown  = 60
	textLenHistogramLimit = 5000 // lengths above this share the last bucket
)

// ageBuckets are the upper bounds (in days, exclusive) of the age histogram.
var ageBuckets = []struct {
	label string
	below float64
}{
	{"0-7 days", 8}, {"8-30 days", 31}, {"31-60 days", 61}, {"61-90 days", 91},
	{"91-180 days", 181}, {"181-365 days", 366}, {"over 1 year", 1e18},
}

// retentionBuckets are the upper bounds of the retention-score histogram; the
// first one is the cold threshold itself.
var retentionBuckets = []struct {
	label string
	below float64
}{
	{"< 0.15 (cold)", ColdRetentionThreshold}, {"0.15 - 0.30", 0.30}, {"0.30 - 0.50", 0.50},
	{"0.50 - 0.75", 0.75}, {">= 0.75", 1e18},
}

func isPinned(e schema.Entry) bool { return e.Metadata[metadataKeyPinned] == "true" }

func hasSourceRef(e schema.Entry) bool {
	_, ok := e.Metadata[metadataKeySourceContentRef]
	return ok
}

func neverAccessed(e schema.Entry) bool {
	return parseAccessCount(e.Metadata[metadataKeyAccessCount]) == 0
}

func topCounts(m map[string]int, n int) []apitypes.CountItem {
	out := make([]apitypes.CountItem, 0, len(m))
	for k, v := range m {
		out = append(out, apitypes.CountItem{Key: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

// InsightsOverview computes the cheap, live part of the memory-health
// dashboard: totals, distributions, and the forgetting outlook.
func (s *MemoryStore) InsightsOverview(now time.Time) apitypes.InsightsOverviewResponse {
	var (
		out        apitypes.InsightsOverviewResponse
		byCategory = map[string]int{}
		bySource   = map[string]int{}
		byDomain   = map[string]*apitypes.DomainStat{}
		byAge      = make([]int, len(ageBuckets))
		deletedDay = map[string]int{}
		lenHist    = make([]int, textLenHistogramLimit+1)
		sumChars   int64
		maxChars   int
	)

	s.mu.RLock()
	for _, e := range s.index {
		out.Totals.Total++
		if isSoftDeleted(e) {
			out.Totals.SoftDeleted++
			if t, err := time.Parse(time.RFC3339, e.Metadata[metadataKeyDeletedAt]); err == nil {
				deletedDay[t.UTC().Format("2006-01-02")]++
			}
			continue
		}
		out.Totals.Active++
		byCategory[e.DomainCategory]++
		bySource[e.Source]++

		ds := byDomain[e.Domain]
		if ds == nil {
			ds = &apitypes.DomainStat{Domain: e.Domain}
			byDomain[e.Domain] = ds
		}
		ds.Count++

		if e.Metadata[metadataKeyValidUntil] != "" {
			out.Totals.WithValidUntil++
		}
		if isExpired(e, now) {
			out.Totals.Expired++
			ds.Expired++
		}
		if isPinned(e) {
			out.Totals.Pinned++
		}
		if hasSourceRef(e) {
			out.Totals.Extracted++
		} else {
			out.Totals.Raw++
		}
		if neverAccessed(e) {
			out.Totals.NeverAccessed++
			ds.NeverAccessed++
		}

		ageDays := now.Sub(e.CreatedAt).Hours() / 24
		for i, b := range ageBuckets {
			if ageDays < b.below {
				byAge[i]++
				break
			}
		}

		n := utf8.RuneCountInString(e.Content)
		sumChars += int64(n)
		if n > maxChars {
			maxChars = n
		}
		lenHist[min(n, textLenHistogramLimit)]++
	}
	s.mu.RUnlock()

	if a := out.Totals.Active; a > 0 {
		out.Totals.NeverAccessedPct = float64(out.Totals.NeverAccessed) / float64(a) * 100
		out.Text = apitypes.InsightsText{
			AvgChars:    float64(sumChars) / float64(a),
			MedianChars: histPercentile(lenHist, a, 0.5),
			P90Chars:    histPercentile(lenHist, a, 0.9),
			MaxChars:    maxChars,
		}
	}

	out.GeneratedAt = now.UTC()
	out.ByCategory = topCounts(byCategory, 0)
	out.BySource = topCounts(bySource, overviewTopSources)
	out.ByAge = make([]apitypes.CountItem, len(ageBuckets))
	for i, b := range ageBuckets {
		out.ByAge[i] = apitypes.CountItem{Key: b.label, Count: byAge[i]}
	}

	out.ByKind, out.ByKindSource = s.KindDistribution()

	out.DomainCount = len(byDomain)
	doms := make([]apitypes.DomainStat, 0, len(byDomain))
	for _, d := range byDomain {
		doms = append(doms, *d)
	}
	sort.Slice(doms, func(i, j int) bool {
		if doms[i].Count != doms[j].Count {
			return doms[i].Count > doms[j].Count
		}
		return doms[i].Domain < doms[j].Domain
	})
	if len(doms) > overviewTopDomains {
		doms = doms[:overviewTopDomains]
	}
	out.TopDomains = doms

	days := make([]string, 0, len(deletedDay))
	for d := range deletedDay {
		days = append(days, d)
	}
	sort.Strings(days)
	if len(days) > softDeletedDaysShown {
		days = days[len(days)-softDeletedDaysShown:]
	}
	out.SoftDeletedByDay = make([]apitypes.CountItem, len(days))
	for i, d := range days {
		out.SoftDeletedByDay[i] = apitypes.CountItem{Key: d, Count: deletedDay[d]}
	}

	out.VectorsIndexed = s.vecs.len()
	out.Forgetting = s.ForgettingStats(now)
	out.Filter = s.filterOverview()
	return out
}

// histPercentile returns the smallest value v with at least q of the n samples
// <= v, from a histogram indexed by value.
func histPercentile(hist []int, n int, q float64) int {
	target := int(float64(n)*q + 0.5)
	if target < 1 {
		target = 1
	}
	cum := 0
	for v, c := range hist {
		cum += c
		if cum >= target {
			return v
		}
	}
	return len(hist) - 1
}

// shadowHorizonsDays are the points in time, relative to now, at which the
// shadow comparison counts what each retention model would forget (D39),
// assuming nothing reads the memories in the meantime. 0 = right now.
var shadowHorizonsDays = []int{0, 30, 60, 90}

// ForgettingStats reports what the next forget-sweep would do, classifying
// entries exactly like sweepCandidates (the same eligibility rules and
// score, for the configured retention model) but only counting, so nothing is
// allocated per candidate. It also fills the shadow comparison of both models.
func (s *MemoryStore) ForgettingStats(now time.Time) apitypes.ForgettingStats {
	model, params := s.Retention()
	out := apitypes.ForgettingStats{
		Threshold:       ColdRetentionThreshold,
		AgeHalfLifeDays: DecayAgeHalfLifeDays,
		GraceDays:       SoftDeleteGraceDays,
		Model:           string(model),
	}
	hist := make([]int, len(retentionBuckets))

	s.mu.RLock()
	// Both models are scored in the same pass; the incoming-link table is
	// needed by the per-kind one only.
	legacy := newRetentionEval(s.index, RetentionLegacy, params)
	perKind := newRetentionEval(s.index, RetentionPerKind, params)
	active := legacy
	if model == RetentionPerKind {
		active = perKind
	}
	shadow := newShadowAccumulator(params)
	for _, e := range s.index {
		if isSoftDeleted(e) {
			out.SoftDeleted++
			if deletedAt, err := time.Parse(time.RFC3339, e.Metadata[metadataKeyDeletedAt]); err == nil &&
				now.Sub(deletedAt) >= SoftDeleteGraceDays*24*time.Hour &&
				(model != RetentionPerKind || (active.eligible(e) && active.cold(e, now))) {
				out.HardDeleteDue++
			}
			continue
		}
		if !isDecayEligible(e) {
			out.Exempt++
			continue
		}
		shadow.add(e, now, legacy, perKind)
		if !active.eligible(e) {
			out.Exempt++
			continue
		}
		out.Eligible++
		score := active.score(e, now)
		if score < ColdRetentionThreshold {
			out.ColdNow++
		}
		for i, b := range retentionBuckets {
			if score < b.below {
				hist[i]++
				break
			}
		}
	}
	s.mu.RUnlock()

	out.RetentionHistogram = make([]apitypes.CountItem, len(retentionBuckets))
	for i, b := range retentionBuckets {
		out.RetentionHistogram[i] = apitypes.CountItem{Key: b.label, Count: hist[i]}
	}
	out.Shadow = shadow.result(model)
	return out
}

// shadowAccumulator counts, per kind and horizon, how many eligible entries
// each model would soft-delete (D39).
type shadowAccumulator struct {
	params RetentionParams
	rows   map[string]*apitypes.ShadowKind
}

func newShadowAccumulator(p RetentionParams) *shadowAccumulator {
	a := &shadowAccumulator{params: p, rows: make(map[string]*apitypes.ShadowKind, len(factkind.All))}
	for _, k := range factkind.All {
		a.rows[k] = &apitypes.ShadowKind{
			Kind:    k,
			Legacy:  make([]int, len(shadowHorizonsDays)),
			PerKind: make([]int, len(shadowHorizonsDays)),
		}
	}
	return a
}

// add counts e, which must be legacy-eligible and active.
func (a *shadowAccumulator) add(e schema.Entry, now time.Time, legacy, perKind retentionEval) {
	row := a.rows[e.KindValue()]
	row.Eligible++
	protected := !perKind.eligible(e)
	if protected {
		row.Protected++
	}
	for i, d := range shadowHorizonsDays {
		at := now.Add(time.Duration(d) * 24 * time.Hour)
		if legacy.cold(e, at) {
			row.Legacy[i]++
		}
		if !protected && perKind.cold(e, at) {
			row.PerKind[i]++
		}
	}
}

func (a *shadowAccumulator) result(active RetentionModel) apitypes.RetentionShadow {
	out := apitypes.RetentionShadow{
		Active:            string(active),
		HorizonsDays:      append([]int(nil), shadowHorizonsDays...),
		Legacy:            make([]int, len(shadowHorizonsDays)),
		PerKind:           make([]int, len(shadowHorizonsDays)),
		HalfLifeDays:      a.params.Clone().HalfLifeDays,
		ExpiredCapEnabled: a.params.ExpiredCapEnabled,
		ExpiredCapDays:    a.params.ExpiredCapDays,
	}
	for _, k := range factkind.All {
		row := *a.rows[k]
		out.Eligible += row.Eligible
		out.Protected += row.Protected
		for i := range shadowHorizonsDays {
			out.Legacy[i] += row.Legacy[i]
			out.PerKind[i] += row.PerKind[i]
		}
		out.ByKind = append(out.ByKind, row)
	}
	return out
}

// filterOverview adapts the ingestion filter's counters (#53) to the API type.
func (s *MemoryStore) filterOverview() apitypes.FilterOverview {
	st := s.FilterStats()
	out := apitypes.FilterOverview{Mode: st.Mode, RulesVersion: st.Version, Rules: make([]apitypes.FilterRuleStats, 0, len(st.Rules))}
	for _, r := range st.Rules {
		out.Rules = append(out.Rules, apitypes.FilterRuleStats{
			ID: r.ID, Scope: r.Scope, Description: r.Description, Action: r.Action,
			Seen: r.Seen, Dropped: r.Dropped, Tagged: r.Tagged,
		})
	}
	return out
}
