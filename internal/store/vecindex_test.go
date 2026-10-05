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
