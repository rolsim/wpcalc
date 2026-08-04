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

`wpcalc` (this binary) is the server: it has direct database access and
holds only what an API client fundamentally cannot do for itself — run
the service, apply migrations, and bootstrap the first account and its
first token. Everything else is administered remotely, over `/api/v1`,
with the separate `wpcalcctl` binary — see
[Users and tokens](#users-and-tokens) below for the full command mapping.

| Command | Purpose |
|---|---|
| `serve --addr :8080 \| --socket PATH` | run the server; exactly one listener |
| `migrate [up\|down\|status]` | apply, roll back one, or report migrations |
| `user add` | create a bare account (bootstrap) |
| `user grant\|revoke <name> [-system\|-tenant ID\|-employee ID] [-role ID]` | assign or remove a role (bootstrap: nothing works without one) |
| `token create <name>` | mint an access/refresh pair (bootstrap: an API client cannot get its first token any other way) |
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
./bin/wpcalc user add alice --db DB   # prompts for a password; no access yet — bootstrap only
```

Every account after the first is easiest to create remotely, once you hold a
token for an account with `manage_users`:

```sh
wpcalcctl user add bob                          # prompts for a password
wpcalcctl user passwd bob                       # also revokes bob's sessions
wpcalcctl user list
```

Passwords are bcrypt-hashed and must be at least 10 characters. Usernames are
case-insensitive. Sessions are stored server-side and last 12 hours, so
signing out or changing a password revokes access immediately rather than
leaving a token valid until it expires; changing a password also revokes
every refresh token for that account (already-issued access tokens still
expire on their own within the hour regardless).

`user add` only creates credentials — the account can do nothing until a role
is granted (see the next section). There is no moment where an account exists
with an implicit role nobody asked for.

> `--allow-weak-password` (server binary only) waives the length requirement.
> It exists so a local development database can be primed with throwaway
> credentials such as `admin`/`admin`, and it prints a warning whenever it is
> used. **Never use it on anything reachable.**

### Interface language

Each account stores a preferred language, or an empty value meaning "follow the
browser". Users change it themselves from the selector in the top bar; you can
set it when creating an account or afterwards:

```sh
./bin/wpcalc user add alice -lang en --db DB   # server binary, bootstrap only
wpcalcctl user lang bob de-CH                 # de_CH is accepted too
wpcalcctl user lang bob ""                    # back to following the browser
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

The very first account and its `super_admin` grant have to happen on the
server itself — nothing else can bootstrap them:

```sh
./bin/wpcalc user add alice --db DB
./bin/wpcalc user grant alice --system -role super_admin --db DB
./bin/wpcalc token create alice --db DB
```

Everything from here on is remote, via `wpcalcctl` (after `wpcalcctl
login` with the pair `token create` just printed):

```sh
wpcalcctl tenant add "Acme Corp"                          # -> tenant id, e.g. 2

wpcalcctl user add bob
wpcalcctl user grant bob -tenant 2 -role mandant_admin

wpcalcctl user add carol
wpcalcctl user grant carol -tenant 2 -employee 5 -role viewer

wpcalcctl user roles bob                                  # what bob can reach
wpcalcctl user revoke carol -tenant 2 -employee 5          # revoke-then-grant to change a role
```

A user holds at most one role per scope *instance* — `-employee 5` twice with
different roles is rejected; revoke first. Use `-system`, `-tenant ID`
alone, or `-tenant ID -employee ID` together — an employee-scope grant
still needs its tenant named, since `/api/v1` nests employee-role
assignments under a tenant (`wpcalc user grant` on the server binary
takes the three flags as strictly mutually exclusive instead, since it
talks to the database directly rather than a nested route).

An account with several tenant- or employee-scope grants across different
tenants sees a **tenant switcher** in the top bar and, on first login (or
after a role change leaves the previously active tenant unreachable), a
chooser page — RBAC96 calls this "session role activation": which of the
account's several tenant memberships is active for this browser session.

### Managing roles themselves

Roles, their permissions, and every grant are editable from the web UI, the
remote `wpcalcctl`, and `/api/v1` directly — nothing here is a fixed enum:

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
wpcalcctl role add auditor -name Auditor -scope tenant
wpcalcctl role permissions auditor -add read
wpcalcctl role permissions auditor -add manage_tenants   # rejected: min_scope=system
wpcalcctl role list
wpcalcctl permission list
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
Issue a token pair from the CLI:

```sh
./bin/wpcalc token create alice -name ci    # prints an access + refresh token pair, once
./bin/wpcalc token list alice               # id, name, created, expiry, last used, active/expired/revoked
./bin/wpcalc token revoke 3
./bin/wpcalc token revoke-all alice         # every access + refresh token for the account
```

Both plaintexts are shown exactly once, at creation; only their SHA-256
hashes are ever stored. Revoking a token does not touch the account's
password or any browser session, and takes effect on the very next
request — nothing is cached.

```sh
curl -H "Authorization: Bearer wpat_..." http://localhost:8080/api/v1/tenants
```

**Access tokens expire after one hour** (`domain.AccessTokenTTL`) —
deliberately short, so a leaked one stops working on its own well before
anyone would notice and revoke it by hand. The paired **refresh token**
(`wprt_...`) lasts 30 days (`domain.RefreshTokenTTL`) and exchanges for a
brand-new pair — a new access token *and* a new, rotated refresh token —
without going back through `wpcalc token create`:

```sh
./bin/wpcalc token refresh wprt_...          # from the CLI, mostly for testing
curl -X POST -H "Content-Type: application/json" \
  -d '{"refreshToken":"wprt_..."}' http://localhost:8080/api/v1/tokens/refresh
```

`POST /api/v1/tokens/refresh` is the one `/api/v1` operation that needs no
bearer header at all (the refresh token in the body *is* the credential) —
by design, since the whole point is reachability once the access token has
already expired. Refresh tokens are **single-use**: exchanging one
invalidates it immediately, whether or not the caller keeps the newly
rotated one. Reusing an already-exchanged, expired, revoked, or unknown
refresh token all read identically (`401`), so a response can never be
used to test whether a given secret was ever valid. Changing a password
(`wpcalc user passwd` / `PUT /api/v1/users/{username}/password`) revokes
every refresh token for that account too, alongside browser sessions —
already-issued access tokens are left to expire on their own within the
hour regardless.

### What differs from the HTML app

- **Every request names its tenant explicitly in the path**
  (`/api/v1/tenants/{tenantId}/...`) — a bearer token has no session to hold
  an "active tenant" in, so there is no tenant switcher or chooser here.
- **No login, logout, or tenant-switch routes** — meaningless for a
  stateless client. Setting a language preference *does* exist
  (`PUT /api/v1/users/{username}/language`), but names the target account
  explicitly rather than acting on "the current session", for the same
  reason.
- Every response is JSON with a real status code — no `303` redirects, no
  `?err=` query-string tokens.

### Users and tokens

`wpcalcctl` *is* an `/api/v1` client — every one of its commands maps
directly onto an operation:

| `wpcalcctl` | API |
|---|---|
| `user add` | `POST /api/v1/users` |
| `user list` | `GET /api/v1/users` |
| `user passwd <name>` | `PUT /api/v1/users/{username}/password` |
| `user lang <name>` | `PUT /api/v1/users/{username}/language` |
| `user roles <name>` | `GET /api/v1/users/{username}/roles` |
| `token create` | `POST /api/v1/tokens` (self, once you have one) |
| `token list` | `GET /api/v1/tokens` (self only) |
| `token revoke` | `DELETE /api/v1/tokens/{tokenId}` (self only) |
| `token revoke-all` | `DELETE /api/v1/tokens` (self only) |

`POST /api/v1/tokens/refresh` has no `wpcalcctl` command at all —
`wpcalcctl` calls it automatically, transparently, whenever an access
token has expired, and saves the rotated pair before returning.

The one thing with no API path, anywhere: `wpcalc token create` on the
**server** binary, direct-database, no bearer credential required. An
endpoint that itself requires a bearer token cannot be how an account
gets its first one — this is that escape hatch, alongside `wpcalc user
add`/`grant` for the account and its first role.

Two access rules apply consistently across the `/users/{username}/*`
routes: `manage_users` system-wide acts on any account, and **any account
may always act on itself** — matching the HTML app's own self-service
language switch. A non-admin naming someone else's account, or one that
doesn't exist, gets an identical `403` either way; this can never be used
to test which usernames exist.

The `/tokens*` routes are scoped even tighter: self-service only, full
stop. There is no way for anyone — including a `manage_users` admin — to
list or revoke *another* account's tokens through the API (and so, through
`wpcalcctl`, none either); a `tokenId` belonging to someone else reads as
`404`, not `403`, so it cannot be used to probe which token ids exist
either. Only the server binary, with direct database access, is not
scoped this way — and even it can only revoke access tokens
(`wpcalc token create` mints; there is no server-side revoke command,
deliberately: reach for `wpcalcctl token revoke` instead, or edit the
database directly for a true emergency).

