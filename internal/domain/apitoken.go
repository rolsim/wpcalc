package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// AccessTokenTTL is how long a bearer token authenticates for after
// issuance — short enough that a leaked token stops working on its own
// well before anyone would notice and revoke it by hand, which is the
// whole point of separating it from RefreshTokenTTL.
const AccessTokenTTL = time.Hour

// RefreshTokenTTL is how long a refresh token remains exchangeable. Unused
// past this, or once exchanged (refresh tokens are single-use — see
// internal/store's ExchangeRefreshToken), it stops working the same way a
// revoked one does; get a new pair with `wpcalc token create`.
const RefreshTokenTTL = 30 * 24 * time.Hour

// APIToken is a bearer credential for /api/v1, distinct from a browser
// session: scriptable and revocable on its own without touching the
// owning account's password or any browser session. Short-lived by
// design (AccessTokenTTL) — see RefreshToken for how a script keeps
// working past that without holding a long-lived secret directly. Only
// metadata is ever held in memory here — the secret itself is never
// stored or returned again after creation.
type APIToken struct {
	ID         int64
	UserID     int64
	Name       string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// RefreshToken exchanges for a new APIToken (and a new, rotated
// RefreshToken) without going back to `wpcalc token create` — the
// longer-lived half of the pair CreateAPIToken issues alongside it.
// Single-use: UsedAt is set the moment it's exchanged, same trust model as
// a password reset link.
type RefreshToken struct {
	ID        int64
	UserID    int64
	Name      string
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
	RevokedAt *time.Time
}

// ErrInvalidAPIToken is the sentinel for API/refresh token validation
// failures.
var ErrInvalidAPIToken = errors.New("invalid api token")

// ValidAPITokenName checks a candidate token label (the name shown back in
// `wpcalc token list`, not the secret itself) — shared by APIToken and
// RefreshToken, which are always named identically since they're always
// issued as a pair.
func ValidAPITokenName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidAPIToken)
	}
	return nil
}
