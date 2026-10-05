// Package eval measures retrieval quality with a small golden dataset
// (D30): each query lists the entry ids that a good search must surface, and
// recall@k is the fraction of those ids found in the top k results. It is a
// development tool run manually before changing ranking, embedding or
// extraction, not a CI gate.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

// Query is one golden query.
type Query struct {
	Query  string   `json:"query"`
	Domain string   `json:"domain"`
	Expect []string `json:"expect"` // entry ids a good search surfaces
}

// Golden is the dataset file format.
type Golden struct {
	Queries []Query `json:"queries"`
}

// Load reads and validates a golden dataset file.
func Load(path string) (Golden, error) {
	var g Golden
	data, err := os.ReadFile(path)
	if err != nil {
		return g, err
	}
	if err := json.Unmarshal(data, &g); err != nil {
		return g, fmt.Errorf("parse %s: %w", path, err)
	}
	for i, q := range g.Queries {
		if q.Query == "" || len(q.Expect) == 0 {
			return g, fmt.Errorf("%s: query %d needs a query and at least one expected id", path, i+1)
		}
	}
	if len(g.Queries) == 0 {
		return g, fmt.Errorf("%s: no queries", path)
	}
	return g, nil
}

// Result is what one search returned: ranked ids and the estimated token
// cost of the context block it would inject.
type Result struct {
	IDs    []string
	Tokens int
}

// SearchFunc runs one golden query in the given mode, asking for limit results.
type SearchFunc func(ctx context.Context, q Query, mode string, limit int) (Result, error)

// Report is the outcome of one mode over the whole dataset.
type Report struct {
	Mode       string
	Recall     map[int]float64 // k -> mean recall@k
	MeanTokens float64         // mean estimated tokens of the top max(k) results
	MeanMillis float64         // mean search latency
	Failed     int             // queries whose search errored (counted as recall 0)
}

// RecallAt is the fraction of expected ids present in the first k ids.
func RecallAt(ids, expect []string, k int) float64 {
	if len(expect) == 0 {
		return 0
	}
	top := map[string]bool{}
	for i, id := range ids {
		if i >= k {
			break
		}
		top[id] = true
	}
	found := 0
	for _, e := range expect {
		if top[e] {
			found++
		}
	}
	return float64(found) / float64(len(expect))
}

// Run evaluates mode against every golden query for each k in ks.
func Run(ctx context.Context, g Golden, mode string, ks []int, search SearchFunc) Report {
	sort.Ints(ks)
	maxK := ks[len(ks)-1]
	rep := Report{Mode: mode, Recall: map[int]float64{}}
	var tokens, millis float64
	for _, q := range g.Queries {
		start := time.Now()
		res, err := search(ctx, q, mode, maxK)
		millis += float64(time.Since(start).Milliseconds())
		if err != nil {
			rep.Failed++
			continue
		}
		tokens += float64(res.Tokens)
		for _, k := range ks {
			rep.Recall[k] += RecallAt(res.IDs, q.Expect, k)
		}
	}
	n := float64(len(g.Queries))
	for _, k := range ks {
		rep.Recall[k] /= n
	}
	rep.MeanTokens = tokens / n
	rep.MeanMillis = millis / n
	return rep
}
