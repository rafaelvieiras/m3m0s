-- +goose Up
-- Full-text index over entries.content for keyword and hybrid search (F5).
-- Standalone FTS5 table keyed by the entries rowid (triggers keep it in
-- sync); `id` is stored so a hit maps back to the entry without a join on
-- rowid. unicode61 with remove_diacritics folds accents ("memoria" matches
-- "memória").
CREATE VIRTUAL TABLE entries_fts USING fts5(
    id UNINDEXED,
    content,
    tokenize = 'unicode61 remove_diacritics 2'
);

INSERT INTO entries_fts (rowid, id, content) SELECT rowid, id, content FROM entries;

-- +goose StatementBegin
CREATE TRIGGER entries_fts_ai AFTER INSERT ON entries BEGIN
    INSERT INTO entries_fts (rowid, id, content) VALUES (new.rowid, new.id, new.content);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER entries_fts_ad AFTER DELETE ON entries BEGIN
    DELETE FROM entries_fts WHERE rowid = old.rowid;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER entries_fts_au AFTER UPDATE OF content ON entries BEGIN
    UPDATE entries_fts SET content = new.content WHERE rowid = new.rowid;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER entries_fts_au;
DROP TRIGGER entries_fts_ad;
DROP TRIGGER entries_fts_ai;
DROP TABLE entries_fts;
