package store

import (
	"math"
	"sort"
	"sync"
)

// vecIndex is a compact in-memory vector index used to pick search and dedup
// candidates without reading embeddings back from SQLite on every query
// (the brute-force path of D28 allocated ~540 MB per query on a 27k-entry
// domain). Vectors are L2-normalized and quantized to int8 with one scale
// per row (4x smaller than float32: ~105 MB for 68k entries of 1536
// dimensions). The int8 dot product only ranks candidates; the store then
// re-scores the few survivors with the exact float32 vectors from SQLite, so
// quantization error never reaches the returned scores.
type vecIndex struct {
	mu      sync.RWMutex
	dims    int
	data    []int8 // row-major, rows*dims
	scales  []float32
	ids     []string // "" marks a free row
	domains []uint32
	rowOf   map[string]int
	free    []int
	domainN map[string]uint32 // domain name -> code
	hint    int               // expected row count (see reserve); sizes data on the first insert
	qbuf    []int8            // scratch for quantizing inserts, guarded by mu
}

func newVecIndex() *vecIndex {
	return &vecIndex{rowOf: map[string]int{}, domainN: map[string]uint32{}}
}

// reserve pre-sizes the index for n rows so a bulk load does not grow the
// backing arrays by repeated append (each growth copies the whole array and
// leaves the old one as garbage). data is sized on the first insert, when
// the dimension is known.
func (v *vecIndex) reserve(n int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.hint = n
	v.ids = make([]string, 0, n)
	v.scales = make([]float32, 0, n)
	v.domains = make([]uint32, 0, n)
	v.rowOf = make(map[string]int, n)
}

// reset empties the index and fixes its dimension (0 = adopt the size of the
// first vector, as before). Used when the embedding configuration changes.
func (v *vecIndex) reset(dims int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.dims = dims
	v.data, v.scales, v.ids, v.domains = nil, nil, nil, nil
	v.rowOf = map[string]int{}
	v.free = nil
	v.hint = 0
}

// quantize returns the int8 form of the unit-normalized vec and its scale
// (dequantized value = q * scale). ok=false for an empty or zero vector.
func quantize(vec []float32) (q []int8, scale float32, ok bool) {
	return quantizeInto(nil, vec)
}

// quantizeInto is quantize writing into dst when it has the capacity (dst is
// otherwise reallocated), so bulk loads reuse one buffer per row.
func quantizeInto(dst []int8, vec []float32) (q []int8, scale float32, ok bool) {
	var sq, maxAbs float64
	for _, x := range vec {
		sq += float64(x) * float64(x)
	}
	if sq == 0 {
		return nil, 0, false
	}
	norm := math.Sqrt(sq)
	for _, x := range vec {
		if a := math.Abs(float64(x) / norm); a > maxAbs {
			maxAbs = a
		}
	}
	if cap(dst) < len(vec) {
		dst = make([]int8, len(vec))
	}
	q = dst[:len(vec)]
	for i, x := range vec {
		q[i] = int8(math.Round(float64(x) / norm / maxAbs * 127))
	}
	return q, float32(maxAbs / 127), true
}

// upsert adds or replaces the vector of id. Vectors whose size differs from
// the index dimension (or empty/zero vectors) are not indexed, which matches
// the old behavior where cosineSimilarity returned 0 for them.
func (v *vecIndex) upsert(id, domain string, vec []float32) {
	v.mu.Lock()
	defer v.mu.Unlock()
	q, scale, ok := quantizeInto(v.qbuf, vec)
	if ok {
		v.qbuf = q
	}
	if !ok {
		v.removeLocked(id)
		return
	}
	if v.dims == 0 {
		v.dims = len(q)
	}
	if v.hint > 0 && cap(v.data) == 0 {
		v.data = make([]int8, 0, v.hint*v.dims)
	}
	if len(q) != v.dims {
		v.removeLocked(id)
		return
	}
	code, known := v.domainN[domain]
	if !known {
		code = uint32(len(v.domainN))
		v.domainN[domain] = code
	}
	row, exists := v.rowOf[id]
	switch {
	case exists:
	case len(v.free) > 0:
		row = v.free[len(v.free)-1]
		v.free = v.free[:len(v.free)-1]
	default:
		row = len(v.ids)
		v.ids = append(v.ids, "")
		v.scales = append(v.scales, 0)
		v.domains = append(v.domains, 0)
		v.data = append(v.data, make([]int8, v.dims)...)
	}
	copy(v.data[row*v.dims:(row+1)*v.dims], q)
	v.scales[row] = scale
	v.ids[row] = id
	v.domains[row] = code
	v.rowOf[id] = row
}

