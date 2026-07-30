-- +goose Up

-- Every access token now expires (1 hour — see domain.AccessTokenTTL) —
-- a leaked token that never expired was a standing liability. expires_at
-- can't be a single-statement `ALTER TABLE ADD COLUMN ... NOT NULL
-- DEFAULT`, because the default must be computed per row from that row's
-- own created_at, which a column DEFAULT expression cannot reference; the
-- nullable-then-backfill split is the same pattern (and the same
-- underlying SQLite limitation) migration 00004 hit adding
-- employees.tenant_id.
--
-- Backfilling relative to created_at rather than "now" means every token
-- that predates this migration reads as already expired the moment it
-- applies, rather than as freshly valid for another hour or permanently
-- valid — the whole point is that no token silently keeps working forever
-- once expiry exists; re-issue with `wpcalc token create`.
ALTER TABLE api_tokens ADD COLUMN expires_at TEXT;
UPDATE api_tokens SET expires_at = datetime(created_at, '+1 hour');

-- Refresh tokens: a longer-lived (30 days — domain.RefreshTokenTTL),
-- single-use credential for minting a new access token without going back
-- to `wpcalc token create`. Deliberately its own table rather than a
-- "kind" column on api_tokens: the two are hashed and looked up the same
-- way, but a refresh token is never itself accepted as a bearer
-- credential, and mixing the two into one table/lookup risks that
-- boundary blurring by accident later.
--
-- used_at marks single use: exchanging a refresh token sets it and, in the
-- same transaction, inserts both a new access token row and a new
-- (rotated) refresh_tokens row — reusing an already-used, expired, or
-- revoked refresh token fails exactly like an unknown one, same
-- indistinguishability principle as api_tokens.
CREATE TABLE refresh_tokens (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        TEXT    NOT NULL CHECK (length(trim(name)) > 0),
    token_hash  TEXT    NOT NULL UNIQUE,
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    expires_at  TEXT    NOT NULL,
    used_at     TEXT,
    revoked_at  TEXT
);

CREATE INDEX idx_refresh_tokens_user ON refresh_tokens (user_id);

-- +goose Down

DROP TABLE refresh_tokens;
ALTER TABLE api_tokens DROP COLUMN expires_at;
