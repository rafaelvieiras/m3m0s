-- +goose Up
-- Accounts (username + password) for human access to the WebUI; API keys stay
-- for machines. password_hash is NULL until the account's owner sets one
-- through an invite link. Sessions and reset/invite tokens are stored only as
-- SHA-256 hashes of the random token.
CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    last_login_at TEXT
);

CREATE TABLE sessions (
    token_hash   TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    last_used_at TEXT NOT NULL,
    user_agent   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_sessions_user ON sessions(user_id);

CREATE TABLE reset_tokens (
    token_hash TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('reset', 'invite')),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    used_at    TEXT
);
CREATE INDEX idx_reset_tokens_user ON reset_tokens(user_id);

-- +goose Down
DROP TABLE reset_tokens;
DROP TABLE sessions;
DROP TABLE users;
