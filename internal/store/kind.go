package store

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/factkind"
	"github.com/rafaelvieiras/m3m0s/internal/redact"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// Fact nature (kind, D38). The tag lives in two reserved metadata keys
// (factkind.MetadataKey / MetadataSourceKey), so introducing it rewrote no
// existing row: a missing tag reads as "other" and the optional backfill below
// fills the gap at a controlled rate and cost.

// withKind returns a copy of base tagged with a kind: the extractor's
// (llmKind, already normalized) when it gave one, otherwise the conservative
// heuristic on text. It always overwrites a previous tag in base — callers
// pass a fresh fact, and manual tags are protected elsewhere (Update,
// adoptKind, the backfill).
func withKind(base map[string]string, text, llmKind string) map[string]string {
	kind, src := llmKind, factkind.SourceLLM
	if kind == "" {
		kind, _ = factkind.Classify(text)
		src = factkind.SourceHeuristic
	}
	return withMetadata(withMetadata(base, factkind.MetadataKey, kind), factkind.MetadataSourceKey, src)
}

// keepKind extracts the kind tag from md (empty when untagged).
func keepKind(md map[string]string) map[string]string {
	out := map[string]string{}
	for _, k := range []string{factkind.MetadataKey, factkind.MetadataSourceKey} {
		if v, ok := md[k]; ok {
			out[k] = v
		}
	}
	return out
}

// adoptKind decides whether an entry that absorbed a duplicate takes the tag of
// the incoming fact: never over a manual or LLM tag, over a heuristic one only
// when the incoming is from the LLM, and over no tag always.
func adoptKind(merged, incoming map[string]string) map[string]string {
	incKind, incSrc := incoming[factkind.MetadataKey], incoming[factkind.MetadataSourceKey]
	if !factkind.Valid(incKind) || !factkind.ValidSource(incSrc) || incSrc == factkind.SourceManual {
		return merged
	}
	cur := merged[factkind.MetadataSourceKey]
	switch {
	case factkind.ValidSource(cur) && cur != factkind.SourceHeuristic:
		return merged
	case cur == factkind.SourceHeuristic && incSrc != factkind.SourceLLM:
		return merged
	}
	return withMetadata(withMetadata(merged, factkind.MetadataKey, incKind), factkind.MetadataSourceKey, incSrc)
}

// KindClassifier is satisfied by extractors that can classify stored facts in
// batches (*extract.Extractor.ClassifyKinds) — what the backfill uses; an
// optional type assertion, like KindExtractor.
type KindClassifier interface {
	ClassifyKinds(ctx context.Context, facts []string) ([]string, error)
}

// ── Backfill job ──

// KindBackfillJobName is the name of the backfill job in the scheduler.
const KindBackfillJobName = "kind-backfill"

var (
	// ErrKindBackfillRunning is returned when a backfill run is already in progress.
	ErrKindBackfillRunning = errors.New("kind backfill is already running")
	// ErrKindBackfillNotConfirmed is returned by a run that spends LLM tokens
	// without an explicit confirmation (POST /kinds/backfill with confirm).
	ErrKindBackfillNotConfirmed = errors.New("kind backfill spends LLM tokens and needs confirmation: estimate it, then confirm (POST /kinds/backfill, or `m3m0s kind backfill start --yes`)")
)

// KindBackfillOptions tunes the job; the zero value of a field means its default.
type KindBackfillOptions struct {
	BatchSize  int     // facts per LLM request
	RatePerSec float64 // LLM requests per second
	FactChars  int     // facts are cut to this many runes before classification
}

const (
	defaultKindBatch    = 20
	maxKindBatch        = 100
	defaultKindRPS      = 1.0
	defaultKindFactChar = 600
	// Fixed token costs used by the estimate: the system prompt plus framing
	// per request, and the answer's framing; ~4 tokens per answered kind.
	kindRequestOverheadTokens = 230
	kindAnswerOverheadTokens  = 12
	kindAnswerTokensPerFact   = 4
	kindAbortAfterFailures    = 5 // consecutive failed batches before the run gives up
)

