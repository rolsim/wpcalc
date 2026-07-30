package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// APIToken is a bearer credential for /api/v1, distinct from a browser
// session: long-lived, scriptable, and revocable on its own without
// touching the owning account's password or any browser session. Only
// metadata is ever held in memory here — the secret itself is never stored
// or returned again after creation.
type APIToken struct {
	ID         int64
	UserID     int64
	Name       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// ErrInvalidAPIToken is the sentinel for API token validation failures.
var ErrInvalidAPIToken = errors.New("invalid api token")

// ValidAPITokenName checks a candidate token label (the name shown back in
// `wpcalc token list`, not the secret itself).
func ValidAPITokenName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidAPIToken)
	}
	return nil
}
