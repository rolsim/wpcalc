package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/rolsim/wpcalc/internal/domain"
)

// UserStore is the slice of persistence the account authenticator needs.
//
// Declared here rather than imported from store so that this package stays
// testable without a database and the dependency points one way.
type UserStore interface {
	Authenticate(ctx context.Context, username, password string) (domain.User, error)
	SessionByToken(ctx context.Context, token string) (domain.User, *int64, error)
	CreateSession(ctx context.Context, token string, userID int64, expires time.Time) error
	DeleteSession(ctx context.Context, token string) error
	SetUserLanguage(ctx context.Context, userID int64, lang string) error
	SetActiveTenant(ctx context.Context, token string, tenantID *int64) error
	UserRolesForUser(ctx context.Context, userID int64) ([]domain.UserRole, error)
	RolePermissionsFor(ctx context.Context, roleIDs []string) (map[string][]string, error)
}

// LanguageWriter is implemented by authenticators whose identities can store a
// language preference.
//
// The WordPress adapter does not implement it: WordPress owns the user record
// there, and writing a second, divergent preference into this database would
// mean two answers to the same question. The handler hides the control when
// the authenticator cannot persist, rather than offering one that silently
// does nothing.
type LanguageWriter interface {
	SetLanguage(r *http.Request, lang string) error
}

// TenantWriter is implemented by authenticators whose identities can persist
// an active-tenant selection (the multi-tenant switcher).
//
// The WordPress adapter does not implement it: it authenticates fresh from
// signed headers on every request, with no session row of its own to store
// the choice in — and there is no switcher shown there anyway, since a
// WordPress-mode identity already has full access to the one dedicated
// database it runs against (see wordpress.go).
type TenantWriter interface {
	SetActiveTenant(r *http.Request, tenantID *int64) error
}

// CookieName is the standalone session cookie.
const CookieName = "wpcalc_session"

// SessionTTL bounds how long a login lasts.
const SessionTTL = 12 * time.Hour

// Accounts authenticates against real user records with server-side sessions.
//
// It replaces Password behind the same interface. Handlers do not change when
// it is swapped in, which was the point of drawing the seam at P0 — if a
// handler had needed touching, the seam would have been in the wrong place.
//
// Sessions live in the database rather than in a self-contained signed cookie
// so that logging out, or changing a password, actually revokes access. A
// signed token stays valid until it expires regardless of what the server
// wants, which is the wrong default for a timesheet holding staff data.
type Accounts struct {
	store  UserStore
	ttl    time.Duration
	secure bool
	now    func() time.Time
}

// NewAccounts builds the account authenticator.
func NewAccounts(store UserStore) *Accounts {
	return &Accounts{store: store, ttl: SessionTTL, now: time.Now}
}

// SetSecureCookies marks issued cookies Secure.
func (a *Accounts) SetSecureCookies(secure bool) { a.secure = secure }

// Identify resolves the session cookie to an account.
func (a *Accounts) Identify(r *http.Request) (Identity, error) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return Identity{}, ErrUnauthenticated
	}
	u, activeTenantID, err := a.store.SessionByToken(r.Context(), c.Value)
	if err != nil {
		return Identity{}, ErrUnauthenticated
	}
	id, err := a.identityFor(r.Context(), u, activeTenantID)
	if err != nil {
		return Identity{}, err
	}
	return id, nil
}

// Login verifies credentials and starts a session.
func (a *Accounts) Login(w http.ResponseWriter, username, password string) error {
	// No request context reaches here through the SessionWriter interface, so
	// the lookup gets a bounded one of its own rather than context.Background
	// with no deadline at all.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	u, err := a.store.Authenticate(ctx, username, password)
	if err != nil {
		return ErrUnauthenticated
	}

	token, err := newSessionToken()
	if err != nil {
		return err
	}
	expiry := a.now().Add(a.ttl)
	if err := a.store.CreateSession(ctx, token, u.ID, expiry); err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiry,
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// Logout revokes the session server-side as well as clearing the cookie.
// Clearing only the cookie would leave a stolen token working.
func (a *Accounts) Logout(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// LogoutRequest revokes the session belonging to a specific request. The
// SessionWriter interface cannot see the request, so the handler calls this
// as well when it has one.
func (a *Accounts) LogoutRequest(r *http.Request) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return
	}
	_ = a.store.DeleteSession(r.Context(), c.Value)
}

// SetLanguage stores the preference for whoever this request belongs to.
func (a *Accounts) SetLanguage(r *http.Request, lang string) error {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return ErrUnauthenticated
	}
	u, _, err := a.store.SessionByToken(r.Context(), c.Value)
	if err != nil {
		return ErrUnauthenticated
	}
	return a.store.SetUserLanguage(r.Context(), u.ID, lang)
}

// SetActiveTenant persists which tenant this request's session has activated
// — RBAC96 session role-activation, adapted to tenant scoping (see
// Identity.ActiveTenantID).
func (a *Accounts) SetActiveTenant(r *http.Request, tenantID *int64) error {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return ErrUnauthenticated
	}
	return a.store.SetActiveTenant(r.Context(), c.Value, tenantID)
}

// identityFor resolves a user's full UA/PA data (UserRoles and their
// permissions) into an Identity. Done fresh on every call — not cached
// across requests — so a permission revoked mid-session takes effect on the
// very next request.
func (a *Accounts) identityFor(ctx context.Context, u domain.User, activeTenantID *int64) (Identity, error) {
	userRoles, err := a.store.UserRolesForUser(ctx, u.ID)
	if err != nil {
		return Identity{}, err
	}

	roleIDs := make([]string, 0, len(userRoles))
	seen := make(map[string]bool, len(userRoles))
	for _, ur := range userRoles {
		if !seen[ur.RoleID] {
			seen[ur.RoleID] = true
			roleIDs = append(roleIDs, ur.RoleID)
		}
	}
	perms, err := a.store.RolePermissionsFor(ctx, roleIDs)
	if err != nil {
		return Identity{}, err
	}

	return Identity{
		UserID:          u.ID,
		Username:        u.Username,
		Language:        u.Language,
		UserRoles:       userRoles,
		RolePermissions: perms,
		ActiveTenantID:  activeTenantID,
	}, nil
}

// newSessionToken produces an unguessable session identifier.
func newSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generate session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Compile-time proof that both modes satisfy the same contract, so a drift
// between them fails here rather than at a call site in a handler.
var (
	_ Authenticator  = (*Accounts)(nil)
	_ Authenticator  = (*WordPress)(nil)
	_ SessionWriter  = (*Accounts)(nil)
	_ LanguageWriter = (*Accounts)(nil)
	_ TenantWriter   = (*Accounts)(nil)
)

// ErrNoAccounts signals that nobody can administer this database yet, so the
// operator is told how to fix it instead of watching every login fail for no
// stated reason.
var ErrNoAccounts = errors.New("auth: no account can manage this database yet; " +
	"create one with `wpcalc user add` and `wpcalc user grant <name> --system -role super_admin`")
