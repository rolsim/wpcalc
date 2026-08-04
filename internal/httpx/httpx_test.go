package httpx

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rolsim/wpcalc/internal/auth"
	"github.com/rolsim/wpcalc/internal/domain"
	"github.com/rolsim/wpcalc/internal/i18n"
	"github.com/rolsim/wpcalc/internal/store"
)

// stubAuth stands in for a real authenticator so handler tests exercise the
// handlers rather than a login flow.
type stubAuth struct{ id auth.Identity }

func (s stubAuth) Identify(*http.Request) (auth.Identity, error) {
	if s.id.IsZero() {
		return auth.Identity{}, auth.ErrUnauthenticated
	}
	return s.id, nil
}

// tenantOnePtr points at tenant 1, the "Default" tenant every fresh migrated
// database seeds — every test here operates within it unless it says
// otherwise.
func tenantOnePtr() *int64 { v := int64(1); return &v }

// superAdminPermissions mirrors migration 00004's seed data for the
// super_admin role, so stub identities exercise the same permission set a
// real one would resolve to.
var superAdminPermissions = []string{
	"manage_tenants", "manage_roles", "manage_employees", "manage_users",
	"read", "print", "write",
}

// adminIdentity is the default test identity: system-wide access, tenant 1
// already active so grid/employees/reports tests never hit the
// tenant-chooser redirect.
func adminIdentity() auth.Identity {
	return adminIdentityWithLanguage("")
}

func adminIdentityWithLanguage(lang string) auth.Identity {
	return auth.Identity{
		Username:        "tester",
		Language:        lang,
		ActiveTenantID:  tenantOnePtr(),
		UserRoles:       []domain.UserRole{{RoleID: "super_admin"}},
		RolePermissions: map[string][]string{"super_admin": superAdminPermissions},
	}
}

type testServer struct {
	*Server
	db      *store.DB
	handler http.Handler
}

func newTestServer(t *testing.T, authn auth.Authenticator) *testServer {
	t.Helper()

	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	bundle, err := i18n.New()
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}

	if authn == nil {
		authn = stubAuth{id: adminIdentity()}
	}

	srv, err := New(Config{
		DB:     db,
		Bundle: bundle,
		Auth:   authn,
		// Discard logs so a passing run stays readable.
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &testServer{Server: srv, db: db, handler: srv.Handler()}
}

func (ts *testServer) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	ts.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func (ts *testServer) post(t *testing.T, path string, form url.Values, json bool) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if json {
		r.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
	w := httptest.NewRecorder()
	ts.handler.ServeHTTP(w, r)
	return w
}

func (ts *testServer) employee(t *testing.T, name, start, end string) int64 {
	t.Helper()
	e := domain.Employee{TenantID: 1, DisplayName: name}
	d, err := domain.ParseDate(start)
	if err != nil {
		t.Fatal(err)
	}
	e.StartDate = d
	if end != "" {
		x, err := domain.ParseDate(end)
		if err != nil {
			t.Fatal(err)
		}
		e.EndDate = &x
	}
	id, err := ts.db.CreateEmployee(t.Context(), e)
	if err != nil {
		t.Fatalf("CreateEmployee: %v", err)
	}
	return id
}

func TestUnauthenticatedIsRedirectedToLogin(t *testing.T) {
	ts := newTestServer(t, stubAuth{})
	w := ts.get(t, "/m/2026-07")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}
}

func TestUnauthenticatedXHRGets401NotRedirect(t *testing.T) {
	// Redirecting an XHR to an HTML login page yields a 200 full of markup,
	// which the caller cannot distinguish from success.
	ts := newTestServer(t, stubAuth{})
	r := httptest.NewRequest(http.MethodPost, "/m/2026-07/hours", nil)
	r.Header.Set("X-Requested-With", "XMLHttpRequest")
	w := httptest.NewRecorder()
	ts.handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", w.Code)
	}
}

