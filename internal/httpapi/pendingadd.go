package httpapi

import (
	"sync"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/idgen"
)

// pendingAddStatus is the status of an add operation tracked by the
// pendingAddTracker (D26, docs/decisions/, asynchronous Add); it mirrors the
// 3 possible values of apitypes.PendingAddResponse.Status.
type pendingAddStatus string

const (
	pendingStatusPending   pendingAddStatus = "pending"
	pendingStatusCompleted pendingAddStatus = "completed"
	pendingStatusFailed    pendingAddStatus = "failed"
)

// pendingAddTTL is how long the result of an already finished operation
// (completed or failed) stays available for polling after it ends; it keeps
// pendingAddTracker.ops from growing without limit in a long-lived process.
// An operation never queried within this window simply disappears from the
// map, with the same practical effect of "never existed" for the poll (404).
// The tracking is ephemeral on purpose (D26): it does not survive a process
// restart, the same thing that already happened with a synchronous Add
// interrupted by a crash in the middle of extraction.
//
// var, not const: TestPendingAddTrackerExpiresAfterTTL (pendingadd_test.go)
// shortens this window so an automated test does not have to wait a real 15
// minutes.
var pendingAddTTL = 15 * time.Minute

// pendingAddOp is one in-flight Add operation (or already finished, within
// the TTL) tracked to support the asynchronous path of POST /memories (D26).
// done closes when the operation leaves "pending"; the original handleAdd
// handler uses it to wait up to addSyncTimeout without active polling (the
// goroutine running store.Add and the HTTP handler live in the same process,
// so a channel is more direct than reimplementing what the external HTTP
// client does via GET /memories/pending/:id).
//
// mu protects status/result/errMsg against concurrent reads: the goroutine
// running store.Add writes exactly once (complete or fail), and both the
// original handleAdd (if still waiting) and handlePendingAdd (a poll from
// another request) may read at the same time.
type pendingAddOp struct {
	id   string
	done chan struct{}

	mu     sync.Mutex
	status pendingAddStatus
	result *apitypes.AddResponse
	errMsg string
}

func newPendingAddOp(id string) *pendingAddOp {
	if id == "" {
		id = idgen.New("op")
	}
	return &pendingAddOp{
		id:     id,
		done:   make(chan struct{}),
		status: pendingStatusPending,
	}
}

// complete marks the operation as successfully completed. It must be called
// at most once per operation (the goroutine that owns the Add is the only
// writer).
func (op *pendingAddOp) complete(result apitypes.AddResponse) {
	op.mu.Lock()
	op.status = pendingStatusCompleted
	op.result = &result
	op.mu.Unlock()
	close(op.done)
}

// fail marks the operation as failed. Same single-call discipline as
// complete.
func (op *pendingAddOp) fail(err error) {
	op.mu.Lock()
	op.status = pendingStatusFailed
	op.errMsg = err.Error()
	op.mu.Unlock()
	close(op.done)
}

// snapshot reads the current state in a concurrency-safe way; used both by
// the original handler (after waiting on done or hitting the timeout) and by
// the GET /memories/pending/:id poll.
func (op *pendingAddOp) snapshot() (status pendingAddStatus, result *apitypes.AddResponse, errMsg string) {
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.status, op.result, op.errMsg
}

// pendingAddTracker keeps the in-flight/recently completed Add operations,
// indexed by OperationID (D26); purely in memory, on purpose: if the process
// restarts with an Add in flight, that specific operation disappears (the
// same practical result as a synchronous Add interrupted by a crash today,
// docs/decisions/ D26).
type pendingAddTracker struct {
	mu  sync.Mutex
	ops map[string]*pendingAddOp
}

func newPendingAddTracker() *pendingAddTracker {
	return &pendingAddTracker{ops: make(map[string]*pendingAddOp)}
}

// start registers a new pending operation and returns it for the handler to follow.
func (t *pendingAddTracker) start() *pendingAddOp {
	return t.startWithID("")
}

// startWithID registers an operation under a given id (the ingest queue row
// id, D40); empty id generates one.
func (t *pendingAddTracker) startWithID(id string) *pendingAddOp {
	op := newPendingAddOp(id)
	t.mu.Lock()
	t.ops[op.id] = op
	t.mu.Unlock()
	return op
}

// finish schedules the removal of the operation after pendingAddTTL; called
// only after complete/fail has already run (the operation has left
// "pending"), to guarantee the poll has the whole window available to read
// the final result before it vanishes from the tracker.
func (t *pendingAddTracker) finish(id string) {
	time.AfterFunc(pendingAddTTL, func() {
		t.mu.Lock()
		delete(t.ops, id)
		t.mu.Unlock()
	})
}

// get returns the operation registered under id, or ok=false if the ID never
// existed or has already expired from the retention TTL.
func (t *pendingAddTracker) get(id string) (*pendingAddOp, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	op, ok := t.ops[id]
	return op, ok
}
