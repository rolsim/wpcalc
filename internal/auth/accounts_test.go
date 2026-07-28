package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"source.simonet.internal/rolsim/wpcalc/internal/domain"
)

// fakeUserStore lets the account authenticator be tested without a database.
type fakeUserStore struct {
	users    map[string]domain.User
	langs    map[int64]string
	password string
	sessions map[string]int64
	expiries map[string]time.Time
	failNext error
}

func newFakeStore() *fakeUserStore {
	return &fakeUserStore{
		users: map[string]domain.User{
			"alice": {ID: 1, Username: "alice", Role: domain.RoleAdmin},
			"bob":   {ID: 2, Username: "bob", Role: domain.RoleUser},
		},
		password: "correct-horse-battery",
		sessions: map[string]int64{},
		expiries: map[string]time.Time{},
		langs:    map[int64]string{},
	}
}

func (f *fakeUserStore) Authenticate(_ context.Context, username, password string) (domain.User, error) {
	u, ok := f.users[username]
	if !ok || password != f.password {
		return domain.User{}, errors.New("no such user or bad password")
	}
	return u, nil
}

func (f *fakeUserStore) SessionUser(_ context.Context, token string) (domain.User, error) {
	id, ok := f.sessions[token]
	if !ok {
		return domain.User{}, errors.New("no session")
	}
	if exp, ok := f.expiries[token]; ok && time.Now().After(exp) {
		return domain.User{}, errors.New("expired")
	}
	for _, u := range f.users {
		if u.ID == id {
			return u, nil
		}
	}
	return domain.User{}, errors.New("orphan session")
}

func (f *fakeUserStore) CreateSession(_ context.Context, token string, userID int64, expires time.Time) error {
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		return err
	}
	f.sessions[token] = userID
	f.expiries[token] = expires
	return nil
}

func (f *fakeUserStore) SetUserLanguage(_ context.Context, userID int64, lang string) error {
	for name, u := range f.users {
		if u.ID == userID {
			u.Language = lang
			f.users[name] = u
			f.langs[userID] = lang
			return nil
		}
	}
	return errors.New("no such user")
}

func (f *fakeUserStore) DeleteSession(_ context.Context, token string) error {
	delete(f.sessions, token)
	delete(f.expiries, token)
	return nil
}

func TestAccountsLoginIssuesServerSideSession(t *testing.T) {
	store := newFakeStore()
	a := NewAccounts(store)

	w := httptest.NewRecorder()
	if err := a.Login(w, "alice", store.password); err != nil {
		t.Fatalf("Login: %v", err)
	}

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	c := cookies[0]
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Errorf("session cookie is not HttpOnly+Lax: %+v", c)
	}
	// The cookie must carry an opaque token, not anything about the account.
	if c.Value == "" || len(c.Value) < 32 {
		t.Errorf("session token %q is too short to be unguessable", c.Value)
	}
	if c.Value == "alice" || c.Value == "1" {
		t.Error("session token encodes the identity directly")
	}
	if len(store.sessions) != 1 {
		t.Error("no session was recorded server-side")
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(c)
	id, err := a.Identify(r)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	if id.Username != "alice" || !id.IsAdmin() {
		t.Errorf("got %+v, want alice as admin", id)
	}
}

func TestAccountsLoginRejectsBadCredentials(t *testing.T) {
	store := newFakeStore()
	a := NewAccounts(store)

	for _, c := range []struct{ user, pass string }{
		{"alice", "wrong"},
		{"nobody", store.password},
		{"", ""},
	} {
		w := httptest.NewRecorder()
		if err := a.Login(w, c.user, c.pass); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("Login(%q,%q) = %v, want ErrUnauthenticated", c.user, c.pass, err)
		}
		if len(w.Result().Cookies()) != 0 {
			t.Errorf("Login(%q,%q) issued a cookie despite failing", c.user, c.pass)
		}
	}
	if len(store.sessions) != 0 {
		t.Error("a failed login created a session")
	}
}

func TestAccountsTokensAreDistinct(t *testing.T) {
	// Two logins must never collide, or one user could land in another's
	// session.
	store := newFakeStore()
	a := NewAccounts(store)
	seen := make(map[string]bool)
	for range 50 {
		w := httptest.NewRecorder()
		if err := a.Login(w, "alice", store.password); err != nil {
			t.Fatal(err)
		}
		tok := w.Result().Cookies()[0].Value
		if seen[tok] {
			t.Fatalf("duplicate session token %q", tok)
		}
		seen[tok] = true
	}
}

