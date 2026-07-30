package store

import (
	"errors"
	"testing"
	"time"

	"github.com/rolsim/wpcalc/internal/domain"
)

func TestTenantCRUD(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()

	id, err := db.CreateTenant(ctx, "Acme Corp")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	got, err := db.Tenant(ctx, id)
	if err != nil || got.Name != "Acme Corp" {
		t.Fatalf("Tenant(%d) = %+v, %v", id, got, err)
	}

	if err := db.RenameTenant(ctx, id, "Acme Corporation"); err != nil {
		t.Fatalf("RenameTenant: %v", err)
	}
	got, _ = db.Tenant(ctx, id)
	if got.Name != "Acme Corporation" {
		t.Errorf("name after rename = %q", got.Name)
	}

	if _, err := db.CreateTenant(ctx, "Acme Corporation"); !errors.Is(err, ErrDuplicateTenant) {
		t.Errorf("duplicate tenant name: got %v, want ErrDuplicateTenant", err)
	}
	if _, err := db.Tenant(ctx, 99999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing tenant: got %v, want ErrNotFound", err)
	}

	tenants, err := db.Tenants(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The migration seeds a "Default" tenant (id 1) plus the one created here.
	if len(tenants) != 2 {
		t.Fatalf("Tenants() = %+v, want 2", tenants)
	}
}

func TestRoleScopeMustCoverItsPermissions(t *testing.T) {
	// Mirrors migration 00004's trg_role_permissions_scope: a role can only
	// hold permissions whose min_scope it is broad enough to satisfy.
	db := testDB(t)
	ctx := t.Context()

	if err := db.CreateRole(ctx, "auditor", "Auditor", domain.ScopeTenant); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if err := db.AddRolePermission(ctx, "auditor", "read"); err != nil {
		t.Fatalf("AddRolePermission(read) on a tenant-scope role: %v", err)
	}
	if err := db.AddRolePermission(ctx, "auditor", "manage_tenants"); !errors.Is(err, ErrRoleScopeTooNarrow) {
		t.Errorf("AddRolePermission(manage_tenants) on a tenant-scope role: got %v, want ErrRoleScopeTooNarrow", err)
	}

	perms, err := db.RolePermissionsFor(ctx, []string{"auditor"})
	if err != nil {
		t.Fatal(err)
	}
	if len(perms["auditor"]) != 1 || perms["auditor"][0] != "read" {
		t.Errorf("auditor permissions = %v, want just [read]", perms["auditor"])
	}
}

func TestRoleCRUDAndDeletionGuard(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()

	if err := db.CreateRole(ctx, "clerk", "Clerk", domain.ScopeEmployee); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if err := db.CreateRole(ctx, "clerk", "Clerk again", domain.ScopeEmployee); !errors.Is(err, ErrDuplicateRole) {
		t.Errorf("duplicate role id: got %v, want ErrDuplicateRole", err)
	}

	empID := mustEmployee(t, db, "Alice", "2026-01-01", "")
	uid, err := db.CreateUser(ctx, "clerkuser", goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.GrantUserRole(ctx, uid, nil, &empID, "clerk"); err != nil {
		t.Fatalf("GrantUserRole: %v", err)
	}

	if err := db.DeleteRole(ctx, "clerk"); err == nil {
		t.Error("DeleteRole succeeded while still assigned; the FK should have restricted it")
	}
	if err := db.RevokeUserRole(ctx, uid, nil, &empID); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteRole(ctx, "clerk"); err != nil {
		t.Errorf("DeleteRole after the only assignment was revoked: %v", err)
	}
}

func TestUserRoleScopeMustMatchTheRolesOwnScope(t *testing.T) {
	// Mirrors migration 00004's trg_user_roles_scope: an employee-scope role
	// cannot be assigned at tenant or system scope and vice versa.
	db := testDB(t)
	ctx := t.Context()
	uid, err := db.CreateUser(ctx, "alice", goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := int64(1)
	empID := mustEmployee(t, db, "Bob", "2026-01-01", "")

	if err := db.GrantUserRole(ctx, uid, &tenantID, nil, "viewer"); err == nil {
		t.Error("assigned an employee-scope role at tenant scope")
	}
	if err := db.GrantUserRole(ctx, uid, nil, &empID, "mandant_admin"); err == nil {
		t.Error("assigned a tenant-scope role at employee scope")
	}
	if err := db.GrantUserRole(ctx, uid, nil, nil, "viewer"); err == nil {
		t.Error("assigned an employee-scope role at system scope")
	}

	// The matching scope succeeds.
	if err := db.GrantUserRole(ctx, uid, nil, &empID, "viewer"); err != nil {
		t.Errorf("GrantUserRole with the correct scope: %v", err)
	}
}

func TestGrantUserRoleIsIdempotentPerScope(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	uid, err := db.CreateUser(ctx, "alice", goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	empID := mustEmployee(t, db, "Bob", "2026-01-01", "")

	for range 3 {
		if err := db.GrantUserRole(ctx, uid, nil, &empID, "viewer"); err != nil {
			t.Fatalf("GrantUserRole: %v", err)
		}
	}
	roles, err := db.UserRolesForUser(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 {
		t.Errorf("got %d user_roles rows after repeated grants, want 1", len(roles))
	}
}

func TestOneRolePerScopeInstance(t *testing.T) {
	// A user can hold only one role at a given scope instance — changing an
	// employee-level role is revoke-then-grant, never two rows disagreeing.
	db := testDB(t)
	ctx := t.Context()
	uid, err := db.CreateUser(ctx, "alice", goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	empID := mustEmployee(t, db, "Bob", "2026-01-01", "")

	if err := db.GrantUserRole(ctx, uid, nil, &empID, "viewer"); err != nil {
		t.Fatal(err)
	}
	if err := db.GrantUserRole(ctx, uid, nil, &empID, "editor"); err == nil {
		t.Error("granted a second, different role at the same employee scope")
	}
}

func TestRevokeUserRole(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	uid, err := db.CreateUser(ctx, "alice", goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := int64(1)

	if err := db.GrantUserRole(ctx, uid, &tenantID, nil, "mandant_admin"); err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeUserRole(ctx, uid, &tenantID, nil); err != nil {
		t.Fatalf("RevokeUserRole: %v", err)
	}
	roles, err := db.UserRolesForUser(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 0 {
		t.Errorf("got %d roles after revoke, want 0", len(roles))
	}
	if err := db.RevokeUserRole(ctx, uid, &tenantID, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoking an already-revoked role: got %v, want ErrNotFound", err)
	}
}

func TestHasSystemAdmin(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()

	if has, err := db.HasSystemAdmin(ctx); err != nil || has {
		t.Fatalf("HasSystemAdmin on an empty database = (%v, %v), want (false, nil)", has, err)
	}

	uid, err := db.CreateUser(ctx, "alice", goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	// A system-scope role that does NOT hold manage_tenants/manage_roles must
	// not count — the check is about actual capability, not merely "system
	// scope".
	if err := db.CreateRole(ctx, "auditor", "Auditor", domain.ScopeSystem); err != nil {
		t.Fatal(err)
	}
	if err := db.AddRolePermission(ctx, "auditor", "read"); err != nil {
		t.Fatal(err)
	}
	if err := db.GrantUserRole(ctx, uid, nil, nil, "auditor"); err != nil {
		t.Fatal(err)
	}
	if has, err := db.HasSystemAdmin(ctx); err != nil || has {
		t.Fatalf("HasSystemAdmin with only a read-only system role = (%v, %v), want (false, nil)", has, err)
	}

	// A user holds one role per scope instance — swap it via revoke-then-grant.
	if err := db.RevokeUserRole(ctx, uid, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.GrantUserRole(ctx, uid, nil, nil, "super_admin"); err != nil {
		t.Fatal(err)
	}
	if has, err := db.HasSystemAdmin(ctx); err != nil || !has {
		t.Fatalf("HasSystemAdmin after granting super_admin = (%v, %v), want (true, nil)", has, err)
	}
}

func TestTenantsAccessibleToUser(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()

	tenantA, err := db.CreateTenant(ctx, "Tenant A")
	if err != nil {
		t.Fatal(err)
	}
	tenantB, err := db.CreateTenant(ctx, "Tenant B")
	if err != nil {
		t.Fatal(err)
	}

	uid, err := db.CreateUser(ctx, "alice", goodPassword)
	if err != nil {
		t.Fatal(err)
	}

	// No roles yet: nothing accessible.
	got, err := db.TenantsAccessibleToUser(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("accessible tenants with no roles = %+v, want none", got)
	}

	// A tenant-scope role reaches exactly that tenant.
	if err := db.GrantUserRole(ctx, uid, &tenantA, nil, "mandant_admin"); err != nil {
		t.Fatal(err)
	}
	got, err = db.TenantsAccessibleToUser(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != tenantA {
		t.Fatalf("accessible tenants = %+v, want just tenant A", got)
	}

	// A system-scope role reaches every tenant, including "Default" and B.
	if err := db.GrantUserRole(ctx, uid, nil, nil, "super_admin"); err != nil {
		t.Fatal(err)
	}
	got, err = db.TenantsAccessibleToUser(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 { // Default (seeded), A, B
		t.Fatalf("accessible tenants with a system-scope role = %+v, want all 3", got)
	}
	_ = tenantB
}

func TestEmployeesAndDayCommentsAreScopedByTenant(t *testing.T) {
	// The whole point of tenant scoping: one tenant's data must never leak
	// into another's listing, even though both share the same tables.
	db := testDB(t)
	ctx := t.Context()

	tenantB, err := db.CreateTenant(ctx, "Tenant B")
	if err != nil {
		t.Fatal(err)
	}

	empDefault := mustEmployee(t, db, "Default Employee", "2026-01-01", "")
	empB, err := db.CreateEmployee(ctx, domain.Employee{
		TenantID: tenantB, DisplayName: "B Employee", StartDate: mustDate(t, "2026-01-01"),
	})
	if err != nil {
		t.Fatal(err)
	}

	defaultEmployees, err := db.Employees(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range defaultEmployees {
		if e.ID == empB {
			t.Error("tenant B's employee leaked into the Default tenant's list")
		}
	}

	bEmployees, err := db.Employees(ctx, tenantB)
	if err != nil {
		t.Fatal(err)
	}
	if len(bEmployees) != 1 || bEmployees[0].ID != empB {
		t.Errorf("tenant B's employee list = %+v, want just its own employee", bEmployees)
	}
	_ = empDefault

	// Two tenants can have a comment on the same date without colliding —
	// the point of day_comments' UNIQUE(tenant_id, work_date) rather than
	// the old global UNIQUE(work_date).
	day := mustDate(t, "2026-07-14")
	if err := db.SetDayComment(ctx, 1, day, "Default's note"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetDayComment(ctx, tenantB, day, "B's note"); err != nil {
		t.Fatal(err)
	}
	defaultComments, err := db.DayComments(ctx, 1, domain.NewYearMonth(2026, time.July))
	if err != nil {
		t.Fatal(err)
	}
	bComments, err := db.DayComments(ctx, tenantB, domain.NewYearMonth(2026, time.July))
	if err != nil {
		t.Fatal(err)
	}
	if defaultComments[day] != "Default's note" || bComments[day] != "B's note" {
		t.Errorf("comments collided across tenants: default=%q, B=%q", defaultComments[day], bComments[day])
	}
}

func TestDeletingEmployeeUnlinksItsUserRoles(t *testing.T) {
	// ON DELETE CASCADE for employee-scope user_roles: removing the employee
	// removes the now-meaningless grant rather than stranding a row that
	// references a nonexistent employee.
	db := testDB(t)
	ctx := t.Context()
	uid, err := db.CreateUser(ctx, "alice", goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	empID := mustEmployee(t, db, "Bob", "2026-01-01", "")
	if err := db.GrantUserRole(ctx, uid, nil, &empID, "viewer"); err != nil {
		t.Fatal(err)
	}

	if err := db.DeleteEmployee(ctx, empID); err != nil {
		t.Fatal(err)
	}
	roles, err := db.UserRolesForUser(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 0 {
		t.Errorf("got %d user_roles rows after the employee was deleted, want 0", len(roles))
	}
}
