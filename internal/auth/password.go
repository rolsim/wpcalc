package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// CookieName is the standalone session cookie.
const CookieName = "wpcalc_session"

// SessionTTL bounds how long a standalone login lasts.
const SessionTTL = 12 * time.Hour

// Password is the standalone stopgap: one shared password from the
// environment, a signed cookie, no user table.
//
// This is explicitly interim. It exists so P0 is not wide open while the grid
// and the reports — the actual point of the project — get built first, and it
// is replaced by real accounts at P3 behind this same interface.
//
// The signing key is random per process rather than derived from the password.
// That means a restart logs everyone out, which is an acceptable trade for a
// stopgap and avoids the password itself ever being the thing that, if leaked,
// lets someone mint sessions forever.
type Password struct {
	password string
	key      []byte
	now      func() time.Time // injectable for expiry tests
	secure   bool
}

// NewPassword builds the stopgap authenticator. An empty password is refused:
// silently accepting one would leave the instance open while looking configured.
func NewPassword(password string) (*Password, error) {
	if strings.TrimSpace(password) == "" {
		return nil, errors.New("auth: WPCALC_PASSWORD is empty; refusing to start unprotected")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("auth: generate session key: %w", err)
	}
	return &Password{password: password, key: key, now: time.Now}, nil
}

// SetSecureCookies marks issued cookies Secure. Off by default because the
// standalone server is commonly reached over plain HTTP on a LAN, where a
// Secure cookie would simply never be sent and login would appear to fail.
func (a *Password) SetSecureCookies(secure bool) { a.secure = secure }

// Identify validates the session cookie.
func (a *Password) Identify(r *http.Request) (Identity, error) {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return Identity{}, ErrUnauthenticated
	}
	expiry, ok := a.verifyToken(c.Value)
	if !ok {
		return Identity{}, ErrUnauthenticated
	}
	if a.now().After(expiry) {
		return Identity{}, ErrUnauthenticated
	}
	// The stopgap has exactly one account, and it is privileged: there is
	// nobody else to be, and pretending otherwise would invent a role system
	// that P3 then has to unpick.
	return Identity{Username: "admin", Roles: []string{RoleAdmin}}, nil
}

// Login checks the shared password and issues a session cookie. The username
// is ignored — there is only one account — but is accepted so the handler and
// the form do not change shape when real accounts arrive.
func (a *Password) Login(w http.ResponseWriter, _, password string) error {
	// Constant-time so the comparison does not leak the password by timing.
	if subtle.ConstantTimeCompare([]byte(password), []byte(a.password)) != 1 {
		return ErrUnauthenticated
	}
	expiry := a.now().Add(SessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    a.mintToken(expiry),
		Path:     "/",
		Expires:  expiry,
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// Logout clears the session cookie.
func (a *Password) Logout(w http.ResponseWriter) {
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

// mintToken produces "<expiry>.<signature>". The expiry is inside the signed
// message, so a client cannot extend its own session by editing the cookie.
func (a *Password) mintToken(expiry time.Time) string {
	payload := strconv.FormatInt(expiry.Unix(), 10)
	return payload + "." + a.sign(payload)
}

func (a *Password) verifyToken(token string) (time.Time, bool) {
	payload, sig, ok := strings.Cut(token, ".")
	if !ok {
		return time.Time{}, false
	}
	if !hmac.Equal([]byte(sig), []byte(a.sign(payload))) {
		return time.Time{}, false
	}
	unix, err := strconv.ParseInt(payload, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(unix, 0), true
}

func (a *Password) sign(payload string) string {
	m := hmac.New(sha256.New, a.key)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
