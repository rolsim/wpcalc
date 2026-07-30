-- +goose Up

-- Multi-tenancy: several companies ("Mandanten") isolated in one database.
-- Existing employees and day comments land in a "Default" tenant so the
-- upgrade never orphans data; new deployments rename or replace it.
CREATE TABLE tenants (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL UNIQUE CHECK (length(trim(name)) > 0),
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO tenants (id, name) VALUES (1, 'Default');

ALTER TABLE employees ADD COLUMN tenant_id INTEGER NOT NULL REFERENCES tenants (id) DEFAULT 1;

-- day_comments' old UNIQUE(work_date) is a column-level constraint, which
-- ALTER TABLE ADD COLUMN cannot remove — it would keep enforcing one comment
-- per day *globally* alongside a new (tenant_id, work_date) index, silently
-- defeating tenant scoping. The table is rebuilt instead, dropping that
-- constraint and replacing it with UNIQUE (tenant_id, work_date).
CREATE TABLE day_comments_new (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id  INTEGER NOT NULL REFERENCES tenants (id),
    work_date  TEXT    NOT NULL CHECK (work_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    comment    TEXT    NOT NULL,
    created_at TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT    NOT NULL DEFAULT (datetime('now')),
    UNIQUE (tenant_id, work_date)
);
INSERT INTO day_comments_new (id, tenant_id, work_date, comment, created_at, updated_at)
    SELECT id, 1, work_date, comment, created_at, updated_at FROM day_comments;
DROP TABLE day_comments;
ALTER TABLE day_comments_new RENAME TO day_comments;

-- ---------------------------------------------------------------------
-- RBAC96 (Sandhu et al. 1996 / ANSI INCITS 359-2004), used verbatim as the
-- schema's own naming rather than as an analogy: Users, Roles, Permissions,
-- User Assignment (UA), Permission Assignment (PA), Sessions.
-- ---------------------------------------------------------------------

-- Permissions (P/PRMS) are the one fixed part of this schema: a permission is
-- only real if some route guard actually checks for it, so inventing one
-- through the UI would do nothing (the same boundary AWS/GCP IAM draw —
-- "actions" are service-defined, "policies" bundling them are user-managed).
-- min_scope is how broad a role assignment must be for the permission to
-- mean anything: manage_tenants only makes sense system-wide; read/print/
-- write can apply as narrowly as one employee.
CREATE TABLE permissions (
    id        TEXT PRIMARY KEY,
    min_scope TEXT NOT NULL CHECK (min_scope IN ('system', 'tenant', 'employee'))
);
INSERT INTO permissions (id, min_scope) VALUES
    ('manage_tenants',   'system'),
    ('manage_roles',     'system'),
    ('manage_employees', 'tenant'),
    ('manage_users',     'tenant'),
    ('read',  'employee'),
    ('print', 'employee'),
    ('write', 'employee');

-- Roles (R) are fully manageable data — no role name is ever hardcoded in Go
-- authorization logic. scope is how broad an assignment of this role is
-- (system/tenant/employee, matching user_roles below).
CREATE TABLE roles (
    id    TEXT PRIMARY KEY,
    name  TEXT NOT NULL,
    scope TEXT NOT NULL CHECK (scope IN ('system', 'tenant', 'employee'))
);
-- Seed data — a starting point, not a fixed set. Anyone holding manage_roles
-- can add, rename, delete (once unassigned), or rebalance any of these.
INSERT INTO roles (id, name, scope) VALUES
    ('super_admin',   'Super-admin',   'system'),
    ('mandant_admin', 'Mandant-admin', 'tenant'),
    ('viewer',        'Viewer',        'employee'),
    ('reporter',      'Reporter',      'employee'),
    ('editor',        'Editor',        'employee');

-- PA (Permission Assignment, P x R).
CREATE TABLE role_permissions (
    role_id       TEXT NOT NULL REFERENCES roles (id)       ON DELETE CASCADE,
    permission_id TEXT NOT NULL REFERENCES permissions (id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);
-- SQLite prohibits subqueries in CHECK constraints, so the "a role's scope
-- must be at least as broad as every permission it holds" rule (system is
-- broadest, employee narrowest — e.g. mandant_admin, scope='tenant', cannot
-- hold manage_tenants, min_scope='system') is enforced with a trigger instead.
-- +goose StatementBegin
CREATE TRIGGER trg_role_permissions_scope
BEFORE INSERT ON role_permissions
FOR EACH ROW
WHEN (
    (SELECT CASE scope WHEN 'system' THEN 0 WHEN 'tenant' THEN 1 ELSE 2 END FROM roles WHERE id = NEW.role_id)
    >
    (SELECT CASE min_scope WHEN 'system' THEN 0 WHEN 'tenant' THEN 1 ELSE 2 END FROM permissions WHERE id = NEW.permission_id)
)
BEGIN
    SELECT RAISE(ABORT, 'role scope too narrow for permission');
END;
-- +goose StatementEnd
INSERT INTO role_permissions (role_id, permission_id) VALUES
    ('super_admin',   'manage_tenants'), ('super_admin',   'manage_roles'),
    ('super_admin',   'manage_employees'), ('super_admin', 'manage_users'),
    ('super_admin',   'read'), ('super_admin',   'print'), ('super_admin',   'write'),
    ('mandant_admin', 'manage_employees'), ('mandant_admin', 'manage_users'),
    ('mandant_admin', 'read'), ('mandant_admin', 'print'), ('mandant_admin', 'write'),
    ('viewer',        'read'),
    ('reporter',      'read'), ('reporter',      'print'),
    ('editor',        'read'), ('editor',        'print'), ('editor',        'write');

-- UA (User Assignment, U x R), named `user_roles` — the name virtually every
-- practical RBAC96 implementation gives this relation. tenant_id/employee_id
-- are the standard scoped-binding extension multi-tenant systems add on top
-- of core RBAC (the same shape as a Kubernetes RoleBinding or a GCP/AWS IAM
-- policy binding: subject + roleRef + scope), not a bespoke mechanism.
CREATE TABLE user_roles (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users (id)     ON DELETE CASCADE,
    tenant_id   INTEGER     NULL REFERENCES tenants (id)   ON DELETE CASCADE,
    employee_id INTEGER     NULL REFERENCES employees (id) ON DELETE CASCADE,
    role_id     TEXT    NOT NULL REFERENCES roles (id),
    created_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);
-- Which of tenant_id/employee_id must be set follows the role's scope. Same
-- subqueries-in-CHECK limitation as above, so this is a trigger too, not a
-- CHECK naming each role — adding a role never means touching this rule.
-- +goose StatementBegin
CREATE TRIGGER trg_user_roles_scope
BEFORE INSERT ON user_roles
FOR EACH ROW
WHEN NOT (
    (NEW.tenant_id IS NULL     AND NEW.employee_id IS NULL     AND NEW.role_id IN (SELECT id FROM roles WHERE scope = 'system'))   OR
    (NEW.tenant_id IS NOT NULL AND NEW.employee_id IS NULL     AND NEW.role_id IN (SELECT id FROM roles WHERE scope = 'tenant'))   OR
    (NEW.tenant_id IS NULL     AND NEW.employee_id IS NOT NULL AND NEW.role_id IN (SELECT id FROM roles WHERE scope = 'employee'))
)
BEGIN
    SELECT RAISE(ABORT, 'role assigned at the wrong scope');
END;
-- +goose StatementEnd
-- Partial unique indexes give each assignment kind its own "one per scope"
-- rule: a user holds only one role at a given scope instance, so changing an
-- employee-level role is revoke-then-grant, never two rows disagreeing.
CREATE UNIQUE INDEX idx_user_roles_system   ON user_roles (user_id)              WHERE tenant_id IS NULL AND employee_id IS NULL;
CREATE UNIQUE INDEX idx_user_roles_tenant   ON user_roles (user_id, tenant_id)   WHERE tenant_id IS NOT NULL;
CREATE UNIQUE INDEX idx_user_roles_employee ON user_roles (user_id, employee_id) WHERE employee_id IS NOT NULL;

-- Access is now 100% user_roles-derived; the old flat column is gone.
ALTER TABLE users DROP COLUMN role;

-- Sessions (S): a session activates a subset of a user's assigned roles.
-- active_tenant_id is exactly that activation, adapted to tenant scoping —
-- which of the user's several tenant memberships is "on" for this session.
ALTER TABLE sessions ADD COLUMN active_tenant_id INTEGER REFERENCES tenants (id);

-- +goose Down

ALTER TABLE sessions DROP COLUMN active_tenant_id;
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user'));
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS permissions;
ALTER TABLE employees DROP COLUMN tenant_id;

CREATE TABLE day_comments_old (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    work_date  TEXT NOT NULL UNIQUE CHECK (work_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    comment    TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO day_comments_old (id, work_date, comment, created_at, updated_at)
    SELECT id, work_date, comment, created_at, updated_at FROM day_comments;
DROP TABLE day_comments;
ALTER TABLE day_comments_old RENAME TO day_comments;

DROP TABLE IF EXISTS tenants;
