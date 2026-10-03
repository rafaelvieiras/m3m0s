-- +goose Up
CREATE TABLE entries (
    id              TEXT PRIMARY KEY,
    content         TEXT NOT NULL,
    domain          TEXT NOT NULL,
    domain_category TEXT NOT NULL,
    source          TEXT NOT NULL,
    agent           TEXT,
    tags_json       TEXT NOT NULL DEFAULT '[]',
    metadata_json   TEXT NOT NULL DEFAULT '{}',
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    embedding       BLOB
);

CREATE INDEX idx_entries_domain ON entries(domain);
CREATE INDEX idx_entries_domain_category ON entries(domain_category);
CREATE INDEX idx_entries_created_at ON entries(created_at);

CREATE TABLE source_blobs (
    hash    TEXT PRIMARY KEY,
    content TEXT NOT NULL
);

CREATE TABLE api_keys (
    prefix     TEXT NOT NULL,
    hash       TEXT PRIMARY KEY,
    label      TEXT,
    created_at TEXT NOT NULL
);

CREATE TABLE job_intervals (
    name             TEXT PRIMARY KEY,
    interval_seconds INTEGER NOT NULL
);

CREATE TABLE telemetry (
    key   TEXT PRIMARY KEY,
    value INTEGER NOT NULL
);

-- +goose Down
DROP TABLE telemetry;
DROP TABLE job_intervals;
DROP TABLE api_keys;
DROP TABLE source_blobs;
DROP TABLE entries;
