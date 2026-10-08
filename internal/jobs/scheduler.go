// Package jobs implements an in-process routine scheduler, with no
// dependency on the operating system's cron or on an external lib: a single
// goroutine with a short ticker (meant for seconds) decides, at each tick,
// which registered Jobs have reached their own Interval (meant for hours) and
// fires each one in its own goroutine, never overlapping two runs of the same
// Job. See docs/decisions/ for the decision context (jobs system, first
// consumer: retroactive fact extraction in internal/store/retroextract.go)
// and D27 (tracking/history/manual trigger/runtime interval, exposed via
// internal/httpapi/jobs.go and the WebUI /jobs page).
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// ErrJobNotFound is returned by TriggerNow/SetInterval when the name does not
// match any registered job.
var ErrJobNotFound = errors.New("job not found")

// ErrJobAlreadyRunning is returned by TriggerNow when the job already has a
// run in progress; it never queues and never runs two in parallel.
var ErrJobAlreadyRunning = errors.New("job is already running")

// ErrJobNotRunning is returned by Cancel when the job has no run in progress.
var ErrJobNotRunning = errors.New("job is not running")

// historyLimit is how many recent runs each job keeps in memory (most recent
// first): the "job log" exposed by Status(), meant for the WebUI to show
// without growing unbounded.
const historyLimit = 20

// Job is a periodic routine registered in a Scheduler.
type Job struct {
	// Name identifies the job in logs and in the 3 new control surfaces
	// (GET /api/v1/jobs, POST .../run, PATCH .../<name>, D27); it must be
	// unique among the jobs registered in the same Scheduler.
	Name string
	// Interval is the gap between the END of one run and the START of the
	// next. It is deliberately not a fixed cron-style clock: a job that takes
	// longer than Interval never fires a second run on top of the first. It
	// can be changed at runtime via SetInterval. Zero or negative means
	// manual: the job only runs through TriggerNow (e.g. the re-embedding
	// job, which costs money and is started by an explicit confirmation).
	Interval time.Duration
	// InitialDelay delays the first run after Register; 0 makes the job
	// eligible already at the Scheduler's first tick after Start.
	InitialDelay time.Duration
	// Progress, when set, reports the live progress of a long run (shown by
	// GET /api/v1/jobs); nil for jobs that finish in moments. Called with the
	// scheduler lock held: it must be cheap, safe for concurrent use and must
	// not call back into the Scheduler.
	Progress func() *Progress
	// Run executes the job and returns a short, human-readable summary
	// (e.g. "15 memory(ies) processed"; "" when there is nothing relevant to
	// say about this run) and an optional error. A returned error is only
	// logged and recorded in the history, and never brings down the
	// Scheduler; a panic inside Run is also recovered and only
	// logged/recorded (see Scheduler.run). A buggy job must not take the
	// server down.
	Run func(ctx context.Context) (summary string, err error)
}

// Progress is the live state of a long-running job: Done and Errors out of
// Total units, and the estimated time left (0 = unknown).
type Progress struct {
	Done   int
	Total  int
	Errors int
	ETA    time.Duration
}

// JobRun is one run recorded in a job's history (D27).
type JobRun struct {
	StartedAt time.Time
	Duration  time.Duration
	Success   bool
	Summary   string
	Err       string
}

// JobStatus is the snapshot of one registered job, returned by
// Scheduler.Status; the basis for GET /api/v1/jobs (D27,
// internal/httpapi/jobs.go translates it into the wire shape in
// internal/apitypes).
type JobStatus struct {
	Name     string
	Interval time.Duration
	Running  bool
	NextRun  time.Time
	// Progress is nil for jobs without progress reporting.
	Progress *Progress
	// LastRun is nil when the job has never run in this process instance
	// (equivalent to History[0] when non-nil; kept separate only for the
	// caller's convenience).
	LastRun *JobRun
	// History comes most recent first, capped at historyLimit.
	History []JobRun
}

// Scheduler fires registered Jobs when each one's individual Interval
// elapses, checking at every tick (short, seconds, even with long-interval
// Jobs: the tick is the clock's heartbeat, not the frequency of each job).
// The zero value is not usable; use NewScheduler.
type Scheduler struct {
	tick time.Duration

	mu   sync.Mutex
	jobs []*scheduledJob
}

type scheduledJob struct {
	Job
	nextRun time.Time
	running atomic.Bool
	// cancel stops the run in progress (nil when idle); protected by Scheduler.mu.
	cancel context.CancelFunc
	// history/nextRun are protected by Scheduler.mu (the same concurrency
	// discipline as before D27: a single mutex covers the job list and each
	// job's mutable state, with low contention given that jobs run on a
	// minutes/hours scale). running uses a separate atomic.Bool because it
	// must be read/locked (CompareAndSwap) without depending on the mutex,
	// both in runDue and in TriggerNow.
	history []JobRun
}

// NewScheduler creates a Scheduler that checks for due jobs at every tick.
func NewScheduler(tick time.Duration) *Scheduler {
	return &Scheduler{tick: tick}
}

// Register adds j to the Scheduler's job list. Call it before Start; calls
// afterwards are also safe (protected by the same mutex as runDue), but the
// expected use is to register everything before bringing the server up.
func (s *Scheduler) Register(j Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, &scheduledJob{
		Job:     j,
		nextRun: time.Now().Add(j.InitialDelay),
	})
}

