package httpx

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"source.simonet.internal/rolsim/wpcalc/internal/auth"
	"source.simonet.internal/rolsim/wpcalc/internal/domain"
	"source.simonet.internal/rolsim/wpcalc/internal/i18n"
	"source.simonet.internal/rolsim/wpcalc/internal/store"
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
		authn = stubAuth{id: auth.Identity{Username: "tester", Roles: []string{auth.RoleAdmin}}}
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
	e := domain.Employee{DisplayName: name}
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
	if err := ts.db.SetDayComment(t.Context(), day, "Notiz"); err != nil {
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
		Auth:     stubAuth{id: auth.Identity{Username: "t", Roles: []string{auth.RoleAdmin}}},
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
