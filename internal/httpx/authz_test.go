package httpx

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/rolsim/wpcalc/internal/auth"
	"github.com/rolsim/wpcalc/internal/domain"
)

// tenant creates a tenant directly via the store and returns its id.
func (ts *testServer) tenant(t *testing.T, name string) int64 {
	t.Helper()
	id, err := ts.db.CreateTenant(t.Context(), name)
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	return id
}

// employeeInTenant is ts.employee for an explicit tenant, for multi-tenant
// tests where the default tenant 1 is not what is being exercised.
func (ts *testServer) employeeInTenant(t *testing.T, tenantID int64, name, start, end string) int64 {
	t.Helper()
	e := domain.Employee{TenantID: tenantID, DisplayName: name}
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

// mandantAdmin builds a mandant-admin identity for one tenant, already
// active — mirrors what accounts.identityFor resolves for a real account.
func mandantAdmin(tenantID int64) auth.Identity {
	return auth.Identity{
		Username:        "mandant",
		ActiveTenantID:  &tenantID,
		UserRoles:       []domain.UserRole{{TenantID: &tenantID, RoleID: "mandant_admin"}},
		RolePermissions: map[string][]string{"mandant_admin": {"manage_employees", "manage_users", "read", "print", "write"}},
	}
}

// employeeScoped builds an identity holding roleID on exactly one employee,
// active in tenantID (as the session would resolve once auto-selected).
func employeeScoped(tenantID, employeeID int64, roleID string, permissions []string) auth.Identity {
	return auth.Identity{
		Username:        "worker",
		ActiveTenantID:  &tenantID,
		UserRoles:       []domain.UserRole{{EmployeeID: &employeeID, RoleID: roleID}},
		RolePermissions: map[string][]string{roleID: permissions},
	}
}

func TestMandantAdminCannotReachAnotherTenant(t *testing.T) {
	ts := newTestServer(t, nil) // admin, to set up two tenants
	tenantB := ts.tenant(t, "Tenant B")
	empB := ts.employeeInTenant(t, tenantB, "B Employee", "2026-01-01", "")
	tenantA := int64(1) // the seeded Default tenant

	ts.Server.authn = stubAuth{id: mandantAdmin(tenantA)}

	if w := ts.get(t, "/employees"); w.Code != http.StatusOK {
		t.Errorf("mandant-admin GET /employees (own tenant): status %d, want 200", w.Code)
	}
	if w := ts.get(t, "/tenants/2/access"); w.Code != http.StatusForbidden {
		t.Errorf("mandant-admin GET /tenants/{other}/access: status %d, want 403", w.Code)
	}
	if w := ts.get(t, "/employees/"+itoa(empB)+"/edit"); w.Code != http.StatusNotFound {
		t.Errorf("mandant-admin editing another tenant's employee: status %d, want 404", w.Code)
	}
	if w := ts.get(t, "/tenants"); w.Code != http.StatusForbidden {
		t.Errorf("mandant-admin GET /tenants (system-wide): status %d, want 403", w.Code)
	}
	if w := ts.get(t, "/roles"); w.Code != http.StatusForbidden {
		t.Errorf("mandant-admin GET /roles: status %d, want 403", w.Code)
	}
}

func TestViewerCanReadButNotWriteOrPrint(t *testing.T) {
	ts := newTestServer(t, nil)
	empID := ts.employee(t, "Anna", "2026-01-01", "")
	ts.Server.authn = stubAuth{id: employeeScoped(1, empID, "viewer", []string{"read"})}

	if w := ts.get(t, "/m/2026-07"); w.Code != http.StatusOK {
		t.Fatalf("viewer GET grid: status %d, want 200", w.Code)
	}
	form := url.Values{"employee_id": {itoa(empID)}, "date": {"2026-07-14"}, "hours": {"7.75"}}
	if w := ts.post(t, "/m/2026-07/hours", form, true); w.Code != http.StatusForbidden {
		t.Errorf("viewer writing hours: status %d, want 403", w.Code)
	}
	if w := ts.get(t, "/report/employee/"+itoa(empID)+"/month/2026-07.pdf"); w.Code != http.StatusForbidden {
		t.Errorf("viewer downloading a report: status %d, want 403", w.Code)
	}
}

func TestReporterCanPrintButNotWrite(t *testing.T) {
	ts := newTestServer(t, nil)
	empID := ts.employee(t, "Anna", "2026-01-01", "")
	ts.Server.authn = stubAuth{id: employeeScoped(1, empID, "reporter", []string{"read", "print"})}

	if w := ts.get(t, "/report/employee/"+itoa(empID)+"/month/2026-07.pdf"); w.Code != http.StatusOK {
		t.Errorf("reporter downloading a report: status %d, want 200", w.Code)
	}
	form := url.Values{"employee_id": {itoa(empID)}, "date": {"2026-07-14"}, "hours": {"7.75"}}
	if w := ts.post(t, "/m/2026-07/hours", form, true); w.Code != http.StatusForbidden {
		t.Errorf("reporter writing hours: status %d, want 403", w.Code)
	}
}

func TestViewerCannotWriteTheSharedDayComment(t *testing.T) {
	// The day comment is shared, tenant-wide state — an employee-scope grant,
	// even editor on one's own employee, must not reach it. Only a
	// tenant-wide write permission (mandant-admin or above) may.
	ts := newTestServer(t, nil)
	empID := ts.employee(t, "Anna", "2026-01-01", "")
	ts.Server.authn = stubAuth{id: employeeScoped(1, empID, "editor", []string{"read", "print", "write"})}

	form := url.Values{"date": {"2026-07-14"}, "comment": {"Betriebsausflug"}}
	if w := ts.post(t, "/m/2026-07/comment", form, true); w.Code != http.StatusForbidden {
		t.Errorf("employee-scope editor writing the shared comment: status %d, want 403", w.Code)
	}
}

func TestEditorCanWrite(t *testing.T) {
	ts := newTestServer(t, nil)
	empID := ts.employee(t, "Anna", "2026-01-01", "")
	ts.Server.authn = stubAuth{id: employeeScoped(1, empID, "editor", []string{"read", "print", "write"})}

	form := url.Values{"employee_id": {itoa(empID)}, "date": {"2026-07-14"}, "hours": {"7.75"}}
	if w := ts.post(t, "/m/2026-07/hours", form, true); w.Code != http.StatusOK {
		t.Errorf("editor writing own hours: status %d, want 200: %s", w.Code, w.Body)
	}
}

func TestEmployeeScopedRoleCannotTouchAnotherEmployee(t *testing.T) {
	ts := newTestServer(t, nil)
	own := ts.employee(t, "Own", "2026-01-01", "")
	other := ts.employee(t, "Other", "2026-01-01", "")
	ts.Server.authn = stubAuth{id: employeeScoped(1, own, "editor", []string{"read", "print", "write"})}

	form := url.Values{"employee_id": {itoa(other)}, "date": {"2026-07-14"}, "hours": {"7.75"}}
	if w := ts.post(t, "/m/2026-07/hours", form, true); w.Code != http.StatusForbidden {
		t.Errorf("writing another employee's hours: status %d, want 403", w.Code)
	}

	body := ts.get(t, "/m/2026-07").Body.String()
	if !strings.Contains(body, "Own") {
		t.Error("own employee missing from the grid")
	}
	if strings.Contains(body, "Other") {
		t.Error("an employee with no grant at all leaked into the grid as a column")
	}
}

func TestGridLocksCellsWithoutWritePermission(t *testing.T) {
	ts := newTestServer(t, nil)
	own := ts.employee(t, "Own", "2026-01-01", "")
	ts.Server.authn = stubAuth{id: employeeScoped(1, own, "viewer", []string{"read"})}

	month, err := domain.ParseYearMonth("2026-07")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/m/2026-07", nil)
	r = r.WithContext(auth.WithIdentity(r.Context(), employeeScoped(1, own, "viewer", []string{"read"})))

	v, err := ts.Server.buildGridView(r, 1, month)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Employees) != 1 {
		t.Fatalf("got %d visible employees, want 1", len(v.Employees))
	}
	for _, row := range v.Rows {
		for _, cell := range row.Cells {
			if !cell.Locked {
				t.Errorf("viewer's cell on %s is not locked", row.DateISO)
			}
		}
	}
}

