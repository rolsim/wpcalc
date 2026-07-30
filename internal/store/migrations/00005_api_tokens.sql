-- +goose Up

-- Bearer tokens for /api/v1, distinct from the browser session cookie: a
-- token is a long-lived credential a script holds, not a short-lived
-- session a login flow issues, so it gets its own table rather than
-- reusing `sessions` (which also carries an active_tenant_id that has no
-- meaning for a stateless API request — every API call names its tenant in
-- the path instead).
--
-- Only the SHA-256 hash is stored; the token itself is shown once, at
-- creation time, and cannot be recovered afterward — the same trust model
-- as a password, not a session cookie that the server could reissue.
CREATE TABLE api_tokens (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        TEXT    NOT NULL CHECK (length(trim(name)) > 0),
    token_hash  TEXT    NOT NULL UNIQUE,
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    last_used_at TEXT,
    revoked_at  TEXT
);

CREATE INDEX idx_api_tokens_user ON api_tokens (user_id);

-- +goose Down

DROP TABLE api_tokens;
