package store

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/embed"
	"github.com/rafaelvieiras/m3m0s/internal/sqlstore"
)

// Re-embedding job (#39 phase 2, D36). The job's state is the database
// itself: a row is pending while its vector space (model, dimensions) differs
// from the active one, or while it has no vector. Stopping, crashing or
// cancelling therefore loses nothing; running it again continues with what is
// still pending.

// ReembedJobName is the name of the re-embedding job in the scheduler (and in
// GET /api/v1/jobs).
const ReembedJobName = "embedding-reembed"

// ErrReembedRunning is returned when a re-embedding run is already in progress.
var ErrReembedRunning = errors.New("re-embedding is already running")

// ReembedOptions tunes the job. The zero value of a field means its default.
type ReembedOptions struct {
	BatchSize     int           // texts per provider request
	MaxBatchChars int           // soft cap on the characters of one request
	RatePerSec    float64       // provider requests per second
	MaxAttempts   int           // tries per request on a transient error
	BackoffBase   time.Duration // first retry delay, doubled each time
	BackoffMax    time.Duration
}

const (
	defaultReembedBatch = 64
	maxReembedBatch     = 2048 // the OpenAI limit on inputs per request
	defaultReembedRPS   = 5.0
	defaultBatchChars   = 200_000 // ~50k tokens, far below provider request limits
)

// ReembedOptionsFromEnv reads the optional overrides M3M0S_REEMBED_BATCH
// (texts per request) and M3M0S_REEMBED_RPS (requests per second).
func ReembedOptionsFromEnv() ReembedOptions {
	var o ReembedOptions
	if n, err := strconv.Atoi(os.Getenv("M3M0S_REEMBED_BATCH")); err == nil && n > 0 {
		o.BatchSize = min(n, maxReembedBatch)
	}
	if f, err := strconv.ParseFloat(os.Getenv("M3M0S_REEMBED_RPS"), 64); err == nil && f > 0 {
		o.RatePerSec = f
	}
	return o
}

func (o ReembedOptions) withDefaults() ReembedOptions {
	if o.BatchSize <= 0 {
		o.BatchSize = defaultReembedBatch
	}
	if o.MaxBatchChars <= 0 {
		o.MaxBatchChars = defaultBatchChars
	}
	if o.RatePerSec <= 0 {
		o.RatePerSec = defaultReembedRPS
	}
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = 6
	}
	if o.BackoffBase <= 0 {
		o.BackoffBase = time.Second
	}
	if o.BackoffMax <= 0 {
		o.BackoffMax = 30 * time.Second
	}
	return o
}

// SetReembedOptions overrides the job tuning (environment overrides at boot,
// small values in tests).
func (s *MemoryStore) SetReembedOptions(o ReembedOptions) {
	s.re.mu.Lock()
	s.re.opts = o
	s.re.mu.Unlock()
}

// ReembedProgress is a snapshot of the current or last run.
type ReembedProgress struct {
	Running bool
	Model   string
	Dims    int
	Total   int // pending when the run (or the last configuration change) started
	Done    int // rows written by this job
	Errors  int
	// Skipped counts rows that were in Total but left the pending set without
	// this job writing them (deleted, edited and re-embedded by Update, or
	// replaced by a concurrent write). Once a run ends cleanly,
	// Done + Errors + Skipped == Total.
	Skipped    int
	StartedAt  time.Time
	FinishedAt time.Time // zero while running
	ETA        time.Duration
	LastError  string // never contains credentials
}

type reembedState struct {
	mu   sync.Mutex
	opts ReembedOptions
	prog ReembedProgress
}

// ReembedStatus returns the progress of the current or last run (the zero
// value when none ran since boot).
func (s *MemoryStore) ReembedStatus() ReembedProgress {
	s.re.mu.Lock()
	defer s.re.mu.Unlock()
	p := s.re.prog
	if p.Running && p.Done > 0 {
		remaining := p.Total - p.Done - p.Errors - p.Skipped
		if remaining > 0 {
			p.ETA = time.Duration(float64(time.Since(p.StartedAt)) / float64(p.Done+p.Errors) * float64(remaining))
		}
	}
	return p
}

