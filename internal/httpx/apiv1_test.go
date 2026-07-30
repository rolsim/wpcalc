package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/rolsim/wpcalc/internal/domain"
)

// bearer issues a real API token for a fresh super-admin account, exercising
// the full path: CreateAPIToken -> BearerTokens.Identify -> requireBearerAuth
// -> the generated strict handler -> apiv1.API -> the store. Nothing here is
// stubbed, unlike the cookie-session tests elsewhere in this package.
func (ts *testServer) bearer(t *testing.T, username string) string {
	t.Helper()
	uid, err := ts.db.CreateUserWeak(t.Context(), username, "x", true)
	if err != nil {
		t.Fatalf("CreateUserWeak: %v", err)
	}
	if err := ts.db.GrantUserRole(t.Context(), uid, nil, nil, domain.RoleSuperAdmin); err != nil {
		t.Fatalf("GrantUserRole: %v", err)
	}
	token, _, err := ts.db.CreateAPIToken(t.Context(), uid, "test")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	return token
}

func (ts *testServer) apiGet(t *testing.T, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	return ts.apiDo(t, http.MethodGet, path, token, "")
}

func (ts *testServer) apiDo(t *testing.T, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	ts.handler.ServeHTTP(w, r)
	return w
}

func TestAPIv1HealthzIsPublic(t *testing.T) {
	ts := newTestServer(t, nil)
	w := ts.apiGet(t, "/api/v1/healthz", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"ok"`) {
		t.Fatalf("body = %s", w.Body.String())
	}
}

func TestAPIv1SpecEndpointsArePublic(t *testing.T) {
	ts := newTestServer(t, nil)
	paths := []string{
		"/api/v1/openapi.json", "/api/v1/openapi.yaml", "/api/v1/openapi.html",
		"/api/v1/openapi-assets/swagger-ui-bundle.js",
		"/api/v1/openapi-assets/swagger-ui-standalone-preset.js",
		"/api/v1/openapi-assets/swagger-ui.css",
	}
	for _, path := range paths {
		if w := ts.apiGet(t, path, ""); w.Code != http.StatusOK {
			t.Errorf("%s: status %d", path, w.Code)
		}
	}
}

// TestAppSpecEndpointsArePublic covers the HTML app's own OpenAPI document
// — a separate, larger spec from /api/v1's, served at the root rather than
// under /api/v1, and requiring no session either.
func TestAppSpecEndpointsArePublic(t *testing.T) {
	ts := newTestServer(t, nil)
	paths := []string{
		"/openapi.json", "/openapi.yaml", "/openapi.html",
		"/openapi-assets/swagger-ui-bundle.js",
		"/openapi-assets/swagger-ui-standalone-preset.js",
		"/openapi-assets/swagger-ui.css",
	}
	for _, path := range paths {
		w := ts.apiGet(t, path, "")
		if w.Code != http.StatusOK {
			t.Errorf("%s: status %d", path, w.Code)
		}
	}
	if w := ts.apiGet(t, "/openapi.json", ""); !strings.Contains(w.Body.String(), "wpcalc HTTP surface") {
		t.Errorf("/openapi.json body missing expected title: %s", w.Body.String()[:200])
	}
	// The Swagger UI page must point at the sibling spec/assets URLs
	// relatively, so it resolves correctly whether served at /openapi.html
	// or (for apiv1's own copy) at /api/v1/openapi.html.
	if w := ts.apiGet(t, "/openapi.html", ""); !strings.Contains(w.Body.String(), `url: "openapi.json"`) {
		t.Errorf("/openapi.html does not reference openapi.json relatively: %s", w.Body.String())
	}
}

func TestAPIv1RejectsRequestsWithoutABearerToken(t *testing.T) {
	ts := newTestServer(t, nil)
	w := ts.apiGet(t, "/api/v1/tenants", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401: %s", w.Code, w.Body.String())
	}
}

func TestAPIv1RejectsAnInvalidBearerToken(t *testing.T) {
	ts := newTestServer(t, nil)
	w := ts.apiGet(t, "/api/v1/tenants", "wpat_not-a-real-token")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401: %s", w.Code, w.Body.String())
	}
}

func TestAPIv1ListTenantsWithARealToken(t *testing.T) {
	ts := newTestServer(t, nil)
	token := ts.bearer(t, "api-admin")

	w := ts.apiGet(t, "/api/v1/tenants", token)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", w.Code, w.Body.String())
	}
	var tenants []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &tenants); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tenants) == 0 || tenants[0]["name"] != "Default" {
		t.Fatalf("tenants = %v", tenants)
	}
}

func TestAPIv1EmployeeScopedTokenCannotListTenants(t *testing.T) {
	ts := newTestServer(t, nil)
	empID := ts.employee(t, "Anna", "2026-01-01", "")

	uid, err := ts.db.CreateUserWeak(t.Context(), "viewer-api", "x", true)
	if err != nil {
		t.Fatalf("CreateUserWeak: %v", err)
	}
	if err := ts.db.GrantUserRole(t.Context(), uid, nil, &empID, domain.RoleViewer); err != nil {
		t.Fatalf("GrantUserRole: %v", err)
	}
	token, _, err := ts.db.CreateAPIToken(t.Context(), uid, "test")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	if w := ts.apiGet(t, "/api/v1/tenants", token); w.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403: %s", w.Code, w.Body.String())
	}

	// But the viewer can read the grid, restricted to their one employee.
	w := ts.apiGet(t, "/api/v1/tenants/1/months/2026-07", token)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", w.Code, w.Body.String())
	}
	var grid struct {
		Employees []map[string]any `json:"employees"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &grid); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(grid.Employees) != 1 || int64(grid.Employees[0]["id"].(float64)) != empID {
		t.Fatalf("employees = %v", grid.Employees)
	}
}