// Start blocks checking for due jobs at every tick, until ctx is cancelled.
// It is typically called in its own goroutine (`go sched.Start(ctx)`), since
// the m3m0s process currently has no graceful shutdown (http.ListenAndServe
// blocks forever).
func (s *Scheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runDue(ctx)
		}
	}
}

// runDue fires, in its own goroutine, every job whose nextRun has passed and
// that does not still have a previous run in progress.
func (s *Scheduler) runDue(ctx context.Context) {
	now := time.Now()
	s.mu.Lock()
	due := make([]*scheduledJob, 0, len(s.jobs))
	for _, j := range s.jobs {
		if j.Interval <= 0 || now.Before(j.nextRun) {
			continue
		}
		// CompareAndSwap (not Load followed by Store) closes the race window
		// with a concurrent TriggerNow that may be deciding the same thing at
		// the same time; only one of the two wins the "running" mark.
		if !j.running.CompareAndSwap(false, true) {
			continue
		}
		due = append(due, j)
	}
	s.mu.Unlock()

	for _, j := range due {
		go s.run(ctx, j)
	}
}

// TriggerNow fires the named job outside the normal nextRun cycle (D27, "run
// now" in the WebUI), with the same overlap protection runDue already had:
// it returns ErrJobAlreadyRunning instead of queueing or running in parallel.
// It uses context.Background() (not the ctx passed to Start) because a
// manually triggered run should not be cancelled by a cancellation of the
// scheduling loop itself; the same "background" ctx that Start typically
// receives in cmd/m3m0s/serve.go.
func (s *Scheduler) TriggerNow(name string) error {
	target := s.findJob(name)
	if target == nil {
		return fmt.Errorf("%w: %s", ErrJobNotFound, name)
	}
	if !target.running.CompareAndSwap(false, true) {
		return fmt.Errorf("%w: %s", ErrJobAlreadyRunning, name)
	}
	go s.run(context.Background(), target)
	return nil
}

// SetInterval changes the Interval of an already registered job at runtime,
// recalculating the next schedule from now; the effect is noticeable without
// a manual "run now" or restarting the process (D27). internal/httpapi
// persists the change (internal/jobs/persist.go) after a successful call.
func (s *Scheduler) SetInterval(name string, interval time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.Name == name {
			j.Interval = interval
			j.nextRun = time.Now().Add(interval)
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrJobNotFound, name)
}

// Status returns a snapshot of all registered jobs (D27); the basis for
// GET /api/v1/jobs.
func (s *Scheduler) Status() []JobStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]JobStatus, 0, len(s.jobs))
	for _, j := range s.jobs {
		history := make([]JobRun, len(j.history))
		copy(history, j.history)
		var lastRun *JobRun
		if len(history) > 0 {
			lr := history[0]
			lastRun = &lr
		}
		var progress *Progress
		if j.Progress != nil {
			progress = j.Progress()
		}
		out = append(out, JobStatus{
			Progress: progress,
			Name:     j.Name,
			Interval: j.Interval,
			Running:  j.running.Load(),
			NextRun:  j.nextRun,
			LastRun:  lastRun,
			History:  history,
		})
	}
	return out
}

// Cancel stops the run in progress of the named job by cancelling its context.
// The job decides how fast it reacts; a cancelled run is recorded in the
// history like any other, with the error its Run returned.
func (s *Scheduler) Cancel(name string) error {
	target := s.findJob(name)
	if target == nil {
		return fmt.Errorf("%w: %s", ErrJobNotFound, name)
	}
	s.mu.Lock()
	cancel := target.cancel
	s.mu.Unlock()
	if cancel == nil {
		return fmt.Errorf("%w: %s", ErrJobNotRunning, name)
	}
	cancel()
	return nil
}

func (s *Scheduler) findJob(name string) *scheduledJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.Name == name {
			return j
		}
	}
	return nil
}

func (s *Scheduler) run(ctx context.Context, j *scheduledJob) {
	start := time.Now()
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	j.cancel = cancel
	s.mu.Unlock()
	var summary string
	var runErr error
	defer func() {
		duration := time.Since(start)
		if r := recover(); r != nil {
			runErr = fmt.Errorf("panic: %v", r)
			log.Printf("m3m0s: job %q panicked: %v", j.Name, r)
		}

		entry := JobRun{
			StartedAt: start,
			Duration:  duration,
			Success:   runErr == nil,
			Summary:   summary,
		}
		if runErr != nil {
			entry.Err = runErr.Error()
		}

		cancel()
		s.mu.Lock()
		j.cancel = nil
		j.nextRun = time.Now().Add(j.Interval)
		j.history = append([]JobRun{entry}, j.history...)
		if len(j.history) > historyLimit {
			j.history = j.history[:historyLimit]
		}
		s.mu.Unlock()

		j.running.Store(false)
	}()

	summary, runErr = j.Run(ctx)
	if runErr != nil {
		log.Printf("m3m0s: job %q failed: %v", j.Name, runErr)
		return
	}
	log.Printf("m3m0s: job %q completed in %s", j.Name, time.Since(start).Round(time.Millisecond))
}