func TestHealthzNeedsNoAuth(t *testing.T) {
	// The PHP shim polls this to decide whether to restart the sidecar; if it
	// required a session the shim could never see a healthy process.
	ts := newTestServer(t, stubAuth{})
	if w := ts.get(t, "/healthz"); w.Code != http.StatusOK {
		t.Errorf("status %d, want 200", w.Code)
	}
}

func TestGridRendersMonthWithEmployeesAndTotals(t *testing.T) {
	ts := newTestServer(t, nil)
	alice := ts.employee(t, "Alice Muster", "2026-01-01", "")
	day, _ := domain.ParseDate("2026-07-14")
	if err := ts.db.SetHours(t.Context(), alice, day, 775); err != nil {
		t.Fatal(err)
	}

	w := ts.get(t, "/m/2026-07")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", w.Code)
	}
	body := w.Body.String()

	for _, want := range []string{"Alice Muster", "Juli 2026", "7.75", "14.07.2026"} {
		if !strings.Contains(body, want) {
			t.Errorf("grid does not contain %q", want)
		}
	}
	// July 2026 has 31 days; every one must be a row.
	if got := strings.Count(body, `scope="row"`); got < 31 {
		t.Errorf("found %d day rows, want at least 31", got)
	}
}

func TestGridOmitsEmployeesNotActiveInMonth(t *testing.T) {
	// A leaver from years ago must not widen every month you page through.
	ts := newTestServer(t, nil)
	ts.employee(t, "Current Person", "2026-01-01", "")
	ts.employee(t, "Old Leaver", "2019-01-01", "2019-12-31")

	body := ts.get(t, "/m/2026-07").Body.String()
	if !strings.Contains(body, "Current Person") {
		t.Error("active employee missing from the grid")
	}
	if strings.Contains(body, "Old Leaver") {
		t.Error("employee with no overlap appears in the grid")
	}
}

func TestLockedCellsAreRenderedButNotEditable(t *testing.T) {
	ts := newTestServer(t, nil)
	// Employed only for the middle of July.
	ts.employee(t, "Partial", "2026-07-10", "2026-07-20")

	body := ts.get(t, "/m/2026-07").Body.String()

	// 11 employed days get an input; the other 20 get a locked marker.
	if got, want := strings.Count(body, `class="cell-form"`), 11; got != want {
		t.Errorf("%d editable cells, want %d", got, want)
	}
	if got, want := strings.Count(body, `class="locked"`), 20; got != want {
		t.Errorf("%d locked cells, want %d", got, want)
	}
}

func TestEntryRejectedOutsideEmploymentOverHTTP(t *testing.T) {
	// The store enforces this too, but the handler must translate it into a
	// 422 rather than a 500, and must not be reachable just because the
	// template would have greyed the cell.
	ts := newTestServer(t, nil)
	id := ts.employee(t, "Joiner", "2026-07-14", "")

	w := ts.post(t, "/m/2026-07/hours", url.Values{
		"employee_id": {itoa(id)},
		"date":        {"2026-07-13"},
		"hours":       {"8"},
	}, true)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", w.Code)
	}
	var res setResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.OK || res.Error == "" {
		t.Errorf("got %+v, want ok=false with a message", res)
	}

	// And nothing was written.
	day, _ := domain.ParseDate("2026-07-13")
	if got, _ := ts.db.Hours(t.Context(), id, day); got != 0 {
		t.Errorf("a locked cell was written: %s", got)
	}
}

func TestSetHoursWorksWithoutJavaScript(t *testing.T) {
	// The no-JS path is the one that must work: a plain form post, a 303 back
	// to the month, and the value actually stored.
	ts := newTestServer(t, nil)
	id := ts.employee(t, "Worker", "2026-01-01", "")

	w := ts.post(t, "/m/2026-07/hours", url.Values{
		"employee_id": {itoa(id)},
		"date":        {"2026-07-14"},
		"hours":       {"7,75"}, // comma, as a de-CH keyboard produces
	}, false)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/m/2026-07" {
		t.Errorf("Location = %q, want /m/2026-07", loc)
	}
	day, _ := domain.ParseDate("2026-07-14")
	if got, _ := ts.db.Hours(t.Context(), id, day); got != 775 {
		t.Errorf("stored %s, want 7.75", got)
	}
}