func TestAPIv1ListAccessibleTenantsForASuperAdminIsEveryTenant(t *testing.T) {
	ts := newTestServer(t, nil)
	token := ts.bearer(t, "api-admin")
	if _, err := ts.db.CreateTenant(t.Context(), "Acme"); err != nil {
		t.Fatal(err)
	}

	w := ts.apiGet(t, "/api/v1/tenants/accessible", token)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", w.Code, w.Body.String())
	}
	var tenants []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &tenants); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tenants) != 2 {
		t.Fatalf("tenants = %v, want 2 (Default + Acme)", tenants)
	}
}

// TestAPIv1ListAccessibleTenantsIsScopedForANonAdminToken is the reason
// this endpoint exists: unlike GET /api/v1/tenants (manage_tenants,
// system-wide only, 403 for a non-admin — see
// TestAPIv1EmployeeScopedTokenCannotListTenants above), this one requires
// no particular permission and answers with exactly what the caller can
// reach — letting a tenant- or employee-scope token discover its own
// tenantId without already knowing it, and never leaking a tenant it
// cannot reach.
func TestAPIv1ListAccessibleTenantsIsScopedForANonAdminToken(t *testing.T) {
	ts := newTestServer(t, nil)
	empID := ts.employee(t, "Anna", "2026-01-01", "")
	otherTenantID, err := ts.db.CreateTenant(t.Context(), "Acme")
	if err != nil {
		t.Fatal(err)
	}

	uid, err := ts.db.CreateUserWeak(t.Context(), "viewer-api", "x", true)
	if err != nil {
		t.Fatalf("CreateUserWeak: %v", err)
	}
	if err := ts.db.GrantUserRole(t.Context(), uid, nil, &empID, domain.RoleViewer); err != nil {
		t.Fatalf("GrantUserRole: %v", err)
	}
	token, _, err := ts.db.CreateAPIToken(t.Context(), uid, "test")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	w := ts.apiGet(t, "/api/v1/tenants/accessible", token)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", w.Code, w.Body.String())
	}
	var tenants []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &tenants); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tenants) != 1 || tenants[0]["name"] != "Default" {
		t.Fatalf("tenants = %v, want only the Default tenant (not %d)", tenants, otherTenantID)
	}
}

func TestAPIv1ListAccessibleTenantsIsEmptyForATokenWithNoRoles(t *testing.T) {
	ts := newTestServer(t, nil)
	uid, err := ts.db.CreateUserWeak(t.Context(), "nobody", "x", true)
	if err != nil {
		t.Fatalf("CreateUserWeak: %v", err)
	}
	token, _, err := ts.db.CreateAPIToken(t.Context(), uid, "test")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	w := ts.apiGet(t, "/api/v1/tenants/accessible", token)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", w.Code, w.Body.String())
	}
	if body := strings.TrimSpace(w.Body.String()); body != "[]" {
		t.Fatalf("body = %s, want []", body)
	}
}

func TestAPIv1RevokedTokenStopsWorking(t *testing.T) {
	ts := newTestServer(t, nil)
	uid, err := ts.db.CreateUserWeak(t.Context(), "revokee", "x", true)
	if err != nil {
		t.Fatalf("CreateUserWeak: %v", err)
	}
	if err := ts.db.GrantUserRole(t.Context(), uid, nil, nil, domain.RoleSuperAdmin); err != nil {
		t.Fatalf("GrantUserRole: %v", err)
	}
	token, id, err := ts.db.CreateAPIToken(t.Context(), uid, "test")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	if w := ts.apiGet(t, "/api/v1/tenants", token); w.Code != http.StatusOK {
		t.Fatalf("before revoke: status %d", w.Code)
	}
	if err := ts.db.RevokeAPIToken(t.Context(), id); err != nil {
		t.Fatalf("RevokeAPIToken: %v", err)
	}
	if w := ts.apiGet(t, "/api/v1/tenants", token); w.Code != http.StatusUnauthorized {
		t.Fatalf("after revoke: status %d, want 401", w.Code)
	}
}