func TestReportIndexNarrowsToPermittedEmployees(t *testing.T) {
	ts := newTestServer(t, nil)
	own := ts.employee(t, "Own Person", "2026-01-01", "")
	ts.employee(t, "Other Person", "2026-01-01", "")
	ts.Server.authn = stubAuth{id: employeeScoped(1, own, "reporter", []string{"read", "print"})}

	body := ts.get(t, "/reports?m=2026-07").Body.String()
	if !strings.Contains(body, "Own Person") {
		t.Error("own employee missing from a non-admin's report index")
	}
	if strings.Contains(body, "Other Person") {
		t.Error("a non-admin's report index leaked another employee's name")
	}
}

func TestUnlinkedNonAdminHasNoAccessAtAll(t *testing.T) {
	// An account with a role that grants nothing usable (or none at all)
	// must fail closed, not open.
	ts := newTestServer(t, nil)
	empID := ts.employee(t, "Anna", "2026-01-01", "")
	unlinked := auth.Identity{Username: "nobody", ActiveTenantID: tenantOnePtr()}
	ts.Server.authn = stubAuth{id: unlinked}

	if w := ts.get(t, "/employees"); w.Code != http.StatusForbidden {
		t.Errorf("GET /employees: status %d, want 403", w.Code)
	}
	if w := ts.get(t, "/report/employee/"+itoa(empID)+"/month/2026-07.pdf"); w.Code != http.StatusForbidden {
		t.Errorf("GET employee report: status %d, want 403", w.Code)
	}
	body := ts.get(t, "/m/2026-07").Body.String()
	if strings.Contains(body, "Anna") {
		t.Error("an identity with no grants at all can see an employee column")
	}
}

