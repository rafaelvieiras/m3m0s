-- +goose Up
-- Durable ingest queue (D40): an accepted POST /memories is written here
-- before the server answers, so an operation survives a restart. `id` is the
-- public operationId. payload_json is the already-redacted AddRequest.
CREATE TABLE ingest_queue (
    id              TEXT PRIMARY KEY,
    domain          TEXT NOT NULL,
    payload_json    TEXT NOT NULL,
    status          TEXT NOT NULL,          -- pending | running | done | failed
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT NOT NULL,
    error           TEXT NOT NULL DEFAULT '',
    result_json     TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);
CREATE INDEX ingest_queue_due ON ingest_queue (status, next_attempt_at);

-- +goose Down
DROP TABLE ingest_queue;
