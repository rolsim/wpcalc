# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

wpcalc is a monthly employee working-hours grid, shipped as one static Go
binary that runs either as a standalone web server or as a sidecar behind a
thin WordPress plugin (the plugin proxies admin requests to it over a unix
socket with a signed HMAC assertion of the WordPress user — no second login).
It is multi-tenant with NIST RBAC96-style roles: several companies ("tenants")
share one database, fully isolated, and an account holds one or more roles
scoped to the whole system, one tenant, or one employee.

Three independently-versioned Go modules live in this repo:
- root (`github.com/rolsim/wpcalc`) — the server, `cmd/wpcalc`
- `sdk/go` — a generated, typed Go client for `/api/v1`
- `cmd/wpcalcctl` — a remote admin CLI depending only on `sdk/go` + stdlib

They are deliberately separate modules so that consuming `sdk/go` or building
`wpcalcctl` never pulls in the server's own dependencies (SQLite, etc.), and
so `./...` from the root never touches them.

## Commands

```sh
make build       # -> bin/wpcalc, static, CGO_ENABLED=0
make check       # build + vet + lint + unit tests — root module only, the gate every commit passes
make check-all   # check, plus sdk-check and ctl-check — all three modules, their own gate each
make test        # go test ./... (root module)
make e2e         # standalone browser e2e in a container, ~10s
make e2e-wp      # WordPress e2e: docker compose + wp-cli + chromedp, ~45s
make fmt         # gofmt -s -w .
```

Single test: `go test ./internal/store/... -run TestRefreshTokenRotates -v`
(standard `go test` targeting — no custom runner). `internal/*_test.go`
files are the whole suite except `e2e/`, which is build-tag gated (`e2e`,
`e2e_wp`) and excluded from `go test ./...` because it needs Docker.

The other two modules are built/tested from their own directories (or via
`make sdk-check` / `make ctl-check`):

```sh
cd sdk/go && go test ./...
cd cmd/wpcalcctl && go build -o ../../bin/wpcalcctl . && go test ./...
```

`sdk/go`'s tests spin up a real in-process server against the root module via
a `replace github.com/rolsim/wpcalc => ../../` directive in its `go.mod`;
`cmd/wpcalcctl` has the same `replace` for the same reason (replace
directives are not inherited from a dependency's own `go.mod`, so both had to
add it directly).

`CGO_ENABLED=0` and `GOPRIVATE=github.com/rolsim/*` are exported in the
Makefile, not left to the environment — required, not incidental (see
Architecture below).

## Architecture

