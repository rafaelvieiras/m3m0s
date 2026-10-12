package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/sqlstore"
	"github.com/rafaelvieiras/m3m0s/internal/store"
)

// ingestBackoff is the wait before attempt 2, 3, ...: a failed extraction is
// retried (D40) and only the last attempt stores the piece raw. var so tests
// can shrink it.
var ingestBackoff = []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute}

// ingestPollInterval is how often idle workers look for due rows (retries
// and rows recovered after a restart); new work wakes them immediately.
var ingestPollInterval = time.Second

const (
	defaultIngestWorkers  = 2
	defaultIngestQueueMax = 5000
	ingestDoneTTL         = 24 * time.Hour
	ingestFailedTTL       = 7 * 24 * time.Hour
)

// ingestQueue is the durable queue and bounded worker pool of D40: an
// accepted POST /memories is persisted in SQLite before the server answers,
// and N workers run store.Add for it with retry and backoff. The in-memory
// pendingAddTracker (D26) is kept only to let the original handler wait up to
// addSyncTimeout without polling the database.
type ingestQueue struct {
	db       *sqlstore.DB
	store    *store.MemoryStore
	tracker  *pendingAddTracker
	workers  int
	queueMax int

	wake chan struct{}

	mu       sync.Mutex
	inflight map[string]bool // domains being processed: one worker per domain keeps dedup/contradiction ordering
	started  bool
}

func newIngestQueue(db *sqlstore.DB, st *store.MemoryStore, tr *pendingAddTracker) *ingestQueue {
	return &ingestQueue{
		db: db, store: st, tracker: tr,
		workers:  envInt("M3M0S_INGEST_WORKERS", defaultIngestWorkers),
		queueMax: envInt("M3M0S_INGEST_QUEUE_MAX", defaultIngestQueueMax),
		wake:     make(chan struct{}, 1),
		inflight: map[string]bool{},
	}
}

func envInt(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return v
	}
	return def
}

// start launches the workers once; it also recovers rows a previous process
// left running. Safe to call repeatedly.
func (q *ingestQueue) start() {
	q.mu.Lock()
	if q.started {
		q.mu.Unlock()
		return
	}
	q.started = true
	q.mu.Unlock()

	ctx := context.Background()
	if n, err := q.db.IngestRecoverRunning(ctx, time.Now()); err == nil && n > 0 {
		log.Printf("m3m0s: ingest queue: %d interrupted operation(s) put back in the queue", n)
	}
	for i := 0; i < q.workers; i++ {
		go q.worker()
	}
	go q.janitor()
}

func (q *ingestQueue) notify() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// full reports whether the queue reached its cap.
func (q *ingestQueue) full(ctx context.Context) bool {
	n, err := q.db.IngestOpen(ctx)
	return err == nil && n >= q.queueMax
}

func (q *ingestQueue) enqueue(ctx context.Context, id string, req apitypes.AddRequest) error {
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if err := q.db.IngestEnqueue(ctx, id, req.Domain, string(payload), time.Now()); err != nil {
		return err
	}
	q.start()
	q.notify()
	return nil
}

func (q *ingestQueue) worker() {
	ctx := context.Background()
	tick := time.NewTicker(ingestPollInterval)
	defer tick.Stop()
	for {
		for {
			it, ok := q.claim(ctx)
			if !ok {
				break
			}
			q.process(ctx, it)
			q.release(it.Domain)
		}
		select {
		case <-q.wake:
		case <-tick.C:
		}
	}
}

