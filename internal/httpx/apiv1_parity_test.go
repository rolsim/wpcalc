package httpx

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/rolsim/wpcalc/internal/domain"
)

// bearerFor issues a real API token for an existing account — unlike
// bearer(t, username), which also creates the account and grants it
// super_admin, this is for tests that already control the account (e.g. a
// freshly created non-admin user) and just need a token for it.
func (ts *testServer) bearerFor(t *testing.T, userID int64, name string) string {
	t.Helper()
	token, _, _, err := ts.db.CreateAPIToken(t.Context(), userID, name)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	return token
}

func TestAPIv1UpdateTenantRenames(t *testing.T) {
	ts := newTestServer(t, nil)
	token := ts.bearer(t, "api-admin")

	w := ts.apiDo(t, http.MethodPatch, "/api/v1/tenants/1", token, `{"name":"Acme Renamed"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", w.Code, w.Body.String())
	}
	tenant, err := ts.db.Tenant(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if tenant.Name != "Acme Renamed" {
		t.Fatalf("tenant name = %q, want %q", tenant.Name, "Acme Renamed")
	}
}

func TestAPIv1CreateUserRequiresManageUsers(t *testing.T) {
	ts := newTestServer(t, nil)
	empID := ts.employee(t, "Anna", "2026-01-01", "")
	uid, err := ts.db.CreateUserWeak(t.Context(), "viewer-api", "x", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.db.GrantUserRole(t.Context(), uid, nil, &empID, domain.RoleViewer); err != nil {
		t.Fatal(err)
	}
	token := ts.bearerFor(t, uid, "test")

	w := ts.apiDo(t, http.MethodPost, "/api/v1/users", token, `{"username":"newperson","password":"correcthorsebattery"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403: %s", w.Code, w.Body.String())
	}
}

func TestAPIv1CreateAndListUsers(t *testing.T) {
	ts := newTestServer(t, nil)
	token := ts.bearer(t, "api-admin")

	w := ts.apiDo(t, http.MethodPost, "/api/v1/users", token, `{"username":"bob","password":"correcthorsebattery"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: status %d: %s", w.Code, w.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created["username"] != "bob" {
		t.Fatalf("created = %v", created)
	}
	if _, ok := created["password"]; ok {
		t.Fatalf("response leaked a password field: %v", created)
	}

	w = ts.apiGet(t, "/api/v1/users", token)
	if w.Code != http.StatusOK {
		t.Fatalf("list: status %d: %s", w.Code, w.Body.String())
	}
	var users []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &users); err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 { // the bearer's own admin account + bob
		t.Fatalf("users = %v, want 2", users)
	}
	for _, u := range users {
		if _, ok := u["passwordHash"]; ok {
			t.Fatalf("user listing leaked a password hash: %v", u)
		}
	}
}

func TestAPIv1UserCanActOnItsOwnAccountOnly(t *testing.T) {
	ts := newTestServer(t, nil)
	adminToken := ts.bearer(t, "api-admin")

	bobID, err := ts.db.CreateUserWeak(t.Context(), "bob", "x", true)
	if err != nil {
		t.Fatal(err)
	}
	bobToken := ts.bearerFor(t, bobID, "bob-token")

	// bob may change his own password and language.
	if w := ts.apiDo(t, http.MethodPut, "/api/v1/users/bob/password", bobToken, `{"password":"anothergoodpassword"}`); w.Code != http.StatusNoContent {
		t.Fatalf("bob->bob password: status %d: %s", w.Code, w.Body.String())
	}
	if w := ts.apiDo(t, http.MethodPut, "/api/v1/users/bob/language", bobToken, `{"lang":"de-CH"}`); w.Code != http.StatusNoContent {
		t.Fatalf("bob->bob language: status %d: %s", w.Code, w.Body.String())
	}
	if w := ts.apiGet(t, "/api/v1/users/bob/roles", bobToken); w.Code != http.StatusOK {
		t.Fatalf("bob->bob roles: status %d: %s", w.Code, w.Body.String())
	}

	// bob may not act on api-admin's account.
	if w := ts.apiDo(t, http.MethodPut, "/api/v1/users/api-admin/password", bobToken, `{"password":"whatever12345"}`); w.Code != http.StatusForbidden {
		t.Fatalf("bob->admin password: status %d, want 403: %s", w.Code, w.Body.String())
	}
	if w := ts.apiDo(t, http.MethodPut, "/api/v1/users/api-admin/language", bobToken, `{"lang":"en"}`); w.Code != http.StatusForbidden {
		t.Fatalf("bob->admin language: status %d, want 403: %s", w.Code, w.Body.String())
	}
	if w := ts.apiGet(t, "/api/v1/users/api-admin/roles", bobToken); w.Code != http.StatusForbidden {
		t.Fatalf("bob->admin roles: status %d, want 403: %s", w.Code, w.Body.String())
	}

	// The system-wide admin may act on bob's account too.
	if w := ts.apiDo(t, http.MethodPut, "/api/v1/users/bob/password", adminToken, `{"password":"adminsetthispassword"}`); w.Code != http.StatusNoContent {
		t.Fatalf("admin->bob password: status %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIv1UnknownUsernameDoesNotLeakExistenceToANonAdmin(t *testing.T) {
	// canActOnAccount decides purely by name comparison, with no database
	// lookup, specifically so this never becomes a username-enumeration
	// oracle: a non-admin naming a real account and a non-admin naming a
	// fictional one must get the identical response.
	ts := newTestServer(t, nil)
	bobID, err := ts.db.CreateUserWeak(t.Context(), "bob", "x", true)
	if err != nil {
		t.Fatal(err)
	}
	bobToken := ts.bearerFor(t, bobID, "bob-token")

	realW := ts.apiDo(t, http.MethodPut, "/api/v1/users/api-admin-does-not-exist-yet/password", bobToken, `{"password":"whatever12345"}`)
	fakeW := ts.apiDo(t, http.MethodPut, "/api/v1/users/totally-made-up-name/password", bobToken, `{"password":"whatever12345"}`)
	if realW.Code != http.StatusForbidden || fakeW.Code != http.StatusForbidden {
		t.Fatalf("statuses = %d, %d, want 403, 403", realW.Code, fakeW.Code)
	}
}

func TestAPIv1TokenSelfService(t *testing.T) {
	ts := newTestServer(t, nil)
	bobID, err := ts.db.CreateUserWeak(t.Context(), "bob", "x", true)
	if err != nil {
		t.Fatal(err)
	}
	firstToken := ts.bearerFor(t, bobID, "bootstrap")

	w := ts.apiDo(t, http.MethodPost, "/api/v1/tokens", firstToken, `{"name":"second"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: status %d: %s", w.Code, w.Body.String())
	}
	var created struct {
		AccessTokenId int64  `json:"accessTokenId"`
		AccessToken   string `json:"accessToken"`
		RefreshToken  string `json:"refreshToken"`
		Name          string `json:"name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.AccessToken == "" || !strings.HasPrefix(created.AccessToken, "wpat_") {
		t.Fatalf("created access token = %+v", created)
	}
	if created.RefreshToken == "" || !strings.HasPrefix(created.RefreshToken, "wprt_") {
		t.Fatalf("created refresh token = %+v", created)
	}

	w = ts.apiGet(t, "/api/v1/tokens", firstToken)
	if w.Code != http.StatusOK {
		t.Fatalf("list: status %d: %s", w.Code, w.Body.String())
	}
	var tokens []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &tokens); err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 {
		t.Fatalf("tokens = %v, want 2 (bootstrap + second)", tokens)
	}
	for _, tk := range tokens {
		if _, ok := tk["token"]; ok {
			t.Fatalf("token listing leaked a secret: %v", tk)
		}
		if _, ok := tk["expiresAt"]; !ok {
			t.Fatalf("token listing missing expiresAt: %v", tk)
		}
	}

	// The new token authenticates independently of the one that minted it.
	if w := ts.apiGet(t, "/api/v1/tenants/accessible", created.AccessToken); w.Code != http.StatusOK {
		t.Fatalf("self-minted token doesn't authenticate: status %d: %s", w.Code, w.Body.String())
	}

	// bob revokes it himself.
	revokePath := "/api/v1/tokens/" + strconv.FormatInt(created.AccessTokenId, 10)
	if w := ts.apiDo(t, http.MethodDelete, revokePath, firstToken, ""); w.Code != http.StatusNoContent {
		t.Fatalf("revoke: status %d: %s", w.Code, w.Body.String())
	}
	if w := ts.apiGet(t, "/api/v1/tenants/accessible", created.AccessToken); w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token still works: status %d", w.Code)
	}
}

func TestAPIv1RefreshTokenFlow(t *testing.T) {
	ts := newTestServer(t, nil)
	bobID, err := ts.db.CreateUserWeak(t.Context(), "bob", "x", true)
	if err != nil {
		t.Fatal(err)
	}
	accessTok, _, _, err := ts.db.CreateAPIToken(t.Context(), bobID, "ci")
	if err != nil {
		t.Fatal(err)
	}
	refreshTok, _, _, err := ts.db.CreateRefreshToken(t.Context(), bobID, "ci")
	if err != nil {
		t.Fatal(err)
	}

	// Public: no Authorization header needed.
	w := ts.apiDo(t, http.MethodPost, "/api/v1/tokens/refresh", "", `{"refreshToken":"`+refreshTok+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("refresh: status %d: %s", w.Code, w.Body.String())
	}
	var pair struct {
		AccessTokenId int64  `json:"accessTokenId"`
		AccessToken   string `json:"accessToken"`
		RefreshToken  string `json:"refreshToken"`
		Name          string `json:"name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pair); err != nil {
		t.Fatal(err)
	}
	if pair.Name != "ci" {
		t.Fatalf("Name = %q, want %q", pair.Name, "ci")
	}
	if pair.RefreshToken == refreshTok {
		t.Fatal("refresh did not rotate — returned the same refresh token")
	}

	// The new access token works; the original one, issued separately, is untouched by the refresh.
	if w := ts.apiGet(t, "/api/v1/tenants/accessible", pair.AccessToken); w.Code != http.StatusOK {
		t.Fatalf("new access token: status %d: %s", w.Code, w.Body.String())
	}
	if w := ts.apiGet(t, "/api/v1/tenants/accessible", accessTok); w.Code != http.StatusOK {
		t.Fatalf("original access token should still work: status %d", w.Code)
	}

	// The spent refresh token cannot be exchanged again.
	w = ts.apiDo(t, http.MethodPost, "/api/v1/tokens/refresh", "", `{"refreshToken":"`+refreshTok+`"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("reused refresh token: status %d, want 401: %s", w.Code, w.Body.String())
	}

	// The rotated one works.
	w = ts.apiDo(t, http.MethodPost, "/api/v1/tokens/refresh", "", `{"refreshToken":"`+pair.RefreshToken+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("rotated refresh token: status %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIv1RevokeAllTokens(t *testing.T) {
	ts := newTestServer(t, nil)
	bobID, err := ts.db.CreateUserWeak(t.Context(), "bob", "x", true)
	if err != nil {
		t.Fatal(err)
	}
	accessTok, _, _, err := ts.db.CreateAPIToken(t.Context(), bobID, "ci")
	if err != nil {
		t.Fatal(err)
	}
	refreshTok, _, _, err := ts.db.CreateRefreshToken(t.Context(), bobID, "ci")
	if err != nil {
		t.Fatal(err)
	}

	w := ts.apiDo(t, http.MethodDelete, "/api/v1/tokens", accessTok, "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("revoke-all: status %d: %s", w.Code, w.Body.String())
	}
	if w := ts.apiGet(t, "/api/v1/tenants/accessible", accessTok); w.Code != http.StatusUnauthorized {
		t.Fatalf("access token survived revoke-all: status %d", w.Code)
	}
	rw := ts.apiDo(t, http.MethodPost, "/api/v1/tokens/refresh", "", `{"refreshToken":"`+refreshTok+`"}`)
	if rw.Code != http.StatusUnauthorized {
		t.Fatalf("refresh token survived revoke-all: status %d: %s", rw.Code, rw.Body.String())
	}
}

func TestAPIv1CannotRevokeAnotherAccountsToken(t *testing.T) {
	ts := newTestServer(t, nil)
	adminToken := ts.bearer(t, "api-admin")

	bobID, err := ts.db.CreateUserWeak(t.Context(), "bob", "x", true)
	if err != nil {
		t.Fatal(err)
	}
	bobToken := ts.bearerFor(t, bobID, "bob-token")

	w := ts.apiGet(t, "/api/v1/tokens", bobToken)
	var tokens []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &tokens); err != nil {
		t.Fatal(err)
	}
	bobsTokenID := int64(tokens[0]["id"].(float64))

	// A system-wide admin still cannot revoke bob's token through the
	// API — ownership is checked before RevokeAPIToken is ever called.
	revokePath := "/api/v1/tokens/" + strconv.FormatInt(bobsTokenID, 10)
	w = ts.apiDo(t, http.MethodDelete, revokePath, adminToken, "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404 (not owned, not merely forbidden): %s", w.Code, w.Body.String())
	}
	if w := ts.apiGet(t, "/api/v1/tenants/accessible", bobToken); w.Code != http.StatusOK {
		t.Fatalf("bob's token was revoked by someone else's request: status %d", w.Code)
	}
}
