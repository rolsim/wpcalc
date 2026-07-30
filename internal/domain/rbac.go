package domain

import (
	"errors"
	"fmt"
)

// Package domain's RBAC types follow NIST RBAC96 (Sandhu, Coyne, Feinstein,
// Youman 1996 / ANSI INCITS 359-2004) naming verbatim: Roles, Permissions,
// UserRole (the User Assignment relation, UA ⊆ U x R). RolePermission (the
// Permission Assignment relation, PA ⊆ P x R) is store-layer only — it has no
// Go type of its own beyond the (roleID, permissionID) pair store.go passes
// around, since nothing outside the store needs to hold one.

// Scope says how broad a role or permission is: system is broadest, employee
// narrowest. It orders the three tiers the same way the migration's
// scope-consistency triggers do, so Go-side validation and the database's own
// enforcement can never drift apart.
type Scope string

const (
	ScopeSystem   Scope = "system"
	ScopeTenant   Scope = "tenant"
	ScopeEmployee Scope = "employee"
)

func (s Scope) rank() int {
	switch s {
	case ScopeSystem:
		return 0
	case ScopeTenant:
		return 1
	default:
		return 2
	}
}

// Covers reports whether s is broad enough to satisfy a requirement of scope
// other — i.e. s is at least as broad as other.
func (s Scope) Covers(other Scope) bool { return s.rank() <= other.rank() }

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
	ID         int64
	UserID     int64
	TenantID   *int64
	EmployeeID *int64
	RoleID     string
}

// ValidUserRoleScope checks that a role of the given scope is being assigned
// with the matching tenant_id/employee_id combination, mirroring the
// migration's trg_user_roles_scope trigger — so a CLI/HTTP caller gets a
// clear error instead of a raw SQLite constraint message, with the database
// trigger as the real, final enforcement.
func ValidUserRoleScope(scope Scope, tenantID, employeeID *int64) error {
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
