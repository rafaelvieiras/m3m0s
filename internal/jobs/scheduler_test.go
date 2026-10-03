package jobs

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// waitFor polls a condition until timeout, failing the test if it is never
// satisfied; it avoids arbitrary fixed sleeps in a concurrent scheduler test.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("condition not satisfied after %s", timeout)
	}
}

func TestSchedulerRunsJobWithoutInitialDelay(t *testing.T) {
	var calls atomic.Int32
	sched := NewScheduler(5 * time.Millisecond)
	sched.Register(Job{
		Name:     "counter",
		Interval: time.Hour,
		Run: func(ctx context.Context) (string, error) {
			calls.Add(1)
			return "", nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Start(ctx)

	waitFor(t, time.Second, func() bool { return calls.Load() >= 1 })
}

func TestSchedulerRespectsInitialDelay(t *testing.T) {
	var calls atomic.Int32
	sched := NewScheduler(5 * time.Millisecond)
	sched.Register(Job{
		Name:         "delayed",
		Interval:     time.Hour,
		InitialDelay: 100 * time.Millisecond,
		Run: func(ctx context.Context) (string, error) {
			calls.Add(1)
			return "", nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Start(ctx)

	time.Sleep(40 * time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Fatalf("job ran before InitialDelay elapsed: %d calls", got)
	}

	waitFor(t, time.Second, func() bool { return calls.Load() >= 1 })
}

func TestSchedulerDoesNotRerunBeforeInterval(t *testing.T) {
	var calls atomic.Int32
	sched := NewScheduler(5 * time.Millisecond)
	sched.Register(Job{
		Name:     "rare",
		Interval: time.Hour,
		Run: func(ctx context.Context) (string, error) {
			calls.Add(1)
			return "", nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Start(ctx)

	waitFor(t, time.Second, func() bool { return calls.Load() >= 1 })
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("job with a 1h Interval ran %d times in <1s of test", got)
	}
}

func TestSchedulerSkipsOverlappingRuns(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	sched := NewScheduler(5 * time.Millisecond)
	sched.Register(Job{
		Name:     "slow",
		Interval: time.Millisecond, // due again almost immediately
		Run: func(ctx context.Context) (string, error) {
			calls.Add(1)
			<-release // holds the run until the test releases it
			return "", nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Start(ctx)

	waitFor(t, time.Second, func() bool { return calls.Load() >= 1 })
	// Several ticks go by with the run still stuck on <-release; none
	// should fire a second concurrent run.
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 call in progress (no overlap), got %d", got)
	}

	close(release)
	waitFor(t, time.Second, func() bool { return calls.Load() >= 2 })
}

func TestSchedulerRecoversFromPanic(t *testing.T) {
	var calls atomic.Int32
	sched := NewScheduler(5 * time.Millisecond)
	sched.Register(Job{
		Name:     "exploding",
		Interval: 20 * time.Millisecond,
		Run: func(ctx context.Context) (string, error) {
			calls.Add(1)
			panic("boom")
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Start(ctx)

	// Survives at least 2 runs (the 2nd only happens if the Scheduler kept
	// running after the 1st's panic, and did not get stuck with running=true
	// forever).
	waitFor(t, time.Second, func() bool { return calls.Load() >= 2 })

	// The panic must also be recorded in the history as a failed run (D27),
	// not just logged and forgotten.
	statuses := sched.Status()
	if len(statuses) != 1 {
		t.Fatalf("Status() = %d jobs, want 1", len(statuses))
	}
	if len(statuses[0].History) == 0 {
		t.Fatalf("empty history after runs with panic")
	}
	if statuses[0].History[0].Success {
		t.Fatalf("run with panic marked as Success=true")
	}
}

func TestSchedulerLogsErrorWithoutStopping(t *testing.T) {
	var calls atomic.Int32
	sched := NewScheduler(5 * time.Millisecond)
	sched.Register(Job{
		Name:     "with-error",
		Interval: 20 * time.Millisecond,
		Run: func(ctx context.Context) (string, error) {
			calls.Add(1)
			return "", errors.New("failed on purpose")
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Start(ctx)

	waitFor(t, time.Second, func() bool { return calls.Load() >= 2 })
}

func TestSchedulerStopsOnContextCancel(t *testing.T) {
	var calls atomic.Int32
	sched := NewScheduler(5 * time.Millisecond)
	sched.Register(Job{
		Name:     "counter2",
		Interval: time.Millisecond,
		Run: func(ctx context.Context) (string, error) {
			calls.Add(1)
			return "", nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	go sched.Start(ctx)

	waitFor(t, time.Second, func() bool { return calls.Load() >= 1 })
	cancel()

	stopped := calls.Load()
	time.Sleep(50 * time.Millisecond)
	// After the cancel, new runs should not start; the count may still
	// increment by 1 if one was already in flight at the instant of the
	// cancel, so tolerate at most +1.
	if got := calls.Load(); got > stopped+1 {
		t.Fatalf("scheduler kept firing jobs after ctx.cancel: %d -> %d", stopped, got)
	}
}

// ── D27: Status/history/TriggerNow/SetInterval ──

func TestSchedulerStatusReflectsExecution(t *testing.T) {
	sched := NewScheduler(5 * time.Millisecond)
	sched.Register(Job{
		Name:     "with-summary",
		Interval: time.Hour,
		Run: func(ctx context.Context) (string, error) {
			return "3 processed", nil
		},
	})

	before := sched.Status()
	if len(before) != 1 {
		t.Fatalf("Status() = %d jobs, want 1", len(before))
	}
	if before[0].LastRun != nil {
		t.Fatalf("LastRun should not exist before any run")
	}
	if len(before[0].History) != 0 {
		t.Fatalf("History should be empty before any run")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Start(ctx)

	waitFor(t, time.Second, func() bool {
		st := sched.Status()
		return len(st) == 1 && st[0].LastRun != nil
	})

	after := sched.Status()[0]
	if after.Running {
		t.Fatalf("Running = true after the run finished")
	}
	if !after.LastRun.Success {
		t.Fatalf("LastRun.Success = false, want true")
	}
	if after.LastRun.Summary != "3 processed" {
		t.Fatalf("LastRun.Summary = %q, want %q", after.LastRun.Summary, "3 processed")
	}
	if len(after.History) != 1 {
		t.Fatalf("History = %d entries, want 1", len(after.History))
	}
	if after.NextRun.Before(time.Now()) {
		t.Fatalf("NextRun in the past after scheduling the next run (Interval=1h)")
	}
}

func TestSchedulerStatusCapsHistory(t *testing.T) {
	var calls atomic.Int32
	sched := NewScheduler(2 * time.Millisecond)
	sched.Register(Job{
		Name:     "noisy",
		Interval: time.Millisecond,
		Run: func(ctx context.Context) (string, error) {
			calls.Add(1)
			return "", nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Start(ctx)

	waitFor(t, 2*time.Second, func() bool { return calls.Load() > int32(historyLimit)+5 })

	st := sched.Status()
	if len(st[0].History) > historyLimit {
		t.Fatalf("History = %d entries, want <= %d", len(st[0].History), historyLimit)
	}
}

func TestSchedulerTriggerNowRunsOutOfCycle(t *testing.T) {
	var calls atomic.Int32
	sched := NewScheduler(time.Hour) // huge tick; only TriggerNow should fire
	sched.Register(Job{
		Name:     "manual",
		Interval: time.Hour,
		Run: func(ctx context.Context) (string, error) {
			calls.Add(1)
			return "ran manually", nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Start(ctx)

	if err := sched.TriggerNow("manual"); err != nil {
		t.Fatalf("TriggerNow: %v", err)
	}
	waitFor(t, time.Second, func() bool { return calls.Load() == 1 })

	st := sched.Status()[0]
	if st.LastRun == nil || st.LastRun.Summary != "ran manually" {
		t.Fatalf("LastRun did not reflect the run fired by TriggerNow: %+v", st.LastRun)
	}
}

func TestSchedulerTriggerNowUnknownJob(t *testing.T) {
	sched := NewScheduler(time.Hour)
	err := sched.TriggerNow("does-not-exist")
	if !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("TriggerNow(unknown name) = %v, want ErrJobNotFound", err)
	}
}

func TestSchedulerTriggerNowRejectsOverlap(t *testing.T) {
	release := make(chan struct{})
	sched := NewScheduler(time.Hour)
	sched.Register(Job{
		Name:     "busy",
		Interval: time.Hour,
		Run: func(ctx context.Context) (string, error) {
			<-release
			return "", nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Start(ctx)

	if err := sched.TriggerNow("busy"); err != nil {
		t.Fatalf("1st TriggerNow: %v", err)
	}
	waitFor(t, time.Second, func() bool { return sched.Status()[0].Running })

	err := sched.TriggerNow("busy")
	if !errors.Is(err, ErrJobAlreadyRunning) {
		t.Fatalf("2nd TriggerNow (job already running) = %v, want ErrJobAlreadyRunning", err)
	}

	close(release)
	waitFor(t, time.Second, func() bool { return !sched.Status()[0].Running })
}

func TestSchedulerSetIntervalChangesSchedule(t *testing.T) {
	sched := NewScheduler(time.Hour)
	sched.Register(Job{
		Name:         "adjustable",
		Interval:     time.Hour,
		InitialDelay: time.Hour, // initial nextRun is far away, to compare with the recalculation below
		Run: func(ctx context.Context) (string, error) {
			return "", nil
		},
	})

	before := sched.Status()[0].NextRun

	if err := sched.SetInterval("adjustable", time.Second); err != nil {
		t.Fatalf("SetInterval: %v", err)
	}

	after := sched.Status()[0]
	if after.Interval != time.Second {
		t.Fatalf("Interval = %s, want 1s", after.Interval)
	}
	// SetInterval recalculates nextRun from NOW (not from the last run), so
	// the new schedule lands well before the original (registered ~1h in the
	// future via InitialDelay).
	if !after.NextRun.Before(before) {
		t.Fatalf("NextRun did not move back after shortening the Interval: before=%s after=%s", before, after.NextRun)
	}
}

func TestSchedulerSetIntervalUnknownJob(t *testing.T) {
	sched := NewScheduler(time.Hour)
	err := sched.SetInterval("does-not-exist", time.Minute)
	if !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("SetInterval(unknown name) = %v, want ErrJobNotFound", err)
	}
}

func TestSchedulerSetIntervalTakesEffectWithoutRestart(t *testing.T) {
	var calls atomic.Int32
	sched := NewScheduler(5 * time.Millisecond)
	sched.Register(Job{
		Name:     "rescheduled",
		Interval: time.Hour, // would never run again within the test without the SetInterval below
		Run: func(ctx context.Context) (string, error) {
			calls.Add(1)
			return "", nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Start(ctx)

	waitFor(t, time.Second, func() bool { return calls.Load() >= 1 })

	if err := sched.SetInterval("rescheduled", 5*time.Millisecond); err != nil {
		t.Fatalf("SetInterval: %v", err)
	}

	waitFor(t, time.Second, func() bool { return calls.Load() >= 2 })
}
