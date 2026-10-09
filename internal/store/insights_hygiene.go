package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
)

// Memory-hygiene analysis job (D37). It is read-only over the memories: it
// looks for identical text, near-identical vectors and low-value patterns, and
// writes one small JSON report into insight_reports. It is registered as a
// manual job (it can also be given an interval), and it is deliberately gentle:
// a duty cycle, a wall-clock limit, and chunked reads that never hold a store
// lock for long.

// InsightsJobName is the name of the analysis job in the scheduler (and in
// GET /api/v1/jobs).
const InsightsJobName = "insights-hygiene"

const insightsGCPercent = 20

const (
	hygieneReportName    = "hygiene"
	hygieneReportVersion = 1
)

// ErrInsightsRunning is returned when an analysis is already in progress.
var ErrInsightsRunning = errors.New("the hygiene analysis is already running")

// InsightsOptions tunes the job. The zero value of a field means its default.
type InsightsOptions struct {
	CPUPercent  int           // duty cycle while running, 1-100 (default 60)
	MaxDuration time.Duration // wall-clock limit; a longer run ends with a partial report (default 30m)
	near        nearParams    // tests only
}

func (o InsightsOptions) withDefaults() InsightsOptions {
	if o.CPUPercent <= 0 || o.CPUPercent > 100 {
		o.CPUPercent = 60
	}
	if o.MaxDuration <= 0 {
		o.MaxDuration = 30 * time.Minute
	}
	return o
}

// InsightsOptionsFromEnv reads M3M0S_INSIGHTS_CPU_PERCENT (duty cycle) and
// M3M0S_INSIGHTS_MAX_MINUTES (wall-clock limit).
func InsightsOptionsFromEnv() InsightsOptions {
	var o InsightsOptions
	if n, err := strconv.Atoi(os.Getenv("M3M0S_INSIGHTS_CPU_PERCENT")); err == nil && n > 0 && n <= 100 {
		o.CPUPercent = n
	}
	if n, err := strconv.Atoi(os.Getenv("M3M0S_INSIGHTS_MAX_MINUTES")); err == nil && n > 0 {
		o.MaxDuration = time.Duration(n) * time.Minute
	}
	return o
}

// SetInsightsOptions overrides the job tuning.
func (s *MemoryStore) SetInsightsOptions(o InsightsOptions) {
	s.ins.mu.Lock()
	s.ins.opts = o
	s.ins.mu.Unlock()
}

type insightsState struct {
	mu     sync.Mutex
	opts   InsightsOptions
	status apitypes.HygieneStatus
}

func (s *MemoryStore) insightsUpdate(f func(st *apitypes.HygieneStatus)) {
	s.ins.mu.Lock()
	f(&s.ins.status)
	s.ins.mu.Unlock()
}

// HygieneStatus returns the live state of the analysis job.
func (s *MemoryStore) HygieneStatus() apitypes.HygieneStatus {
	s.ins.mu.Lock()
	defer s.ins.mu.Unlock()
	return s.ins.status
}

// HygieneReport returns the latest stored report, or nil when the analysis
// never ran.
func (s *MemoryStore) HygieneReport(ctx context.Context) (*apitypes.HygieneReport, error) {
	raw, ok, err := s.db.LoadInsightReport(ctx, hygieneReportName)
	if err != nil || !ok {
		return nil, err
	}
	var rep apitypes.HygieneReport
	if err := json.Unmarshal(raw, &rep); err != nil {
		return nil, fmt.Errorf("decode hygiene report: %w", err)
	}
	return &rep, nil
}

