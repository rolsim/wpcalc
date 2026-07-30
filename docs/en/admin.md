# wpcalc — administrator guide

Installing, running and maintaining wpcalc.

*Deutsch: [../de-CH/admin.md](../de-CH/admin.md)*

---

> **This is a multi-tenant application with enforced RBAC.** Several
> companies ("Mandanten") can share one database, fully isolated from each
> other. Access follows the NIST RBAC96 model: an account holds one or more
> **roles**, each scoped to the whole system, to one tenant, or to one
> employee. See [Multi-tenancy and roles](#multi-tenancy-and-roles) below —
> a freshly created account has **no access at all** until a role is
> granted.

## Two ways to run

**Standalone** — the binary serves HTTP itself, with its own accounts. Use
this behind a reverse proxy, or on a LAN.

**WordPress** — the binary runs as a sidecar on a unix socket and a plugin
proxies admin requests to it. Identity comes from WordPress; there is no
second login and no separate account list.

Both serve exactly the same application.

## Standalone

```sh
make build                                            # -> bin/wpcalc
./bin/wpcalc migrate --db /var/lib/wpcalc/wpcalc.db   # creates the file
./bin/wpcalc user add alice --db /var/lib/wpcalc/wpcalc.db
./bin/wpcalc user grant alice --system -role super_admin --db /var/lib/wpcalc/wpcalc.db
./bin/wpcalc serve --addr :8080 --db /var/lib/wpcalc/wpcalc.db
```

`serve` **refuses to start when no account can manage the database**, rather
than offering a login that cannot succeed. Create the first account and
grant it `super_admin` before starting it — see
[Multi-tenancy and roles](#multi-tenancy-and-roles).

The binary is statically linked with no cgo, so it has no runtime
dependencies — copy it to the host and run it.

### Commands

| Command | Purpose |
|---|---|
| `serve --addr :8080 \| --socket PATH` | run the server; exactly one listener |
| `migrate [up\|down\|status]` | apply, roll back one, or report migrations |
| `user add\|passwd\|lang\|roles\|list` | manage accounts |
| `user grant\|revoke <name> [-system\|-tenant ID\|-employee ID] [-role ID]` | assign or remove a role |
| `tenant add\|list\|rename` | manage tenants ("Mandanten") |
| `role add\|list\|delete\|permissions` | manage the role catalog and what each role can do |
| `permission list` | list the fixed permission catalog (read-only) |
| `sample-employees [--month YYYY-MM] [--tenant ID]` | create placeholder employees; records **no hours** |
| `manual [user\|admin]` | show an embedded manual |
| `plugin export DIR` | write the WordPress plugin out of the binary |
| `version [--short]` | print the build: version, commit, date, Go |

Flags work before or after positional arguments.

### Reading the manuals

Both guides are embedded in the binary, so they travel with it — a server with
no source tree beside it still has them:

```sh
wpcalc manual              # user guide, in your shell's language
wpcalc manual admin        # this document
wpcalc manual admin --lang en
wpcalc manual --list       # what is available
wpcalc manual admin --raw  # markdown, for piping
```

Install [glow](https://github.com/charmbracelet/glow) for a rendered, paged
version. Without it the raw markdown is printed, which is still readable. The
raw form is also used automatically when the output is not a terminal, so
`wpcalc manual admin > admin.md` does not fill the file with escape codes.

### Environment

| Variable | Used by | Meaning |
|---|---|---|
| `WPCALC_DB` | all | default database path |
| `WPCALC_SECRET` | `serve --socket` | secret shared with the WordPress plugin |
| `WPCALC_BASE_PATH` | `serve` | URL prefix, or full base URL with `--link-param` |
| `WPCALC_LINK_PARAM` | `serve` | carry the app path in this query parameter |

### Behind a reverse proxy

Terminate TLS at the proxy and pass `--secure-cookies` so session cookies are
marked `Secure`. It is off by default because the standalone server is often
reached over plain HTTP on a LAN, where a `Secure` cookie is simply never sent
and login appears to fail for no reason.

## Accounts

```sh
./bin/wpcalc user add alice --db DB   # prompts for a password; no access yet
./bin/wpcalc user passwd alice --db DB            # also revokes alice's sessions
./bin/wpcalc user list --db DB
```

Passwords are bcrypt-hashed and must be at least 10 characters. Usernames are
case-insensitive. Sessions are stored server-side and last 12 hours, so
signing out or changing a password revokes access immediately rather than
leaving a token valid until it expires.

`user add` only creates credentials — the account can do nothing until a role
is granted (see the next section). There is no moment where an account exists
with an implicit role nobody asked for.

> `--allow-weak-password` waives the length requirement. It exists so a local
> development database can be primed with throwaway credentials such as
> `admin`/`admin`, and it prints a warning whenever it is used. **Never use it
> on anything reachable.**

### Interface language

Each account stores a preferred language, or an empty value meaning "follow the
browser". Users change it themselves from the selector in the top bar; you can
set it when creating an account or afterwards:

```sh
./bin/wpcalc user add alice -lang en --db DB
./bin/wpcalc user lang alice de-CH --db DB    # de_CH is accepted too
./bin/wpcalc user lang alice "" --db DB       # back to following the browser
```

A stored preference beats the browser's `Accept-Language`, because it is the
more specific statement. A preference naming a locale that no longer ships
falls back to negotiation rather than breaking the account.

**Under WordPress this does not apply.** WordPress owns the user record, so the
interface follows each user's WordPress profile locale and the app hides its
own selector. Storing a second preference here could disagree with the one the
site administrator set, with no way to tell which was authoritative.

There is no way to delete an account from the CLI yet; remove the row from the
`users` table directly if you need to. Its sessions go with it.

## Multi-tenancy and roles

wpcalc follows [NIST RBAC96](https://csrc.nist.gov/projects/role-based-access-control)
(Sandhu, Coyne, Feinstein, Youman 1996) — the same model as Kubernetes
RoleBindings or AWS/GCP IAM policies: a **role** bundles **permissions**, and
a **role assignment** grants a role to a user at a **scope**. There are three
scopes, from broadest to narrowest:

- **system** — the whole database; a system-scope role covers every tenant.
- **tenant** — one company ("Mandant"); covers every employee in it.
- **employee** — one person's timesheet data only.

Out of the box, five roles exist (all fully editable — see below):

| Role | Scope | Can do |
|---|---|---|
| `super_admin` | system | Everything: manage tenants, roles, employees, and users, anywhere. |
| `mandant_admin` | tenant | Manage one tenant's employees and its users' employee-scope roles. |
| `viewer` | employee | Read one employee's grid. |
| `reporter` | employee | Read, and download that employee's PDF reports. |
| `editor` | employee | Read, print, and enter that employee's hours. |

Every permission check in the app is a lookup against the role a caller holds
at a scope covering the thing they're touching — nothing is hardcoded to a
role's name. A `mandant_admin`'s access ends at their own tenant: they get a
`404` (not `403`, so existence isn't leaked) reaching into another tenant's
employee, and `403` on system-wide pages like tenant management.

### Setting it up

```sh
./bin/wpcalc tenant add "Acme Corp" --db DB               # -> tenant id, e.g. 2
./bin/wpcalc user add alice --db DB
./bin/wpcalc user grant alice --system -role super_admin --db DB

./bin/wpcalc user add bob --db DB
./bin/wpcalc user grant bob -tenant 2 -role mandant_admin --db DB

./bin/wpcalc user add carol --db DB
./bin/wpcalc user grant carol -employee 5 -role viewer --db DB

./bin/wpcalc user roles bob --db DB                       # what bob can reach
./bin/wpcalc user revoke carol -employee 5 --db DB         # revoke-then-grant to change a role
```

A user holds at most one role per scope *instance* — `-employee 5` twice with
different roles is rejected; revoke first. `-system`, `-tenant ID`, and
`-employee ID` are mutually exclusive; exactly one is required.

An account with several tenant- or employee-scope grants across different
tenants sees a **tenant switcher** in the top bar and, on first login (or
after a role change leaves the previously active tenant unreachable), a
chooser page — RBAC96 calls this "session role activation": which of the
account's several tenant memberships is active for this browser session.

### Managing roles themselves

Roles, their permissions, and every grant are editable from the web UI as
well as the CLI — nothing here is a fixed enum:

- **`/tenants`** (`manage_tenants`, system-wide) — list and create tenants.
- **`/tenants/{id}/access`** (`manage_users`, per tenant) — grant or revoke an
  *employee-scope* role for a user on one of that tenant's employees. A
  mandant-admin reaches this but cannot mint another mandant-admin here —
  that's deliberate; see below.
- **`/roles`** (`manage_roles`, system-wide) — the only page that can create
  another `super_admin` or `mandant_admin`. Also where roles themselves are
  defined: create a role, delete one (fails while still assigned or holding
  permissions), and toggle which permissions it holds.

```sh
./bin/wpcalc role add auditor -name Auditor -scope tenant --db DB
./bin/wpcalc role permissions auditor -add read --db DB
./bin/wpcalc role permissions auditor -add manage_tenants --db DB   # rejected: min_scope=system
./bin/wpcalc role list --db DB
./bin/wpcalc permission list --db DB
```

Permissions themselves (`read`, `print`, `write`, `manage_employees`,
`manage_users`, `manage_tenants`, `manage_roles`) are the one fixed part of
this: each corresponds to an actual check in the code, so there is no
`permission add` — inventing one through the UI would do nothing. A role's
`scope` must be broad enough for every permission it holds (`manage_tenants`
needs `system`; `read` can apply as narrowly as `employee`) — both the CLI
and a database trigger enforce this.

## Employees

Manage them under **Mitarbeitende / Employees**, scoped to whichever tenant
is currently active (see [Multi-tenancy and roles](#multi-tenancy-and-roles)
above) — this page and its actions all require `manage_employees` in that
tenant. Each employee has a name, a start date and an optional end date;
leave the end date empty while someone is still employed.

Two behaviours worth knowing:

- An employee appears in a month only if their employment overlaps it. Leavers
  disappear from later months rather than widening every grid you page through.
- **Shortening an employment does not delete hours** already recorded outside
  the new dates. Those entries stop being visible and editable but remain in
  the database, because silently destroying recorded hours to fix a typo in a
  date would be worse. Widen the dates again to see them.

Deleting an employee **does** delete all their recorded hours, by cascade.
There is no undo.

## Reports

Under **Auswertungen / Reports**, or directly:

```
/report/month/2026-07.pdf                  monthly overview
/report/employee/3/month/2026-07.pdf       one employee, one month
/report/employee/3/year/2026.pdf           one employee, one year
```

Every figure comes from the same queries the grid renders from, so a printed
page cannot disagree with the screen.

## API

wpcalc documents its own HTTP surface — `GET /openapi.json`, `/openapi.yaml`,
and `/openapi.html`, no session required. That covers this page's own
routes: the HTML app, its form bodies, its redirects.

`/openapi.html` is an interactive Swagger UI page — every operation,
schema, and a live "Authorize" + "Try it out" flow — not just a static
listing. Its JS/CSS (`internal/specdoc/vendor/swagger-ui/`, Apache-2.0) are
vendored into the binary at a pinned version, not loaded from a CDN, so the
page still renders with no outbound network access.

A second, separate surface, `/api/v1`, mirrors most of the same resources
(tenants, employees, the hours grid, day comments, reports, roles,
permissions, role assignments) as a stateless JSON API — for scripts, not
browsers. It documents itself the same way, under
`/api/v1/openapi.{json,yaml,html}`.

### Authentication

`/api/v1` never accepts the `wpcalc_session` cookie, and the HTML app never
accepts a bearer token — two separate `Authenticator`s, by construction.
Issue a token from the CLI:

```sh
./bin/wpcalc token create alice -name ci    # prints the token once — store it now
./bin/wpcalc token list alice               # id, name, created, last used, active/revoked
./bin/wpcalc token revoke 3
```

The plaintext is shown exactly once, at creation; only its SHA-256 hash is
ever stored. Revoking a token does not touch the account's password or any
browser session, and takes effect on the very next request — nothing is
cached.

```sh
curl -H "Authorization: Bearer wpat_..." http://localhost:8080/api/v1/tenants
```

### What differs from the HTML app

- **Every request names its tenant explicitly in the path**
  (`/api/v1/tenants/{tenantId}/...`) — a bearer token has no session to hold
  an "active tenant" in, so there is no tenant switcher or chooser here.
- **No login, logout, language-preference, or tenant-switch routes** —
  meaningless for a stateless client.
- Every response is JSON with a real status code — no `303` redirects, no
  `?err=` query-string tokens.

### Authorization

Same RBAC96 model as the rest of the app (see
[Multi-tenancy and roles](#multi-tenancy-and-roles) above) — a bearer token
resolves to the same identity a session cookie would, so the same roles and
permissions apply. Each operation documents which permission it requires
and at what scope; a few examples:

| Operation | Permission | Scope |
|---|---|---|
| `GET /api/v1/tenants` | `manage_tenants` | system |
| `GET /api/v1/tenants/{tenantId}/employees` | `manage_employees` | tenant |
| `GET /api/v1/tenants/{tenantId}/months/{ym}` | `read` | employee |
| `PUT .../months/{ym}/entries` | `write` | employee |
| `GET .../months/{ym}/report` | `print` | tenant or employee |
| `POST /api/v1/roles` | `manage_roles` | system |

The full, authoritative list — every operation, its request and response
shapes, and its permission — is in `/api/v1/openapi.html`, generated from
the same spec (`internal/apiv1/openapi.yaml`) that `oapi-codegen` built the
server from.

### Contract enforcement

`openapi.yaml` isn't only documentation: every `/api/v1` request is validated
against it before any handler runs — a missing required field, a value
outside an enum, a string that doesn't match its declared pattern, a
path parameter of the wrong type — and every response is validated against
it too, before it reaches the client. A request that violates the spec gets
a `400` with a message naming what failed, not a partial or best-effort
attempt to handle it; a response that would violate the spec becomes a
clean `500` rather than shipping a body a client's own generated types
couldn't parse.

## WordPress

The binary carries the plugin, so it can install itself:

```sh
wpcalc plugin export /var/www/html/wp-content/plugins
```

That writes `wpcalc/wpcalc.php` and `wpcalc/bin/wpcalc` — a copy of the binary
that wrote it, which is what keeps the two versions in step. Then activate
**wpcalc** in WordPress and open **Working hours** in the admin menu.

It refuses to overwrite an existing plugin directory unless you pass
`--force`. `--php-only` writes the PHP without a binary, for the case where the
sidecar is installed system-wide and the path is set in the settings.

The WordPress e2e suite mounts the exported plugin rather than the source
directory, so a broken export fails the tests instead of going unnoticed.

The plugin starts the binary on demand as a sidecar, supervises it, and proxies
requests with a signed assertion of the current WordPress user. Access requires
the `manage_options` capability; writes carry a WordPress nonce.

Runtime files live in `wp-content/uploads/wpcalc/` — the database, the socket,
a PID file and a log. The plugin writes an `.htaccess` there denying web
access; **if your server ignores `.htaccess`, block that directory yourself**,
or the database is downloadable.

**wpcalc → Settings** shows whether the service is running, whether the binary
is present and executable, whether `proc_open` and `curl` are available, the
runtime paths, and the last lines of the log. It can also restart the service
and regenerate the shared secret.

Requirements: PHP 8.1+, WordPress 6.4+, `proc_open` enabled, the `curl`
extension with unix-socket support. If `proc_open` is disabled the admin page
says so rather than failing silently.

## Database and backup

One SQLite file. In WAL mode there are also `-wal` and `-shm` files.

```sh
# Consistent copy while the server is running:
sqlite3 /var/lib/wpcalc/wpcalc.db ".backup '/backup/wpcalc-$(date +%F).db'"
```

Do not copy the `.db` file with `cp` while the server is running — you may get
a torn copy that omits everything still in the WAL. Migrations run
automatically at startup; take a backup before upgrading the binary.

## Troubleshooting

**`address already in use`** — something else holds the port. Note that a
container published on `0.0.0.0:PORT` also blocks `127.0.0.1:PORT`.

```sh
ss -ltnp | grep :8080
```

**`no account can manage this database yet`** — either no account exists, or
none holds `super_admin`. Run `user add` and `user grant --system -role
super_admin`.

**`serve: one of --addr or --socket is required`** — deliberate. Defaulting to
TCP would publish the app on a host that meant to use the socket.

**The WordPress page shows "wpcalc could not start"** — the message names the
cause. Check **wpcalc → Settings** and
`wp-content/uploads/wpcalc/wpcalc.log`.

**Hours look wrong in a report** — reports cover only the period named. Check
you are looking at the same month as the grid.

## Upgrading

Replace the binary and restart. Migrations apply at startup. Under WordPress,
replace `wordpress/wpcalc/bin/wpcalc` and use **Restart service** on the
settings screen, or deactivate and reactivate the plugin.

Take a backup first: migrations roll forward automatically, and rolling back
means restoring the file.