// claim picks the oldest due row whose domain no other worker is processing.
func (q *ingestQueue) claim(ctx context.Context) (sqlstore.IngestItem, bool) {
	due, err := q.db.IngestDue(ctx, time.Now(), 50)
	if err != nil {
		return sqlstore.IngestItem{}, false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, it := range due {
		if q.inflight[it.Domain] {
			continue
		}
		if err := q.db.IngestMarkRunning(ctx, it.ID, time.Now()); err != nil {
			continue
		}
		q.inflight[it.Domain] = true
		return it, true
	}
	return sqlstore.IngestItem{}, false
}

func (q *ingestQueue) release(domain string) {
	q.mu.Lock()
	delete(q.inflight, domain)
	q.mu.Unlock()
	q.notify() // another row of the same domain may be waiting
}

func (q *ingestQueue) process(ctx context.Context, it sqlstore.IngestItem) {
	var req apitypes.AddRequest
	if err := json.Unmarshal([]byte(it.PayloadJSON), &req); err != nil {
		q.finishFailed(ctx, it, "decode payload: "+err.Error())
		return
	}
	attempt := it.Attempts + 1
	last := attempt > len(ingestBackoff)
	out, err := q.store.Add(ctx, store.AddInput{
		Content: req.Content, Domain: req.Domain, Source: req.Source, Agent: req.Agent,
		Model: req.Model, Project: req.Project, Tags: req.Tags, Metadata: req.Metadata,
		IDPrefix: req.IDPrefix, Capture: req.Capture,
		RequireExtraction: !last,
	}, time.Now())
	switch {
	case errors.Is(err, store.ErrExtractionFailed):
		wait := ingestBackoff[attempt-1]
		log.Printf("m3m0s: ingest queue: extraction failed for %s (domain=%s, attempt %d), retrying in %s", it.ID, it.Domain, attempt, wait)
		_ = q.db.IngestRetry(ctx, it.ID, attempt, time.Now().Add(wait), "extraction failed", time.Now())
	case err != nil:
		q.finishFailed(ctx, it, err.Error())
	default:
		resp := buildAddResponse(out, "completed")
		raw, _ := json.Marshal(resp)
		_ = q.db.IngestFinish(ctx, it.ID, sqlstore.IngestDone, attempt, string(raw), "", time.Now())
		if op, ok := q.tracker.get(it.ID); ok {
			op.complete(resp)
			q.tracker.finish(it.ID)
		}
	}
}

func (q *ingestQueue) finishFailed(ctx context.Context, it sqlstore.IngestItem, msg string) {
	_ = q.db.IngestFinish(ctx, it.ID, sqlstore.IngestFailed, it.Attempts+1, "", msg, time.Now())
	if op, ok := q.tracker.get(it.ID); ok {
		op.fail(errors.New(msg))
		q.tracker.finish(it.ID)
	}
}

func (q *ingestQueue) janitor() {
	for {
		_ = q.db.IngestPurge(context.Background(), time.Now(), ingestDoneTTL, ingestFailedTTL)
		time.Sleep(10 * time.Minute)
	}
}

// pollFromDB answers GET /memories/pending/:id for an operation that is no
// longer in the in-memory tracker (finished earlier, or from before a
// restart).
func (q *ingestQueue) pollFromDB(ctx context.Context, id string) (apitypes.PendingAddResponse, bool) {
	it, err := q.db.IngestGet(ctx, id)
	if err != nil {
		return apitypes.PendingAddResponse{}, false
	}
	resp := apitypes.PendingAddResponse{OperationID: id, Status: string(pendingStatusPending)}
	switch it.Status {
	case sqlstore.IngestDone:
		var r apitypes.AddResponse
		if json.Unmarshal([]byte(it.ResultJSON), &r) == nil {
			resp.Status = string(pendingStatusCompleted)
			resp.Result = &r
		}
	case sqlstore.IngestFailed:
		resp.Status = string(pendingStatusFailed)
		resp.Error = it.Error
	}
	return resp, true
}

func (q *ingestQueue) overview(ctx context.Context) *apitypes.IngestOverview {
	st, err := q.db.IngestStats(ctx)
	if err != nil {
		return nil
	}
	o := &apitypes.IngestOverview{
		Pending: st.Pending, Running: st.Running, Retrying: st.Retrying, Failed: st.Failed,
		Workers: q.workers, QueueMax: q.queueMax,
	}
	if !st.OldestPending.IsZero() {
		o.OldestPending = st.OldestPending.UTC().Format(time.RFC3339)
	}
	return o
}

// StartIngest launches the ingest workers and re-queues operations a previous
// process left running (D40). serve.go calls it at boot so recovered rows are
// processed without waiting for the first request.
func (s *Server) StartIngest() { s.ingest.start() }
