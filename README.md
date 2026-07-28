# wpcalc

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

---

## Build

```sh
make build          # -> bin/wpcalc, statically linked, no cgo
make check          # build + vet + lint + unit tests — the gate every commit passes
```

Go 1.26+. No npm, no bundler, no build step for assets.

## Run in VS Code

Press **F5**. It checks port 8080 is free, seeds `.dev/wpcalc.db` on first
run, starts the server under the debugger on <http://127.0.0.1:8080>, and
opens a browser once the port is actually listening. Breakpoints work.

If the port is taken the launch stops before the debugger starts and names
what is holding it. That check exists because the failure is otherwise
misleading: the server exits, the browser opens anyway, and whatever owns the
port answers instead — so the app looks broken rather than absent.

Dev login: **`dev` / `devpassword123`** — created by the prelaunch task, in a
gitignored database, for local use only.

The first run seeds a demo month so there is something to look at. Re-running
F5 never reseeds; use the **wpcalc: reset dev database** task if you want a
clean one.

Other configurations: *serve + open in editor* (Simple Browser instead of an
external one), *serve (WordPress sidecar)* for stepping through the socket and
signed-header paths, and *debug current test*.

## Run standalone

```sh
./bin/wpcalc migrate --db wpcalc.db          # creates the file if absent
./bin/wpcalc user add alice -role admin --db wpcalc.db
./bin/wpcalc serve --addr :8080 --db wpcalc.db
```

Then open <http://localhost:8080>.

`serve` refuses to start if no accounts exist — it will tell you to run
`user add` rather than offer a login that cannot succeed.

To look at a populated grid without typing a month of hours:

```sh
./bin/wpcalc demo-seed --db wpcalc.db --month 2026-07
```

### Commands

| Command | Purpose |
|---|---|
| `wpcalc serve --addr :8080 \| --socket PATH` | run the server; exactly one listener |
| `wpcalc migrate [up\|down\|status]` | apply, roll back one, or report migrations |
| `wpcalc user add\|passwd\|list` | manage standalone accounts |
| `wpcalc demo-seed [--month YYYY-MM]` | fill a database with a plausible month |
| `wpcalc version` | print the version |

Flags may appear before or after positional arguments.

### Environment

| Variable | Used by | Meaning |
|---|---|---|
| `WPCALC_DB` | all | default database path |
| `WPCALC_SECRET` | `serve --socket` | HMAC secret shared with the WordPress plugin |
| `WPCALC_BASE_PATH` | `serve` | URL prefix, or full base URL with `--link-param` |
| `WPCALC_LINK_PARAM` | `serve` | carry the app path in this query parameter |

## Install the WordPress plugin

1. `make build`, then copy the binary into the plugin: `wordpress/wpcalc/bin/wpcalc`
2. Copy `wordpress/wpcalc/` into `wp-content/plugins/`
3. Activate **wpcalc** in WordPress
4. Open **Working hours** in the admin menu

The plugin starts the binary on demand as a sidecar on a unix socket under
`wp-content/uploads/wpcalc/`, supervises it, and proxies admin requests to it
with a signed assertion of the current WordPress user. Access requires
`manage_options`; writes carry a WordPress nonce.

If it cannot start — `proc_open` disabled, binary missing, wrong permissions —
the admin page says which of those it is rather than showing a blank screen.

## Tests

```sh
make test           # unit and handler tests, no containers, seconds
make e2e-wp         # WordPress integration: docker compose + wp-cli, minutes
```

`make e2e-wp` builds a Linux binary into the plugin, brings up WordPress and
MariaDB, installs and activates the plugin, and drives the admin page over
HTTP. It tears the stack down on every exit path including failure.

## Architecture

```
cmd/wpcalc/        subcommands and both listeners
internal/domain/   calendar, employment, integer hours — pure, no I/O
internal/store/    SQLite; goose migrations embedded; every query lives here
internal/httpx/    one handler tree, served identically in both modes
internal/auth/     Authenticator: local accounts, or signed WordPress headers
internal/report/   the three PDFs
internal/i18n/     embedded catalogs; de-CH default, en available
wordpress/wpcalc/  the PHP shim
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
