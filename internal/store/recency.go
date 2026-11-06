package store

import (
	"sort"

	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// RecencyWindow is how close two cosine scores must be for the newer entry to
// be preferred (D59). It is applied as a band: consecutive results whose
// scores fall inside the same window are re-ordered newest first, while a
// difference larger than the window keeps the ranking untouched.
//
// Why a band and not a score boost: `score` is documented as the exact cosine
// similarity, and a durable rule from months ago is often the right answer,
// so the ranking itself must not learn to prefer new material. What is wrong
// today is only the tie: with a large store, the same fact gets captured
// repeatedly over weeks and those near-identical hits come back in whatever
// order the index produced, sometimes surfacing an older superseded version
// above the current one.
const RecencyWindow = 0.03

// OrderWithRecencyTieBreak re-orders runs of near-equal scored entries newest
// first (D59). Entries without a score (keyword-only hits in hybrid mode)
// keep their position: they cannot be compared on cosine.
func OrderWithRecencyTieBreak(entries []schema.Entry, window float64) []schema.Entry {
	if window <= 0 || len(entries) < 2 {
		return entries
	}
	out := make([]schema.Entry, len(entries))
	copy(out, entries)

	start := 0
	for i := 1; i <= len(out); i++ {
		var breakHere bool
		if i == len(out) {
			breakHere = true
		} else if out[i].Score == nil || out[start].Score == nil {
			// A missing score ends the current run; the unscored entry is
			// left exactly where it was.
			breakHere = true
		} else if *out[start].Score-*out[i].Score > window {
			breakHere = true
		}
		if !breakHere {
			continue
		}
		if i-start > 1 {
			// Inside the band the cosine difference is noise for the
			// caller, while the date is not: newest first.
			run := out[start:i]
			sort.SliceStable(run, func(a, b int) bool {
				return run[a].CreatedAt.After(run[b].CreatedAt)
			})
		}
		start = i
	}
	return out
}
