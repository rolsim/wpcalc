package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/rolsim/wpcalc/internal/domain"
)

// ErrDuplicateRole is returned when a role id is already taken.
var ErrDuplicateRole = errors.New("role id already exists")

// ErrRoleScopeTooNarrow is returned when a role's scope cannot satisfy a
// permission's min_scope (see migration 00004's trg_role_permissions_scope)
// or a user_roles row targets the wrong combination of tenant/employee for
// its role's scope (trg_user_roles_scope). Both triggers RAISE(ABORT, ...)
// with a message this sentinel matches, via isCheckViolation.
var ErrRoleScopeTooNarrow = errors.New("role scope too narrow")

// ErrRoleAlreadyAssignedDifferently is returned when a scope instance
// (a user's system access, or their access to one tenant or employee)
// already holds a different role than the one being granted. A user holds
// at most one role per scope instance; changing it is revoke-then-grant.
var ErrRoleAlreadyAssignedDifferently = errors.New("a different role is already assigned at this scope")

// Permissions lists the fixed permission catalog.
func (db *DB) Permissions(ctx context.Context) ([]domain.Permission, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, min_scope FROM permissions ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list permissions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Permission
	for rows.Next() {
		var p domain.Permission
		if err := rows.Scan(&p.ID, &p.MinScope); err != nil {
			return nil, fmt.Errorf("store: list permissions: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreateRole adds a role. Roles are fully manageable data — this is not
// restricted to the seeded starting set.
func (db *DB) CreateRole(ctx context.Context, id, name string, scope domain.Scope) error {
	if err := domain.ValidRoleID(id); err != nil {
		return err
	}
	if err := domain.ValidScope(string(scope)); err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: name is required", domain.ErrInvalidRole)
	}
	_, err := db.ExecContext(ctx, `INSERT INTO roles (id, name, scope) VALUES (?, ?, ?)`, id, name, string(scope))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("store: create role %q: %w", id, ErrDuplicateRole)
		}
		return fmt.Errorf("store: create role: %w", err)
	}
	return nil
}

// DeleteRole removes a role. Fails (via FK RESTRICT) if any role_permissions
// or user_roles row still references it — a role in use is not silently
// orphaned or cascaded away.
func (db *DB) DeleteRole(ctx context.Context, id string) error {
	res, err := db.ExecContext(ctx, `DELETE FROM roles WHERE id = ?`, id)
	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("store: delete role %q: role is still assigned or holds permissions", id)
		}
		return fmt.Errorf("store: delete role %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete role %q: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("store: delete role %q: %w", id, ErrNotFound)
	}
	return nil
}

// Roles lists every role.
func (db *DB) Roles(ctx context.Context) ([]domain.Role, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, scope FROM roles ORDER BY scope, id`)
	if err != nil {
		return nil, fmt.Errorf("store: list roles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Role
	for rows.Next() {
		var r domain.Role
		if err := rows.Scan(&r.ID, &r.Name, &r.Scope); err != nil {
			return nil, fmt.Errorf("store: list roles: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Role fetches one role by id.
func (db *DB) Role(ctx context.Context, id string) (domain.Role, error) {
	var r domain.Role
	err := db.QueryRowContext(ctx, `SELECT id, name, scope FROM roles WHERE id = ?`, id).Scan(&r.ID, &r.Name, &r.Scope)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Role{}, fmt.Errorf("store: role %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return domain.Role{}, fmt.Errorf("store: role %q: %w", id, err)
	}
	return r, nil
}

// AddRolePermission grants a role a permission (PA in RBAC96). Rejected if
// the role's scope is too narrow for the permission's min_scope — the same
// rule migration 00004's trg_role_permissions_scope enforces, surfaced here
// as ErrRoleScopeTooNarrow rather than a raw SQLite trigger message.
func (db *DB) AddRolePermission(ctx context.Context, roleID, permissionID string) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO role_permissions (role_id, permission_id) VALUES (?, ?)`, roleID, permissionID)
	if err != nil {
		if isCheckViolation(err) {
			return fmt.Errorf("store: add permission %q to role %q: %w", permissionID, roleID, ErrRoleScopeTooNarrow)
		}
		if isUniqueViolation(err) {
			return nil // already granted; idempotent
		}
		if isForeignKeyViolation(err) {
			return fmt.Errorf("store: add permission %q to role %q: %w", permissionID, roleID, ErrNotFound)
		}
		return fmt.Errorf("store: add permission %q to role %q: %w", permissionID, roleID, err)
	}
	return nil
}

// RemoveRolePermission revokes a permission from a role.
func (db *DB) RemoveRolePermission(ctx context.Context, roleID, permissionID string) error {
	if _, err := db.ExecContext(ctx,
		`DELETE FROM role_permissions WHERE role_id = ? AND permission_id = ?`, roleID, permissionID); err != nil {
		return fmt.Errorf("store: remove permission %q from role %q: %w", permissionID, roleID, err)
	}
	return nil
}

