-- +goose Up
-- Latest result of the memory-hygiene analysis (D37): one small JSON document
-- per report name, replaced on every run. The analysis only reads `entries`;
-- this table is the only thing it writes.
CREATE TABLE insight_reports (
    name        TEXT PRIMARY KEY,
    computed_at TEXT NOT NULL,
    report_json TEXT NOT NULL
);

-- +goose Down
DROP TABLE insight_reports;
