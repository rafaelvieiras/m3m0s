package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Ingest queue statuses (D40).
const (
	IngestPending = "pending"
	IngestRunning = "running"
	IngestDone    = "done"
	IngestFailed  = "failed"
)

// ErrIngestNotFound is returned by IngestGet for an unknown id.
var ErrIngestNotFound = errors.New("ingest operation not found")

// IngestItem is one row of the durable ingest queue (D40).
type IngestItem struct {
	ID            string
	Domain        string
	PayloadJSON   string
	Status        string
	Attempts      int
	NextAttemptAt time.Time
	Error         string
	ResultJSON    string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// IngestStats is the queue summary shown in Insights.
type IngestStats struct {
	Pending, Running, Done, Failed int
	OldestPending                  time.Time // zero when none
	Retrying                       int       // pending rows that already failed once
}

const ingestTime = time.RFC3339Nano

func (db *DB) IngestEnqueue(ctx context.Context, id, domain, payloadJSON string, now time.Time) error {
	ts := now.UTC().Format(ingestTime)
	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO ingest_queue (id, domain, payload_json, status, attempts, next_attempt_at, created_at, updated_at)
		VALUES (?, ?, ?, 'pending', 0, ?, ?, ?)`, id, domain, payloadJSON, ts, ts, ts)
	if err != nil {
		return fmt.Errorf("enqueue ingest: %w", err)
	}
	return nil
}

// IngestRecoverRunning puts rows left "running" by a previous process back to
// pending (a restart interrupted them). Returns how many were recovered.
func (db *DB) IngestRecoverRunning(ctx context.Context, now time.Time) (int, error) {
	res, err := db.sql.ExecContext(ctx, `
		UPDATE ingest_queue SET status='pending', updated_at=? WHERE status='running'`,
		now.UTC().Format(ingestTime))
	if err != nil {
		return 0, fmt.Errorf("recover ingest queue: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// IngestDue lists up to limit pending rows whose next attempt is due, oldest
// first. The caller picks one whose domain is not in flight and calls
// IngestMarkRunning.
func (db *DB) IngestDue(ctx context.Context, now time.Time, limit int) ([]IngestItem, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT id, domain, payload_json, status, attempts, next_attempt_at, error, result_json, created_at, updated_at
		FROM ingest_queue WHERE status='pending' AND next_attempt_at <= ?
		ORDER BY created_at LIMIT ?`, now.UTC().Format(ingestTime), limit)
	if err != nil {
		return nil, fmt.Errorf("list due ingest: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanIngest(rows)
}

func (db *DB) IngestMarkRunning(ctx context.Context, id string, now time.Time) error {
	_, err := db.sql.ExecContext(ctx, `
		UPDATE ingest_queue SET status='running', updated_at=? WHERE id=? AND status='pending'`,
		now.UTC().Format(ingestTime), id)
	return err
}

// IngestRetry puts a row back to pending for a later attempt.
func (db *DB) IngestRetry(ctx context.Context, id string, attempts int, next time.Time, errMsg string, now time.Time) error {
	_, err := db.sql.ExecContext(ctx, `
		UPDATE ingest_queue SET status='pending', attempts=?, next_attempt_at=?, error=?, updated_at=? WHERE id=?`,
		attempts, next.UTC().Format(ingestTime), errMsg, now.UTC().Format(ingestTime), id)
	return err
}

// IngestFinish stores the terminal state of a row: done with its result JSON,
// or failed with an error message.
func (db *DB) IngestFinish(ctx context.Context, id, status string, attempts int, resultJSON, errMsg string, now time.Time) error {
	_, err := db.sql.ExecContext(ctx, `
		UPDATE ingest_queue SET status=?, attempts=?, result_json=?, error=?, updated_at=? WHERE id=?`,
		status, attempts, resultJSON, errMsg, now.UTC().Format(ingestTime), id)
	return err
}

func (db *DB) IngestGet(ctx context.Context, id string) (IngestItem, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT id, domain, payload_json, status, attempts, next_attempt_at, error, result_json, created_at, updated_at
		FROM ingest_queue WHERE id=?`, id)
	if err != nil {
		return IngestItem{}, err
	}
	defer func() { _ = rows.Close() }()
	items, err := scanIngest(rows)
	if err != nil {
		return IngestItem{}, err
	}
	if len(items) == 0 {
		return IngestItem{}, ErrIngestNotFound
	}
	return items[0], nil
}

// IngestOpen counts rows that are pending or running (the queue depth).
func (db *DB) IngestOpen(ctx context.Context) (int, error) {
	var n int
	err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM ingest_queue WHERE status IN ('pending','running')`).Scan(&n)
	return n, err
}

// IngestPurge deletes finished rows: done older than doneTTL, failed older
// than failedTTL.
func (db *DB) IngestPurge(ctx context.Context, now time.Time, doneTTL, failedTTL time.Duration) error {
	_, err := db.sql.ExecContext(ctx, `
		DELETE FROM ingest_queue
		WHERE (status='done' AND updated_at < ?) OR (status='failed' AND updated_at < ?)`,
		now.Add(-doneTTL).UTC().Format(ingestTime), now.Add(-failedTTL).UTC().Format(ingestTime))
	return err
}

func (db *DB) IngestStats(ctx context.Context) (IngestStats, error) {
	var st IngestStats
	rows, err := db.sql.QueryContext(ctx, `SELECT status, COUNT(*) FROM ingest_queue GROUP BY status`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			_ = rows.Close()
			return st, err
		}
		switch s {
		case IngestPending:
			st.Pending = n
		case IngestRunning:
			st.Running = n
		case IngestDone:
			st.Done = n
		case IngestFailed:
			st.Failed = n
		}
	}
	_ = rows.Close()
	var oldest sql.NullString
	if err := db.sql.QueryRowContext(ctx, `SELECT MIN(created_at) FROM ingest_queue WHERE status IN ('pending','running')`).Scan(&oldest); err == nil && oldest.Valid {
		st.OldestPending, _ = time.Parse(ingestTime, oldest.String)
	}
	_ = db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM ingest_queue WHERE status='pending' AND attempts>0`).Scan(&st.Retrying)
	return st, nil
}

func scanIngest(rows *sql.Rows) ([]IngestItem, error) {
	var out []IngestItem
	for rows.Next() {
		var it IngestItem
		var next, created, updated string
		if err := rows.Scan(&it.ID, &it.Domain, &it.PayloadJSON, &it.Status, &it.Attempts, &next, &it.Error, &it.ResultJSON, &created, &updated); err != nil {
			return nil, err
		}
		it.NextAttemptAt, _ = time.Parse(ingestTime, next)
		it.CreatedAt, _ = time.Parse(ingestTime, created)
		it.UpdatedAt, _ = time.Parse(ingestTime, updated)
		out = append(out, it)
	}
	return out, rows.Err()
}
