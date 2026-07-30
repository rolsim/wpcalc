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
// leaked-secret scanner, a shell history) as a wpcalc API token specifically,
// the way "ghp_" or "sk-" identify their own issuers.
const tokenPrefix = "wpat_"

// CreateAPIToken mints a new bearer token for userID and returns it in full.
// This is the only time the plaintext is ever available — only its SHA-256
// hash is persisted, so a leaked database dump does not also leak usable
// credentials.
func (db *DB) CreateAPIToken(ctx context.Context, userID int64, name string) (token string, id int64, err error) {
	if err := domain.ValidAPITokenName(name); err != nil {
		return "", 0, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", 0, fmt.Errorf("store: create api token: %w", err)
	}
	token = tokenPrefix + base64.RawURLEncoding.EncodeToString(secret)

	res, err := db.ExecContext(ctx,
		`INSERT INTO api_tokens (user_id, name, token_hash) VALUES (?, ?, ?)`,
		userID, name, hashToken(token))
	if err != nil {
		if isForeignKeyViolation(err) {
			return "", 0, fmt.Errorf("store: create api token: %w", ErrNotFound)
		}
		return "", 0, fmt.Errorf("store: create api token: %w", err)
	}
	id, err = res.LastInsertId()
	if err != nil {
		return "", 0, fmt.Errorf("store: create api token: %w", err)
	}
	return token, id, nil
}

// APITokens lists the tokens a user holds, most recent first. token_hash is
// never selected — this is metadata only, never enough to authenticate.
func (db *DB) APITokens(ctx context.Context, userID int64) ([]domain.APIToken, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, user_id, name, created_at, last_used_at, revoked_at
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

// UserByAPIToken resolves a bearer token to its owning account and records
// the use. An unknown, revoked, or otherwise invalid token reads as
// ErrNotFound, same as every other "no such thing" lookup in this package
// — the caller (internal/auth) is responsible for turning that into an
// undifferentiated 401, not distinguishing "revoked" from "never existed"
// for anyone who doesn't already hold the secret.
func (db *DB) UserByAPIToken(ctx context.Context, token string) (domain.User, error) {
	hash := hashToken(token)

	var u domain.User
	var tokenID int64
	err := db.QueryRowContext(ctx,
		`SELECT u.id, u.username, u.password_hash, u.language, t.id
		 FROM api_tokens t JOIN users u ON u.id = t.user_id
		 WHERE t.token_hash = ? AND t.revoked_at IS NULL`, hash).
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
	var created string
	var lastUsed, revoked sql.NullString
	if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &created, &lastUsed, &revoked); err != nil {
		return domain.APIToken{}, err
	}
	createdAt, err := parseSQLiteTimestamp(created)
	if err != nil {
		return domain.APIToken{}, fmt.Errorf("parse created_at %q: %w", created, err)
	}
	t.CreatedAt = createdAt
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

// parseSQLiteTimestamp reads back what `datetime('now')` writes: UTC,
// space-separated, no zone suffix.
func parseSQLiteTimestamp(s string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02 15:04:05", s, time.UTC)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
