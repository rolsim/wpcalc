package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/rolsim/wpcalc/internal/domain"
)

// CreateRefreshToken mints a new refresh token for userID, valid for
// domain.RefreshTokenTTL. Always issued alongside a CreateAPIToken call
// with the same name — see ExchangeRefreshToken for the pair this one
// exists to renew without a trip back through `wpcalc token create`.
// expiresAt is the actual stored expiry, not just now+TTL recomputed by
// the caller.
func (db *DB) CreateRefreshToken(ctx context.Context, userID uuid.UUID, name string) (token string, id uuid.UUID, expiresAt time.Time, err error) {
	if err := domain.ValidAPITokenName(name); err != nil {
		return "", uuid.Nil(), time.Time{}, err
	}
	token, err = newOpaqueToken(refreshTokenPrefix)
	if err != nil {
		return "", uuid.Nil(), time.Time{}, fmt.Errorf("store: create refresh token: %w", err)
	}
	expiresAt = time.Now().Add(domain.RefreshTokenTTL).UTC().Truncate(time.Second)

	id = uuid.NewV4()
	_, err = db.ExecContext(ctx,
		`INSERT INTO refresh_tokens (id, user_id, name, token_hash, expires_at) VALUES (?, ?, ?, ?, ?)`,
		arg(id), arg(userID), name, hashToken(token), formatSQLiteTimestamp(expiresAt))
	if err != nil {
		if isForeignKeyViolation(err) {
			return "", uuid.Nil(), time.Time{}, fmt.Errorf("store: create refresh token: %w", ErrNotFound)
		}
		return "", uuid.Nil(), time.Time{}, fmt.Errorf("store: create refresh token: %w", err)
	}
	return token, id, expiresAt, nil
}

// ErrRefreshTokenUsed distinguishes "this refresh token already got
// exchanged" from a plain not-found — useful to a caller deciding how
// alarmed to be (reuse of an already-used refresh token is a strong
// signal of a leaked credential, not just an expired one), even though
// both currently map to the same 401 at the HTTP layer.
var ErrRefreshTokenUsed = errors.New("refresh token already used")

// TokenExchange is what redeeming a refresh token produces: a brand-new
// access token and a rotated refresh token, both already persisted, plus
// enough metadata (id, name, expiries) that a caller never has to
// recompute or re-query anything the store already knows precisely.
type TokenExchange struct {
	AccessToken           string
	AccessTokenID         uuid.UUID
	AccessTokenExpiresAt  time.Time
	RefreshToken          string
	RefreshTokenExpiresAt time.Time
	Name                  string
}

// ExchangeRefreshToken redeems a refresh token for a new access token and
// a new, rotated refresh token — single-use: the redeemed token is marked
// used atomically (an UPDATE ... RETURNING that only matches an
// unused, unrevoked, unexpired row), so two concurrent exchanges of the
// same token cannot both succeed, and the same token can never be redeemed
// twice. An unknown, expired, revoked, or already-used token reads as
// ErrNotFound, same indistinguishability principle as UserByAPIToken —
// except a *specifically already-used* one additionally wraps
// ErrRefreshTokenUsed, for callers (this package's own tests, security
// logging) that want to tell "stale" from "reused" apart; from the HTTP
// layer's perspective both still mean 401.
func (db *DB) ExchangeRefreshToken(ctx context.Context, token string) (TokenExchange, error) {
	hash := hashToken(token)
	now := formatSQLiteTimestamp(time.Now())

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return TokenExchange{}, fmt.Errorf("store: exchange refresh token: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op once committed

	var id, userID uuid.UUID
	var name string
	err = tx.QueryRowContext(ctx,
		`UPDATE refresh_tokens SET used_at = datetime('now')
		  WHERE token_hash = ? AND revoked_at IS NULL AND used_at IS NULL AND expires_at > ?
		  RETURNING id, user_id, name`,
		hash, now).Scan(scan{&id}, scan{&userID}, &name)
	if errors.Is(err, sql.ErrNoRows) {
		// Same open transaction, same (only) pooled connection: querying
		// through db here instead of tx would block forever waiting for a
		// second connection that only frees up once this transaction ends.
		wasUsed, checkErr := refreshTokenWasUsed(ctx, tx, hash)
		if checkErr == nil && wasUsed {
			return TokenExchange{}, fmt.Errorf("store: exchange refresh token: %w", ErrRefreshTokenUsed)
		}
		return TokenExchange{}, fmt.Errorf("store: exchange refresh token: %w", ErrNotFound)
	}
	if err != nil {
		return TokenExchange{}, fmt.Errorf("store: exchange refresh token: %w", err)
	}

	result := TokenExchange{Name: name}

	result.AccessToken, err = newOpaqueToken(tokenPrefix)
	if err != nil {
		return TokenExchange{}, fmt.Errorf("store: exchange refresh token: %w", err)
	}
	result.AccessTokenExpiresAt = time.Now().Add(domain.AccessTokenTTL).UTC().Truncate(time.Second)
	result.AccessTokenID = uuid.NewV4()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO api_tokens (id, user_id, name, token_hash, expires_at) VALUES (?, ?, ?, ?, ?)`,
		arg(result.AccessTokenID), arg(userID), name, hashToken(result.AccessToken),
		formatSQLiteTimestamp(result.AccessTokenExpiresAt)); err != nil {
		return TokenExchange{}, fmt.Errorf("store: exchange refresh token: issue access token: %w", err)
	}

	result.RefreshToken, err = newOpaqueToken(refreshTokenPrefix)
	if err != nil {
		return TokenExchange{}, fmt.Errorf("store: exchange refresh token: %w", err)
	}
	result.RefreshTokenExpiresAt = time.Now().Add(domain.RefreshTokenTTL).UTC().Truncate(time.Second)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO refresh_tokens (id, user_id, name, token_hash, expires_at) VALUES (?, ?, ?, ?, ?)`,
		arg(uuid.NewV4()), arg(userID), name, hashToken(result.RefreshToken),
		formatSQLiteTimestamp(result.RefreshTokenExpiresAt)); err != nil {
		return TokenExchange{}, fmt.Errorf("store: exchange refresh token: issue refresh token: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return TokenExchange{}, fmt.Errorf("store: exchange refresh token: %w", err)
	}
	return result, nil
}

func refreshTokenWasUsed(ctx context.Context, tx *sql.Tx, hash string) (bool, error) {
	var usedAt sql.NullString
	err := tx.QueryRowContext(ctx,
		`SELECT used_at FROM refresh_tokens WHERE token_hash = ?`, hash).Scan(&usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return usedAt.Valid, nil
}
