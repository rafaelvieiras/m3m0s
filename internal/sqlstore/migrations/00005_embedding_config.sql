-- +goose Up
-- Admin-defined embedding configuration (#39, D36) and per-row provenance of
-- the vector space: vectors from different models or dimensions are not
-- comparable, so every stored embedding records what produced it.
CREATE TABLE embedding_config (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    enabled     INTEGER NOT NULL DEFAULT 1,
    base_url    TEXT NOT NULL DEFAULT '',
    api_key     TEXT NOT NULL DEFAULT '',
    model       TEXT NOT NULL,
    dimensions  INTEGER NOT NULL DEFAULT 0, -- requested "dimensions" parameter, 0 = provider default
    vector_dims INTEGER NOT NULL DEFAULT 0, -- length the provider actually returned when saved
    updated_at  TEXT NOT NULL
);

ALTER TABLE entries ADD COLUMN embedding_model TEXT;
ALTER TABLE entries ADD COLUMN embedding_dims INTEGER;

-- Vectors stored before this migration have NULL here and are read as
-- text-embedding-3-small with dims = length(embedding) / 4 (see
-- sqlstore.effectiveModelSQL). No backfill UPDATE on purpose: it would rewrite
-- every ~6 KB embedding blob (hundreds of MB of WAL and minutes on a small
-- disk) just to store what is already implied.

-- +goose Down
ALTER TABLE entries DROP COLUMN embedding_dims;
ALTER TABLE entries DROP COLUMN embedding_model;
DROP TABLE embedding_config;
