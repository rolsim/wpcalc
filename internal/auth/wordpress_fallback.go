package auth

import (
	"errors"
	"net/http"
)

// WordPressFallback composes the WordPress identity source with a local
// Accounts fallback, for the frontend shortcode path only.
//
// A ScopeAdmin request always succeeds or fails through WordPress alone (see
// wordpress.go) — the standalone login form must never become a second,
// weaker door into an admin-gated request, which is why buildAuthenticator
// keeps the two modes mutually exclusive everywhere else. But a ScopeSelf
// request whose WordPress username has no linked wpcalc account
// (ErrNoLinkedAccount) has no other way in at all: without a fallback, that
// employee is simply locked out until an admin links their account. Falling
// back to a local session/login is a deliberate escape hatch for exactly
// that case — a double login, not the intended common path.
type WordPressFallback struct {
	*WordPress
	fallback *Accounts
}

// NewWordPressFallback builds the composite authenticator.
func NewWordPressFallback(wp *WordPress, fallback *Accounts) *WordPressFallback {
	return &WordPressFallback{WordPress: wp, fallback: fallback}
}

// Identify tries WordPress first; only ErrNoLinkedAccount falls through to
// the local Accounts session. Every other outcome — success,
// ErrHeadersOverTCP, a stale/tampered signature — is WordPress's alone to
// decide, unchanged from running it without a fallback at all.
func (a *WordPressFallback) Identify(r *http.Request) (Identity, error) {
	id, err := a.WordPress.Identify(r)
	if errors.Is(err, ErrNoLinkedAccount) {
		return a.fallback.Identify(r)
	}
	return id, err
}

// Login, Logout, and LogoutRequest delegate to the local Accounts fallback —
// the only one of the two sources that owns a session of its own. This is
// what makes the standalone /login form (see handlers_auth.go's
// SessionWriter type assertions) reachable for the escape-hatch path.
//
// SetLanguage and SetActiveTenant are deliberately NOT delegated, even
// though Accounts implements both: views.go's newView gates the
// language/tenant-switcher controls on a single type assertion against
// Server.authn as a whole, not per-identity — so if WordPressFallback
// implemented LanguageWriter/TenantWriter, those controls would appear for
// every WordPress-mode request, including a plain ScopeAdmin one that was
// never touched by the fallback and has no session to persist a preference
// in. That is exactly the "a control that silently does nothing" case
// LanguageWriter's own doc comment says to avoid, so under WordPress mode —
// escape hatch included — neither preference is offered, same as before
// this fallback existed.
func (a *WordPressFallback) Login(w http.ResponseWriter, username, password string) error {
	return a.fallback.Login(w, username, password)
}

func (a *WordPressFallback) Logout(w http.ResponseWriter) {
	a.fallback.Logout(w)
}

func (a *WordPressFallback) LogoutRequest(r *http.Request) {
	a.fallback.LogoutRequest(r)
}

var (
	_ Authenticator = (*WordPressFallback)(nil)
	_ SessionWriter = (*WordPressFallback)(nil)
)
