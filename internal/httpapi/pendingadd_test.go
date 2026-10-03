package httpapi

import (
	"errors"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
)

// TestPendingAddTrackerCompleteFlow checks the happy path: start() returns a
// pending operation, get() by ID finds it, complete() changes the status and
// makes the result available, and the ID remains queryable (it does not
// expire before the TTL, tested separately in
// TestPendingAddTrackerExpiresAfterTTL).
func TestPendingAddTrackerCompleteFlow(t *testing.T) {
	tracker := newPendingAddTracker()
	op := tracker.start()
	if op.id == "" {
		t.Fatal("start() returned an operation without an id")
	}

	got, ok := tracker.get(op.id)
	if !ok {
		t.Fatalf("get(%q) did not find the newly created operation", op.id)
	}
	if got != op {
		t.Error("get() returned a *pendingAddOp different from the one created by start()")
	}

	status, result, errMsg := op.snapshot()
	if status != pendingStatusPending {
		t.Errorf("initial status = %q, want %q", status, pendingStatusPending)
	}
	if result != nil || errMsg != "" {
		t.Errorf("result/errMsg should be zeroed before complete/fail: result=%v errMsg=%q", result, errMsg)
	}

	want := apitypes.AddResponse{Extracted: true, Status: "completed"}
	op.complete(want)

	select {
	case <-op.done:
	default:
		t.Fatal("done did not close after complete()")
	}

	status, result, errMsg = op.snapshot()
	if status != pendingStatusCompleted {
		t.Errorf("status after complete = %q, want %q", status, pendingStatusCompleted)
	}
	if result == nil || result.Extracted != true || result.Status != "completed" {
		t.Errorf("result after complete = %+v, want %+v", result, want)
	}
	if errMsg != "" {
		t.Errorf("errMsg after complete = %q, want empty", errMsg)
	}
}

// TestPendingAddTrackerFailFlow checks the error path: fail() marks the
// operation as failed and makes the error message available (with no
// result).
func TestPendingAddTrackerFailFlow(t *testing.T) {
	tracker := newPendingAddTracker()
	op := tracker.start()

	op.fail(errors.New("openrouter unavailable"))

	select {
	case <-op.done:
	default:
		t.Fatal("done did not close after fail()")
	}

	status, result, errMsg := op.snapshot()
	if status != pendingStatusFailed {
		t.Errorf("status after fail = %q, want %q", status, pendingStatusFailed)
	}
	if result != nil {
		t.Errorf("result after fail = %+v, want nil", result)
	}
	if errMsg != "openrouter unavailable" {
		t.Errorf("errMsg after fail = %q, want %q", errMsg, "openrouter unavailable")
	}
}

// TestPendingAddTrackerGetUnknownID checks that a never-registered ID returns
// ok=false; the same behavior expected from handlePendingAdd returning 404
// for this case.
func TestPendingAddTrackerGetUnknownID(t *testing.T) {
	tracker := newPendingAddTracker()
	if _, ok := tracker.get("op_never_existed"); ok {
		t.Error("get() of an unknown id should return ok=false")
	}
}

// TestPendingAddTrackerExpiresAfterTTL checks that finish() removes the
// operation from the tracker after pendingAddTTL; it uses an artificially
// short window by reducing pendingAddTTL only for this test (the package
// variable exists precisely to allow this without changing production
// behavior outside the test).
func TestPendingAddTrackerExpiresAfterTTL(t *testing.T) {
	orig := pendingAddTTL
	pendingAddTTL = 20 * time.Millisecond
	t.Cleanup(func() { pendingAddTTL = orig })

	tracker := newPendingAddTracker()
	op := tracker.start()
	op.complete(apitypes.AddResponse{Status: "completed"})
	tracker.finish(op.id)

	if _, ok := tracker.get(op.id); !ok {
		t.Fatal("get() should find the operation right after finish(), before the TTL expires")
	}

	deadline := time.Now().Add(1 * time.Second)
	for {
		if _, ok := tracker.get(op.id); !ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("operation did not expire from the tracker after the configured TTL")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