func TestSetHoursJSONReturnsServerComputedTotals(t *testing.T) {
	// The browser must never add up hours itself, or the on-screen totals can
	// drift from the printed ones.
	ts := newTestServer(t, nil)
	alice := ts.employee(t, "Alice", "2026-01-01", "")
	bob := ts.employee(t, "Bob", "2026-01-01", "")

	day, _ := domain.ParseDate("2026-07-14")
	if err := ts.db.SetHours(t.Context(), bob, day, 200); err != nil {
		t.Fatal(err)
	}

	w := ts.post(t, "/m/2026-07/hours", url.Values{
		"employee_id": {itoa(alice)},
		"date":        {"2026-07-14"},
		"hours":       {"7.75"},
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", w.Code, w.Body)
	}

	var res setResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !res.OK || res.Value != "7.75" {
		t.Errorf("got %+v, want ok with value 7.75", res)
	}
	if res.EmployeeTotal != "7.75" {
		t.Errorf("employee total %q, want 7.75", res.EmployeeTotal)
	}
	if res.DayTotal != "9.75" { // Alice 7.75 + Bob 2.00
		t.Errorf("day total %q, want 9.75", res.DayTotal)
	}
	if res.GrandTotal != "9.75" {
		t.Errorf("grand total %q, want 9.75", res.GrandTotal)
	}
}

func TestInvalidHoursRejectedWithFieldError(t *testing.T) {
	ts := newTestServer(t, nil)
	id := ts.employee(t, "Worker", "2026-01-01", "")

	for _, in := range []string{"abc", "7.755", "25", "-1"} {
		w := ts.post(t, "/m/2026-07/hours", url.Values{
			"employee_id": {itoa(id)},
			"date":        {"2026-07-14"},
			"hours":       {in},
		}, true)
		if w.Code != http.StatusUnprocessableEntity && w.Code != http.StatusBadRequest {
			t.Errorf("hours %q: status %d, want 4xx", in, w.Code)
		}
	}
	// Nothing was stored by any of the rejected attempts.
	day, _ := domain.ParseDate("2026-07-14")
	if got, _ := ts.db.Hours(t.Context(), id, day); got != 0 {
		t.Errorf("a rejected value was stored: %s", got)
	}
}

func TestDayCommentRoundTrip(t *testing.T) {
	ts := newTestServer(t, nil)
	ts.employee(t, "Worker", "2026-01-01", "")

	w := ts.post(t, "/m/2026-07/comment", url.Values{
		"date":    {"2026-07-14"},
		"comment": {"Betriebsausflug"},
	}, false)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", w.Code)
	}
	if !strings.Contains(ts.get(t, "/m/2026-07").Body.String(), "Betriebsausflug") {
		t.Error("comment not rendered back into the grid")
	}
}

func TestMonthNavigationLinksCrossYearBoundary(t *testing.T) {
	ts := newTestServer(t, nil)
	ts.employee(t, "Worker", "2020-01-01", "")

	body := ts.get(t, "/m/2026-12").Body.String()
	if !strings.Contains(body, `href="/m/2027-01"`) {
		t.Error("December does not link forward to January of the next year")
	}
	if !strings.Contains(body, `href="/m/2026-11"`) {
		t.Error("December does not link back to November")
	}

	body = ts.get(t, "/m/2026-01").Body.String()
	if !strings.Contains(body, `href="/m/2025-12"`) {
		t.Error("January does not link back to December of the previous year")
	}
}

func TestInvalidMonthIsNotFound(t *testing.T) {
	ts := newTestServer(t, nil)
	for _, path := range []string{"/m/2026-13", "/m/nonsense", "/m/2026"} {
		if w := ts.get(t, path); w.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", path, w.Code)
		}
	}
}

