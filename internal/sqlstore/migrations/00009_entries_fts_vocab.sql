-- +goose Up
-- Term list of the keyword index (D51): every distinct token of entries_fts
-- with its document count. Read by the query-term correction to repair
-- misspelled names; it stores nothing itself (derived from entries_fts).
CREATE VIRTUAL TABLE entries_fts_vocab USING fts5vocab(entries_fts, 'row');

-- +goose Down
DROP TABLE entries_fts_vocab;
