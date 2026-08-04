# wpcalc

*Deutsch (CH): [README.de-CH.md](README.de-CH.md)*

A monthly employee working-hours grid, shipped as one static Go binary that
runs either as a standalone web server or as the sidecar behind a thin
WordPress plugin.

- **y-axis** — every day of the month, weekends shaded
- **x-axis** — the employees whose employment overlaps that month, and only those
- **cells** — decimal hours ("industrial minutes": `7.75` is 7 h 45 min), locked
  outside each person's employment period
- **accumulators** — totals per employee along the bottom, per day down the
  right, grand total in the corner
- **reports** — monthly overview PDF, and per-employee monthly and yearly PDFs

The grid works with JavaScript disabled. JavaScript only removes the page
reload.

## Documentation

| | Deutsch (CH) | English |
|---|---|---|
| Recording hours | [docs/de-CH/user.md](docs/de-CH/user.md) | [docs/en/user.md](docs/en/user.md) |
| Running it | [docs/de-CH/admin.md](docs/de-CH/admin.md) | [docs/en/admin.md](docs/en/admin.md) |
| Trying it out | [docs/de-CH/testing.md](docs/de-CH/testing.md) | [docs/en/testing.md](docs/en/testing.md) |

The administrator guide covers both run modes, accounts, the WordPress plugin,
backup and troubleshooting.

Both are embedded in the binary and readable from a terminal:

```sh
wpcalc manual              # user guide, in your shell's language
wpcalc manual admin        # administrator guide
wpcalc manual testing      # how to try it out
wpcalc manual --lang en    # force a language
wpcalc manual --list       # what is available
```