// RolePermissionsFor loads the permission sets for a batch of roles in one
// query, keyed by role id — used to resolve an identity's full permission set
// at login/session-load time (see internal/auth).
func (db *DB) RolePermissionsFor(ctx context.Context, roleIDs []string) (map[string][]string, error) {
	out := make(map[string][]string, len(roleIDs))
	if len(roleIDs) == 0 {
		return out, nil
	}

	placeholders := make([]string, len(roleIDs))
	args := make([]any, len(roleIDs))
	for i, id := range roleIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf(
		`SELECT role_id, permission_id FROM role_permissions WHERE role_id IN (%s)`,
		strings.Join(placeholders, ","))

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: role permissions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var roleID, permID string
		if err := rows.Scan(&roleID, &permID); err != nil {
			return nil, fmt.Errorf("store: role permissions: %w", err)
		}
		out[roleID] = append(out[roleID], permID)
	}
	return out, rows.Err()
}

// GrantUserRole assigns a role to a user at a scope (UA in RBAC96), one of
// system (tenantID and employeeID nil), tenant (tenantID set), or employee
// (employeeID set). Rejected if the combination doesn't match the role's own
// scope — mirrors migration 00004's trg_user_roles_scope, checked here first
// for a clear Go-level error before the trigger's.
//
// A user holds at most one role per scope instance (idx_user_roles_system/
// _tenant/_employee). Granting the same role again at the same scope is a
// no-op; granting a *different* role there is rejected with
// ErrRoleAlreadyAssignedDifferently rather than silently ignored — changing
// it is revoke-then-grant, never two rows disagreeing.
func (db *DB) GrantUserRole(ctx context.Context, userID int64, tenantID, employeeID *int64, roleID string) error {
	role, err := db.Role(ctx, roleID)
	if err != nil {
		return err
	}
	if err := domain.ValidUserRoleScope(role.Scope, tenantID, employeeID); err != nil {
		return err
	}

	_, err = db.ExecContext(ctx,
		`INSERT INTO user_roles (user_id, tenant_id, employee_id, role_id) VALUES (?, ?, ?, ?)`,
		userID, tenantID, employeeID, roleID)
	if err != nil {
		if isUniqueViolation(err) {
			existing, lookErr := db.userRoleAtScope(ctx, userID, tenantID, employeeID)
			if lookErr == nil && existing == roleID {
				return nil // already held at this scope; idempotent
			}
			return fmt.Errorf("store: grant %q: already holds %q at this scope: %w", roleID, existing, ErrRoleAlreadyAssignedDifferently)
		}
		if isCheckViolation(err) {
			return fmt.Errorf("store: grant %q: %w", roleID, ErrRoleScopeTooNarrow)
		}
		if isForeignKeyViolation(err) {
			return fmt.Errorf("store: grant %q: %w", roleID, ErrNotFound)
		}
		return fmt.Errorf("store: grant %q: %w", roleID, err)
	}
	return nil
}

// userRoleAtScope looks up which role (if any) a user currently holds at
// exactly one scope instance.
func (db *DB) userRoleAtScope(ctx context.Context, userID int64, tenantID, employeeID *int64) (string, error) {
	query := `SELECT role_id FROM user_roles WHERE user_id = ?`
	args := []any{userID}
	switch {
	case tenantID != nil:
		query += ` AND tenant_id = ?`
		args = append(args, *tenantID)
	case employeeID != nil:
		query += ` AND employee_id = ?`
		args = append(args, *employeeID)
	default:
		query += ` AND tenant_id IS NULL AND employee_id IS NULL`
	}
	var roleID string
	err := db.QueryRowContext(ctx, query, args...).Scan(&roleID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return roleID, err
}

// RevokeUserRole removes a user's role assignment at a scope. tenantID and
// employeeID nil both means the system scope.
func (db *DB) RevokeUserRole(ctx context.Context, userID int64, tenantID, employeeID *int64) error {
	query := `DELETE FROM user_roles WHERE user_id = ?`
	args := []any{userID}
	switch {
	case tenantID != nil:
		query += ` AND tenant_id = ?`
		args = append(args, *tenantID)
	case employeeID != nil:
		query += ` AND employee_id = ?`
		args = append(args, *employeeID)
	default:
		query += ` AND tenant_id IS NULL AND employee_id IS NULL`
	}
	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("store: revoke role: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: revoke role: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("store: revoke role: %w", ErrNotFound)
	}
	return nil
}

// UserRolesForUser lists every role a user holds, across every scope.
func (db *DB) UserRolesForUser(ctx context.Context, userID int64) ([]domain.UserRole, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, user_id, tenant_id, employee_id, role_id FROM user_roles WHERE user_id = ?`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: user roles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.UserRole
	for rows.Next() {
		var (
			ur         domain.UserRole
			tenantID   sql.NullInt64
			employeeID sql.NullInt64
		)
		if err := rows.Scan(&ur.ID, &ur.UserID, &tenantID, &employeeID, &ur.RoleID); err != nil {
			return nil, fmt.Errorf("store: user roles: %w", err)
		}
		if tenantID.Valid {
			ur.TenantID = &tenantID.Int64
		}
		if employeeID.Valid {
			ur.EmployeeID = &employeeID.Int64
		}
		out = append(out, ur)
	}
	return out, rows.Err()
}

// HasSystemAdmin reports whether any user_roles row grants manage_tenants or
// manage_roles at system scope — the real equivalent of "can anyone actually
// get in and set things up", expressed as a permission check rather than a
// role name. Used to gate `serve` startup the way an empty user table used
// to.
func (db *DB) HasSystemAdmin(ctx context.Context) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM user_roles ur
		JOIN role_permissions rp ON rp.role_id = ur.role_id
		WHERE ur.tenant_id IS NULL AND ur.employee_id IS NULL
		  AND rp.permission_id IN ('manage_tenants', 'manage_roles')`).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("store: has system admin: %w", err)
	}
	return n > 0, nil
}