func TestEmployeeCreateValidatesAndPersists(t *testing.T) {
	ts := newTestServer(t, nil)

	w := ts.post(t, "/employees", url.Values{
		"name":       {"Neue Person"},
		"start_date": {"2026-07-01"},
	}, false)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303: %s", w.Code, w.Body)
	}
	if !strings.Contains(ts.get(t, "/employees").Body.String(), "Neue Person") {
		t.Error("created employee missing from the list")
	}

	// A rejected submission redisplays the form with the input intact rather
	// than discarding what was typed.
	w = ts.post(t, "/employees", url.Values{
		"name":       {"Rückwärts"},
		"start_date": {"2026-07-01"},
		"end_date":   {"2026-06-01"},
	}, false)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Rückwärts") {
		t.Error("rejected form lost the submitted name")
	}
	if !strings.Contains(body, "Austritt liegt vor dem Eintritt") {
		t.Errorf("rejected form did not explain the failure")
	}
}

func TestEmployeeUpdateAndDelete(t *testing.T) {
	ts := newTestServer(t, nil)
	id := ts.employee(t, "Original", "2026-01-01", "")

	w := ts.post(t, "/employees/"+itoa(id), url.Values{
		"name":       {"Umbenannt"},
		"start_date": {"2026-01-01"},
		"end_date":   {"2026-12-31"},
	}, false)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("update status %d, want 303", w.Code)
	}
	if !strings.Contains(ts.get(t, "/employees").Body.String(), "Umbenannt") {
		t.Error("rename not reflected in the list")
	}

	if w := ts.post(t, "/employees/"+itoa(id)+"/delete", nil, false); w.Code != http.StatusSeeOther {
		t.Fatalf("delete status %d, want 303", w.Code)
	}
	if strings.Contains(ts.get(t, "/employees").Body.String(), "Umbenannt") {
		t.Error("deleted employee still listed")
	}
}

func TestMissingEmployeeIs404(t *testing.T) {
	ts := newTestServer(t, nil)
	for _, path := range []string{"/employees/99999/edit", "/employees/abc/edit"} {
		if w := ts.get(t, path); w.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", path, w.Code)
		}
	}
}

func TestNoPageRendersAnUntranslatedMarker(t *testing.T) {
	// i18n.T renders "!!key!!" for anything missing. Rendering every page and
	// grepping for that marker is what turns "every string goes through T"
	// from a convention into something the build enforces.
	ts := newTestServer(t, nil)
	id := ts.employee(t, "Anna Muster", "2026-07-10", "2026-07-20")
	day, _ := domain.ParseDate("2026-07-14")
	if err := ts.db.SetHours(t.Context(), id, day, 775); err != nil {
		t.Fatal(err)
	}
	if err := ts.db.SetDayComment(t.Context(), 1, day, "Notiz"); err != nil {
		t.Fatal(err)
	}

	paths := []string{
		"/m/2026-07",     // populated grid, locked and editable cells
		"/m/2030-01",     // empty-state grid
		"/employees",     // populated list
		"/employees/new", // create form
		"/employees/" + itoa(id) + "/edit",
		"/m/2026-13", // error page
	}
	for _, p := range paths {
		body := ts.get(t, p).Body.String()
		if strings.Contains(body, "!!") {
			t.Errorf("%s contains an untranslated key: %s", p, excerptAround(body, "!!"))
		}
	}
}

// excerptAround returns a short window around a marker, for a useful failure.
func excerptAround(body, needle string) string {
	i := strings.Index(body, needle)
	if i < 0 {
		return ""
	}
	start := max(0, i-40)
	end := min(len(body), i+60)
	return body[start:end]
}

func TestGridEscapesEmployeeNames(t *testing.T) {
	// Names are free text and end up in attributes as well as in text nodes.
	ts := newTestServer(t, nil)
	ts.employee(t, `<script>alert("x")</script>`, "2026-01-01", "")

	body := ts.get(t, "/m/2026-07").Body.String()
	if strings.Contains(body, "<script>alert") {
		t.Error("employee name was not escaped")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Error("expected the name to appear escaped")
	}
}

