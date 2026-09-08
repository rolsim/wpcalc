package domain

import (
	"errors"
	"fmt"
	"uuid"
)

// Package domain's RBAC types follow NIST RBAC96 (Sandhu, Coyne, Feinstein,
// Youman 1996 / ANSI INCITS 359-2004) naming verbatim: Roles, Permissions,
// UserRole (the User Assignment relation, UA ⊆ U x R). RolePermission (the
// Permission Assignment relation, PA ⊆ P x R) is store-layer only — it has no
// Go type of its own beyond the (roleID, permissionID) pair store.go passes
// around, since nothing outside the store needs to hold one.

// Scope says how broad a role or permission is: system is broadest, employee
// narrowest.
//
// The ordering itself is not duplicated here. Access decisions walk the tiers
// in auth.Identity's Can/CanInTenant/CanSystemWide cascade, and the
// role-scope-vs-permission-min_scope rule lives in the migration's
// trg_role_permissions_scope trigger, surfaced as store.ErrRoleScopeTooNarrow.
// A Go-side comparison helper existed once, went uncalled for exactly that
// reason, and was removed rather than left to drift.
type Scope string

const (
	ScopeSystem   Scope = "system"
	ScopeTenant   Scope = "tenant"
	ScopeEmployee Scope = "employee"
)

// ErrInvalidScope is the sentinel for an unrecognised scope value.
var ErrInvalidScope = errors.New("invalid scope")

// ValidScope checks a candidate scope string.
func ValidScope(s string) error {
	switch Scope(s) {
	case ScopeSystem, ScopeTenant, ScopeEmployee:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidScope, s)
	}
}

// Permission (P/PRMS in RBAC96) is a fixed, code-defined capability. The
// database seeds these once at migration time; there is no CLI or UI to
// create one, because a permission is only real if some route guard actually
// checks for it — inventing one through the UI would do nothing.
type Permission struct {
	ID       string
	MinScope Scope
}

// Permission IDs, matching migration 00004's seed data exactly. Every route
// guard in internal/httpx checks one of these.
const (
	PermManageTenants   = "manage_tenants"
	PermManageRoles     = "manage_roles"
	PermManageEmployees = "manage_employees"
	PermManageUsers     = "manage_users"
	PermRead            = "read"
	PermPrint           = "print"
	PermWrite           = "write"
)

// Role (R in RBAC96) is fully manageable data — no role ID is ever compared
// against in authorization logic. These ID constants name the *seeded
// starting roles* only, used by the WordPress identity and as CLI/doc
// examples; a deployment may rename, delete, or add to them freely.
type Role struct {
	ID    string
	Name  string
	Scope Scope
}

// Seeded role IDs, matching migration 00004.
const (
	RoleSuperAdmin   = "super_admin"
	RoleMandantAdmin = "mandant_admin"
	RoleViewer       = "viewer"
	RoleReporter     = "reporter"
	RoleEditor       = "editor"
)

// ErrInvalidRole is the sentinel for role/permission-assignment validation
// failures.
var ErrInvalidRole = errors.New("invalid role")

// ValidRoleID checks the shape of a candidate role ID (not whether it exists
// — that is a store-layer lookup). Mirrors ValidUsername's shape rules: a
// role ID travels through CLI args and URLs, so whitespace makes it
// ambiguous there for no benefit.
func ValidRoleID(id string) error {
	if id == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidRole)
	}
	for _, r := range id {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return fmt.Errorf("%w: %q must be lowercase letters, digits, or underscores", ErrInvalidRole, id)
		}
	}
	return nil
}

// UserRole (UA in RBAC96, U x R) is one row of the user_roles table: an
// account holding a role at some scope. Exactly one of TenantID/EmployeeID is
// set, matching RoleID's own Scope — nil/nil means a system-scope
// assignment (see ValidUserRoleScope).
type UserRole struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	TenantID   *uuid.UUID
	EmployeeID *uuid.UUID
	RoleID     string
}

// ValidUserRoleScope checks that a role of the given scope is being assigned
// with the matching tenant_id/employee_id combination, mirroring the
// migration's trg_user_roles_scope trigger — so a CLI/HTTP caller gets a
// clear error instead of a raw SQLite constraint message, with the database
// trigger as the real, final enforcement.
func ValidUserRoleScope(scope Scope, tenantID, employeeID *uuid.UUID) error {
	switch {
	case scope == ScopeSystem && tenantID == nil && employeeID == nil:
		return nil
	case scope == ScopeTenant && tenantID != nil && employeeID == nil:
		return nil
	case scope == ScopeEmployee && tenantID == nil && employeeID != nil:
		return nil
	default:
		return fmt.Errorf("%w: a %s-scope role needs exactly the matching tenant/employee target", ErrInvalidRole, scope)
	}
}