func TestTenantChooserShowsWhenMultipleTenantsAccessible(t *testing.T) {
	ts := newTestServer(t, nil)
	ts.tenant(t, "Second Tenant")

	// A super-admin identity with no ActiveTenantID yet must be sent to the
	// chooser rather than guessing.
	superAdmin := auth.Identity{
		Username:        "root",
		UserRoles:       []domain.UserRole{{RoleID: "super_admin"}},
		RolePermissions: map[string][]string{"super_admin": superAdminPermissions},
	}
	ts.Server.authn = stubAuth{id: superAdmin}

	w := ts.get(t, "/m/2026-07")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303 to the chooser", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/tenants/choose" {
		t.Errorf("Location = %q, want /tenants/choose", loc)
	}

	choose := ts.get(t, "/tenants/choose")
	if choose.Code != http.StatusOK {
		t.Fatalf("GET /tenants/choose: status %d, want 200", choose.Code)
	}
	if !strings.Contains(choose.Body.String(), "Second Tenant") {
		t.Error("chooser is missing an accessible tenant")
	}
}

func TestTenantChooserSkippedWithOneAccessibleTenant(t *testing.T) {
	// mandantAdmin(1) has exactly one accessible tenant and is already
	// active in it — this is the common case and must never redirect.
	ts := newTestServer(t, nil)
	ts.employee(t, "Anna", "2026-01-01", "")
	ts.Server.authn = stubAuth{id: mandantAdmin(1)}

	if w := ts.get(t, "/m/2026-07"); w.Code != http.StatusOK {
		t.Errorf("status %d, want 200 (no chooser redirect)", w.Code)
	}
}

func TestNoAccessibleTenantsShowsAClearError(t *testing.T) {
	ts := newTestServer(t, nil)
	noTenant := auth.Identity{Username: "ghost"} // no roles, no active tenant
	ts.Server.authn = stubAuth{id: noTenant}

	w := ts.get(t, "/m/2026-07")
	if w.Code != http.StatusForbidden {
		t.Errorf("status %d, want 403", w.Code)
	}
	if strings.Contains(w.Body.String(), "!!") {
		t.Errorf("no-tenant-access page has an untranslated key: %s", excerptAround(w.Body.String(), "!!"))
	}
}

