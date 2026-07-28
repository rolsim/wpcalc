package domain

import (
	"errors"
	"fmt"
	"strings"
)

// Roles a user may hold.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// User is an account that can sign in to the standalone server.
//
// Under WordPress this type is unused: identity comes from WordPress's own
// user table via the signed headers, and maintaining a parallel account list
// there would be a second, weaker way into the same data.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Role         string

	// Language is the interface locale this account prefers, or "" to follow
	// the browser. It is deliberately not validated against the shipped
	// catalogs here: a locale can be removed later, and a stale preference
	// should quietly fall back rather than make the account unusable.
	Language string
}

// LanguageAuto is the stored value meaning "follow the browser".
const LanguageAuto = ""

// ErrInvalidUser is the sentinel for account validation failures.
var ErrInvalidUser = errors.New("invalid user")

// IsAdmin reports whether the account may manage employees and other users.
func (u User) IsAdmin() bool { return u.Role == RoleAdmin }

// ValidUsername checks the name is usable before it reaches the database.
func ValidUsername(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: username is required", ErrInvalidUser)
	}
	if len(name) > 64 {
		return fmt.Errorf("%w: username is too long", ErrInvalidUser)
	}
	// Whitespace inside a username makes it ambiguous in logs and CLI output,
	// and offers nothing a user actually wants.
	if strings.ContainsAny(name, " \t\n\r") {
		return fmt.Errorf("%w: username may not contain whitespace", ErrInvalidUser)
	}
	return nil
}

// ValidRole checks a role string.
func ValidRole(role string) error {
	switch role {
	case RoleAdmin, RoleUser:
		return nil
	default:
		return fmt.Errorf("%w: unknown role %q", ErrInvalidUser, role)
	}
}

// MinPasswordLength is the shortest password the CLI will set.
//
// Low enough not to obstruct a small team, high enough that the bcrypt cost
// is doing real work rather than protecting a four-character secret.
const MinPasswordLength = 10

// ValidPassword checks a candidate password.
func ValidPassword(pw string) error {
	if len(pw) < MinPasswordLength {
		return fmt.Errorf("%w: password must be at least %d characters",
			ErrInvalidUser, MinPasswordLength)
	}
	return nil
}