// KindBackfillOptionsFromEnv reads M3M0S_KIND_BACKFILL_BATCH and
// M3M0S_KIND_BACKFILL_RPS.
func KindBackfillOptionsFromEnv() KindBackfillOptions {
	var o KindBackfillOptions
	if n, err := strconv.Atoi(os.Getenv("M3M0S_KIND_BACKFILL_BATCH")); err == nil && n > 0 {
		o.BatchSize = min(n, maxKindBatch)
	}
	if f, err := strconv.ParseFloat(os.Getenv("M3M0S_KIND_BACKFILL_RPS"), 64); err == nil && f > 0 {
		o.RatePerSec = f
	}
	return o
}

func (o KindBackfillOptions) withDefaults() KindBackfillOptions {
	if o.BatchSize <= 0 {
		o.BatchSize = defaultKindBatch
	}
	if o.RatePerSec <= 0 {
		o.RatePerSec = defaultKindRPS
	}
	if o.FactChars <= 0 {
		o.FactChars = defaultKindFactChar
	}
	return o
}

// SetKindBackfillOptions overrides the job tuning (environment at boot, small
// values in tests).
func (s *MemoryStore) SetKindBackfillOptions(o KindBackfillOptions) {
	s.kb.mu.Lock()
	s.kb.opts = o
	s.kb.mu.Unlock()
}

// KindBackfillProgress is a snapshot of the current or last run.
type KindBackfillProgress struct {
	Running    bool
	Mode       string // "llm" or "heuristic"
	Total      int    // pending when the run started
	Done       int    // entries tagged by this run
	Errors     int
	Skipped    int // left the pending set without this run tagging them (deleted, edited, tagged meanwhile, manual)
	StartedAt  time.Time
	FinishedAt time.Time
	ETA        time.Duration
	LastError  string
}

type kindBackfillState struct {
	mu    sync.Mutex
	opts  KindBackfillOptions
	prog  KindBackfillProgress
	armed bool // an LLM run was confirmed and has not started yet
}

// KindBackfillStatus returns the progress of the current or last run.
func (s *MemoryStore) KindBackfillStatus() KindBackfillProgress {
	s.kb.mu.Lock()
	defer s.kb.mu.Unlock()
	p := s.kb.prog
	if p.Running && p.Done > 0 {
		if remaining := p.Total - p.Done - p.Errors - p.Skipped; remaining > 0 {
			p.ETA = time.Duration(float64(time.Since(p.StartedAt)) / float64(p.Done+p.Errors) * float64(remaining))
		}
	}
	return p
}

func (s *MemoryStore) kindBackfillUpdate(f func(p *KindBackfillProgress)) {
	s.kb.mu.Lock()
	f(&s.kb.prog)
	s.kb.mu.Unlock()
}

// ConfirmKindBackfill records the explicit confirmation of an LLM-backed run;
// the next run consumes it. It exists so that triggering the job from the Jobs
// page cannot spend tokens that nobody estimated and approved.
func (s *MemoryStore) ConfirmKindBackfill() {
	s.kb.mu.Lock()
	s.kb.armed = true
	s.kb.mu.Unlock()
}

// RevokeKindBackfillConfirmation withdraws a confirmation (the trigger failed).
func (s *MemoryStore) RevokeKindBackfillConfirmation() {
	s.kb.mu.Lock()
	s.kb.armed = false
	s.kb.mu.Unlock()
}

func (s *MemoryStore) kindClassifier() (KindClassifier, bool) {
	if s.extractor == nil {
		return nil, false
	}
	kc, ok := s.extractor.(KindClassifier)
	return kc, ok
}

type kindCandidate struct {
	ID        string
	Content   string
	CreatedAt time.Time
}