func TestWordPressFullAccessIdentityIsUnrestricted(t *testing.T) {
	// WordPress mode grants FullAccess rather than a resolved role set (see
	// wordpress.go) — it must still pass every permission check.
	ts := newTestServer(t, nil)
	empID := ts.employee(t, "Anna", "2026-01-01", "")
	ts.Server.authn = stubAuth{id: auth.Identity{Username: "wp", FullAccess: true}}

	if w := ts.get(t, "/employees"); w.Code != http.StatusOK {
		t.Errorf("FullAccess GET /employees: status %d, want 200", w.Code)
	}
	if w := ts.get(t, "/tenants"); w.Code != http.StatusOK {
		t.Errorf("FullAccess GET /tenants: status %d, want 200", w.Code)
	}
	if w := ts.get(t, "/roles"); w.Code != http.StatusOK {
		t.Errorf("FullAccess GET /roles: status %d, want 200", w.Code)
	}
	form := url.Values{"employee_id": {itoa(empID)}, "date": {"2026-07-14"}, "hours": {"7.75"}}
	if w := ts.post(t, "/m/2026-07/hours", form, true); w.Code != http.StatusOK {
		t.Errorf("FullAccess writing hours: status %d, want 200: %s", w.Code, w.Body)
	}

	// FullAccess has no session-backed ActiveTenantID, so the middleware
	// falls back to auto-selecting on every request: with only the Default
	// tenant existing, that resolves silently (as above). Once a second
	// tenant exists, "auto-select" can no longer be a guess, so it falls
	// back to the same chooser a real multi-tenant account would see.
	ts.tenant(t, "Second Tenant")
	if w := ts.get(t, "/m/2026-07"); w.Code != http.StatusSeeOther {
		t.Errorf("FullAccess with multiple tenants and no active one: status %d, want 303", w.Code)
	}
}

func TestNavHidesAdminLinksForNonAdmin(t *testing.T) {
	ts := newTestServer(t, nil)
	empID := ts.employee(t, "Anna", "2026-01-01", "")
	ts.Server.authn = stubAuth{id: employeeScoped(1, empID, "viewer", []string{"read"})}

	body := ts.get(t, "/m/2026-07").Body.String()
	if strings.Contains(body, `href="/employees"`) {
		t.Error("non-admin page still links to /employees")
	}
	if strings.Contains(body, `href="/tenants"`) || strings.Contains(body, `href="/roles"`) {
		t.Error("non-admin page links to system-wide admin pages")
	}
}

func TestGridEmptyStateHidesAddEmployeeLinkWithoutManagePermission(t *testing.T) {
	// A tenant-scoped viewer has no employee grants at all, so the grid's
	// empty state renders — but they can't create an employee, so the "Add
	// employee" link must not appear even though the nav-hidden /employees
	// link is what governs the equivalent nav entry.
	ts := newTestServer(t, nil)
	tenantID := ts.tenant(t, "Acme")
	ts.Server.authn = stubAuth{id: auth.Identity{
		Username:        "viewer",
		ActiveTenantID:  &tenantID,
		UserRoles:       []domain.UserRole{{TenantID: &tenantID, RoleID: "viewer"}},
		RolePermissions: map[string][]string{"viewer": {"read"}},
	}}

	body := ts.get(t, "/m/2026-07").Body.String()
	if strings.Contains(body, `href="/employees/new"`) {
		t.Error("viewer with no manage_employees permission still sees the Add employee link")
	}
}

func TestGridEmptyStateShowsAddEmployeeLinkWithManagePermission(t *testing.T) {
	ts := newTestServer(t, nil)
	tenantID := ts.tenant(t, "Acme")
	ts.Server.authn = stubAuth{id: mandantAdmin(tenantID)}

	body := ts.get(t, "/m/2026-07").Body.String()
	if !strings.Contains(body, `href="/employees/new"`) {
		t.Error("mandant-admin with manage_employees permission should see the Add employee link")
	}
}

func TestAdminRoleManagementRoundTrip(t *testing.T) {
	ts := newTestServer(t, nil) // super-admin by default
	tenantB := ts.tenant(t, "Tenant B")

	uid, err := ts.db.CreateUser(t.Context(), "newmandant", goodTestPassword)
	if err != nil {
		t.Fatal(err)
	}

	w := ts.post(t, "/roles/assign", url.Values{
		"username": {"newmandant"}, "tenant_id": {itoa(tenantB)}, "role_id": {"mandant_admin"},
	}, false)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("assign: status %d, want 303: %s", w.Code, w.Body)
	}
	roles, err := ts.db.UserRolesForUser(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || roles[0].RoleID != "mandant_admin" {
		t.Fatalf("roles after assign = %+v, want just mandant_admin", roles)
	}

	w = ts.post(t, "/roles/revoke", url.Values{
		"user_id": {itoa(uid)}, "tenant_id": {itoa(tenantB)},
	}, false)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("revoke: status %d, want 303: %s", w.Code, w.Body)
	}
	roles, err = ts.db.UserRolesForUser(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 0 {
		t.Errorf("roles after revoke = %+v, want none", roles)
	}
}

const goodTestPassword = "a-sufficiently-long-password"
