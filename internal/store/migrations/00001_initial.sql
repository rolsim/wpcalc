-- +goose Up

-- The whole schema, declared once. This project has no deployment whose data
-- survives, so the incremental history that used to live in six files (initial
-- grid, accounts, user language, RBAC/multi-tenancy, API tokens, token expiry)
-- has been collapsed into the state those migrations added up to. Existing
-- databases are not upgraded — they are deleted and recreated.
--
-- ---------------------------------------------------------------------
-- Two conventions run through everything below.
--
-- Dates and timestamps are ISO TEXT ('YYYY-MM-DD'), not SQLite's numeric date
-- types. They sort and compare lexicographically in exactly calendar order,
-- they survive a sqlite3 shell dump unambiguously, and they cannot pick up a
-- timezone offset on the way in or out. The GLOB checks are deliberate
-- belt-and-braces: the domain layer already refuses malformed dates, but these
-- tables outlive any one version of that code, and a bad date here silently
-- drops rows out of month queries.
--
-- Surrogate keys are UUIDv4 in canonical text form, minted by the application,
-- never by the database. An INTEGER AUTOINCREMENT is one counter shared by
-- every tenant, so the id handed back on a create discloses how many rows exist
-- database-wide: a tenant admin who adds an employee and receives id 47 learns
-- that 46 employees exist across all tenants, and sampling that over time
-- yields another company's headcount and its growth rate. No authorization
-- check can close that, because the leak is in the identifier itself.
-- Sequential ids also turn any single missing authorization check into a
-- whole-table sweep rather than one wrong row.
--
-- v4 (random) and not v7: a v7's 48-bit millisecond prefix would put record
-- creation time back into the identifier, which is the class of disclosure the
-- choice exists to remove, and there is no write-throughput problem at the
-- scale of one company's timesheet for v7's index locality to solve. Text and
-- not BLOB(16): 20 extra bytes per id buys a database that stays legible in a
-- sqlite3 shell and in a dump.
--
-- Roles and permissions are the exception — their ids are semantic slugs
-- ('super_admin', 'manage_tenants'), not surrogates, and nothing about them is
-- enumerable or tenant-specific.
-- ---------------------------------------------------------------------

-- Multi-tenancy: several companies ("Mandanten") isolated in one database.
CREATE TABLE tenants (
    id         TEXT NOT NULL PRIMARY KEY CHECK (length(id) = 36 AND id GLOB '*-*-*-*-*'),
    name       TEXT NOT NULL UNIQUE CHECK (length(trim(name)) > 0),
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- The Default tenant's id is generated rather than hardcoded, so not even the
-- seeded row has a well-known identifier. This is SQLite's standard recipe for
-- a v4 UUID: random bytes with the version nibble pinned to 4 and the variant
-- nibble drawn from 8/9/a/b.
INSERT INTO tenants (id, name) VALUES (
    lower(
        hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' ||
        substr(hex(randomblob(2)), 2) || '-' ||
        substr('89ab', abs(random()) % 4 + 1, 1) ||
        substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))
    ),
    'Default'
);