func (v *vecIndex) remove(id string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.removeLocked(id)
}

func (v *vecIndex) removeLocked(id string) {
	row, ok := v.rowOf[id]
	if !ok {
		return
	}
	delete(v.rowOf, id)
	v.ids[row] = ""
	v.free = append(v.free, row)
}

func (v *vecIndex) len() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.rowOf)
}

type rowScore struct {
	id    string
	score float32
}

// top returns up to k ids of the highest approximate cosine similarity to
// query within domain (all domains when domain is ""), best first. The
// boolean reports whether more rows exist beyond the k returned.
func (v *vecIndex) top(domain string, query []float32, k int) ([]string, bool) {
	qq, qscale, ok := quantize(query)
	if !ok || k <= 0 {
		return nil, false
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.dims == 0 || len(qq) != v.dims {
		return nil, false
	}
	var want uint32
	filter := domain != ""
	if filter {
		code, known := v.domainN[domain]
		if !known {
			return nil, false
		}
		want = code
	}

	h := make([]rowScore, 0, k) // min-heap on score
	matched := 0
	d := v.dims
	for row, id := range v.ids {
		if id == "" || (filter && v.domains[row] != want) {
			continue
		}
		matched++
		r := v.data[row*d : (row+1)*d]
		s := float32(dotInt8(r, qq)) * v.scales[row] * qscale
		switch {
		case len(h) < k:
			h = append(h, rowScore{id, s})
			siftUp(h, len(h)-1)
		case s > h[0].score:
			h[0] = rowScore{id, s}
			siftDown(h, 0)
		}
	}
	sort.Slice(h, func(i, j int) bool { return h[i].score > h[j].score })
	out := make([]string, len(h))
	for i, r := range h {
		out[i] = r.id
	}
	return out, matched > len(h)
}

// dotInt8 is the hot loop of every search: an int32-accumulated dot product,
// unrolled by 4 (a dimension count that is not a multiple of 4 is handled by
// the tail loop).
func dotInt8(a, b []int8) int32 {
	b = b[:len(a)]
	var s0, s1, s2, s3 int32
	i := 0
	for ; i+4 <= len(a); i += 4 {
		s0 += int32(a[i]) * int32(b[i])
		s1 += int32(a[i+1]) * int32(b[i+1])
		s2 += int32(a[i+2]) * int32(b[i+2])
		s3 += int32(a[i+3]) * int32(b[i+3])
	}
	for ; i < len(a); i++ {
		s0 += int32(a[i]) * int32(b[i])
	}
	return s0 + s1 + s2 + s3
}

func siftUp(h []rowScore, i int) {
	for i > 0 {
		p := (i - 1) / 2
		if h[p].score <= h[i].score {
			return
		}
		h[p], h[i] = h[i], h[p]
		i = p
	}
}

func siftDown(h []rowScore, i int) {
	for {
		l, r, m := 2*i+1, 2*i+2, i
		if l < len(h) && h[l].score < h[m].score {
			m = l
		}
		if r < len(h) && h[r].score < h[m].score {
			m = r
		}
		if m == i {
			return
		}
		h[i], h[m] = h[m], h[i]
		i = m
	}
}