func TestAPIv1SessionCookieDoesNotAuthenticateTheAPI(t *testing.T) {
	// The HTML app's requireAuth and /api/v1's requireBearerAuth are
	// separate middlewares over separate authenticators — a session cookie
	// must not double as a bearer credential, and a bearer token must not
	// work against the HTML routes either (not exercised here, but the
	// separation is symmetric by construction: two distinct Authenticator
	// instances, never shared).
	ts := newTestServer(t, nil) // default authn: stubAuth with an admin identity, no cookie involved
	r := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	r.AddCookie(&http.Cookie{Name: "wpcalc_session", Value: "irrelevant"})
	w := httptest.NewRecorder()
	ts.handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401: %s", w.Code, w.Body.String())
	}
}

// The remaining tests cover the runtime request/response validation layer
// (apiv1.RequestValidator / (*apiv1.API).ResponseValidator) — the gap where
// oapi-codegen's generated code alone only decodes JSON structurally and
// type-coerces path params, without enforcing `required`, `pattern`,
// `enum`, or any other constraint openapi.yaml declares, and answers
// malformed requests in plain text rather than the documented Error shape.

func TestAPIv1MalformedJSONBodyIsRejectedAsJSON(t *testing.T) {
	ts := newTestServer(t, nil)
	token := ts.bearer(t, "api-admin")

	w := ts.apiDo(t, http.MethodPost, "/api/v1/tenants", token, `{not valid json`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, w.Body.String())
	}
	if body["error"] != "invalid_request" {
		t.Fatalf("error = %v, want invalid_request", body["error"])
	}
}

func TestAPIv1MissingRequiredFieldIsRejectedBeforeReachingTheHandler(t *testing.T) {
	ts := newTestServer(t, nil)
	token := ts.bearer(t, "api-admin")

	// {} lacks TenantCreate's required "name" — the request validator must
	// catch this itself; a handler-level check (domain.ValidTenantName)
	// exists too, but this test is specifically about the layer in front
	// of it never letting the request through in the first place.
	w := ts.apiDo(t, http.MethodPost, "/api/v1/tenants", token, `{}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
	}
	tenants, err := ts.db.Tenants(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(tenants) != 1 { // just the seeded "Default" tenant — nothing created
		t.Fatalf("tenants = %v, want only the seed tenant", tenants)
	}
}

func TestAPIv1PathParamViolatingItsPatternIsRejected(t *testing.T) {
	ts := newTestServer(t, nil)
	token := ts.bearer(t, "api-admin")

	// roleId must match ^[a-z0-9_]+$ — this must never reach the store.
	w := ts.apiDo(t, http.MethodDelete, "/api/v1/roles/HAS%20SPACES!!", token, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestAPIv1NonIntegerPathParamIsRejected(t *testing.T) {
	ts := newTestServer(t, nil)
	token := ts.bearer(t, "api-admin")

	w := ts.apiGet(t, "/api/v1/tenants/not-a-number", token)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestAPIv1UnmatchedPathReturns404AsJSON(t *testing.T) {
	ts := newTestServer(t, nil)
	token := ts.bearer(t, "api-admin")

	w := ts.apiGet(t, "/api/v1/nonexistent", token)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
}

func TestAPIv1WriteEndpointsPassResponseValidation(t *testing.T) {
	// (*apiv1.API).ResponseValidator buffers and validates every JSON
	// response against openapi.yaml; a mismatch would turn a real 200/201
	// into a 500. This walks the write endpoints most likely to drift from
	// their schema (varying status codes: 200, 201, 204) end to end,
	// through the real validator, not a stub.
	ts := newTestServer(t, nil)
	token := ts.bearer(t, "api-admin")
	empID := ts.employee(t, "Anna", "2026-01-01", "")

	if w := ts.apiDo(t, http.MethodPost, "/api/v1/tenants", token, `{"name":"Acme"}`); w.Code != http.StatusCreated {
		t.Fatalf("create tenant: status %d: %s", w.Code, w.Body.String())
	}
	body := `{"employeeId":` + strconv.FormatInt(empID, 10) + `,"date":"2026-07-14","hours":"7.75"}`
	if w := ts.apiDo(t, http.MethodPut, "/api/v1/tenants/1/months/2026-07/entries", token, body); w.Code != http.StatusOK {
		t.Fatalf("set hours: status %d: %s", w.Code, w.Body.String())
	}
	if w := ts.apiDo(t, http.MethodPut, "/api/v1/tenants/1/months/2026-07/comment", token, `{"date":"2026-07-14","comment":"x"}`); w.Code != http.StatusNoContent {
		t.Fatalf("set comment: status %d: %s", w.Code, w.Body.String())
	}
	if w := ts.apiDo(t, http.MethodPost, "/api/v1/roles", token, `{"id":"auditor","name":"Auditor","scope":"tenant"}`); w.Code != http.StatusCreated {
		t.Fatalf("create role: status %d: %s", w.Code, w.Body.String())
	}
}
