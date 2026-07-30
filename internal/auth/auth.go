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

	"github.com/rolsim/wpcalc/internal/domain"
)

// ErrUnauthenticated means no valid identity accompanied the request.
var ErrUnauthenticated = errors.New("unauthenticated")

// Identity is the authenticated caller.
//
// Access is entirely derived from UserRoles (RBAC96's UA relation — see
// domain.UserRole) via Can/CanInTenant/CanSystemWide: no handler or
// middleware ever compares a role ID to a hardcoded string. Both fields are
// resolved once, when the identity is built (standalone: accounts.go's
// identityFor; WordPress: wordpress.go's synthetic FullAccess identity), so a
// permission revoked mid-session takes effect on the very next request
// without anything here caching stale data.
type Identity struct {
	Username string

	// Language is this caller's preferred interface locale, or "" to fall back
	// to content negotiation. Carried on the identity so a handler never has
	// to query for it: it is resolved once, where the identity is.
	Language string

	// UserID identifies the account, for store calls keyed by user (e.g.
	// listing accessible tenants). Zero for a WordPress-mode identity, which
	// is not tied to a stored account row.
	UserID int64

	// UserRoles are this account's user_roles rows (UA in RBAC96).
	// RolePermissions maps each held RoleID to the permission IDs its
	// role_permissions grant (PA in RBAC96) — resolved alongside UserRoles so
	// Can/CanInTenant/CanSystemWide need no further DB access.
	UserRoles       []domain.UserRole
	RolePermissions map[string][]string

	// ActiveTenantID is which of the account's several tenant memberships (if
	// more than one) is active for this session — RBAC96 session
	// role-activation, adapted to tenant scoping.
	ActiveTenantID *int64

	// FullAccess marks an identity that may do anything in this database,
	// bypassing the UserRoles walk entirely. Set only for WordPress-mode
	// identities: PHP already gates every proxied request on manage_options
	// before it reaches here, so there is no lesser tier under WordPress —
	// modeling it as an ordinary role assignment would need the WordPress
	// authenticator to depend on the store, which it deliberately does not
	// (see wordpress.go).
	FullAccess bool
}

// IsZero reports the absence of an identity.
func (i Identity) IsZero() bool { return i.Username == "" }

// CanSystemWide reports whether the identity holds a system-scope role
// granting this permission.
func (i Identity) CanSystemWide(permission string) bool {
	if i.FullAccess {
		return true
	}
	for _, ur := range i.UserRoles {
		if ur.TenantID == nil && ur.EmployeeID == nil && i.roleHas(ur.RoleID, permission) {
			return true
		}
	}
	return false
}

// CanInTenant reports whether the identity holds this permission for
// tenantID: a tenant-scope role for it, or CanSystemWide.
func (i Identity) CanInTenant(permission string, tenantID int64) bool {
	if i.CanSystemWide(permission) {
		return true
	}
	for _, ur := range i.UserRoles {
		if ur.TenantID != nil && *ur.TenantID == tenantID && i.roleHas(ur.RoleID, permission) {
			return true
		}
	}
	return false
}

// Can reports whether the identity holds this permission for employeeID,
// which belongs to tenantID: an employee-scope role for it, or CanInTenant
// for its tenant. The caller supplies tenantID (already known from the
// employee record) rather than Identity looking it up, since Identity has no
// store access of its own.
func (i Identity) Can(permission string, employeeID, tenantID int64) bool {
	if i.CanInTenant(permission, tenantID) {
		return true
	}
	for _, ur := range i.UserRoles {
		if ur.EmployeeID != nil && *ur.EmployeeID == employeeID && i.roleHas(ur.RoleID, permission) {
			return true
		}
	}
	return false
}

func (i Identity) roleHas(roleID, permission string) bool {
	return slices.Contains(i.RolePermissions[roleID], permission)
}

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
