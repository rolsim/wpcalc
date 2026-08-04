package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rolsim/wpcalc/internal/domain"
)

// fakeSessionStore backs a fallback Accounts authenticator with an
// in-memory session, so the composite's cookie path can be exercised without
// a real database.
type fakeSessionStore struct {
	sessions map[string]int64
	users    map[int64]domain.User
	roles    map[int64][]domain.UserRole
	perms    map[string][]string
}

func (f *fakeSessionStore) Authenticate(context.Context, string, string) (domain.User, error) {
	return domain.User{}, errors.New("not implemented")
}

func (f *fakeSessionStore) SessionByToken(_ context.Context, token string) (domain.User, *int64, error) {
	id, ok := f.sessions[token]
	if !ok {
		return domain.User{}, nil, errors.New("no such session")
	}
	return f.users[id], nil, nil
}

func (f *fakeSessionStore) CreateSession(context.Context, string, int64, time.Time) error { return nil }
func (f *fakeSessionStore) DeleteSession(context.Context, string) error                   { return nil }
func (f *fakeSessionStore) SetUserLanguage(context.Context, int64, string) error          { return nil }
func (f *fakeSessionStore) SetActiveTenant(context.Context, string, *int64) error         { return nil }

func (f *fakeSessionStore) UserRolesForUser(_ context.Context, userID int64) ([]domain.UserRole, error) {
	return f.roles[userID], nil
}

func (f *fakeSessionStore) RolePermissionsFor(_ context.Context, roleIDs []string) (map[string][]string, error) {
	out := make(map[string][]string, len(roleIDs))
	for _, id := range roleIDs {
		out[id] = f.perms[id]
	}
	return out, nil
}

func TestFallbackUnusedWhenWordPressResolves(t *testing.T) {
	// A ScopeAdmin request must never touch the fallback at all — the two
	// modes stay mutually exclusive except for ErrNoLinkedAccount.
	wp := newWP(t)
	fallback := NewAccounts(&fakeSessionStore{})
	a := NewWordPressFallback(wp, fallback)

	r := wpScopedRequest(t, wp, ConnUnix, "alice", "administrator", ScopeAdmin, time.Now())
	id, err := a.Identify(r)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	if !id.FullAccess {
		t.Errorf("got %+v, want FullAccess admin identity", id)
	}
}

func TestFallbackUsedWhenNoAccountLinked(t *testing.T) {
	empID := int64(10)
	sessionStore := &fakeSessionStore{
		sessions: map[string]int64{"tok123": 5},
		users:    map[int64]domain.User{5: {ID: 5, Username: "localbob"}},
		roles:    map[int64][]domain.UserRole{5: {{RoleID: "viewer", EmployeeID: &empID}}},
		perms:    map[string][]string{"viewer": {"read"}},
	}
	wp := newWP(t).WithStore(&fakeScopedStore{}) // store configured, but no matching account
	fallback := NewAccounts(sessionStore)
	a := NewWordPressFallback(wp, fallback)

	// No wpcalc session cookie at all: falls through WordPress's
	// ErrNoLinkedAccount into Accounts.Identify, which then reports
	// ErrUnauthenticated for the missing cookie — the caller (httpx's
	// requireAuth) is the one that turns that into a /login redirect.
	r := wpScopedRequest(t, wp, ConnUnix, "alice", "", ScopeSelf, time.Now())
	if _, err := a.Identify(r); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("Identify with no cookie = %v, want ErrUnauthenticated", err)
	}

	// A valid local session cookie present on the same unresolved-WP-user
	// request succeeds via the fallback.
	r = wpScopedRequest(t, wp, ConnUnix, "alice", "", ScopeSelf, time.Now())
	r.AddCookie(&http.Cookie{Name: CookieName, Value: "tok123"})
	id, err := a.Identify(r)
	if err != nil {
		t.Fatalf("Identify with valid session cookie: %v", err)
	}
	if id.FullAccess {
		t.Error("fallback local identity must not be FullAccess")
	}
	if id.Username != "localbob" {
		t.Errorf("got %+v, want the local account localbob", id)
	}
	if !id.Can("read", empID, 999) {
		t.Error("fallback identity's own role did not carry through")
	}
}

func TestFallbackSessionWriterDelegatesToAccounts(t *testing.T) {
	wp := newWP(t)
	fallback := NewAccounts(&fakeSessionStore{})
	a := NewWordPressFallback(wp, fallback)

	w := httptest.NewRecorder()
	a.Logout(w) // must not panic and must delegate, not no-op like WordPress alone
	if len(w.Result().Cookies()) == 0 {
		t.Error("Logout did not clear the fallback's session cookie")
	}
}
