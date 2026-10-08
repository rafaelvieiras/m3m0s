package store

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"
)

func TestVecIndexTopMatchesExactRanking(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	const n, k = 3000, 10
	v := newVecIndex()
	vecs := make([][]float32, n)
	for i := range vecs {
		vecs[i] = randomUnitVector(r)
		v.upsert(fmt.Sprintf("m%04d", i), "d", vecs[i])
	}
	hits, total := 0, 0
	for q := 0; q < 20; q++ {
		query := randomUnitVector(r)
		type sc struct {
			id string
			s  float64
		}
		exact := make([]sc, n)
		for i := range vecs {
			exact[i] = sc{fmt.Sprintf("m%04d", i), cosineSimilarity(query, vecs[i])}
		}
		sort.Slice(exact, func(i, j int) bool { return exact[i].s > exact[j].s })
		// The index proposes a wider window than the final k, like the store.
		got, _ := v.top("d", query, 10*k)
		in := map[string]bool{}
		for _, id := range got {
			in[id] = true
		}
		for _, e := range exact[:k] {
			total++
			if in[e.id] {
				hits++
			}
		}
	}
	if recall := float64(hits) / float64(total); recall < 0.99 {
		t.Errorf("recall of the exact top-%d within the index's top-%d = %.3f, want >= 0.99", k, 10*k, recall)
	}
}

func TestVecIndexDomainFilterReplaceAndRemove(t *testing.T) {
	v := newVecIndex()
	a := []float32{1, 0, 0, 0}
	b := []float32{0, 1, 0, 0}
	v.upsert("a", "d1", a)
	v.upsert("b", "d2", b)

	if ids, _ := v.top("d1", a, 5); len(ids) != 1 || ids[0] != "a" {
		t.Errorf("domain filter: got %v", ids)
	}
	if ids, _ := v.top("", a, 5); len(ids) != 2 || ids[0] != "a" {
		t.Errorf("all domains, best first: got %v", ids)
	}
	if ids, _ := v.top("nope", a, 5); len(ids) != 0 {
		t.Errorf("unknown domain: got %v", ids)
	}

	// Replace "a" with b's direction and move it to d2.
	v.upsert("a", "d2", b)
	if ids, _ := v.top("d1", a, 5); len(ids) != 0 {
		t.Errorf("a moved out of d1, got %v", ids)
	}
	v.remove("b")
	v.upsert("c", "d2", a) // reuses the freed row
	if v.len() != 2 || len(v.ids) != 2 {
		t.Errorf("len=%d rows=%d, want freed row reused", v.len(), len(v.ids))
	}
	if ids, more := v.top("d2", a, 1); len(ids) != 1 || ids[0] != "c" || !more {
		t.Errorf("top-1 = %v more=%v", ids, more)
	}
}

func TestVecIndexIgnoresMismatchedAndZeroVectors(t *testing.T) {
	v := newVecIndex()
	v.upsert("a", "d", []float32{1, 0, 0})
	v.upsert("wrong-dims", "d", []float32{1, 0})
	v.upsert("zero", "d", []float32{0, 0, 0})
	if v.len() != 1 {
		t.Errorf("len = %d, want 1", v.len())
	}
	if ids, _ := v.top("d", []float32{1, 0}, 5); len(ids) != 0 {
		t.Errorf("query with wrong dims should match nothing, got %v", ids)
	}
}

func TestVecIndexReservedLoadMatchesGrownLoad(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	const n = 500
	grown, reserved := newVecIndex(), newVecIndex()
	reserved.reserve(n)
	scratch := make([]float32, 64) // reused like ForEachEmbedding does
	for i := 0; i < n; i++ {
		vec := randomUnitVector(r)[:64]
		copy(scratch, vec)
		id := fmt.Sprintf("m%04d", i)
		grown.upsert(id, "d", vec)
		reserved.upsert(id, "d", scratch)
	}
	if reserved.len() != n || grown.len() != n {
		t.Fatalf("len = %d/%d, want %d", reserved.len(), grown.len(), n)
	}
	query := randomUnitVector(r)[:64]
	a, _ := grown.top("d", query, 10)
	b, _ := reserved.top("d", query, 10)
	if fmt.Sprint(a) != fmt.Sprint(b) {
		t.Errorf("reserved index ranks differently: %v vs %v", b, a)
	}
}

// Growing the index past a block boundary must neither move the vectors
// already stored nor lose any of them: that copy is what used to double the
// index's memory for a GC cycle on the first insert after boot.
func TestVecIndexGrowsWithoutCopyingExistingRows(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	v := newVecIndex()
	n := blockRows*2 + 10
	v.reserve(n - 20) // deliberately smaller than the final size
	vecs := make([][]float32, n)
	for i := range vecs {
		vecs[i] = randomUnitVector(r)
		v.upsert(fmt.Sprintf("m%05d", i), "d", vecs[i])
	}
	first := &v.blocks[0][0]
	v.upsert("extra", "d", randomUnitVector(r))
	if &v.blocks[0][0] != first {
		t.Fatal("inserting a row moved the first block")
	}
	for _, i := range []int{0, blockRows - 1, blockRows, 2*blockRows + 5, n - 1} {
		ids, _ := v.top("d", vecs[i], 1)
		if len(ids) != 1 || ids[0] != fmt.Sprintf("m%05d", i) {
			t.Errorf("row %d: top-1 = %v", i, ids)
		}
	}
}
