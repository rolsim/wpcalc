package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"source.simonet.internal/rolsim/wpcalc/internal/domain"
)

// UserStore is the slice of persistence the account authenticator needs.
//
// Declared here rather than imported from store so that this package stays
// testable without a database and the dependency points one way.
type UserStore interface {
	Authenticate(ctx context.Context, username, password string) (domain.User, error)
	SessionUser(ctx context.Context, token string) (domain.User, error)
	CreateSession(ctx context.Context, token string, userID int64, expires time.Time) error
	DeleteSession(ctx context.Context, token string) error
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
	u, err := a.store.SessionUser(r.Context(), c.Value)
	if err != nil {
		return Identity{}, ErrUnauthenticated
	}
	return identityFor(u), nil
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

func identityFor(u domain.User) Identity {
	role := RoleUser
	if u.IsAdmin() {
		role = RoleAdmin
	}
	return Identity{Username: u.Username, Roles: []string{role}}
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
	_ Authenticator = (*Accounts)(nil)
	_ Authenticator = (*WordPress)(nil)
	_ SessionWriter = (*Accounts)(nil)
)

// ErrNoAccounts signals an empty user table, so the operator is told to create
// the first account instead of watching every login fail for no stated reason.
var ErrNoAccounts = errors.New("auth: no accounts exist; create one with `wpcalc user add`")