func TestAccountsRoleIsTakenFromTheStoreNotTheCookie(t *testing.T) {
	// Privilege must come from the record, so it cannot be raised by editing
	// anything the browser holds.
	store := newFakeStore()
	a := NewAccounts(store)

	w := httptest.NewRecorder()
	if err := a.Login(w, "bob", store.password); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(w.Result().Cookies()[0])

	id, err := a.Identify(r)
	if err != nil {
		t.Fatal(err)
	}
	if id.IsAdmin() {
		t.Error("a plain user was identified as admin")
	}

	// Promote in the store; the same cookie must now report admin.
	store.users["bob"] = domain.User{ID: 2, Username: "bob", Role: domain.RoleAdmin}
	if id, _ := a.Identify(r); !id.IsAdmin() {
		t.Error("role change in the store did not reach the identity")
	}
}

func TestAccountsIdentifyRejectsUnknownAndExpiredTokens(t *testing.T) {
	store := newFakeStore()
	a := NewAccounts(store)

	for _, v := range []string{"", "made-up-token", "../../etc/passwd"} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(&http.Cookie{Name: CookieName, Value: v})
		if _, err := a.Identify(r); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("token %q accepted", v)
		}
	}

	// No cookie at all.
	if _, err := a.Identify(httptest.NewRequest(http.MethodGet, "/", nil)); !errors.Is(err, ErrUnauthenticated) {
		t.Error("a request with no cookie was authenticated")
	}

	// Expired.
	store.sessions["old"] = 1
	store.expiries["old"] = time.Now().Add(-time.Minute)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: "old"})
	if _, err := a.Identify(r); !errors.Is(err, ErrUnauthenticated) {
		t.Error("expired session accepted")
	}
}

func TestAccountsLogoutRevokesServerSide(t *testing.T) {
	// Clearing the cookie alone would leave a copied token working, which is
	// not what clicking "log out" is understood to mean.
	store := newFakeStore()
	a := NewAccounts(store)

	w := httptest.NewRecorder()
	if err := a.Login(w, "alice", store.password); err != nil {
		t.Fatal(err)
	}
	c := w.Result().Cookies()[0]

	r := httptest.NewRequest(http.MethodPost, "/logout", nil)
	r.AddCookie(c)
	a.LogoutRequest(r)

	if len(store.sessions) != 0 {
		t.Error("session survived logout on the server")
	}
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.AddCookie(c)
	if _, err := a.Identify(r2); !errors.Is(err, ErrUnauthenticated) {
		t.Error("the old cookie still authenticates after logout")
	}

	// And the cookie itself is expired in the response.
	w2 := httptest.NewRecorder()
	a.Logout(w2)
	got := w2.Result().Cookies()
	if len(got) != 1 || got[0].MaxAge >= 0 || got[0].Value != "" {
		t.Errorf("Logout did not expire the cookie: %+v", got)
	}
}

func TestAccountsLoginSurfacesStoreFailure(t *testing.T) {
	store := newFakeStore()
	store.failNext = errors.New("database is down")
	a := NewAccounts(store)

	w := httptest.NewRecorder()
	err := a.Login(w, "alice", store.password)
	if err == nil {
		t.Fatal("Login reported success despite the session not being stored")
	}
	if len(w.Result().Cookies()) != 0 {
		t.Error("a cookie was issued for a session that was never recorded")
	}
}

func TestIdentityCarriesTheStoredLanguage(t *testing.T) {
	// The preference has to reach the identity, or every handler would need a
	// second query to find out which language to render in.
	store := newFakeStore()
	store.users["alice"] = domain.User{ID: 1, Username: "alice", Role: domain.RoleAdmin, Language: "en"}
	a := NewAccounts(store)

	w := httptest.NewRecorder()
	if err := a.Login(w, "alice", store.password); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(w.Result().Cookies()[0])

	id, err := a.Identify(r)
	if err != nil {
		t.Fatal(err)
	}
	if id.Language != "en" {
		t.Errorf("Identity.Language = %q, want %q", id.Language, "en")
	}
}

func TestSetLanguagePersistsAndClears(t *testing.T) {
	store := newFakeStore()
	a := NewAccounts(store)

	w := httptest.NewRecorder()
	if err := a.Login(w, "alice", store.password); err != nil {
		t.Fatal(err)
	}
	cookie := w.Result().Cookies()[0]

	req := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/language", nil)
		r.AddCookie(cookie)
		return r
	}

	if err := a.SetLanguage(req(), "de-CH"); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}
	if id, _ := a.Identify(req()); id.Language != "de-CH" {
		t.Errorf("after setting: %q, want de-CH", id.Language)
	}

	// The empty value clears it back to following the browser.
	if err := a.SetLanguage(req(), domain.LanguageAuto); err != nil {
		t.Fatalf("SetLanguage(auto): %v", err)
	}
	if id, _ := a.Identify(req()); id.Language != "" {
		t.Errorf("after clearing: %q, want empty", id.Language)
	}
}

func TestSetLanguageRequiresASession(t *testing.T) {
	a := NewAccounts(newFakeStore())
	r := httptest.NewRequest(http.MethodPost, "/language", nil)
	if err := a.SetLanguage(r, "en"); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("SetLanguage with no session: %v, want ErrUnauthenticated", err)
	}
}