### Authorization

Same RBAC96 model as the rest of the app (see
[Multi-tenancy and roles](#multi-tenancy-and-roles) above) — a bearer token
resolves to the same identity a session cookie would, so the same roles and
permissions apply. Each operation documents which permission it requires
and at what scope; a few examples:

| Operation | Permission | Scope |
|---|---|---|
| `GET /api/v1/tenants` | `manage_tenants` | system |
| `GET /api/v1/tenants/accessible` | none — self-scoping | — |
| `GET /api/v1/tenants/{tenantId}/employees` | `manage_employees` | tenant |
| `GET /api/v1/tenants/{tenantId}/months/{ym}` | `read` | employee |
| `PUT .../months/{ym}/entries` | `write` | employee |
| `GET .../months/{ym}/report` | `print` | tenant or employee |
| `POST /api/v1/roles` | `manage_roles` | system |

`GET /api/v1/tenants/accessible` is the exception to "every operation
requires a permission": it needs no particular one, because what it
returns is already filtered to the caller — every tenant reachable via any
role, at any scope. Unlike `GET /api/v1/tenants` (system-wide
`manage_tenants` only, `403` otherwise), this is how a tenant- or
employee-scope token discovers which `tenantId` to put in every other
path, without already being told one out of band.

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

### The `[wpcalc]` frontend shortcode

Drop `[wpcalc]` into any page or post to show a logged-in WordPress user
*their own* hours there, with no `manage_options` capability and no wp-admin
access needed — for employees who have a WordPress login but no reason to
ever see the backend.

It only shows anything once you link the WordPress username to a wpcalc
account holding an employee-scope role, with `wpcalcctl`:

```sh
wpcalcctl user add alice
wpcalcctl user grant alice -employee 42 -role viewer   # or "editor" to allow entry
```

What that account's role covers is what the shortcode shows — one employee
column for an employee-scope role, more for anything broader — the same
RBAC96 scoping the admin grid already enforces. An unlinked WordPress user
falls back to wpcalc's own login form (a second, separate login) rather than
seeing an empty page; that fallback is an escape hatch for accounts nobody
has linked yet, not the intended path.

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
