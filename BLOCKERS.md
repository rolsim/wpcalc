# Blockers and known gaps

Things skipped, deferred, or worth knowing before the next session. Nothing
here is silently missing from `REPORT.md`.

---

## Nothing blocked the build

Every priority level in the brief was reached. This file records the smaller
gaps and the traps found on the way, not abandoned work.

## Known gaps

**No `wp_ajax` route for the enhanced path under WordPress.** The JavaScript
enhancement posts to the same admin URL the form uses, which works, but goes
through the full WordPress admin bootstrap on every cell edit. That is a few
tens of milliseconds per keystroke-commit that a dedicated `admin-ajax`
endpoint would avoid. It is a performance nicety, not a correctness gap — the
no-JS path is unaffected either way.

**Settings page is registered but not rendered.** `register_setting` declares
the binary-path option and `binary_path()` reads it, so the option is usable
via WP-CLI (`wp option update wpcalc_binary_path /path/to/wpcalc`). The form
for editing it in the admin UI was not built. The default — `bin/wpcalc`
inside the plugin directory — is what the install instructions use, so this
only matters for someone running the sidecar from a system path.

**No pagination or archiving on the employee list.** Every employee ever
recorded appears on `/employees`, including long-departed ones. The *grid* is
correctly filtered by month, which is what the brief specified; the management
list is not. Fine for a company; visibly wrong at a few hundred records.

**Keyboard navigation is arrow-keys and Enter only.** Tab order follows the
DOM, which runs across the row rather than down the column. Entering a month
for one person means arrow-down, which works, but Tab does something else.

**Concurrent editing is last-write-wins.** Two people editing the same cell
in the same second will have one of them silently overwrite the other. SQLite
serialises the writes so nothing corrupts, but nobody is told. A version
column on `time_entries` and a conflict check would fix it; the brief did not
ask for multi-user editing.

**Session cleanup is opportunistic.** Expired sessions are deleted when
someone tries to use one, and `PurgeExpiredSessions` exists but nothing calls
it on a schedule. Rows for sessions that expire and are never touched again
accumulate. At one company's login rate this is a handful of rows a month.

## Traps found and fixed, worth not reintroducing

**`proc_close()` waits for the process to exit.** The first version of the
shim called it on a sidecar meant to run forever, so every admin page request
hung until the browser gave up — a 30-second stall with no error anywhere.
Leaking the handle does not help either: PHP closes it at request shutdown
with the same wait. The sidecar is now detached through a throwaway shell.

**`nohup VAR=x prog` does not set an environment variable.** It makes nohup
look for a program literally named `VAR=x`. Assignments must precede `nohup`,
not follow it. Cost one round trip to find, in a log line that said exactly
what was wrong.

**Go's `flag` package stops at the first positional argument.** `wpcalc user
add alice --db /path` silently left `--db` at its default, wrote the account
into `./wpcalc.db`, and then the server — correctly pointed at the intended
file — reported that no accounts existed. Two confusing symptoms, one cause.
Fixed in `cmd/wpcalc/flags.go` with a regression test.

**A PDF test can pass while proving nothing.** fpdf compresses content
streams, so `strings.Contains(pdf, "Müller")` is false whether the text is
correct, mojibake, or absent. The umlaut test now renders with compression
disabled specifically so the assertion has something to inspect.

**Asserting on the wrong process.** An early e2e check fetched
`localhost:8099/healthz` to prove the sidecar was not exposed over TCP. That
URL reaches Apache, which answers 200 for unknown paths, so the check failed
for a reason unrelated to what it claimed to test. It now reads the
container's own listener table instead.