-- Accounts that can sign in to the standalone server. Under WordPress this
-- table is unused: identity comes from WordPress's own user table via the
-- signed headers, and a parallel account list would be a second, weaker way
-- into the same data.
--
-- An account's access is entirely derived from its user_roles rows — there is
-- no role column here. language empty means "follow the browser", and is not
-- constrained to a list of locales: a catalog can be removed or renamed, and a
-- preference naming one that no longer ships should degrade to the default
-- rather than make the row unreadable.
CREATE TABLE users (
    id            TEXT NOT NULL PRIMARY KEY CHECK (length(id) = 36 AND id GLOB '*-*-*-*-*'),
    username      TEXT NOT NULL UNIQUE COLLATE NOCASE
                       CHECK (length(trim(username)) > 0),
    password_hash TEXT NOT NULL,
    language      TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Sessions (S in RBAC96) live server-side rather than in a signed cookie so
-- that logging someone out actually revokes their access; a self-contained
-- signed token stays valid until it expires no matter what the server wants.
--
-- active_tenant_id is RBAC96's session role activation, adapted to tenant
-- scoping: which of the user's several tenant memberships is "on" for this
-- session.
CREATE TABLE sessions (
    token            TEXT NOT NULL PRIMARY KEY,
    user_id          TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    active_tenant_id TEXT     NULL REFERENCES tenants (id),
    expires_at       TEXT NOT NULL,
    created_at       TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_sessions_expires ON sessions (expires_at);

CREATE TABLE employees (
    id           TEXT NOT NULL PRIMARY KEY CHECK (length(id) = 36 AND id GLOB '*-*-*-*-*'),
    tenant_id    TEXT NOT NULL REFERENCES tenants (id),
    display_name TEXT NOT NULL CHECK (length(trim(display_name)) > 0),
    start_date   TEXT NOT NULL CHECK (start_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    end_date     TEXT     NULL CHECK (end_date IS NULL OR end_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at   TEXT NOT NULL DEFAULT (datetime('now')),

    -- A zero-length employment is not a thing; equal dates mean one day.
    CHECK (end_date IS NULL OR end_date >= start_date)
);

-- Hours live as integer hundredths ("industrial minutes"): 7.75 h is 775.
-- Summing these along both grid axes and again in the PDFs must agree exactly,
-- which REAL cannot guarantee. The upper bound matches domain.MaxDailyCentihours.
CREATE TABLE time_entries (
    id          TEXT    NOT NULL PRIMARY KEY CHECK (length(id) = 36 AND id GLOB '*-*-*-*-*'),
    employee_id TEXT    NOT NULL REFERENCES employees (id) ON DELETE CASCADE,
    work_date   TEXT    NOT NULL CHECK (work_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    centihours  INTEGER NOT NULL CHECK (centihours >= 0 AND centihours <= 2400),
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT    NOT NULL DEFAULT (datetime('now')),

    -- One cell in the grid is one row here.
    UNIQUE (employee_id, work_date)
);

-- Every read is "one month of entries", so the range scan wants work_date first.
CREATE INDEX idx_time_entries_date ON time_entries (work_date, employee_id);

-- One comment per calendar day per tenant, belonging to the day rather than to
-- any employee — it is the row header note, not a per-cell annotation.
CREATE TABLE day_comments (
    id         TEXT NOT NULL PRIMARY KEY CHECK (length(id) = 36 AND id GLOB '*-*-*-*-*'),
    tenant_id  TEXT NOT NULL REFERENCES tenants (id),
    work_date  TEXT NOT NULL CHECK (work_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    comment    TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (tenant_id, work_date)
);

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
    id          TEXT NOT NULL PRIMARY KEY CHECK (length(id) = 36 AND id GLOB '*-*-*-*-*'),
    user_id     TEXT NOT NULL REFERENCES users (id)     ON DELETE CASCADE,
    tenant_id   TEXT     NULL REFERENCES tenants (id)   ON DELETE CASCADE,
    employee_id TEXT     NULL REFERENCES employees (id) ON DELETE CASCADE,
    role_id     TEXT NOT NULL REFERENCES roles (id),
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
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

-- Bearer tokens for /api/v1, distinct from the browser session cookie: a token
-- is a long-lived credential a script holds, not a short-lived session a login
-- flow issues, so it gets its own table rather than reusing `sessions` (which
-- also carries an active_tenant_id that has no meaning for a stateless API
-- request — every API call names its tenant in the path instead).
--
-- Only the SHA-256 hash is stored; the token itself is shown once, at creation
-- time, and cannot be recovered afterward — the same trust model as a password,
-- not a session cookie that the server could reissue. Every access token
-- expires (1 hour — domain.AccessTokenTTL); a leaked token that never expired
-- was a standing liability.
CREATE TABLE api_tokens (
    id           TEXT NOT NULL PRIMARY KEY CHECK (length(id) = 36 AND id GLOB '*-*-*-*-*'),
    user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         TEXT NOT NULL CHECK (length(trim(name)) > 0),
    token_hash   TEXT NOT NULL UNIQUE,
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at   TEXT,
    last_used_at TEXT,
    revoked_at   TEXT
);
CREATE INDEX idx_api_tokens_user ON api_tokens (user_id);

-- Refresh tokens: a longer-lived (30 days — domain.RefreshTokenTTL),
-- single-use credential for minting a new access token without going back to
-- `wpcalc token create`. Deliberately its own table rather than a "kind" column
-- on api_tokens: the two are hashed and looked up the same way, but a refresh
-- token is never itself accepted as a bearer credential, and mixing the two
-- into one table/lookup risks that boundary blurring by accident later.
--
-- used_at marks single use: exchanging a refresh token sets it and, in the same
-- transaction, inserts both a new access token row and a new (rotated)
-- refresh_tokens row — reusing an already-used, expired, or revoked refresh
-- token fails exactly like an unknown one, same indistinguishability principle
-- as api_tokens.
CREATE TABLE refresh_tokens (
    id         TEXT NOT NULL PRIMARY KEY CHECK (length(id) = 36 AND id GLOB '*-*-*-*-*'),
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name       TEXT NOT NULL CHECK (length(trim(name)) > 0),
    token_hash TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at TEXT NOT NULL,
    used_at    TEXT,
    revoked_at TEXT
);
CREATE INDEX idx_refresh_tokens_user ON refresh_tokens (user_id);

-- +goose Down

DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS api_tokens;
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS day_comments;
DROP TABLE IF EXISTS time_entries;
DROP TABLE IF EXISTS employees;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS tenants;
