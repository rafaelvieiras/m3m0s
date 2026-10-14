package store

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/sqlstore"
)

// benchDims matches text-embedding-3-small, the production embedding size.
const benchDims = 1536

func randomUnitVector(r *rand.Rand) []float32 {
	v := make([]float32, benchDims)
	var sq float64
	for i := range v {
		v[i] = float32(r.NormFloat64())
		sq += float64(v[i]) * float64(v[i])
	}
	n := float32(math.Sqrt(sq))
	for i := range v {
		v[i] /= n
	}
	return v
}

// seedBenchStore fills one domain with n random entries straight through
// the SQLite layer (no embedding calls) and reloads the index. The query
// embedding is stubbed with a random vector, so timings measure the search
// path only. Set M3M0S_BENCH_N to scale (default 20000; production-sized
// runs use 68000).
func seedBenchStore(tb testing.TB, n int) *MemoryStore {
	tb.Helper()
	// The seeded directory is built once per process and reused, because the
	// benchmark function runs several times (b.N calibration) and seeding is
	// the slow part.
	benchSeedOnce.Do(func() {
		dir, err := os.MkdirTemp("", "m3m0s-bench-")
		if err != nil {
			tb.Fatal(err)
		}
		benchSeedDir = dir
		seedBenchDir(tb, dir, n)
	})
	s, err := New(benchSeedDir, "fake-key", "http://127.0.0.1:1")
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = s.db.Close() })
	qr := rand.New(rand.NewSource(2))
	s.emb.emb = &embedder{fn: func(context.Context, string) ([]float32, error) { return randomUnitVector(qr), nil }, model: "bench"}
	return s
}

var (
	benchSeedOnce sync.Once
	benchSeedDir  string
)

func seedBenchDir(tb testing.TB, dir string, n int) {
	s, err := New(dir, "fake-key", "http://127.0.0.1:1")
	if err != nil {
		tb.Fatal(err)
	}
	defer func() { _ = s.db.Close() }()
	r := rand.New(rand.NewSource(1))
	now := time.Now().UTC()
	for i := 0; i < n; i++ {
		err := s.db.UpsertEntry(context.Background(), sqlstore.EntryRow{
			ID:             fmt.Sprintf("mem_%06d", i),
			Content:        fmt.Sprintf("synthetic memory number %d about topic %d", i, i%97),
			Domain:         "code:bench",
			DomainCategory: "code",
			Source:         "bench",
			CreatedAt:      now.Add(time.Duration(i) * time.Second),
			UpdatedAt:      now,
			Embedding:      randomUnitVector(r),
		})
		if err != nil {
			tb.Fatal(err)
		}
	}
}

func benchN() int {
	if v, err := strconv.Atoi(os.Getenv("M3M0S_BENCH_N")); err == nil && v > 0 {
		return v
	}
	return 20000
}

// BenchmarkSearchLargeDomain measures one Search over a single large domain:
// latency (ns/op), allocated bytes per query (B/op) and peak heap in use.
func BenchmarkSearchLargeDomain(b *testing.B) {
	s := seedBenchStore(b, benchN())
	runtime.GC()
	var peak uint64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Search(context.Background(), "query", 5, Filter{Domain: "code:bench"}, time.Now()); err != nil {
			b.Fatal(err)
		}
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		if m.HeapInuse > peak {
			peak = m.HeapInuse
		}
	}
	b.ReportMetric(float64(peak)/(1<<20), "peakHeapMB")
}