func (s *MemoryStore) reembedUpdate(f func(p *ReembedProgress)) {
	s.re.mu.Lock()
	f(&s.re.prog)
	s.re.mu.Unlock()
}

// spaceOf reads the vector space of e; dims can be learned concurrently.
func (s *MemoryStore) spaceOf(e *embedder) sqlstore.EmbeddingSpace {
	s.emb.mu.RLock()
	defer s.emb.mu.RUnlock()
	return e.space()
}

// RunReembed re-embeds every row outside the active vector space with the
// active provider, in batches, until nothing is pending or ctx is cancelled.
// It follows configuration changes: when the active embedder changes mid-run
// the pending set is recomputed for the new space, and a result computed for
// the old one is discarded rather than stored.
func (s *MemoryStore) RunReembed(ctx context.Context) (string, error) {
	s.re.mu.Lock()
	if s.re.prog.Running {
		s.re.mu.Unlock()
		return "", ErrReembedRunning
	}
	opts := s.re.opts.withDefaults()
	s.re.prog = ReembedProgress{Running: true, StartedAt: time.Now()}
	s.re.mu.Unlock()

	err := s.reembedLoop(ctx, opts)

	s.reembedUpdate(func(p *ReembedProgress) {
		p.Running, p.FinishedAt, p.ETA = false, time.Now(), 0
		if err != nil {
			p.LastError = err.Error()
		}
		if p.Done+p.Errors+p.Skipped > p.Total {
			p.Total = p.Done + p.Errors + p.Skipped
		}
		// A clean end means nothing is pending any more: whatever was counted
		// in Total and neither written nor failed left the pending set on its
		// own (deleted or re-embedded by a normal write), so account for it.
		if err == nil && p.Done+p.Errors+p.Skipped < p.Total {
			p.Skipped = p.Total - p.Done - p.Errors
		}
	})
	p := s.ReembedStatus()
	summary := fmt.Sprintf("%d re-embedded, %d errors, %d skipped, of %d pending (%s, %d dims)", p.Done, p.Errors, p.Skipped, p.Total, p.Model, p.Dims)
	log.Printf("m3m0s: re-embedding finished: %s", summary)
	return summary, err
}

func (s *MemoryStore) reembedLoop(ctx context.Context, opts ReembedOptions) error {
	var cur *embedder
	cursor := ""
	lim := &rateLimiter{every: time.Duration(float64(time.Second) / opts.RatePerSec)}
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("cancelled: %w", err)
		}
		e := s.currentEmbedder()
		if e == nil {
			return errors.New("embeddings are disabled or have no API key")
		}
		sp := s.spaceOf(e)
		if e != cur { // first pass, or the configuration changed under us
			cur, cursor = e, ""
			n, _, err := s.db.PendingStats(ctx, sp, maxEmbedChars)
			if err != nil {
				return err
			}
			s.reembedUpdate(func(p *ReembedProgress) {
				p.Model, p.Dims, p.Total, p.Done, p.Errors, p.Skipped = sp.Model, sp.Dims, n, 0, 0, 0
			})
			log.Printf("m3m0s: re-embedding %d rows into %s (%d dims)", n, sp.Model, sp.Dims)
		}
		items, err := s.db.PendingEmbeddings(ctx, sp, cursor, opts.BatchSize)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		items = capByChars(items, opts.MaxBatchChars)
		cursor = items[len(items)-1].ID

		texts := make([]string, len(items))
		for i, it := range items {
			texts[i] = truncateForEmbedding(it.Content)
		}
		vecs, errs, fatal := embedMany(ctx, e, texts, opts, lim)
		if fatal != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("cancelled: %w", ctx.Err())
			}
			return fatal
		}
		for i, it := range items {
			if errs[i] != nil {
				s.reembedUpdate(func(p *ReembedProgress) { p.Errors++; p.LastError = errs[i].Error() })
				continue
			}
			stored, err := s.storeReembedded(ctx, e, it, vecs[i])
			if errors.Is(err, errSpaceChanged) {
				cur = nil // discard the rest of this batch and recompute for the new space
				break
			}
			if err != nil {
				s.reembedUpdate(func(p *ReembedProgress) { p.Errors++; p.LastError = err.Error() })
				continue
			}
			s.reembedUpdate(func(p *ReembedProgress) {
				if stored {
					p.Done++
				} else {
					p.Skipped++ // edited, deleted or re-embedded meanwhile
				}
			})
		}
	}
}

