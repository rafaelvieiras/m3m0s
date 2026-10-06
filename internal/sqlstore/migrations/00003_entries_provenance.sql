-- +goose Up
-- Provenance (F2): which model wrote the entry and which project it belongs
-- to. Both nullable: NULL model = unknown, NULL project = global (existing
-- entries stay global with unknown model). The tool that wrote an entry is
-- already `source`.
ALTER TABLE entries ADD COLUMN model TEXT;
ALTER TABLE entries ADD COLUMN project TEXT;
CREATE INDEX idx_entries_project ON entries(project);

-- +goose Down
DROP INDEX idx_entries_project;
ALTER TABLE entries DROP COLUMN project;
ALTER TABLE entries DROP COLUMN model;