Rendered with [glow](https://github.com/charmbracelet/glow) when it is
installed and the output is a terminal; plain markdown otherwise, so piping to
a file or another program stays clean.

---

## Build

```sh
make build          # -> bin/wpcalc, statically linked, no cgo
make check          # build + vet + lint + unit tests — the gate every commit passes
```

Go 1.26+. No npm, no bundler, no build step for assets.

## Run in VS Code

Press **F5**. It checks port 8090 is free, seeds `.dev/wpcalc.db` on first
run, starts the server under the debugger on <http://127.0.0.1:8090>, and
opens a browser once the port is actually listening. Breakpoints work.

If the port is taken the launch stops before the debugger starts and names
what is holding it. That check exists because the failure is otherwise
misleading: the server exits, the browser opens anyway, and whatever owns the
port answers instead — so the app looks broken rather than absent.

Dev logins, created by the prelaunch task: **`admin` / `admin`** (English) and
**`user` / `user`** (German) — different languages on purpose, so the stored
preference is visible without changing anything.

> These bypass the password-length requirement, via an explicit
> `--allow-weak-password` flag, **for local testing only**. They exist in a
> gitignored database on your machine. Never create accounts this way on
> anything reachable — `user add` refuses passwords this short unless the flag
> is passed, and that is deliberate.

The first run also creates four placeholder employees so the grid has columns.
**No working hours are recorded.** This is a timesheet: invented entries are
indistinguishable from real ones once they are in the database, so the sample
data stops at the employment periods — which is enough to show the grid, the
weekend shading, the visibility rule and the locked cells. Re-running F5
changes nothing; use the **wpcalc: reset dev database** task for a clean one.

Other configurations: *serve + open in editor* (Simple Browser instead of an
external one), *serve (WordPress sidecar)* for stepping through the socket and
signed-header paths, and *debug current test*.

## Run standalone

```sh
./bin/wpcalc migrate --db wpcalc.db          # creates the file if absent
./bin/wpcalc user add alice --db wpcalc.db
./bin/wpcalc user grant alice --system -role super_admin --db wpcalc.db
./bin/wpcalc serve --addr :8080 --db wpcalc.db
```

Then open <http://localhost:8080>.

`serve` refuses to start if no account can manage the database — it will tell
you to run `user add` and `user grant` rather than offer a login that cannot
succeed. wpcalc is multi-tenant with RBAC96-style roles (several companies in
one database, each account holding roles scoped to the system, one tenant, or
one employee); see the [administrator guide](docs/en/admin.md) for the full
model.

To give the grid some columns to render:

```sh
./bin/wpcalc sample-employees --db wpcalc.db --month 2026-07
```

This creates placeholder employees only. It records no hours, deliberately —
see the note above.

### Commands

`wpcalc` (this binary) is the server. It has direct database access and
holds only what an API client fundamentally cannot do for itself: run the
service, apply migrations, and bootstrap the very first account and its
first token. Everything else — tenants, roles, permissions, and
day-to-day user/token management — is administered remotely, over
`/api/v1`, by the separate [`wpcalcctl`](cmd/wpcalcctl) tool below.

| Command | Purpose |
|---|---|
| `wpcalc serve --addr :8080 \| --socket PATH` | run the server; exactly one listener |
| `wpcalc migrate [up\|down\|status]` | apply, roll back one, or report migrations |
| `wpcalc user add [-lang L]` | create a bare account (bootstrap; `--allow-weak-password` for local testing only) |
| `wpcalc user grant\|revoke <name> [-system\|-tenant ID\|-employee ID] [-role ID]` | assign or remove a role (bootstrap: an account needs one before any token it holds can pass a permission check) |
| `wpcalc token create <name>` | mint an access/refresh pair for an account (bootstrap: an API client cannot get its first token any other way) |
| `wpcalc sample-employees [--month YYYY-MM]` | create placeholder employees; records no hours |
| `wpcalc manual [user\|admin] [--lang L] [--raw] [--list]` | show an embedded manual, via glow when available |
| `wpcalc plugin export DIR [--force] [--php-only]` | write the WordPress plugin out of the binary |
| `wpcalc version [--short]` | print the build: version, commit, date, Go |

Flags may appear before or after positional arguments.

### Command-line administration: `wpcalcctl`

[`cmd/wpcalcctl`](cmd/wpcalcctl) is a separate binary, built from a
separate Go module — it has no dependency on the server's own packages,
only on [`sdk/go`](sdk/go) and the standard library, and talks to a
running server exclusively over `/api/v1`. It can run from a machine that
has never seen the server's filesystem or database file.

```sh
./bin/wpcalcctl login --server http://localhost:8080/api/v1 \
  --access-token wpat_... --refresh-token wprt_...   # from `wpcalc token create`, above
./bin/wpcalcctl tenant add "Acme Corp"
./bin/wpcalcctl user add bob
./bin/wpcalcctl user grant bob -tenant 2 -role mandant_admin
```

Credentials are stored (mode `0600`) at
`$XDG_CONFIG_HOME/wpcalcctl/credentials.json` and refreshed
automatically — access tokens expire after an hour, and `wpcalcctl`
exchanges the refresh token and saves the new pair before any command
returns, so this only has to be done once.

| Command | Purpose |
|---|---|
| `wpcalcctl login --server URL --access-token T --refresh-token T` | store credentials for every command below |
| `wpcalcctl tenant add\|list\|rename` | manage tenants |
| `wpcalcctl role add\|list\|delete\|permissions` | manage the role catalog |
| `wpcalcctl permission list` | the fixed permission catalog |
| `wpcalcctl user add\|passwd\|lang\|roles\|list` | manage accounts beyond the first |
| `wpcalcctl user grant\|revoke <name> [-system\|-tenant ID\|-employee ID] [-role ID]` | assign or remove a role |
| `wpcalcctl token create\|list\|revoke\|revoke-all` | issue, list, or revoke *this account's own* token pairs |

Build it the same way as the server, from its own directory:

```sh
cd cmd/wpcalcctl && go build -o ../../bin/wpcalcctl .
```

### Environment

| Variable | Used by | Meaning |
|---|---|---|
| `WPCALC_DB` | all | default database path |
| `WPCALC_SECRET` | `serve --socket` | HMAC secret shared with the WordPress plugin |
| `WPCALC_BASE_PATH` | `serve` | URL prefix, or full base URL with `--link-param` |
| `WPCALC_LINK_PARAM` | `serve` | carry the app path in this query parameter |

## API

The HTML app documents itself — `GET /openapi.json`, `/openapi.yaml`, and an
interactive `/openapi.html` (Swagger UI, vendored — no CDN, works offline),
no session required.

A separate, stateless JSON API mirrors most of the same resources at
`/api/v1`, for scripts rather than browsers. It authenticates with a bearer
token instead of the session cookie, and every request names its tenant
explicitly in the path — there is no session to hold an "active tenant" in:

```sh
./bin/wpcalc token create alice           # prints an access + refresh token pair, once
curl -H "Authorization: Bearer wpat_..." http://localhost:8080/api/v1/tenants
```

Access tokens (`wpat_...`) expire after an hour; exchange the paired
refresh token (`wprt_...`, valid 30 days, single-use — each exchange
rotates it) for a new pair via `POST /api/v1/tokens/refresh`, without
going back to the server binary at all:

```sh
curl -X POST -d '{"refreshToken":"wprt_..."}' http://localhost:8080/api/v1/tokens/refresh
```

[`wpcalcctl`](#command-line-administration-wpcalcctl) and
[`sdk/go`](#go-sdk) both do this automatically — neither ever needs the
server binary again once they hold an initial pair.

It documents itself the same way, at `/api/v1/openapi.{json,yaml,html}` —
open `/api/v1/openapi.html` in a browser to browse every endpoint and, via
its "Authorize" button, try requests against the running server with a
token from `wpcalc token create`.

### Go SDK

[`sdk/go`](sdk/go) is a typed Go client for `/api/v1` — generated from the
same spec, plus transparent access-token refresh, as its own Go module so
consuming it doesn't pull in the server's own dependencies:

```sh
go get github.com/rolsim/wpcalc/sdk/go
```

```go
sess, _ := wpcalc.New("http://localhost:8080/api/v1", wpcalc.TokenPair{
    AccessToken: "wpat_...", RefreshToken: "wprt_...",
})
resp, _ := sess.ListAccessibleTenantsWithResponse(ctx)
```

See [`sdk/go/README.md`](sdk/go/README.md) for the full guide.

## Install the WordPress plugin

With shell access, the binary carries the plugin and installs itself:

```sh
wpcalc plugin export /var/www/html/wp-content/plugins
```

This writes `wpcalc/wpcalc.php` and `wpcalc/bin/wpcalc` — a copy of the binary
that wrote it, so the plugin and the sidecar are always the same version.

Without shell access — most shared hosting — download
`wpcalc-wordpress-plugin-<tag>-linux-amd64.zip` from a
[release](../../releases) and upload it directly on **Plugins → Add New →
Upload Plugin**; it already contains the linux/amd64 binary, which covers
the overwhelming majority of shared/cPanel hosting. If the host's zip
extraction drops the binary's executable bit (common with PHP's own
`ZipArchive`, which is what that screen uses), the plugin retries `chmod`
itself on first activation before giving up.

Either way, then activate **wpcalc** and open **Working hours** in the admin
menu.

The plugin starts the binary on demand as a sidecar on a unix socket under
`wp-content/uploads/wpcalc/`, supervises it, and proxies admin requests to it
with a signed assertion of the current WordPress user. Access requires
`manage_options`; writes carry a WordPress nonce.

If it cannot start — `proc_open` disabled, binary missing, wrong permissions —
the admin page says which of those it is rather than showing a blank screen.

### Frontend shortcode: self-service hours for employees without wp-admin

`[wpcalc]`, dropped into any page or post, shows *that specific WordPress
user's own hours* — not the admin grid — to anyone who is logged in, with no
wp-admin access required. It exists for employees who have a WordPress
account but no reason to ever see the WordPress backend.

For it to show anything, the WordPress username has to be linked to a wpcalc
account holding an employee-scoped role, via `wpcalcctl` against the same
server the sidecar runs (`--server unix:///path/to/wp-content/uploads/wpcalc/wpcalc.sock`,
or over TCP if you also run one):

```sh
wpcalcctl user add alice                       # username must match the WP login
wpcalcctl user grant alice -employee 42 -role viewer   # or "editor" to allow entering hours
```

An employee logged into WordPress as `alice` then sees exactly what that
role grants — one column, nothing else — the same
[RBAC96 scoping](#architecture) the standalone app already enforces.
Whatever the role covers is what shows: a broader (tenant- or system-scope)
role shows more than one employee, same as anywhere else in the app.

If nobody has linked the account yet, the shortcode falls back to wpcalc's
own login form instead of showing nothing — a second, separate login (not
the WordPress one) for a plain wpcalc account, so an employee is never
simply locked out while an admin gets around to linking them. This is a
deliberate escape hatch, not the intended path: link the account and the
double login goes away.

## Tests

```sh
make test           # unit and handler tests, no containers, seconds
make e2e-wp         # WordPress integration: docker compose + wp-cli, minutes
```

`make e2e-wp` builds a Linux binary into the plugin, brings up WordPress and
MariaDB, installs and activates the plugin, and drives the admin page over
HTTP. It tears the stack down on every exit path including failure.

## Releases

Every push to `main` and every pull request runs `make check-all` via
[`.github/workflows/ci.yml`](.github/workflows/ci.yml) — the root module,
[`sdk/go`](sdk/go), and [`cmd/wpcalcctl`](cmd/wpcalcctl) each gated
separately, since all three are independent Go modules. Cutting a release is
then just tagging:

```sh
git tag v0.1.0        # or v0.1.0-alpha, v0.1.0-rc.1, ...
git push origin v0.1.0
```

Pushing a tag matching `v*` runs
[`.github/workflows/release.yml`](.github/workflows/release.yml), which first
runs `make check-all` on the tagged commit — a tag on a broken commit is built
and checked again here rather than trusted to have already passed CI — and
only then cross-compiles both `wpcalc` and `wpcalcctl` for `linux/amd64`,
`linux/arm64`, `darwin/amd64`, `darwin/arm64`, and `windows/amd64` from one
Linux runner (no cgo, so no per-OS runner is needed), stamps
`-X main.version=<tag>` into `wpcalc`, and publishes a GitHub release with one
archive per binary per platform (`wpcalc-<tag>-<os>-<arch>` and
`wpcalcctl-<tag>-<os>-<arch>`), a ready-to-upload
`wpcalc-wordpress-plugin-<tag>-linux-amd64.zip` (see [Install the WordPress
plugin](#install-the-wordpress-plugin)), and `checksums.txt`.

A tag with a hyphenated suffix (`-alpha`, `-beta`, `-rc.1`, ...) is published
as a GitHub prerelease automatically; a plain `vX.Y.Z` is a stable release.

## Architecture

```
cmd/wpcalc/        subcommands and both listeners
internal/domain/   calendar, employment, integer hours — pure, no I/O
internal/store/    SQLite; goose migrations embedded; every query lives here
internal/httpx/    one handler tree, served identically in both modes
internal/apiv1/    generated strict JSON server for /api/v1, over the same store
internal/specdoc/  serves a parsed OpenAPI 3.1 doc as JSON, YAML, and interactive HTML (Swagger UI, vendored)
internal/auth/     Authenticator: local accounts, bearer tokens, or signed WordPress headers
internal/report/   the three PDFs
internal/i18n/     embedded catalogs; de-CH default, en available
wordpress/wpcalc/  the PHP shim
sdk/go/            typed Go client for /api/v1 — its own Go module, generated from the same spec
cmd/wpcalcctl/     remote admin CLI — its own Go module, sdk/go and the standard library only
```

Two properties are worth knowing before changing anything:

**One handler tree, two modes.** Nothing in `internal/httpx` knows whether it
is standalone or behind WordPress. The differences are the injected
`Authenticator` and the bound listener. If a change needs a handler to know,
the seam is in the wrong place.

**Hours are integers.** `domain.Centihours` is hundredths of an hour. The grid
sums the same entries two ways and the PDFs a third; those totals must agree
exactly, which floats cannot promise. Never sum `.Hours()` — sum `Centihours`
and convert once at the end.

See [DECISIONS.md](DECISIONS.md) for the reasoning behind these and the other
judgement calls, each with what it would take to reverse.

## License

MIT — see [LICENSE](LICENSE). The WordPress plugin header declares the same.