// TenantsAccessibleToUser lists tenants a user can act in at all: every
// tenant if they hold any system-scope role (checked by permission-free
// existence, since a system-scope assignment always means "everything" —
// only its *permissions* determine what it may do once there), otherwise the
// tenants reached by a tenant-scope role plus the tenants of any
// employee-scope role's employee.
func (db *DB) TenantsAccessibleToUser(ctx context.Context, userID int64) ([]domain.Tenant, error) {
	var systemWide int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM user_roles WHERE user_id = ? AND tenant_id IS NULL AND employee_id IS NULL`,
		userID).Scan(&systemWide); err != nil {
		return nil, fmt.Errorf("store: accessible tenants: %w", err)
	}
	if systemWide > 0 {
		return db.Tenants(ctx)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT t.id, t.name FROM tenants t
		WHERE t.id IN (
			SELECT tenant_id FROM user_roles WHERE user_id = ? AND tenant_id IS NOT NULL
			UNION
			SELECT e.tenant_id FROM user_roles ur JOIN employees e ON e.id = ur.employee_id
			WHERE ur.user_id = ? AND ur.employee_id IS NOT NULL
		)
		ORDER BY t.name COLLATE NOCASE, t.id`, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("store: accessible tenants: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Tenant
	for rows.Next() {
		var t domain.Tenant
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, fmt.Errorf("store: accessible tenants: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// EmployeeRoleAssignment is one employee-scope user_roles row, joined with
// display names — the shape the /tenants/{id}/access page and its CLI
// equivalent list.
type EmployeeRoleAssignment struct {
	UserID       int64
	Username     string
	EmployeeID   int64
	EmployeeName string
	RoleID       string
	RoleName     string
}

// EmployeeRoleAssignmentsForTenant lists every employee-scope role
// assignment for employees in one tenant.
func (db *DB) EmployeeRoleAssignmentsForTenant(ctx context.Context, tenantID int64) ([]EmployeeRoleAssignment, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT u.id, u.username, e.id, e.display_name, r.id, r.name
		  FROM user_roles ur
		  JOIN users u ON u.id = ur.user_id
		  JOIN employees e ON e.id = ur.employee_id
		  JOIN roles r ON r.id = ur.role_id
		 WHERE e.tenant_id = ?
		 ORDER BY e.display_name COLLATE NOCASE, u.username COLLATE NOCASE`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: employee role assignments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []EmployeeRoleAssignment
	for rows.Next() {
		var a EmployeeRoleAssignment
		if err := rows.Scan(&a.UserID, &a.Username, &a.EmployeeID, &a.EmployeeName, &a.RoleID, &a.RoleName); err != nil {
			return nil, fmt.Errorf("store: employee role assignments: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AdminRoleAssignment is one system- or tenant-scope user_roles row, joined
// with display names — what the /roles page and its CLI equivalent list.
type AdminRoleAssignment struct {
	UserID     int64
	Username   string
	TenantID   *int64
	TenantName string // empty for a system-scope assignment
	RoleID     string
	RoleName   string
}

// AdminRoleAssignments lists every system-scope and tenant-scope role
// assignment across the whole database — the accounts that can manage
// tenants, roles, or one tenant's employees and users.
func (db *DB) AdminRoleAssignments(ctx context.Context) ([]AdminRoleAssignment, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT u.id, u.username, ur.tenant_id, COALESCE(t.name, ''), r.id, r.name
		  FROM user_roles ur
		  JOIN users u ON u.id = ur.user_id
		  JOIN roles r ON r.id = ur.role_id
		  LEFT JOIN tenants t ON t.id = ur.tenant_id
		 WHERE ur.employee_id IS NULL
		 ORDER BY r.scope, COALESCE(t.name, ''), u.username COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("store: admin role assignments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []AdminRoleAssignment
	for rows.Next() {
		var (
			a        AdminRoleAssignment
			tenantID sql.NullInt64
		)
		if err := rows.Scan(&a.UserID, &a.Username, &tenantID, &a.TenantName, &a.RoleID, &a.RoleName); err != nil {
			return nil, fmt.Errorf("store: admin role assignments: %w", err)
		}
		if tenantID.Valid {
			a.TenantID = &tenantID.Int64
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