// RunHygieneAnalysis executes the analysis and stores its report. It returns a
// short summary for the job history. Cancelling ctx aborts without replacing
// the previous report; reaching the wall-clock limit stores a partial report.
func (s *MemoryStore) RunHygieneAnalysis(ctx context.Context) (string, error) {
	s.ins.mu.Lock()
	if s.ins.status.Running {
		s.ins.mu.Unlock()
		return "", ErrInsightsRunning
	}
	opts := s.ins.opts.withDefaults()
	started := time.Now()
	s.ins.status = apitypes.HygieneStatus{Running: true, Phase: "scanning text", StartedAt: &started}
	s.ins.mu.Unlock()

	rep, err := s.analyze(ctx, opts, started)
	// Hand the job's transient memory back instead of leaving it to the scavenger.
	defer debug.FreeOSMemory()

	finished := time.Now()
	s.insightsUpdate(func(st *apitypes.HygieneStatus) {
		st.Running = false
		st.FinishedAt = &finished
		if err != nil {
			st.LastError = err.Error()
		} else {
			st.Phase, st.Done, st.Total = "", 0, 0
		}
	})
	if err != nil {
		return "", err
	}

	raw, mErr := json.Marshal(rep)
	if mErr != nil {
		return "", mErr
	}
	// Persist even if ctx was cancelled just now: the work is done.
	if err := s.db.SaveInsightReport(context.WithoutCancel(ctx), hygieneReportName, rep.ComputedAt, raw); err != nil {
		s.insightsUpdate(func(st *apitypes.HygieneStatus) { st.LastError = err.Error() })
		return "", err
	}
	summary := fmt.Sprintf("%d scanned, %d identical-text duplicates, %d conservative noise candidates",
		rep.Scanned, rep.Exact.Redundant, rep.Noise.ConservativeUnion)
	if rep.Near.Available && len(rep.Near.Thresholds) > 1 {
		summary += fmt.Sprintf(", %d near-duplicate pairs >= %.2f", rep.Near.Thresholds[1].Pairs, rep.Near.Thresholds[1].Threshold)
	}
	if rep.Partial {
		summary += " (partial: " + rep.PartialReason + ")"
	}
	return summary, nil
}

func (s *MemoryStore) analyze(parent context.Context, opts InsightsOptions, started time.Time) (apitypes.HygieneReport, error) {
	ctx, cancel := context.WithTimeout(parent, opts.MaxDuration)
	defer cancel()
	// The heap goal is a multiple of the (large, long-lived) index, so with the
	// server's default setting even small job allocations would be allowed to
	// pile up for tens of MB before a collection. Tighten it while the job
	// runs; the cost is a few more GC cycles, paid at the job's low duty cycle.
	defer debug.SetGCPercent(debug.SetGCPercent(insightsGCPercent))
	pc := newPacer(opts.CPUPercent)

	rep := apitypes.HygieneReport{Version: hygieneReportVersion}
	progress := func(done, total int, phase string) {
		s.insightsUpdate(func(st *apitypes.HygieneStatus) { st.Phase, st.Done, st.Total = phase, done, total })
	}
	ts, err := s.scanText(ctx, pc, progress)
	switch {
	case err == nil:
	case parent.Err() == nil && ctx.Err() == context.DeadlineExceeded:
		rep.Partial, rep.PartialReason = true, "time limit reached while scanning text"
	default:
		return rep, err
	}
	rep.Scanned, rep.Exact, rep.Noise = ts.active, ts.exact, ts.noise

	switch {
	case !s.HasEmbeddings() && s.vecs.len() == 0:
		rep.Near = apitypes.NearDupReport{Available: false, Reason: "embeddings are disabled and no vectors are indexed",
			Thresholds: []apitypes.NearDupThreshold{}, CrossSource: []apitypes.CountItem{}, Bands: []apitypes.NearDupBand{}}
	case rep.Partial:
		rep.Near = apitypes.NearDupReport{Available: false, Reason: "skipped: time limit reached",
			Thresholds: []apitypes.NearDupThreshold{}, CrossSource: []apitypes.CountItem{}, Bands: []apitypes.NearDupBand{}}
	default:
		s.insightsUpdate(func(st *apitypes.HygieneStatus) { st.Phase = "near-duplicates" })
		res, err := s.findNearPairs(ctx, opts.near, pc, progress)
		if err != nil {
			return rep, err
		}
		if res.partial {
			rep.Partial, rep.PartialReason = true, "time limit reached while comparing vectors; near-duplicate counts are a lower bound"
		}
		rep.Near = s.summarizeNear(res)
	}

	rep.ComputedAt = time.Now().UTC()
	rep.DurationMS = time.Since(started).Milliseconds()
	return rep, nil
}
