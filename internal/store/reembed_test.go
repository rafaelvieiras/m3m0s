package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testSecret = "sk-test-secret-123"

// provider simulates an OpenAI-compatible embeddings API that accepts both
// the single and the array form of "input". Vectors depend on the model, so a
// vector computed for one model is recognizable if it lands in another's space.
type provider struct {
	srv      *httptest.Server
	requests atomic.Int32
	inputs   atomic.Int32 // total texts received
	mu       sync.Mutex
	hook     func(n int, inputs []string) (status int) // called per request; non-zero status short-circuits
	seenAuth []string
}

func modelVector(model, text string, dims int) []float32 {
	base := fakeVector(text)
	rot := len(model) % len(base) // a different model reads the same text differently
	out := make([]float32, dims)
	for i := range out {
		out[i] = base[(i+rot)%len(base)]
	}
	return out
}

func newProvider(t *testing.T) *provider {
	t.Helper()
	p := &provider{}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input      json.RawMessage `json:"input"`
			Model      string          `json:"model"`
			Dimensions int             `json:"dimensions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var inputs []string
		if err := json.Unmarshal(req.Input, &inputs); err != nil {
			var one string
			_ = json.Unmarshal(req.Input, &one)
			inputs = []string{one}
		}
		n := int(p.requests.Add(1))
		p.inputs.Add(int32(len(inputs)))
		p.mu.Lock()
		p.seenAuth = append(p.seenAuth, r.Header.Get("Authorization"))
		hook := p.hook
		p.mu.Unlock()
		if hook != nil {
			if status := hook(n, inputs); status != 0 {
				http.Error(w, "simulated", status)
				return
			}
		}
		dims := 8
		if req.Dimensions > 0 {
			dims = req.Dimensions
		}
		data := make([]map[string]any, len(inputs))
		for i, in := range inputs {
			data[i] = map[string]any{"index": i, "embedding": modelVector(req.Model, in, dims)}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *provider) setHook(h func(n int, inputs []string) int) {
	p.mu.Lock()
	p.hook = h
	p.mu.Unlock()
}

// noteText returns eight-character notes (i < 8) whose fake vectors are far
// apart, so Add's near-duplicate detection keeps all of them.
func noteText(i int) string {
	b := []byte("!!!!!!!!")
	b[i%8], b[(i+1)%8] = 'z', 'z'
	return string(b)
}

func fastOpts() ReembedOptions {
	return ReembedOptions{BatchSize: 3, RatePerSec: 10000, BackoffBase: time.Millisecond, BackoffMax: 4 * time.Millisecond, MaxAttempts: 4}
}

// staleStore builds a store with n notes embedded at 8 dims, then switches to
// 4 dims (confirmed), leaving every row stale.
func staleStore(t *testing.T, p *provider, dir string, n int) *MemoryStore {
	t.Helper()
	s, err := New(dir, testSecret, p.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		addNote(t, s, noteText(i))
	}
	if _, err := s.SetEmbeddingSettings(context.Background(), EmbeddingUpdate{EmbeddingSettings: EmbeddingSettings{Enabled: true, Model: "text-embedding-3-small", Dimensions: 4}}, true); err != nil {
		t.Fatal(err)
	}
	s.SetReembedOptions(fastOpts())
	return s
}

func TestReembedRunsInBatchesAndKeepsSearchWorking(t *testing.T) {
	p := newProvider(t)
	s := staleStore(t, p, t.TempDir(), 7)
	ctx := context.Background()
	p.requests.Store(0)
	p.inputs.Store(0)

	if st, _ := s.EmbeddingStatus(ctx); st.Stale != 7 || s.vecs.len() != 0 {
		t.Fatalf("before: %+v indexed=%d", st, s.vecs.len())
	}
	// Mid-run the keyword side still answers.
	if got, err := s.Search(ctx, "zz", 5, Filter{}, time.Now()); err != nil || len(got) == 0 {
		t.Fatalf("search while stale: %v %d", err, len(got))
	}

	p.requests.Store(0)
	p.inputs.Store(0)
	summary, err := s.RunReembed(ctx)
	if err != nil {
		t.Fatalf("RunReembed: %v (%s)", err, summary)
	}
	reqs, ins := p.requests.Load(), p.inputs.Load()
	st, _ := s.EmbeddingStatus(ctx)
	if st.Stale != 0 || st.Current != 7 || s.vecs.len() != 7 {
		t.Fatalf("after: %+v indexed=%d", st, s.vecs.len())
	}
	if reqs != 3 || ins != 7 { // 3+3+1
		t.Errorf("requests=%d inputs=%d, want 3 batched requests carrying 7 texts", reqs, ins)
	}
	pr := s.ReembedStatus()
	if pr.Running || pr.Done != 7 || pr.Total != 7 || pr.Errors != 0 || pr.FinishedAt.IsZero() {
		t.Errorf("progress = %+v", pr)
	}
	// A second run has nothing to do.
	if _, err := s.RunReembed(ctx); err != nil || s.ReembedStatus().Total != 0 {
		t.Errorf("idempotent rerun: %v %+v", err, s.ReembedStatus())
	}
	// Vector search works over the new space.
	if got, err := s.Search(ctx, noteText(3), 3, Filter{Mode: "vector"}, time.Now()); err != nil || len(got) == 0 {
		t.Fatalf("vector search after: %v %d", err, len(got))
	}
}

func TestReembedFillsRowsStoredWithoutVector(t *testing.T) {
	p := newProvider(t)
	dir := t.TempDir()
	s, err := New(dir, "", p.srv.URL) // keyword-only
	if err != nil {
		t.Fatal(err)
	}
	addNote(t, s, "keyword only one")
	addNote(t, s, "keyword only two")
	key := testSecret
	if _, err := s.SetEmbeddingSettings(context.Background(), EmbeddingUpdate{EmbeddingSettings: EmbeddingSettings{Enabled: true, Model: "text-embedding-3-small"}, APIKey: &key}, true); err != nil {
		t.Fatal(err)
	}
	s.SetReembedOptions(fastOpts())
	if _, err := s.RunReembed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.EmbeddingStatus(context.Background()); st.Current != 2 || s.vecs.len() != 2 {
		t.Fatalf("status %+v indexed=%d", st, s.vecs.len())
	}
}

func TestReembedRetriesTransientErrors(t *testing.T) {
	p := newProvider(t)
	s := staleStore(t, p, t.TempDir(), 4)
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(testLogSink{})
	p.requests.Store(0)
	p.setHook(func(n int, _ []string) int {
		switch n {
		case 1:
			return http.StatusServiceUnavailable
		case 2:
			return http.StatusTooManyRequests
		}
		return 0
	})
	if _, err := s.RunReembed(context.Background()); err != nil {
		t.Fatalf("RunReembed: %v", err)
	}
	if st, _ := s.EmbeddingStatus(context.Background()); st.Stale != 0 {
		t.Fatalf("stale left: %+v", st)
	}
	if got := s.ReembedStatus(); got.Errors != 0 || got.Done != 4 {
		t.Errorf("progress %+v", got)
	}
	if !strings.Contains(buf.String(), "transient provider error") {
		t.Errorf("retry not logged: %s", buf.String())
	}
	if strings.Contains(buf.String(), testSecret) {
		t.Errorf("log leaked the API key: %s", buf.String())
	}
}

// testLogSink swallows log output after a test redirected it.
type testLogSink struct{}

func (testLogSink) Write(b []byte) (int, error) { return len(b), nil }

func TestReembedGivesUpAfterRetriesAndAbortsOnFatal(t *testing.T) {
	p := newProvider(t)
	s := staleStore(t, p, t.TempDir(), 3)
	p.requests.Store(0)
	p.setHook(func(int, []string) int { return http.StatusServiceUnavailable })
	_, err := s.RunReembed(context.Background())
	if err == nil || p.requests.Load() != 4 { // MaxAttempts
		t.Fatalf("persistent 503: err=%v requests=%d", err, p.requests.Load())
	}
	if st, _ := s.EmbeddingStatus(context.Background()); st.Stale != 3 {
		t.Errorf("a failed run must leave rows pending: %+v", st)
	}

	p.requests.Store(0)
	p.setHook(func(int, []string) int { return http.StatusUnauthorized })
	if _, err := s.RunReembed(context.Background()); err == nil || p.requests.Load() != 1 {
		t.Fatalf("401 must abort at once: err=%v requests=%d", err, p.requests.Load())
	}
	if lp := s.ReembedStatus(); strings.Contains(lp.LastError, testSecret) || lp.Running {
		t.Errorf("progress %+v", lp)
	}
}

func TestReembedIsolatesAPoisonedEntry(t *testing.T) {
	p := newProvider(t)
	s := staleStore(t, p, t.TempDir(), 5)
	ctx := context.Background()
	// The provider refuses (400) the batch that contains one particular text;
	// the job retries that batch text by text and loses only that entry.
	p.setHook(func(_ int, inputs []string) int {
		for _, in := range inputs {
			if in == noteText(2) {
				return http.StatusBadRequest
			}
		}
		return 0
	})
	if _, err := s.RunReembed(ctx); err != nil {
		t.Fatalf("a rejected entry must not abort the run: %v", err)
	}
	pr := s.ReembedStatus()
	if pr.Errors != 1 || pr.Done != 4 {
		t.Errorf("progress %+v, want 4 done and 1 error", pr)
	}
	if st, _ := s.EmbeddingStatus(ctx); st.Current != 4 || st.Stale != 1 {
		t.Errorf("status %+v", st)
	}
}

func TestReembedCancelAndResumeAfterRestart(t *testing.T) {
	p := newProvider(t)
	dir := t.TempDir()
	s := staleStore(t, p, dir, 8)
	ctx, cancel := context.WithCancel(context.Background())
	p.requests.Store(0)
	p.inputs.Store(0)
	p.setHook(func(n int, _ []string) int {
		if n == 2 {
			cancel() // the admin cancels while the second batch is in flight
		}
		return 0
	})
	_, err := s.RunReembed(ctx)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("want a cancellation error, got %v", err)
	}
	st, _ := s.EmbeddingStatus(context.Background())
	if st.Current == 0 || st.Stale == 0 || st.Current+st.Stale != 8 {
		t.Fatalf("expected partial progress, got %+v", st)
	}
	done := st.Current

	// "Restart": reopen the same data directory with a fresh store.
	if err := s.db.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := New(dir, testSecret, p.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	s2.SetReembedOptions(fastOpts())
	if s2.vecs.len() != done {
		t.Fatalf("index after restart = %d, want the %d rows already done", s2.vecs.len(), done)
	}
	p.setHook(nil)
	p.inputs.Store(0)
	if _, err := s2.RunReembed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := int(p.inputs.Load()); got != 8-done {
		t.Errorf("resume re-sent %d texts, want only the %d pending", got, 8-done)
	}
	if st, _ := s2.EmbeddingStatus(context.Background()); st.Stale != 0 || st.Current != 8 || s2.vecs.len() != 8 {
		t.Fatalf("after resume %+v indexed=%d", st, s2.vecs.len())
	}
}

func TestReembedDiscardsResultWhenConfigChangesDuringCall(t *testing.T) {
	p := newProvider(t)
	s := staleStore(t, p, t.TempDir(), 6) // now text-embedding-3-small @ 4 dims
	ctx := context.Background()
	var switched atomic.Bool
	p.requests.Store(0)
	p.setHook(func(n int, _ []string) int {
		// While the first batch is in flight the admin moves to another model of
		// the same size. The old model's vectors must not be stored as the new one.
		if n == 1 && switched.CompareAndSwap(false, true) {
			done := make(chan struct{})
			go func() {
				defer close(done)
				if _, err := s.SetEmbeddingSettings(ctx, EmbeddingUpdate{EmbeddingSettings: EmbeddingSettings{Enabled: true, Model: "other-model", Dimensions: 4}}, true); err != nil {
					t.Errorf("switch: %v", err)
				}
			}()
			<-done
		}
		return 0
	})
	if _, err := s.RunReembed(ctx); err != nil {
		t.Fatalf("RunReembed: %v", err)
	}
	counts, _ := s.db.EmbeddingSpaceCounts(ctx)
	for sp, n := range counts {
		if sp.Model != "other-model" || sp.Dims != 4 {
			t.Errorf("%d rows left in space %+v", n, sp)
		}
	}
	// Every stored vector was computed by the model it is tagged with.
	rows, _ := s.db.LoadAllEntries(ctx)
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	vecs, _ := s.db.EmbeddingsByIDs(ctx, ids)
	for _, r := range rows {
		want := modelVector("other-model", r.Content, 4)
		got := vecs[r.ID]
		if len(got) != 4 || got[0] != want[0] || got[3] != want[3] {
			t.Errorf("%s holds a vector from another model: got %v want %v", r.ID, got, want)
		}
	}
	if st, _ := s.EmbeddingStatus(ctx); st.Current != 6 || st.Stale != 0 || s.vecs.len() != 6 {
		t.Errorf("status %+v indexed=%d", st, s.vecs.len())
	}
}

func TestReembedRejectsConcurrentRunAndStopsWhenDisabled(t *testing.T) {
	p := newProvider(t)
	s := staleStore(t, p, t.TempDir(), 3)
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	p.setHook(func(int, []string) int {
		once.Do(func() { close(started) })
		<-release
		return 0
	})
	errc := make(chan error, 1)
	go func() { _, err := s.RunReembed(context.Background()); errc <- err }()
	<-started
	if _, err := s.RunReembed(context.Background()); !errors.Is(err, ErrReembedRunning) {
		t.Errorf("second run: %v", err)
	}
	if !s.ReembedStatus().Running {
		t.Error("status must say running")
	}
	close(release)
	if err := <-errc; err != nil {
		t.Fatal(err)
	}

	if _, err := s.SetEmbeddingSettings(context.Background(), EmbeddingUpdate{EmbeddingSettings: EmbeddingSettings{Enabled: false, Model: "text-embedding-3-small", Dimensions: 4}}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunReembed(context.Background()); err == nil {
		t.Error("a disabled provider must fail the run, not spin")
	}
}

func TestEstimateReembed(t *testing.T) {
	p := newProvider(t)
	s := staleStore(t, p, t.TempDir(), 5)
	ctx := context.Background()

	est, err := s.EstimateReembed(ctx, nil)
	if err != nil || est.Pending != 5 || est.Tokens != (est.Chars+3)/4 || est.Tokens == 0 {
		t.Fatalf("active estimate = %+v, %v", est, err)
	}
	if est.CostUSD != nil {
		t.Errorf("custom endpoint must get no price: %+v", est)
	}

	// On the public endpoint known models are priced, unknown ones are not.
	s.emb.mu.Lock()
	s.emb.envBase = ""
	s.emb.mu.Unlock()
	est, _ = s.EstimateReembed(ctx, nil)
	if est.CostUSD == nil || est.PricePerMTok == nil || *est.PricePerMTok != 0.02 {
		t.Fatalf("expected a price for text-embedding-3-small: %+v", est)
	}
	if want := float64(est.Tokens) / 1e6 * 0.02; *est.CostUSD < want-1e-6 || *est.CostUSD > want+1e-6 {
		t.Errorf("cost %v, want about %v", *est.CostUSD, want)
	}
	// What-if: the same model at its default size leaves all 5 pending too;
	// a model with no known price gets null.
	est, err = s.EstimateReembed(ctx, &EmbeddingSettings{Model: "mystery-model"})
	if err != nil || est.Pending != 5 || est.CostUSD != nil || est.PricePerMTok != nil {
		t.Errorf("unknown model: %+v, %v", est, err)
	}
	if _, err := s.EstimateReembed(ctx, &EmbeddingSettings{}); !errors.Is(err, ErrEmbeddingInvalid) {
		t.Errorf("empty model: %v", err)
	}
}

// Rows can leave the pending set while the job runs without the job writing
// them: a deleted row, an edited row (Update re-embeds it with the active
// model), a new note. The counters must still add up at the end: every row
// counted in Total is Done, an Error or Skipped.
func TestReembedCountersAccountForRowsThatLeavePendingConcurrently(t *testing.T) {
	p := newProvider(t)
	s := staleStore(t, p, t.TempDir(), 8)
	ctx := context.Background()
	var once atomic.Bool
	p.requests.Store(0)
	p.setHook(func(n int, _ []string) int {
		if !once.CompareAndSwap(false, true) {
			return 0
		}
		pend, err := s.db.PendingEmbeddings(ctx, s.spaceOf(s.currentEmbedder()), "", 100)
		if err != nil || len(pend) != 8 {
			t.Errorf("pending in hook: %v %d", err, len(pend))
			return 0
		}
		// one row of the batch in flight, one edited ahead of the cursor, one deleted ahead of it
		if _, err := s.Delete(ctx, pend[0].ID); err != nil {
			t.Errorf("delete: %v", err)
		}
		edited := "edited note ahead of the cursor"
		if _, err := s.Update(ctx, pend[6].ID, UpdatePatch{Content: &edited}, time.Now()); err != nil {
			t.Errorf("update: %v", err)
		}
		if _, err := s.Delete(ctx, pend[7].ID); err != nil {
			t.Errorf("delete: %v", err)
		}
		addNote(t, s, "brand new note embedded with the active model")
		return 0
	})
	if _, err := s.RunReembed(ctx); err != nil {
		t.Fatalf("RunReembed: %v", err)
	}
	pr := s.ReembedStatus()
	if st, _ := s.EmbeddingStatus(ctx); st.Stale != 0 {
		t.Fatalf("still stale: %+v", st)
	}
	if pr.Total != 8 || pr.Errors != 0 || pr.Done != 5 || pr.Skipped != 3 || pr.Done+pr.Errors+pr.Skipped != pr.Total {
		t.Errorf("progress = %+v, want total 8 = done 5 + skipped 3", pr)
	}
}
