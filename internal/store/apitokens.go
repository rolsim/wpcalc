package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/rolsim/wpcalc/internal/domain"
)

// tokenPrefix makes a token recognizable at a glance (in a log line, a
// leaked-secret scanner, a shell history) as a wpcalc API token
// specifically, the way "ghp_" or "sk-" identify their own issuers.
// refreshTokenPrefix does the same for refresh tokens, and — just as
// importantly — makes the two visually impossible to confuse with each
// other, on top of the fact that they're already looked up in separate
// tables and never valid as each other's kind of credential.
const (
	tokenPrefix        = "wpat_"
	refreshTokenPrefix = "wprt_"
)

// CreateAPIToken mints a new bearer token for userID, valid for
// domain.AccessTokenTTL, and returns it in full. This is the only time the
// plaintext is ever available — only its SHA-256 hash is persisted, so a
// leaked database dump does not also leak usable credentials. expiresAt is
// the actual stored expiry, not just now+TTL recomputed by the caller —
// one source of truth for what the database will actually enforce.
func (db *DB) CreateAPIToken(ctx context.Context, userID int64, name string) (token string, id int64, expiresAt time.Time, err error) {
	if err := domain.ValidAPITokenName(name); err != nil {
		return "", 0, time.Time{}, err
	}
	token, err = newOpaqueToken(tokenPrefix)
	if err != nil {
		return "", 0, time.Time{}, fmt.Errorf("store: create api token: %w", err)
	}
	// Truncated to whole seconds: what's returned here must read back
	// identically to what SQLite actually stores (formatSQLiteTimestamp
	// drops sub-second precision), or a caller comparing the two would see
	// spurious drift.
	expiresAt = time.Now().Add(domain.AccessTokenTTL).UTC().Truncate(time.Second)

	res, err := db.ExecContext(ctx,
		`INSERT INTO api_tokens (user_id, name, token_hash, expires_at) VALUES (?, ?, ?, ?)`,
		userID, name, hashToken(token), formatSQLiteTimestamp(expiresAt))
	if err != nil {
		if isForeignKeyViolation(err) {
			return "", 0, time.Time{}, fmt.Errorf("store: create api token: %w", ErrNotFound)
		}
		return "", 0, time.Time{}, fmt.Errorf("store: create api token: %w", err)
	}
	id, err = res.LastInsertId()
	if err != nil {
		return "", 0, time.Time{}, fmt.Errorf("store: create api token: %w", err)
	}
	return token, id, expiresAt, nil
}

// APITokens lists the tokens a user holds, most recent first. token_hash is
// never selected — this is metadata only, never enough to authenticate.
func (db *DB) APITokens(ctx context.Context, userID int64) ([]domain.APIToken, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, user_id, name, created_at, expires_at, last_used_at, revoked_at
		 FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: list api tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.APIToken
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list api tokens: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list api tokens: %w", err)
	}
	return out, nil
}

// RevokeAPIToken disables a token by id. Revocation is permanent — the row
// stays (an audit trail of what once had access), only revoked_at is set.
func (db *DB) RevokeAPIToken(ctx context.Context, id int64) error {
	res, err := db.ExecContext(ctx,
		`UPDATE api_tokens SET revoked_at = datetime('now') WHERE id = ? AND revoked_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("store: revoke api token %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: revoke api token %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("store: revoke api token %d: %w", id, ErrNotFound)
	}
	return nil
}

// RevokeAllUserTokens revokes every access token and refresh token userID
// holds — a script's equivalent of "log out everywhere". Already-expired
// or already-revoked rows are left as they are (no error, no-op); this
// never fails just because there was nothing left to revoke.
func (db *DB) RevokeAllUserTokens(ctx context.Context, userID int64) error {
	if _, err := db.ExecContext(ctx,
		`UPDATE api_tokens SET revoked_at = datetime('now') WHERE user_id = ? AND revoked_at IS NULL`, userID); err != nil {
		return fmt.Errorf("store: revoke all api tokens for user %d: %w", userID, err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = datetime('now') WHERE user_id = ? AND revoked_at IS NULL AND used_at IS NULL`, userID); err != nil {
		return fmt.Errorf("store: revoke all refresh tokens for user %d: %w", userID, err)
	}
	return nil
}

// UserByAPIToken resolves a bearer token to its owning account and records
// the use. An unknown, expired, revoked, or otherwise invalid token reads
// as ErrNotFound, same as every other "no such thing" lookup in this
// package — the caller (internal/auth) is responsible for turning that
// into an undifferentiated 401, not distinguishing "expired" from
// "revoked" from "never existed" for anyone who doesn't already hold the
// secret.
func (db *DB) UserByAPIToken(ctx context.Context, token string) (domain.User, error) {
	hash := hashToken(token)

	var u domain.User
	var tokenID int64
	err := db.QueryRowContext(ctx,
		`SELECT u.id, u.username, u.password_hash, u.language, t.id
		 FROM api_tokens t JOIN users u ON u.id = t.user_id
		 WHERE t.token_hash = ? AND t.revoked_at IS NULL AND t.expires_at > ?`,
		hash, formatSQLiteTimestamp(time.Now())).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Language, &tokenID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, fmt.Errorf("store: api token: %w", ErrNotFound)
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("store: api token: %w", err)
	}

	// Best-effort: a failed touch must not fail the request it is auditing.
	_, _ = db.ExecContext(ctx,
		`UPDATE api_tokens SET last_used_at = datetime('now') WHERE id = ?`, tokenID)

	return u, nil
}

func scanAPIToken(rows *sql.Rows) (domain.APIToken, error) {
	var t domain.APIToken
	var created, expires string
	var lastUsed, revoked sql.NullString
	if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &created, &expires, &lastUsed, &revoked); err != nil {
		return domain.APIToken{}, err
	}
	createdAt, err := parseSQLiteTimestamp(created)
	if err != nil {
		return domain.APIToken{}, fmt.Errorf("parse created_at %q: %w", created, err)
	}
	t.CreatedAt = createdAt
	expiresAt, err := parseSQLiteTimestamp(expires)
	if err != nil {
		return domain.APIToken{}, fmt.Errorf("parse expires_at %q: %w", expires, err)
	}
	t.ExpiresAt = expiresAt
	if lastUsed.Valid {
		ts, err := parseSQLiteTimestamp(lastUsed.String)
		if err != nil {
			return domain.APIToken{}, fmt.Errorf("parse last_used_at %q: %w", lastUsed.String, err)
		}
		t.LastUsedAt = &ts
	}
	if revoked.Valid {
		ts, err := parseSQLiteTimestamp(revoked.String)
		if err != nil {
			return domain.APIToken{}, fmt.Errorf("parse revoked_at %q: %w", revoked.String, err)
		}
		t.RevokedAt = &ts
	}
	return t, nil
}

// parseSQLiteTimestamp reads back what `datetime('now')` (or
// formatSQLiteTimestamp) writes: UTC, space-separated, no zone suffix.
func parseSQLiteTimestamp(s string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02 15:04:05", s, time.UTC)
}

// formatSQLiteTimestamp writes a time.Time in the same shape
// `datetime('now')` produces, so a Go-computed expiry (created_at + a TTL)
// compares and parses identically to one SQLite computed itself.
func formatSQLiteTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

// newOpaqueToken generates an unguessable bearer secret: 32 random bytes,
// base64url-encoded, with prefix prepended so the kind of credential is
// identifiable at a glance without decoding anything.
func newOpaqueToken(prefix string) (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(secret), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
