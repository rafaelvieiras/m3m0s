package store

import (
	"context"
	"math"
	"math/rand/v2"
	"slices"
	"sort"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// Near-duplicate search over the int8 vector index (D37). It never builds an
// NxN matrix and never loads float32 vectors: pairs are generated either by
// brute force (small indexes) or by random-hyperplane LSH (large ones), then
// verified with the same int8 dot product search uses. Memory is a few bytes
// per row per hash table; the vector blocks are only read, in short slices
// under the index read lock so inserts and searches are never stalled.

var nearThresholds = []float64{0.98, 0.95, 0.92}

// nearParams tunes the search. The zero value of a field means its default.
type nearParams struct {
	exactBelow int    // up to this many rows, compare all pairs
	tables     int    // LSH hash tables
	bits       int    // signature bits per table (<= 16)
	bucketCap  int    // largest window compared inside one bucket
	maxPairs   int    // cap on retained pairs (memory bound)
	seed       uint64 // PRNG seed: same data, same result
}

func (p nearParams) withDefaults() nearParams {
	if p.exactBelow <= 0 {
		p.exactBelow = 3000
	}
	if p.tables <= 0 {
		p.tables = 24
	}
	if p.bits <= 0 || p.bits > 16 {
		p.bits = 14
	}
	if p.bucketCap <= 0 {
		p.bucketCap = 256
	}
	if p.maxPairs <= 0 {
		p.maxPairs = 300_000
	}
	if p.seed == 0 {
		p.seed = 0x6d336d3073 // "m3m0s"
	}
	return p
}

// nearPair is a verified pair of positions in the candidate list.
type nearPair struct {
	a, b int32
	sim  float32
}

type nearResult struct {
	method    string
	rowIDs    []string // id of every candidate position
	domains   []uint32
	pairs     []nearPair
	tables    int
	bits      int
	oversized int
	truncated bool
	partial   bool // the context ended before every table was processed
}

type nearProgress func(done, total int, phase string)

// findNearPairs returns the pairs of active entries whose cosine similarity is
// at least the lowest threshold. A context that ends because of its deadline
// yields the pairs found so far with partial=true; any other cancellation is
// returned as an error.
func (s *MemoryStore) findNearPairs(ctx context.Context, p nearParams, pc *pacer, progress nearProgress) (nearResult, error) {
	p = p.withDefaults()
	minSim := float32(nearThresholds[len(nearThresholds)-1])
	v := s.vecs

	// 1. Snapshot the row -> id mapping, then keep the rows of active entries.
	v.mu.RLock()
	ids := slices.Clone(v.ids)
	doms := slices.Clone(v.domains)
	dims := v.dims
	v.mu.RUnlock()

	var res nearResult
	rows := make([]int32, 0, len(ids))
	s.mu.RLock()
	for row, id := range ids {
		if id == "" {
			continue
		}
		if e, ok := s.index[id]; ok && !isSoftDeleted(e) {
			rows = append(rows, int32(row))
		}
	}
	s.mu.RUnlock()
	n := len(rows)
	res.rowIDs = make([]string, n)
	res.domains = make([]uint32, n)
	for i, r := range rows {
		res.rowIDs[i], res.domains[i] = ids[r], doms[r]
	}
	if n < 2 || dims == 0 {
		res.method = "exact"
		return res, nil
	}

	found := map[uint64]float32{}
	// verify scores a batch of candidate pairs under one read lock.
	verify := func(batch [][2]int32) {
		v.mu.RLock()
		defer v.mu.RUnlock()
		for _, c := range batch {
			if res.truncated {
				return
			}
			key := uint64(c[0])<<32 | uint64(c[1])
			if _, dup := found[key]; dup {
				continue
			}
			ra, rb := int(rows[c[0]]), int(rows[c[1]])
			if v.ids[ra] != res.rowIDs[c[0]] || v.ids[rb] != res.rowIDs[c[1]] {
				continue // the row was reused since the snapshot
			}
			sim := float32(dotInt8(v.rowSlice(ra), v.rowSlice(rb))) * v.scales[ra] * v.scales[rb]
			if sim >= minSim {
				found[key] = sim
				if len(found) >= p.maxPairs {
					res.truncated = true
				}
			}
		}
	}
	batch := make([][2]int32, 0, 4096)
	flush := func() error {
		verify(batch)
		batch = batch[:0]
		return pc.tick(ctx)
	}
	add := func(a, b int32) error {
		batch = append(batch, [2]int32{a, b})
		if len(batch) == cap(batch) {
			return flush()
		}
		return nil
	}

	var err error
	if n <= p.exactBelow {
		res.method = "exact"
		if progress != nil {
			progress(0, 1, "comparing vectors")
		}
		err = func() error {
			for i := 0; i < n; i++ {
				for j := i + 1; j < n; j++ {
					if err := add(int32(i), int32(j)); err != nil {
						return err
					}
				}
			}
			return flush()
		}()
	} else {
		res.method = "lsh"
		res.tables, res.bits = p.tables, p.bits
		err = s.lshPairs(ctx, p, pc, progress, &res, rows, dims, add, flush)
	}
	if err != nil {
		if ctx.Err() != nil && isDeadline(ctx) {
			res.partial = true
		} else {
			return res, err
		}
	}
	// Flush what is pending even after a deadline: it is still valid work.
	if res.partial {
		verify(batch)
	}

	res.pairs = make([]nearPair, 0, len(found))
	for k, sim := range found {
		res.pairs = append(res.pairs, nearPair{a: int32(k >> 32), b: int32(k & 0xffffffff), sim: sim})
	}
	sort.Slice(res.pairs, func(i, j int) bool {
		if res.pairs[i].sim != res.pairs[j].sim {
			return res.pairs[i].sim > res.pairs[j].sim
		}
		if res.pairs[i].a != res.pairs[j].a {
			return res.pairs[i].a < res.pairs[j].a
		}
		return res.pairs[i].b < res.pairs[j].b
	})
	return res, nil
}

func isDeadline(ctx context.Context) bool { return ctx.Err() == context.DeadlineExceeded }

// lshPairs generates candidate pairs with random-hyperplane hashing: vectors
// that are close in angle fall on the same side of most hyperplanes, so they
// share a bucket in at least one table with high probability (about 97% for
// cosine 0.92 and above with the defaults). Vectors are centered on the
// mean first, because embeddings share a common direction that would
// otherwise make most signature bits constant.
func (s *MemoryStore) lshPairs(ctx context.Context, p nearParams, pc *pacer, progress nearProgress, res *nearResult,
	rows []int32, dims int, add func(a, b int32) error, flush func() error) error {
	v := s.vecs
	n := len(rows)
	total := 2 + p.tables
	report := func(done int, phase string) {
		if progress != nil {
			progress(done, total, phase)
		}
	}

	// Mean of the (dequantized) vectors.
	report(0, "centering vectors")
	mean := make([]float64, dims)
	count := 0
	for start := 0; start < n; start += blockRows {
		end := min(start+blockRows, n)
		v.mu.RLock()
		for i := start; i < end; i++ {
			r := int(rows[i])
			if v.ids[r] != res.rowIDs[i] {
				continue
			}
			sc := float64(v.scales[r])
			for d, q := range v.rowSlice(r) {
				mean[d] += float64(q) * sc
			}
			count++
		}
		v.mu.RUnlock()
		if err := pc.tick(ctx); err != nil {
			return err
		}
	}
	if count == 0 {
		return nil
	}
	for d := range mean {
		mean[d] /= float64(count)
	}

	// Rademacher hyperplanes and their offset against the mean.
	nPlanes := p.tables * p.bits
	rng := rand.New(rand.NewPCG(p.seed, p.seed^0x9e3779b97f4a7c15))
	planes := make([]int8, nPlanes*dims)
	offset := make([]float32, nPlanes)
	for pl := 0; pl < nPlanes; pl++ {
		var dot float64
		for d := 0; d < dims; d++ {
			x := int8(1)
			if rng.Uint32()&1 == 0 {
				x = -1
			}
			planes[pl*dims+d] = x
			dot += float64(x) * mean[d]
		}
		offset[pl] = float32(dot)
	}

	// Signatures: tables x rows of `bits` bits.
	report(1, "hashing vectors")
	sig := make([][]uint16, p.tables)
	for t := range sig {
		sig[t] = make([]uint16, n)
	}
	alive := make([]bool, n)
	for start := 0; start < n; start += 256 {
		end := min(start+256, n)
		v.mu.RLock()
		for i := start; i < end; i++ {
			r := int(rows[i])
			if v.ids[r] != res.rowIDs[i] {
				continue
			}
			alive[i] = true
			q, sc := v.rowSlice(r), v.scales[r]
			for t := 0; t < p.tables; t++ {
				var key uint16
				for b := 0; b < p.bits; b++ {
					pl := t*p.bits + b
					if float32(dotInt8(q, planes[pl*dims:(pl+1)*dims]))*sc-offset[pl] > 0 {
						key |= 1 << b
					}
				}
				sig[t][i] = key
			}
		}
		v.mu.RUnlock()
		if err := pc.tick(ctx); err != nil {
			return err
		}
	}
	planes = nil

	// Buckets: sort (signature, position) and compare inside each run.
	keys := make([]uint64, 0, n)
	for t := 0; t < p.tables; t++ {
		report(2+t, "matching candidates")
		keys = keys[:0]
		for i := 0; i < n; i++ {
			if alive[i] {
				keys = append(keys, uint64(sig[t][i])<<32|uint64(i))
			}
		}
		slices.Sort(keys)
		for lo := 0; lo < len(keys); {
			hi := lo + 1
			for hi < len(keys) && keys[hi]>>32 == keys[lo]>>32 {
				hi++
			}
			if hi-lo > p.bucketCap {
				res.oversized++
			}
			for x := lo; x < hi; x++ {
				a := int32(keys[x] & 0xffffffff)
				for y := x + 1; y < hi && y <= x+p.bucketCap; y++ {
					if err := add(a, int32(keys[y]&0xffffffff)); err != nil {
						return err
					}
				}
			}
			lo = hi
		}
		sig[t] = nil
	}
	return flush()
}

// ── aggregation ──

type unionFind map[int32]int32

func (u unionFind) find(x int32) int32 {
	for {
		p, ok := u[x]
		if !ok || p == x {
			u[x] = x
			return x
		}
		u[x] = u[p]
		x = u[x]
	}
}

func (u unionFind) union(a, b int32) {
	ra, rb := u.find(a), u.find(b)
	if ra != rb {
		u[ra] = rb
	}
}

// digitTokens returns the set of digit runs in s; two near-identical texts
// whose numbers differ are usually distinct facts of the same shape.
func digitTokens(s string) map[string]struct{} {
	out := map[string]struct{}{}
	start := -1
	for i, r := range s {
		if r >= '0' && r <= '9' {
			if start < 0 {
				start = i
			}
		} else if start >= 0 {
			out[s[start:i]] = struct{}{}
			start = -1
		}
	}
	if start >= 0 {
		out[s[start:]] = struct{}{}
	}
	return out
}

func sameSet(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

func bandOf(sim float64, sameDomain bool) int {
	switch {
	case sim >= 0.98:
		return 0
	case sim >= 0.95 && sameDomain:
		return 1
	case sim >= 0.95:
		return 2
	default:
		return 3
	}
}

var bandLabels = []string{
	"cosine >= 0.98",
	"0.95 - 0.98, same domain",
	"0.95 - 0.98, different domains",
	"0.92 - 0.95",
}

// summarizeNear aggregates the verified pairs into the report.
func (s *MemoryStore) summarizeNear(res nearResult) apitypes.NearDupReport {
	out := apitypes.NearDupReport{
		Available: true, Method: res.method, Indexed: len(res.rowIDs),
		Tables: res.tables, Bits: res.bits, OversizedBuckets: res.oversized, PairsTruncated: res.truncated,
		CrossSource: []apitypes.CountItem{},
	}
	th := make([]apitypes.NearDupThreshold, len(nearThresholds))
	ufs := make([]unionFind, len(nearThresholds))
	for i, t := range nearThresholds {
		th[i].Threshold = t
		ufs[i] = unionFind{}
	}
	bands := make([][]apitypes.PairExample, len(bandLabels))
	srcPairs := map[string]int{}

	s.mu.RLock()
	entry := func(pos int32) (schema.Entry, bool) {
		e, ok := s.index[res.rowIDs[pos]]
		return e, ok && !isSoftDeleted(e)
	}
	type pairInfo struct {
		sim                                      float64
		sameDomain, crossSource, sameRef, numsOK bool
	}
	for _, pr := range res.pairs {
		ea, ok1 := entry(pr.a)
		eb, ok2 := entry(pr.b)
		if !ok1 || !ok2 {
			continue
		}
		pi := pairInfo{sim: float64(pr.sim), sameDomain: ea.Domain == eb.Domain, crossSource: ea.Source != eb.Source}
		refA, refB := ea.Metadata[metadataKeySourceContentRef], eb.Metadata[metadataKeySourceContentRef]
		pi.sameRef = refA != "" && refA == refB
		pi.numsOK = sameSet(digitTokens(ea.Content), digitTokens(eb.Content))
		for i, t := range nearThresholds {
			if pi.sim < t-1e-9 {
				continue
			}
			th[i].Pairs++
			if pi.sameDomain {
				th[i].SameDomain++
			} else {
				th[i].CrossDomain++
			}
			if pi.crossSource {
				th[i].CrossSource++
			}
			if pi.sameRef {
				th[i].SameCapture++
			}
			if !pi.numsOK {
				th[i].NumbersDiffer++
			}
			ufs[i].union(pr.a, pr.b)
		}
		if pi.crossSource {
			a, b := ea.Source, eb.Source
			if a > b {
				a, b = b, a
			}
			srcPairs[a+" x "+b]++
		}
		if bi := bandOf(pi.sim, pi.sameDomain); len(bands[bi]) < examplesPerRule {
			xa, oka := exampleOf(ea, true)
			xb, okb := exampleOf(eb, true)
			if oka && okb {
				bands[bi] = append(bands[bi], apitypes.PairExample{Similarity: math.Round(math.Min(pi.sim, 1)*1000) / 1000, A: xa, B: xb})
			}
		}
	}
	s.mu.RUnlock()

	for i := range th {
		sizes := map[int32]int{}
		for pos := range ufs[i] {
			sizes[ufs[i].find(pos)]++
		}
		th[i].Clusters = len(sizes)
		for _, sz := range sizes {
			th[i].Redundant += sz - 1
			th[i].LargestCluster = max(th[i].LargestCluster, sz)
		}
	}
	out.Thresholds = th
	out.CrossSource = topCounts(srcPairs, 8)
	out.Bands = make([]apitypes.NearDupBand, len(bandLabels))
	for i, l := range bandLabels {
		ex := bands[i]
		if ex == nil {
			ex = []apitypes.PairExample{}
		}
		out.Bands[i] = apitypes.NearDupBand{Label: l, Examples: ex}
	}
	return out
}