func TestBasePathPrefixesGeneratedURLs(t *testing.T) {
	// WordPress mounts the app under an admin page rather than at the root.
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	bundle, _ := i18n.New()
	srv, err := New(Config{
		DB: db, Bundle: bundle,
		Auth:     stubAuth{id: adminIdentity()},
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		BasePath: "/wp-admin/admin.php",
	})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "/wp-admin/admin.php/m/") {
		t.Errorf("redirect %q does not carry the base path", loc)
	}
}

func TestNormaliseBase(t *testing.T) {
	cases := map[string]string{
		"":            "",
		"/":           "",
		"/prefix":     "/prefix",
		"/prefix/":    "/prefix",
		"prefix":      "/prefix",
		"  /prefix  ": "/prefix",
	}
	for in, want := range cases {
		if got := normaliseBase(in); got != want {
			t.Errorf("normaliseBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestErrorKeyIsWhitelisted(t *testing.T) {
	// ?err= is client-controlled; it must not be able to name arbitrary
	// catalog entries.
	if got := errorKey("invalid_hours"); got != "error.invalid_hours" {
		t.Errorf("known token mapped to %q", got)
	}
	for _, evil := range []string{"app.title", "../secret", "auth.password", ""} {
		if got := errorKey(evil); got != "error.invalid_input" {
			t.Errorf("errorKey(%q) = %q, want the generic fallback", evil, got)
		}
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func TestReportRoutesServePDFs(t *testing.T) {
	ts := newTestServer(t, nil)
	id := ts.employee(t, "Anna Muster", "2026-01-01", "")
	day, _ := domain.ParseDate("2026-07-14")
	if err := ts.db.SetHours(t.Context(), id, day, 775); err != nil {
		t.Fatal(err)
	}

	// Both the documented .pdf form and the bare form must route, because Go's
	// mux cannot match a partial path segment and the suffix is stripped.
	paths := []string{
		"/report/month/2026-07.pdf",
		"/report/month/2026-07",
		"/report/employee/" + itoa(id) + "/month/2026-07.pdf",
		"/report/employee/" + itoa(id) + "/year/2026.pdf",
	}
	for _, p := range paths {
		w := ts.get(t, p)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s: status %d, want 200", p, w.Code)
			continue
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/pdf" {
			t.Errorf("GET %s: Content-Type %q, want application/pdf", p, ct)
		}
		if !strings.HasPrefix(w.Body.String(), "%PDF-") {
			t.Errorf("GET %s: body is not a PDF", p)
		}
		if !strings.Contains(w.Body.String(), "%%EOF") {
			t.Errorf("GET %s: PDF is truncated", p)
		}
		if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, ".pdf") {
			t.Errorf("GET %s: Content-Disposition %q has no filename", p, cd)
		}
	}
}

func TestReportRoutesRejectBadParameters(t *testing.T) {
	ts := newTestServer(t, nil)
	id := ts.employee(t, "Anna", "2026-01-01", "")

	bad := []string{
		"/report/month/2026-13.pdf",
		"/report/month/nonsense",
		"/report/employee/99999/month/2026-07.pdf",
		"/report/employee/" + itoa(id) + "/year/abc",
		"/report/employee/" + itoa(id) + "/year/12",
	}
	for _, p := range bad {
		if w := ts.get(t, p); w.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", p, w.Code)
		}
	}
}

func TestReportIndexListsActiveEmployees(t *testing.T) {
	ts := newTestServer(t, nil)
	ts.employee(t, "Aktuell Person", "2026-01-01", "")
	ts.employee(t, "Alt Person", "2019-01-01", "2019-12-31")

	body := ts.get(t, "/reports?m=2026-07").Body.String()
	if !strings.Contains(body, "Aktuell Person") {
		t.Error("active employee missing from the report index")
	}
	if strings.Contains(body, "Alt Person") {
		t.Error("inactive employee listed on the report index")
	}
	if strings.Contains(body, "!!") {
		t.Errorf("report index has an untranslated key: %s", excerptAround(body, "!!"))
	}
}

func TestQueryParamMountingBuildsWordPressURLs(t *testing.T) {
	// WordPress addresses admin screens by query string and cannot route
	// /m/2026-07 at all, so under the plugin the application path travels as a
	// query parameter. Only link generation changes; the handler tree still
	// sees ordinary paths.
	cases := []struct {
		base, param, path, want string
	}{
		{"", "", "/m/2026-07", "/m/2026-07"},
		{"/prefix", "", "/m/2026-07", "/prefix/m/2026-07"},
		{
			"/wp-admin/admin.php?page=wpcalc", "wpcalc_path", "/m/2026-07",
			"/wp-admin/admin.php?page=wpcalc&wpcalc_path=%2Fm%2F2026-07",
		},
		{
			"/wp-admin/admin.php", "wpcalc_path", "/employees",
			"/wp-admin/admin.php?wpcalc_path=%2Femployees",
		},
		// A path carrying its own query must survive intact once escaped.
		{
			"/wp-admin/admin.php?page=wpcalc", "wpcalc_path", "/reports?m=2026-08",
			"/wp-admin/admin.php?page=wpcalc&wpcalc_path=%2Freports%3Fm%3D2026-08",
		},
	}
	for _, c := range cases {
		if got := buildURL(c.base, c.param, c.path); got != c.want {
			t.Errorf("buildURL(%q,%q,%q) = %q, want %q", c.base, c.param, c.path, got, c.want)
		}
	}
}

func TestFragmentModeOmitsTheDocumentShell(t *testing.T) {
	// The plugin renders inside WordPress's admin page, which already owns
	// <html> and <head>. A nested document there is invalid markup and fights
	// WordPress for the head.
	ts := newTestServer(t, nil)
	ts.employee(t, "Anna Muster", "2026-01-01", "")

	full := ts.get(t, "/m/2026-07").Body.String()
	if !strings.Contains(full, "<!DOCTYPE html>") || !strings.Contains(full, "<html") {
		t.Fatal("the standalone page is not a complete document")
	}

	r := httptest.NewRequest(http.MethodGet, "/m/2026-07", nil)
	r.Header.Set(FragmentHeader, "1")
	w := httptest.NewRecorder()
	ts.handler.ServeHTTP(w, r)

	frag := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("fragment request returned %d", w.Code)
	}
	for _, forbidden := range []string{"<!DOCTYPE", "<html", "<head", "<body"} {
		if strings.Contains(frag, forbidden) {
			t.Errorf("fragment contains %q; it must not carry a document shell", forbidden)
		}
	}
	// It must still be the real page, not an empty div.
	for _, want := range []string{"wpcalc-app", "Anna Muster", "Juli 2026"} {
		if !strings.Contains(frag, want) {
			t.Errorf("fragment is missing %q", want)
		}
	}
	if strings.Contains(frag, "!!") {
		t.Errorf("fragment has an untranslated key: %s", excerptAround(frag, "!!"))
	}
}

func TestMountHeadersOverrideConfiguredBasePath(t *testing.T) {
	// The WordPress plugin's admin proxy (admin.php) and frontend shortcode
	// proxy (admin-ajax.php) are two different URLs served by the same
	// running sidecar. A fragment rendered for one must not bake in links
	// pointing at the other, so the mount is a per-request override, not
	// only the process-wide default.
	ts := newTestServer(t, nil)
	ts.employee(t, "Anna Muster", "2026-01-01", "")

	r := httptest.NewRequest(http.MethodGet, "/m/2026-07", nil)
	r.Header.Set(FragmentHeader, "1")
	r.Header.Set(BasePathHeader, "/wp-admin/admin-ajax.php?action=wpcalc_proxy")
	r.Header.Set(LinkParamHeader, "wpcalc_path")
	w := httptest.NewRecorder()
	ts.handler.ServeHTTP(w, r)

	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("request returned %d", w.Code)
	}
	if strings.Contains(body, "admin.php?page=wpcalc") {
		t.Error("response still contains a link mounted at the configured default")
	}
	if !strings.Contains(body, "admin-ajax.php?action=wpcalc_proxy&amp;wpcalc_path=") {
		t.Errorf("response is missing a link mounted at the overridden base path: %s",
			excerptAround(body, "wpcalc_path"))
	}

	// A second, header-less request must fall back to the server's
	// configured default rather than leaking the previous request's
	// override — mountFor must read per-request state, not mutate anything
	// shared.
	plain := ts.get(t, "/m/2026-07").Body.String()
	if strings.Contains(plain, "admin-ajax.php") {
		t.Error("an unrelated request picked up the previous request's base-path override")
	}
}

