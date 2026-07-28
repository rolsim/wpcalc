package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const testSecret = "a-shared-secret-of-sufficient-length"

// wpRequest builds a request carrying a valid signature, arriving over the
// given listener kind.
func wpRequest(t *testing.T, a *WordPress, kind ConnKind, user, roles string, at time.Time) *http.Request {
	t.Helper()
	ts, sig := a.Sign(user, roles, at)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(HeaderUser, user)
	r.Header.Set(HeaderRoles, roles)
	r.Header.Set(HeaderTimestamp, ts)
	r.Header.Set(HeaderSignature, sig)
	return r.WithContext(WithConnKind(r.Context(), kind))
}

func newWP(t *testing.T) *WordPress {
	t.Helper()
	a, err := NewWordPress(testSecret)
	if err != nil {
		t.Fatalf("NewWordPress: %v", err)
	}
	return a
}

func TestSignedHeadersRejectedOverTCP(t *testing.T) {
	// The core of the WordPress bridge's security. Identity arrives as headers,
	// and headers are forgeable by anything that can reach the listener. The
	// signature proves the sender knows the secret; the socket proves it is
	// local. Both are required, so a perfectly valid signature arriving over
	// TCP must still be refused.
	a := newWP(t)
	now := time.Now()

	r := wpRequest(t, a, ConnTCP, "alice", "administrator", now)
	id, err := a.Identify(r)
	if !errors.Is(err, ErrHeadersOverTCP) {
		t.Fatalf("valid signature over TCP: got (%+v, %v), want ErrHeadersOverTCP", id, err)
	}
	if !id.IsZero() {
		t.Errorf("identity leaked despite rejection: %+v", id)
	}

	// The same request over the socket is the accepted case, which proves the
	// rejection above is about the transport and not a broken signature.
	r = wpRequest(t, a, ConnUnix, "alice", "administrator", now)
	id, err = a.Identify(r)
	if err != nil {
		t.Fatalf("valid signature over unix socket: %v", err)
	}
	if id.Username != "alice" || !id.IsAdmin() {
		t.Errorf("got %+v, want alice with admin rights", id)
	}
}

func TestUntaggedContextFailsClosed(t *testing.T) {
	// A context that never passed through the server's middleware must be
	// treated as untrusted, not as trusted-by-default.
	a := newWP(t)
	ts, sig := a.Sign("alice", "administrator", time.Now())
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(HeaderUser, "alice")
	r.Header.Set(HeaderRoles, "administrator")
	r.Header.Set(HeaderTimestamp, ts)
	r.Header.Set(HeaderSignature, sig)

	if _, err := a.Identify(r); err == nil {
		t.Error("untagged context accepted signed headers; must fail closed")
	}
	if got := ConnKindFrom(r.Context()); got != ConnTCP {
		t.Errorf("ConnKindFrom on an untagged context = %q, want %q", got, ConnTCP)
	}
}

func TestWordPressRejectsTamperedHeaders(t *testing.T) {
	a := newWP(t)
	now := time.Now()

	cases := []struct {
		name   string
		mutate func(r *http.Request)
	}{
		{"escalated role", func(r *http.Request) { r.Header.Set(HeaderRoles, "administrator") }},
		{"swapped user", func(r *http.Request) { r.Header.Set(HeaderUser, "mallory") }},
		{"stripped signature", func(r *http.Request) { r.Header.Del(HeaderSignature) }},
		{"garbage signature", func(r *http.Request) { r.Header.Set(HeaderSignature, "zzzz") }},
		{"shifted timestamp", func(r *http.Request) { r.Header.Set(HeaderTimestamp, "1700000000") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := wpRequest(t, a, ConnUnix, "bob", "subscriber", now)
			c.mutate(r)
			if id, err := a.Identify(r); err == nil {
				t.Errorf("tampered request accepted as %+v", id)
			}
		})
	}
}

func TestWordPressRejectsReplayedSignature(t *testing.T) {
	// A captured header set must not be usable forever.
	a := newWP(t)
	stale := time.Now().Add(-SignatureSkew - time.Minute)
	if _, err := a.Identify(wpRequest(t, a, ConnUnix, "alice", "administrator", stale)); err == nil {
		t.Error("stale signature accepted; replay window is not enforced")
	}
	// Clocks drift in both directions, so the future is bounded too.
	future := time.Now().Add(SignatureSkew + time.Minute)
	if _, err := a.Identify(wpRequest(t, a, ConnUnix, "alice", "administrator", future)); err == nil {
		t.Error("far-future signature accepted")
	}
}

func TestWordPressFieldSeparatorPreventsCollision(t *testing.T) {
	// Without a separator that cannot appear in a field, user "a" with roles
	// "b,c" and user "a,b" with roles "c" would sign identically, letting one
	// be substituted for the other.
	a := newWP(t)
	at := time.Now()
	_, sig1 := a.Sign("a", "b,c", at)
	_, sig2 := a.Sign("a,b", "c", at)
	if sig1 == sig2 {
		t.Error("distinct user/role splits produced the same signature")
	}
}

func TestWordPressRequiresStrongSecret(t *testing.T) {
	for _, s := range []string{"", "   ", "short", "0123456789012345"[:15]} {
		if _, err := NewWordPress(s); err == nil {
			t.Errorf("NewWordPress(%q) accepted a weak secret", s)
		}
	}
	if _, err := NewWordPress(testSecret); err != nil {
		t.Errorf("NewWordPress rejected a good secret: %v", err)
	}
}

func TestIdentityHelpers(t *testing.T) {
	if !(Identity{Username: "a", Roles: []string{RoleAdmin}}).IsAdmin() {
		t.Error("admin role not recognised")
	}
	// WordPress spells it differently; both modes must agree on who is privileged.
	if !(Identity{Username: "a", Roles: []string{"administrator"}}).IsAdmin() {
		t.Error("WordPress administrator role not recognised")
	}
	if (Identity{Username: "a", Roles: []string{RoleUser}}).IsAdmin() {
		t.Error("plain user treated as admin")
	}
	if !(Identity{}).IsZero() {
		t.Error("zero identity not reported as zero")
	}
}

func TestIdentityContextRoundTrip(t *testing.T) {
	ctx := WithIdentity(t.Context(), Identity{Username: "alice", Roles: []string{RoleAdmin}})
	id, ok := IdentityFrom(ctx)
	if !ok || id.Username != "alice" {
		t.Errorf("IdentityFrom = (%+v, %v)", id, ok)
	}
	if _, ok := IdentityFrom(t.Context()); ok {
		t.Error("IdentityFrom reported an identity on a bare context")
	}
}
