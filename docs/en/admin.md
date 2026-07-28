# wpcalc — administrator guide

Installing, running and maintaining wpcalc.

*Deutsch: [../de-CH/admin.md](../de-CH/admin.md)*

---

> **Roles are recorded but not yet enforced.** Every signed-in account can add,
> edit and delete employees and download every report, whichever role it
> holds. The `admin` / `user` distinction exists in the database and in the
> WordPress bridge, but no route checks it. Treat every account as a full
> administrator until that changes, and do not hand out `user` accounts
> expecting them to be limited.

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
./bin/wpcalc user add alice -role admin --db /var/lib/wpcalc/wpcalc.db
./bin/wpcalc serve --addr :8080 --db /var/lib/wpcalc/wpcalc.db
```

`serve` **refuses to start when no accounts exist**, rather than offering a
login that cannot succeed. Create the first account before starting it.

The binary is statically linked with no cgo, so it has no runtime
dependencies — copy it to the host and run it.

### Commands

| Command | Purpose |
|---|---|
| `serve --addr :8080 \| --socket PATH` | run the server; exactly one listener |
| `migrate [up\|down\|status]` | apply, roll back one, or report migrations |
| `user add\|passwd\|list` | manage accounts |
| `sample-employees [--month YYYY-MM]` | create placeholder employees; records **no hours** |
| `version` | print the version |

Flags work before or after positional arguments.

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
./bin/wpcalc user add alice -role admin --db DB   # prompts for a password
./bin/wpcalc user passwd alice --db DB            # also revokes alice's sessions
./bin/wpcalc user list --db DB
```

Passwords are bcrypt-hashed and must be at least 10 characters. Usernames are
case-insensitive. Sessions are stored server-side and last 12 hours, so
signing out or changing a password revokes access immediately rather than
leaving a token valid until it expires.

> `--allow-weak-password` waives the length requirement. It exists so a local
> development database can be primed with throwaway credentials such as
> `admin`/`admin`, and it prints a warning whenever it is used. **Never use it
> on anything reachable.**

There is no way to delete an account from the CLI yet; remove the row from the
`users` table directly if you need to. Its sessions go with it.

## Employees

Manage them under **Mitarbeitende / Employees**. Each has a name, a start date
and an optional end date; leave the end date empty while someone is still
employed.

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

## WordPress

1. `make build`, then copy the binary to `wordpress/wpcalc/bin/wpcalc`
2. Copy `wordpress/wpcalc/` into `wp-content/plugins/`
3. Activate **wpcalc**
4. Open **Working hours** in the admin menu

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

**`no accounts exist`** — the user table is empty. Run `user add`.

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