var errSpaceChanged = errors.New("embedding configuration changed during the call")

// storeReembedded writes one vector, but only while e is still the active
// embedder: a vector computed with the previous configuration must never be
// tagged with the new one (D36). The check and the write share one read lock,
// and SetEmbeddingSettings swaps the embedder under the write lock, so the
// two cannot interleave.
func (s *MemoryStore) storeReembedded(ctx context.Context, e *embedder, it sqlstore.PendingEntry, vec []float32) (bool, error) {
	s.emb.mu.RLock()
	if s.emb.emb == e && e.dims == 0 {
		s.emb.mu.RUnlock()
		s.emb.mu.Lock() // the provider's default size, learned from the first vector
		if s.emb.emb == e && e.dims == 0 {
			e.dims = len(vec)
		}
		s.emb.mu.Unlock()
		s.emb.mu.RLock()
	}
	defer s.emb.mu.RUnlock()
	if s.emb.emb != e {
		return false, errSpaceChanged
	}
	if len(vec) != e.dims {
		return false, fmt.Errorf("provider returned %d dimensions, expected %d", len(vec), e.dims)
	}
	ok, err := s.db.SetEmbeddingIfPending(ctx, it.ID, it.Content, sqlstore.EmbeddingSpace{Model: e.model, Dims: e.dims}, vec)
	if err != nil {
		return false, err
	}
	if ok { // false: the row was edited, deleted or re-embedded meanwhile
		s.vecs.upsert(it.ID, it.Domain, vec)
	}
	return ok, nil
}

func capByChars(items []sqlstore.PendingEntry, maxChars int) []sqlstore.PendingEntry {
	total := 0
	for i, it := range items {
		total += min(len([]rune(it.Content)), maxEmbedChars)
		if total > maxChars && i > 0 {
			return items[:i]
		}
	}
	return items
}

// rateLimiter spaces provider requests at least every apart.
type rateLimiter struct {
	every time.Duration
	next  time.Time
}