```
cmd/wpcalc/        subcommands and both listeners (server: serve, migrate, bootstrap-only user/token)
internal/domain/   calendar, employment, integer hours, RBAC types — pure, no I/O
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

**One handler tree, two modes.** Nothing in `internal/httpx` knows whether it
is standalone or behind WordPress. The only differences are the injected
`Authenticator` and the bound listener. If a change needs a handler to know
which mode it's in, the seam is in the wrong place.

**Two front ends, one authorization model.** `internal/httpx` (the HTML app,
session-cookie auth) and `internal/apiv1` (JSON, bearer-token auth,
oapi-codegen-generated `StrictServerInterface`) both sit on top of the same
`store`/`domain` layer and the same RBAC96 checks — `/api/v1` is a second
front end onto the same data, not a separate application. Each documents
itself at its own OpenAPI endpoint (`/openapi.{json,yaml,html}` for the app,
`/api/v1/openapi.{json,yaml,html}` for the API) via `internal/specdoc`,
because they describe structurally different things (form/redirect vs.
JSON/status-code) and are genuinely separate documents, not two encodings of
one.

**Server vs. CLI split.** `cmd/wpcalc` keeps only what an API client
fundamentally cannot do for itself: run the service, apply migrations, and
bootstrap the first account/role/token (an endpoint that requires a bearer
token can't be how you get your first one). Everything else — tenants,
roles, permissions, day-to-day user/token management — is administered
remotely over `/api/v1` through `cmd/wpcalcctl`, which has no direct
database access and depends only on `sdk/go` + stdlib.

**RBAC96, named verbatim.** `internal/domain/rbac.go` and `internal/store`
use NIST RBAC96's own terms as the actual type/table names: `Role`,
`Permission`, `UserRole` (the User Assignment relation, UA ⊆ U×R). No handler
or middleware ever compares a role ID to a hardcoded string — access is
entirely `Identity.Can`/`CanInTenant`/`CanSystemWide`, resolved once when the
identity is built (`auth.accounts.go`'s `identityFor` for standalone,
`auth.wordpress.go`'s synthetic full-access identity under WordPress) against
`UserRoles`/`RolePermissions`, so a permission revoked mid-session takes
effect on the very next request. `Scope` (system > tenant > employee) is
enforced both in Go (`Scope.Covers`) and by database CHECK
constraints/triggers in the migrations — the two must never drift apart.

**Hours are integers.** `domain.Centihours` is hundredths of an hour. The
grid sums the same entries two ways and the PDFs a third; those totals must
agree exactly, which floats cannot promise. Never sum `.Hours()` — sum
`Centihours` and convert once at the end.

**`domain.Date` is timezone-free**, not `time.Time` — a calendar day must
mean the same thing regardless of server offset or DST.

**Tokens.** API bearer tokens (`wpat_...`) expire after 1 hour
(`domain.AccessTokenTTL`); refresh tokens (`wprt_...`) are single-use and
rotate on every exchange, valid 30 days (`domain.RefreshTokenTTL`). Exchange
happens via `store.ExchangeRefreshToken`, which claims the token atomically
inside one transaction (`UPDATE ... RETURNING ... WHERE used_at IS NULL`) —
every query inside that transaction must go through the held `*sql.Tx`, never
back through `*sql.DB`, because the pool is `SetMaxOpenConns(1)` and querying
through `db` while `tx` holds the only connection deadlocks silently. Both
`sdk/go` and `cmd/wpcalcctl` refresh transparently and save the rotated pair
before any command/request returns.

**CGO_ENABLED=0 is load-bearing, not a style choice.** The WordPress plugin
spawns the binary on a host this project doesn't control, so it has to be one
static file with no libc dependency — this is also why the SQLite driver is
the pure-Go `modernc.org/sqlite`, not `mattn/go-sqlite3`.

**Migrations use goose's `Provider` API**, not the package-level one — the
latter keeps dialect/filesystem in process globals, which race as soon as two
tests migrate two databases at once.

**Manuals are embedded and shown via `glow`** (`wpcalc manual`), read from an
`embed.FS` so they travel with the binary with no source tree beside it;
`glow` is used only when it's on `PATH` *and* stdout is a terminal, otherwise
raw markdown, so piping to a file doesn't fill it with ANSI escapes.

Further reasoning behind these and other judgement calls, each with what it
would take to reverse, lives in `DECISIONS.md`. Known gaps and deferred work
are tracked in `BLOCKERS.md`.

## Documentation

`README.md` / `README.de-CH.md` are the primary reference for build/run/API/
release details — don't duplicate them here; read them directly. The admin
and user guides (`docs/en/`, `docs/de-CH/`) are also embedded in the binary
(`wpcalc manual`) and should be kept in sync with whatever they describe —
past drift includes CLI commands that had moved to `wpcalcctl` and were still
documented as `wpcalc` subcommands.

## CI/release

`.github/workflows/ci.yml` runs `make check-all` on every push to `main` and
every PR. `.github/workflows/release.yml` triggers on `v*` tags, re-runs
`make check-all` on the tagged commit (never trusts that CI already passed
for it), cross-compiles both `wpcalc` and `wpcalcctl` for
linux/darwin/windows (amd64+arm64 except windows), and publishes a GitHub
release with one archive per binary per platform. A tag with a hyphenated
suffix (`-alpha`, `-rc.1`, ...) publishes as a prerelease automatically.
