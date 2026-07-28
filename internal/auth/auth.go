// Package auth resolves who is making a request.
//
// The interface exists from P0 even though the implementation behind it starts
// as a single shared password. Both run modes authenticate completely
// differently — a cookie session standalone, WordPress's own session behind
// the plugin — and handlers must not know which one they are under. Swapping
// the standalone implementation for real accounts at P3 must not touch a
// single handler; if it does, this seam was drawn in the wrong place.
package auth

import (
	"context"
	"errors"
	"net/http"
	"slices"
)

// ErrUnauthenticated means no valid identity accompanied the request.
var ErrUnauthenticated = errors.New("unauthenticated")

// Identity is the authenticated caller.
type Identity struct {
	Username string
	Roles    []string
}

// IsZero reports the absence of an identity.
func (i Identity) IsZero() bool { return i.Username == "" }

// HasRole reports role membership.
func (i Identity) HasRole(role string) bool { return slices.Contains(i.Roles, role) }

// IsAdmin reports whether this identity may manage employees and users.
func (i Identity) IsAdmin() bool {
	return i.HasRole(RoleAdmin) || i.HasRole("administrator")
}

// Roles. "administrator" is WordPress's spelling and is accepted alongside
// ours so the two modes agree on who is privileged.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// Authenticator resolves the identity for a request, or reports that there is
// none. Implementations must not write to the response: redirecting to a login
// page is the handler's decision, not the authenticator's.
type Authenticator interface {
	Identify(r *http.Request) (Identity, error)
}

// SessionWriter is implemented by authenticators that own a browser session.
// The WordPress adapter does not: WordPress owns that session, and offering a
// login form there would be a second, weaker way into the same data.
type SessionWriter interface {
	Login(w http.ResponseWriter, username, password string) error
	Logout(w http.ResponseWriter)
}

// ConnKind records which listener accepted a connection.
//
// It exists because the WordPress adapter trusts identity headers, and headers
// are trivially forged by anything that can reach the port. Trust is therefore
// conditioned on the request having arrived over the unix socket, which only a
// process on the same host with filesystem access can reach.
type ConnKind string

const (
	ConnUnix ConnKind = "unix"
	ConnTCP  ConnKind = "tcp"
)

type connKindKey struct{}

// WithConnKind tags a context with the listener kind. The server does this
// once, in middleware, based on how it is listening.
func WithConnKind(ctx context.Context, k ConnKind) context.Context {
	return context.WithValue(ctx, connKindKey{}, k)
}

// ConnKindFrom reports the listener kind, defaulting to TCP.
//
// The default is deliberately the untrusted one: a context that lost its tag,
// or a code path that forgot to set it, must fail closed.
func ConnKindFrom(ctx context.Context) ConnKind {
	if k, ok := ctx.Value(connKindKey{}).(ConnKind); ok {
		return k
	}
	return ConnTCP
}

type identityKey struct{}

// WithIdentity attaches a resolved identity to a context.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// IdentityFrom retrieves the identity attached by the auth middleware.
func IdentityFrom(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityKey{}).(Identity)
	return id, ok && !id.IsZero()
}