// pendingKindCandidates lists, oldest first, the active entries the backfill
// would tag. The state is the data itself, so a stopped run resumes by running
// again: untagged entries are pending; heuristic tags are pending too when an
// LLM classifier is available (it improves them); manual and LLM tags never.
func (s *MemoryStore) pendingKindCandidates(llm bool) []kindCandidate {
	s.mu.RLock()
	var out []kindCandidate
	for _, e := range s.index {
		if isSoftDeleted(e) {
			continue
		}
		switch e.KindSourceValue() {
		case factkind.SourceManual, factkind.SourceLLM:
			continue
		case factkind.SourceHeuristic:
			if !llm {
				continue
			}
		}
		out = append(out, kindCandidate{ID: e.ID, Content: e.Content, CreatedAt: e.CreatedAt})
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// KindBackfillEstimate sizes the run that would start now. Tokens are
// approximate (characters / 4); the cost is only known when the operator
// supplies the extraction model's prices (M3M0S_EXTRACT_PRICE_IN_PER_MTOK and
// M3M0S_EXTRACT_PRICE_OUT_PER_MTOK, USD per million tokens) — the project
// keeps no price table for arbitrary chat models.
type KindBackfillEstimate struct {
	Mode            string // "llm" or "heuristic"
	Model           string
	Pending         int
	Requests        int
	InputTokens     int64
	OutputTokens    int64
	PriceInPerMTok  *float64
	PriceOutPerMTok *float64
	CostUSD         *float64
	Seconds         int // at the configured request rate
}

func envPrice(name string) *float64 {
	if f, err := strconv.ParseFloat(os.Getenv(name), 64); err == nil && f >= 0 {
		return &f
	}
	return nil
}

// EstimateKindBackfill computes the size and approximate cost of a backfill
// run. It changes nothing and calls no provider.
func (s *MemoryStore) EstimateKindBackfill() KindBackfillEstimate {
	s.kb.mu.Lock()
	opts := s.kb.opts.withDefaults()
	s.kb.mu.Unlock()

	_, llm := s.kindClassifier()
	est := KindBackfillEstimate{Mode: "heuristic"}
	cands := s.pendingKindCandidates(llm)
	est.Pending = len(cands)
	if !llm {
		return est
	}
	est.Mode = "llm"
	_, est.Model = s.ExtractionInfo()
	est.Requests = (len(cands) + opts.BatchSize - 1) / opts.BatchSize
	est.InputTokens = int64(est.Requests) * kindRequestOverheadTokens
	est.OutputTokens = int64(est.Requests) * kindAnswerOverheadTokens
	for _, c := range cands {
		est.InputTokens += approxTokens(cutRunes(c.Content, opts.FactChars)) + 6
		est.OutputTokens += kindAnswerTokensPerFact
	}
	est.Seconds = int(float64(est.Requests) / opts.RatePerSec)
	est.PriceInPerMTok = envPrice("M3M0S_EXTRACT_PRICE_IN_PER_MTOK")
	est.PriceOutPerMTok = envPrice("M3M0S_EXTRACT_PRICE_OUT_PER_MTOK")
	if est.PriceInPerMTok != nil && est.PriceOutPerMTok != nil {
		c := (float64(est.InputTokens)*(*est.PriceInPerMTok) + float64(est.OutputTokens)*(*est.PriceOutPerMTok)) / 1e6
		est.CostUSD = &c
	}
	return est
}

func cutRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// RunKindBackfill tags the entries that have no tag yet (and upgrades
// heuristic tags when an LLM classifier is configured), in rate-limited
// batches, until nothing is pending or ctx is cancelled. A run with an LLM
// classifier needs a prior ConfirmKindBackfill. Manual tags are never
// touched, and neither is content or updatedAt.
func (s *MemoryStore) RunKindBackfill(ctx context.Context) (string, error) {
	kc, llm := s.kindClassifier()

	s.kb.mu.Lock()
	if s.kb.prog.Running {
		s.kb.mu.Unlock()
		return "", ErrKindBackfillRunning
	}
	if llm && !s.kb.armed {
		s.kb.mu.Unlock()
		return "", ErrKindBackfillNotConfirmed
	}
	s.kb.armed = false
	opts := s.kb.opts.withDefaults()
	mode := "heuristic"
	if llm {
		mode = "llm"
	}
	s.kb.prog = KindBackfillProgress{Running: true, Mode: mode, StartedAt: time.Now()}
	s.kb.mu.Unlock()

	cands := s.pendingKindCandidates(llm)
	s.kindBackfillUpdate(func(p *KindBackfillProgress) { p.Total = len(cands) })
	log.Printf("m3m0s: kind backfill (%s): %d entries pending", mode, len(cands))

	err := s.kindBackfillLoop(ctx, cands, kc, llm, opts)

	s.kindBackfillUpdate(func(p *KindBackfillProgress) {
		p.Running, p.FinishedAt, p.ETA = false, time.Now(), 0
		if err != nil {
			p.LastError = err.Error()
		} else if p.Done+p.Errors+p.Skipped < p.Total {
			p.Skipped = p.Total - p.Done - p.Errors
		}
	})
	p := s.KindBackfillStatus()
	summary := fmt.Sprintf("%d tagged, %d errors, %d skipped, of %d pending (%s)", p.Done, p.Errors, p.Skipped, p.Total, p.Mode)
	log.Printf("m3m0s: kind backfill finished: %s", summary)
	return summary, err
}

func (s *MemoryStore) kindBackfillLoop(ctx context.Context, cands []kindCandidate, kc KindClassifier, llm bool, opts KindBackfillOptions) error {
	lim := &rateLimiter{every: time.Duration(float64(time.Second) / opts.RatePerSec)}
	failures := 0
	for start := 0; start < len(cands); start += opts.BatchSize {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("cancelled: %w", err)
		}
		batch := cands[start:min(start+opts.BatchSize, len(cands))]
		kinds := make([]string, len(batch))
		src := factkind.SourceHeuristic
		if llm {
			src = factkind.SourceLLM
			texts := make([]string, len(batch))
			for i, c := range batch {
				texts[i] = cutRunes(c.Content, opts.FactChars)
			}
			if err := lim.wait(ctx); err != nil {
				return fmt.Errorf("cancelled: %w", err)
			}
			got, err := kc.ClassifyKinds(ctx, texts)
			if err != nil {
				if ctx.Err() != nil {
					return fmt.Errorf("cancelled: %w", ctx.Err())
				}
				failures++
				s.kindBackfillUpdate(func(p *KindBackfillProgress) { p.Errors += len(batch); p.LastError = err.Error() })
				if failures >= kindAbortAfterFailures {
					return fmt.Errorf("giving up after %d consecutive failed batches: %w", failures, err)
				}
				continue
			}
			failures = 0
			for i, k := range got {
				kinds[i] = factkind.Normalize(k)
			}
		} else {
			for i, c := range batch {
				kinds[i], _ = factkind.Classify(c.Content)
			}
		}
		for i, c := range batch {
			ok, err := s.applyBackfillKind(ctx, c, kinds[i], src)
			s.kindBackfillUpdate(func(p *KindBackfillProgress) {
				switch {
				case err != nil:
					p.Errors++
					p.LastError = err.Error()
				case ok:
					p.Done++
				default:
					p.Skipped++
				}
			})
		}
	}
	return nil
}

// applyBackfillKind writes one tag, re-checking under the write lock that the
// entry still exists, still has the content that was classified and was not
// tagged by a human or an LLM meanwhile. It rewrites only metadata_json and
// keeps updated_at (like access reinforcement): a tag is not an edit.
func (s *MemoryStore) applyBackfillKind(ctx context.Context, c kindCandidate, kind, source string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.index[c.ID]
	if !ok || e.Content != c.Content || isSoftDeleted(e) {
		return false, nil
	}
	switch cur := e.KindSourceValue(); {
	case cur == factkind.SourceManual || cur == factkind.SourceLLM:
		return false, nil
	case cur == factkind.SourceHeuristic && source == factkind.SourceHeuristic:
		return false, nil
	}
	e.Metadata = withMetadata(withMetadata(e.Metadata, factkind.MetadataKey, kind), factkind.MetadataSourceKey, source)
	if err := s.db.UpdateMetadata(ctx, e.ID, e.Metadata, e.UpdatedAt); err != nil {
		return false, err
	}
	s.index[e.ID] = e
	return true, nil
}

// ── Distribution and labelling sample ──

// KindDistribution counts active entries by kind (in enum order, untagged ones
// as "other") and by tag source ("none" for untagged).
func (s *MemoryStore) KindDistribution() (byKind, bySource []apitypes.CountItem) {
	kinds := map[string]int{}
	srcs := map[string]int{}
	s.mu.RLock()
	for _, e := range s.index {
		if isSoftDeleted(e) {
			continue
		}
		kinds[e.KindValue()]++
		src := e.KindSourceValue()
		if src == "" {
			src = "none"
		}
		srcs[src]++
	}
	s.mu.RUnlock()
	for _, k := range factkind.All {
		byKind = append(byKind, apitypes.CountItem{Key: k, Count: kinds[k]})
	}
	for _, k := range []string{"none", factkind.SourceLLM, factkind.SourceHeuristic, factkind.SourceManual} {
		bySource = append(bySource, apitypes.CountItem{Key: k, Count: srcs[k]})
	}
	return byKind, bySource
}

const sampleContentRunes = 800

// KindSample draws up to n active entries for hand-labelling, spread across
// domain categories and age buckets (round-robin over the strata, a seeded
// shuffle inside each) so a small sample is not dominated by the biggest
// domain or the newest week. Content is the stored (already redacted) text,
// redacted again and cut to a reviewable length.
func (s *MemoryStore) KindSample(n int, seed int64, now time.Time) []factkind.SampleItem {
	if n <= 0 {
		n = 200
	}
	type stratum struct {
		key     string
		entries []schema.Entry
	}
	strata := map[string]*stratum{}
	s.mu.RLock()
	for _, e := range s.index {
		if isSoftDeleted(e) {
			continue
		}
		age := 0
		for i, b := range ageBuckets {
			if now.Sub(e.CreatedAt).Hours()/24 < b.below {
				age = i
				break
			}
		}
		key := e.DomainCategory + "|" + strconv.Itoa(age)
		st := strata[key]
		if st == nil {
			st = &stratum{key: key}
			strata[key] = st
		}
		st.entries = append(st.entries, e)
	}
	s.mu.RUnlock()

	list := make([]*stratum, 0, len(strata))
	for _, st := range strata {
		sort.Slice(st.entries, func(i, j int) bool { return st.entries[i].ID < st.entries[j].ID })
		list = append(list, st)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].key < list[j].key })
	rng := rand.New(rand.NewSource(seed)) //nolint:gosec // reproducible sampling, not security
	for _, st := range list {
		rng.Shuffle(len(st.entries), func(i, j int) { st.entries[i], st.entries[j] = st.entries[j], st.entries[i] })
	}

	var picked []schema.Entry
	for round := 0; len(picked) < n; round++ {
		progressed := false
		for _, st := range list {
			if round < len(st.entries) && len(picked) < n {
				picked = append(picked, st.entries[round])
				progressed = true
			}
		}
		if !progressed {
			break
		}
	}
	rng.Shuffle(len(picked), func(i, j int) { picked[i], picked[j] = picked[j], picked[i] })

	out := make([]factkind.SampleItem, len(picked))
	for i, e := range picked {
		out[i] = factkind.SampleItem{
			ID: e.ID, Domain: e.Domain, CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
			Content: cutRunes(redact.Redact(e.Content).Text, sampleContentRunes),
			Kind:    e.KindValue(), KindSource: e.KindSourceValue(),
		}
	}
	return out
}