func (l *rateLimiter) wait(ctx context.Context) error {
	if d := time.Until(l.next); d > 0 {
		if err := sleepCtx(ctx, d); err != nil {
			return err
		}
	}
	l.next = time.Now().Add(l.every)
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// callProvider embeds texts once (batch when the provider client has it).
func callProvider(ctx context.Context, e *embedder, texts []string) ([][]float32, error) {
	if e.batch != nil {
		return e.batch(ctx, texts)
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, err := e.fn(ctx, t)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// callWithRetry retries transient errors (network, 408, 429, 5xx) with an
// exponential backoff; any other error is returned at once.
func callWithRetry(ctx context.Context, e *embedder, texts []string, o ReembedOptions, lim *rateLimiter) ([][]float32, error) {
	delay := o.BackoffBase
	for attempt := 1; ; attempt++ {
		if err := lim.wait(ctx); err != nil {
			return nil, err
		}
		vecs, err := callProvider(ctx, e, texts)
		if err == nil || !embed.IsTransient(err) || attempt >= o.MaxAttempts {
			return vecs, err
		}
		log.Printf("m3m0s: re-embedding: transient provider error (attempt %d/%d), retrying in %s: %v", attempt, o.MaxAttempts, delay, err)
		if err := sleepCtx(ctx, delay); err != nil {
			return nil, err
		}
		delay = min(delay*2, o.BackoffMax)
	}
}

// embedMany embeds texts; a non-transient failure of a multi-text request is
// retried one text at a time, so a single bad entry costs one error instead
// of the whole batch. fatal means the run cannot continue (bad key, unknown
// model, retries exhausted, cancellation).
func embedMany(ctx context.Context, e *embedder, texts []string, o ReembedOptions, lim *rateLimiter) (vecs [][]float32, errs []error, fatal error) {
	errs = make([]error, len(texts))
	vecs, err := callWithRetry(ctx, e, texts, o, lim)
	if err == nil {
		return vecs, errs, nil
	}
	if ctx.Err() != nil || embed.IsFatal(err) || embed.IsTransient(err) {
		return nil, nil, fmt.Errorf("provider: %w", err)
	}
	if len(texts) == 1 {
		errs[0] = err
		return make([][]float32, 1), errs, nil
	}
	vecs = make([][]float32, len(texts))
	for i, t := range texts {
		one, err := callWithRetry(ctx, e, []string{t}, o, lim)
		switch {
		case err == nil:
			vecs[i] = one[0]
		case ctx.Err() != nil || embed.IsFatal(err) || embed.IsTransient(err):
			return nil, nil, fmt.Errorf("provider: %w", err)
		default:
			errs[i] = err
		}
	}
	return vecs, errs, nil
}

// ── Cost estimate ──

// Embedding list prices in USD per 1M input tokens, as published by OpenAI
// (https://platform.openai.com/docs/pricing, "Embeddings"). Dated constants,
// not fetched: re-check the page when changing them. Unknown models have no
// entry and get no cost estimate; prices are never guessed. They only apply to
// the public OpenAI endpoint: a custom base URL may be priced differently.
const pricesAsOf = "2026-10-08"

var embeddingPricePerMTok = map[string]float64{
	"text-embedding-3-small": 0.02,
	"text-embedding-3-large": 0.13,
	"text-embedding-ada-002": 0.10,
}

// defaultModelDims is the vector size these models return when no
// "dimensions" is requested.
var defaultModelDims = map[string]int{
	"text-embedding-3-small": 1536,
	"text-embedding-3-large": 3072,
	"text-embedding-ada-002": 1536,
}

// ReembedEstimate is the approximate size and cost of a re-embedding run.
type ReembedEstimate struct {
	Model   string
	Dims    int   // target dimensions, 0 = any/unknown
	Pending int   // rows outside the target space (including rows with no vector)
	Chars   int64 // characters to send (each row capped like the real request)
	// Tokens is an approximation: Chars/4. Real tokenization differs by
	// language and content.
	Tokens       int64
	PricePerMTok *float64 // nil = unknown model or custom endpoint
	CostUSD      *float64 // nil when the price is unknown
	PricesAsOf   string
}

// EstimateReembed sizes the run that would follow switching to proposed (nil =
// the active configuration). It changes nothing.
func (s *MemoryStore) EstimateReembed(ctx context.Context, proposed *EmbeddingSettings) (ReembedEstimate, error) {
	s.emb.mu.RLock()
	cfg, e := s.emb.cfg, s.emb.emb
	s.emb.mu.RUnlock()
	target := cfg
	dims := 0
	if proposed != nil {
		target = *proposed
		target.Model = strings.TrimSpace(target.Model)
		if target.Model == "" {
			return ReembedEstimate{}, fmt.Errorf("%w: model is required", ErrEmbeddingInvalid)
		}
		if target.Dimensions < 0 {
			return ReembedEstimate{}, fmt.Errorf("%w: dimensions must be >= 0", ErrEmbeddingInvalid)
		}
		dims = target.Dimensions
	} else if e != nil {
		dims = s.spaceOf(e).Dims
	} else {
		dims = target.Dimensions
	}
	if dims == 0 {
		dims = defaultModelDims[target.Model] // 0 when unknown: any size matches
	}
	sp := sqlstore.EmbeddingSpace{Model: target.Model, Dims: dims}
	n, chars, err := s.db.PendingStats(ctx, sp, maxEmbedChars)
	if err != nil {
		return ReembedEstimate{}, err
	}
	est := ReembedEstimate{Model: sp.Model, Dims: dims, Pending: n, Chars: chars, Tokens: (chars + 3) / 4, PricesAsOf: pricesAsOf}
	if price, ok := embeddingPricePerMTok[sp.Model]; ok && s.baseFor(target) == embed.DefaultBaseURL {
		cost := math.Round(float64(est.Tokens)/1e6*price*1e6) / 1e6
		est.PricePerMTok, est.CostUSD = &price, &cost
	}
	return est, nil
}
