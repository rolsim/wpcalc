-- +goose Up

-- Accounts arrive in their own migration rather than in the initial schema.
-- By the time this runs the database already holds employees and recorded
-- hours, so it exercises the migration system against real data — which is
-- the case that actually breaks in production, and the one a schema created
-- all at once never tests.

CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT    NOT NULL UNIQUE COLLATE NOCASE
                          CHECK (length(trim(username)) > 0),
    password_hash TEXT    NOT NULL,
    role          TEXT    NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user')),
    created_at    TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- Sessions live server-side rather than in a signed cookie so that logging
-- someone out actually revokes their access. A self-contained signed token
-- stays valid until it expires no matter what the server wants.
CREATE TABLE sessions (
    token      TEXT    PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at TEXT    NOT NULL,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_sessions_expires ON sessions (expires_at);

-- +goose Down

DROP INDEX IF EXISTS idx_sessions_expires;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