func TestSetHoursAcceptsMultipartBodies(t *testing.T) {
	// The regression this guards cost a browser e2e run to find. Every other
	// test here posts application/x-www-form-urlencoded, and a handler that
	// only calls ParseForm looks perfectly correct under all of them —
	// ParseForm does not read a multipart body, leaves PostForm non-nil and
	// empty, and thereby stops PostFormValue from parsing it either. Every
	// field then reads as "" and a valid request is rejected as malformed.
	//
	// A browser sending FormData produces exactly that shape.
	ts := newTestServer(t, nil)
	id := ts.employee(t, "Worker", "2026-01-01", "")

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range map[string]string{
		"employee_id": itoa(id),
		"date":        "2026-07-14",
		"hours":       "7,75",
	} {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest(http.MethodPost, "/m/2026-07/hours", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("X-Requested-With", "XMLHttpRequest")
	w := httptest.NewRecorder()
	ts.handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("multipart POST returned %d, want 200: %s", w.Code, w.Body)
	}

	day, _ := domain.ParseDate("2026-07-14")
	got, err := ts.db.Hours(t.Context(), id, day)
	if err != nil {
		t.Fatal(err)
	}
	if got != 775 {
		t.Errorf("stored %s, want 7.75 — the multipart body was not parsed", got)
	}
}

func TestEmployeeCreateAcceptsMultipartBodies(t *testing.T) {
	ts := newTestServer(t, nil)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("name", "Multipart Person")
	_ = mw.WriteField("start_date", "2026-01-01")
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest(http.MethodPost, "/employees", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	ts.handler.ServeHTTP(w, r)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("multipart POST returned %d, want 303: %s", w.Code, w.Body)
	}
	if !strings.Contains(ts.get(t, "/employees").Body.String(), "Multipart Person") {
		t.Error("employee created via multipart is not listed")
	}
}

func TestStoredLanguageOverridesAcceptLanguage(t *testing.T) {
	// The stored preference is the more specific statement: someone whose
	// laptop is set to English but who wants the German interface has said so.
	ts := newTestServer(t, stubAuth{id: adminIdentityWithLanguage("de-CH")})
	ts.employee(t, "Muster A", "2026-01-01", "")

	r := httptest.NewRequest(http.MethodGet, "/m/2026-07", nil)
	r.Header.Set("Accept-Language", "en-GB,en;q=0.9")
	w := httptest.NewRecorder()
	ts.handler.ServeHTTP(w, r)

	body := w.Body.String()
	if !strings.Contains(body, "Juli 2026") {
		t.Errorf("stored de-CH lost to Accept-Language: en; body has %q",
			excerptAround(body, "2026"))
	}
	if strings.Contains(body, "July 2026") {
		t.Error("rendered in English despite a stored German preference")
	}
	if !strings.Contains(body, `lang="de-CH"`) {
		t.Error("the html lang attribute does not reflect the stored preference")
	}
}

func TestAcceptLanguageUsedWhenNoPreferenceStored(t *testing.T) {
	ts := newTestServer(t, stubAuth{id: adminIdentity()}) // Language empty
	ts.employee(t, "Muster A", "2026-01-01", "")

	r := httptest.NewRequest(http.MethodGet, "/m/2026-07", nil)
	r.Header.Set("Accept-Language", "en-GB,en;q=0.9")
	w := httptest.NewRecorder()
	ts.handler.ServeHTTP(w, r)

	if !strings.Contains(w.Body.String(), "July 2026") {
		t.Error("with no stored preference, Accept-Language should decide")
	}
}

func TestUnknownStoredLanguageFallsBackRatherThanBreaking(t *testing.T) {
	// A catalog can be removed after someone has chosen it. That must degrade
	// to negotiation, not render untranslated markers.
	ts := newTestServer(t, stubAuth{id: adminIdentityWithLanguage("fr-CH")})
	ts.employee(t, "Muster A", "2026-01-01", "")

	r := httptest.NewRequest(http.MethodGet, "/m/2026-07", nil)
	r.Header.Set("Accept-Language", "en-GB,en;q=0.9")
	w := httptest.NewRecorder()
	ts.handler.ServeHTTP(w, r)

	body := w.Body.String()
	if strings.Contains(body, "!!") {
		t.Errorf("unsupported preference produced untranslated keys: %s", excerptAround(body, "!!"))
	}
	if !strings.Contains(body, "July 2026") {
		t.Error("did not fall back to Accept-Language")
	}
}

// langWritingAuth is a stub that can persist a preference, so the selector
// appears and the handler has something to write to.
type langWritingAuth struct {
	stubAuth
	saved string
}

func (a *langWritingAuth) Identify(r *http.Request) (auth.Identity, error) {
	id, err := a.stubAuth.Identify(r)
	if err != nil {
		return id, err
	}
	id.Language = a.saved
	return id, nil
}

func (a *langWritingAuth) SetLanguage(_ *http.Request, lang string) error {
	a.saved = lang
	return nil
}

func TestLanguageSelectorAppearsOnlyWhenItCanPersist(t *testing.T) {
	// Offering a control that silently does nothing is worse than not offering
	// one, which is the WordPress case: WordPress owns the user record there.
	withStore := newTestServer(t, &langWritingAuth{stubAuth: stubAuth{
		id: adminIdentity()}})
	if !strings.Contains(withStore.get(t, "/employees").Body.String(), `name="lang"`) {
		t.Error("selector missing when the authenticator can persist")
	}

	withoutStore := newTestServer(t, nil) // plain stubAuth: no SetLanguage
	if strings.Contains(withoutStore.get(t, "/employees").Body.String(), `name="lang"`) {
		t.Error("selector shown when the authenticator cannot persist it")
	}
}

func TestSetLanguagePersistsValidatesAndReturns(t *testing.T) {
	a := &langWritingAuth{stubAuth: stubAuth{
		id: adminIdentity()}}
	ts := newTestServer(t, a)

	// The POSIX spelling is accepted, because that is what a shell locale
	// looks like and what people type.
	w := ts.post(t, "/language", url.Values{"lang": {"de_CH"}, "return_to": {"/m/2026-07"}}, false)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", w.Code)
	}
	if a.saved != "de-CH" {
		t.Errorf("stored %q, want de-CH", a.saved)
	}
	if loc := w.Header().Get("Location"); loc != "/m/2026-07" {
		t.Errorf("returned to %q, want the page we came from", loc)
	}

	// Empty clears the preference back to following the browser.
	ts.post(t, "/language", url.Values{"lang": {""}}, false)
	if a.saved != "" {
		t.Errorf("clearing stored %q, want empty", a.saved)
	}

	// An unshipped locale is refused rather than stored, or the account would
	// render in a language that does not exist.
	a.saved = "en"
	if w := ts.post(t, "/language", url.Values{"lang": {"fr-CH"}}, false); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("unknown locale: status %d, want 422", w.Code)
	}
	if a.saved != "en" {
		t.Errorf("unknown locale overwrote the preference: %q", a.saved)
	}
}

func TestLanguageReturnToCannotLeaveTheSite(t *testing.T) {
	// return_to comes from a form field, so it is an open redirect unless the
	// target is checked.
	a := &langWritingAuth{stubAuth: stubAuth{
		id: adminIdentity()}}
	ts := newTestServer(t, a)

	for _, evil := range []string{"//evil.example/", "https://evil.example/", "javascript:alert(1)", ""} {
		w := ts.post(t, "/language", url.Values{"lang": {"en"}, "return_to": {evil}}, false)
		if loc := w.Header().Get("Location"); loc != "/" {
			t.Errorf("return_to=%q redirected to %q; want the local root", evil, loc)
		}
	}
}
