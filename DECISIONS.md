# Decisions

Judgement calls made during the unattended build, with the reasoning and what
it would take to reverse each. Newest last.

---

**Module path is `source.simonet.internal/rolsim/wpcalc`, with `GOPRIVATE` set
in the Makefile.** The host is not publicly resolvable, so any module-proxy or
checksum-database lookup would hang or fail. Harmless for a main module today,
but a stall at 2am with nobody watching is exactly what the setting prevents.
*Reverse:* change `module` in `go.mod` and the `GOPRIVATE` line.

**`CGO_ENABLED=0` is enforced in the Makefile, not left to the environment.**
The WordPress plugin spawns the binary on a host we do not control, so it has
to be one static file with no libc dependency. This also made the e2e test
possible: the binary built on the host runs unchanged inside the container.
*Reverse:* nothing depends on it except the SQLite driver choice below.

**SQLite driver is `modernc.org/sqlite`, not `mattn/go-sqlite3`.** The latter
needs cgo and would cost the static binary. The pure-Go driver is slower, which
does not matter at the scale of one company's timesheet.
*Reverse:* swap the import and the driver name in `store.Open`; drop
`CGO_ENABLED=0`.

**Hours are stored as integer centihours, never as float.** The grid sums the
same entries along two axes and the PDFs sum them a third time; those totals
must agree exactly. Binary floats cannot promise that and the error compounds
over a month. `TestCentihoursSumIsExact` and `TestTotalsEqualSumOfEntries` pin
it. *Reverse:* would require changing the column type and every total, and
would reintroduce the drift.

**`domain.Date` is a timezone-free calendar type, not `time.Time`.**
"2026-07-14" must mean the same day regardless of server offset or DST.
Modelling an instant is how month boundaries acquire off-by-one-day bugs.
*Reverse:* not advisable; the type is small and its tests are cheap.

**`ParseHours` rejects rather than coerces.** "7.755" and "7:45" are refused
instead of being read as 7.75 and 7. A quietly altered figure in a timesheet is
worse than a visible error. Both decimal separators are accepted because a
de-CH keyboard gives "7,75" and a numpad gives "7.75".
*Reverse:* relax the checks in `internal/domain/hours.go`.

**Clearing a cell deletes the row rather than storing zero.** Keeps "nobody
touched this" distinguishable from "someone recorded nothing".
*Reverse:* change the `h == 0` branch in `store.SetHours`.

**The employment lock is enforced three times.** The template greys the cell,
the handler returns 422, and the store refuses regardless of caller. The first
two are presentation and convenience; only the store cannot be bypassed by a
crafted request or a future second caller.
*Reverse:* drop the check in `SetHours`, and accept that the rule then depends
on every caller remembering it.

**Goose uses the `Provider` API, not the package-level one.** The latter keeps
dialect and filesystem in process globals, which race as soon as two tests
migrate two databases at once.
*Reverse:* `internal/store/store.go`, `newProvider`.

**Accounts arrive in a second migration rather than in the initial schema.**
By the time it runs the database already holds employees and hours, so it
exercises goose against real data — the case that actually breaks in
production, and one a schema created all at once never tests.
*Reverse:* merge the two `.sql` files; loses the test value.

**The up/down/up test rolls back every migration, not one step.** A Down block
that has never executed is not known to work, and the oldest is the likeliest
to be wrong. This needed `MigrateReset`.
*Reverse:* call `MigrateDown` instead in the test.

**Sessions live server-side, not in a signed self-contained cookie.** Logging
out and changing a password have to actually revoke access; a signed token
stays valid until it expires no matter what the server wants. For a timesheet
holding staff data that is the wrong default.
*Reverse:* replace `internal/auth/accounts.go` with an HMAC token scheme.

**The single-password stopgap was deleted when accounts landed, not kept as a
fallback.** Two ways in is the same problem the WordPress bridge exists to
avoid. *Reverse:* `git show 9f51497^:internal/auth/password.go`.

**Standalone `serve` refuses to start with an empty user table.** An operator
staring at a failed login with correct credentials has no way to discover the
table is simply empty. *Reverse:* drop the `HasUsers` check in
`buildAuthenticator`.

**`Authenticate` always runs a bcrypt comparison, including for unknown
usernames.** Returning early would make the login form a user enumerator by
timing. *Reverse:* remove the dummy-hash branch; not recommended.

**The WordPress bridge requires both a valid HMAC *and* arrival on the unix
socket.** Headers are forgeable by anything that can reach the listener, so the
signature proves the sender knows the secret and the socket proves it is local.
A valid signature over TCP is still refused, and `ConnKindFrom` defaults to the
untrusted value so a context that lost its tag fails closed.
*Reverse:* would weaken the only thing protecting header-asserted identity.

**Links are generated two ways: path prefix, or query parameter.** WordPress
addresses admin screens by query string and cannot route `/m/2026-07` at all,
so under the plugin the application path travels as `wpcalc_path=`. Only link
*generation* differs — the handler tree still sees ordinary paths, which is
what keeps the WordPress mode from being a second implementation.
*Reverse:* `buildURL` in `internal/httpx/views.go`.

**The plugin renders a fragment, not a full document.** WordPress owns
`<html>` and `<head>` on an admin page; nesting a second document there would
be invalid and would fight WordPress for the head. The shim sends
`X-Wpcalc-Fragment: 1` and the server picks `fragment.html` over `base.html`.
*Reverse:* have the shim strip the body, which is more fragile.

**The grid works with JavaScript disabled, and that path was built first.**
Every cell is a real form that posts and redirects; `app.js` only intercepts
those submissions. If it fails to load, the grid still works.
*Reverse:* nothing depends on the no-JS path except correctness under a CSP or
a failed asset load.

**The browser never computes totals.** The JSON response carries figures from
the same SQL the PDFs use, so the on-screen numbers cannot drift from the
printed ones. *Reverse:* would reintroduce exactly the disagreement the integer
hours decision exists to prevent.

**PDFs render into a buffer before the response is touched.** Streaming would
commit a 200 and a PDF content type before a mid-render failure, leaving a
truncated download that looks successful. Same reasoning for HTML pages.

**The PDF umlaut test renders with compression disabled.** fpdf's core fonts
are Latin-1, so a string that bypasses the cp1252 translator still produces a
valid PDF and still passes a `%PDF` check — it is only wrong on the page
someone files. With compression on, no text survives in the bytes and the
assertion would have passed vacuously. This was caught by a test that failed
for the wrong reason first.
*Reverse:* `Renderer.SetCompression`.

**`.pdf` is stripped rather than routed on.** Go's mux matches whole path
segments, so `{ym}.pdf` is not an expressible pattern. Both URL forms work.

**Flags are accepted before or after positional arguments.** Go's `flag`
package stops at the first positional, so `user add alice --db /path` silently
left `--db` at its default: the account went into `./wpcalc.db` while the
server, correctly pointed at the intended file, reported that no accounts
existed. A flag that is ignored rather than rejected is the worst of both
worlds. Found by the P3 smoke test, guarded by `TestParseArgsAcceptsFlagsAfterPositionals`.
*Reverse:* `cmd/wpcalc/flags.go`.

**Deviation from the brief: the `en` catalog landed complete at P3 rather than
as translation-only work.** The brief anticipated the catalog would already be
full by then; it was, so adding the locale was the cheap job it predicted.
No `T()` call site changed.

**The sidecar is detached through a throwaway shell, not held as a proc_open
handle.** `proc_close()` waits for the process to exit, and this one runs
forever, so the first version hung every admin page request for 30 seconds
with no error anywhere. Leaking the handle does not help: PHP closes it at
request shutdown with the same wait.
*Reverse:* there is no version of holding the handle that works; a systemd
unit or supervisor outside WordPress is the alternative.

**Health is decided by asking the socket, not by checking the PID file.** A
process that is alive but wedged passes a PID check and fails every request,
which is the hardest failure to diagnose from an admin screen.

**The WordPress e2e stack comes up once per package via `TestMain`.** The
first version brought it up per test and tore it down in the first test's
cleanup, leaving every later test with nothing to talk to. The teardown is
deferred before setup runs, so it happens even when setup itself fails.

**`is_our_page()` reads `$_REQUEST`, not `$_GET`.** The rendered form's action
carries the query string, so `$_GET` is populated in practice — but a client
posting the same fields in the body would otherwise reach no handler at all
and get a 200 that looks like success.

**The e2e asserts on the sidecar's command line, not on listening sockets.**
Two earlier attempts tested the wrong thing: fetching `/healthz` over HTTP
reaches Apache, which answers 200 for unknown paths, and reading
`/proc/net/tcp` flags Docker's own DNS resolver on 127.0.0.11. Whether the
process was started with `--socket` and not `--addr` is the property that
actually matters, and it has no false positives.

**The sample-data command records no working hours.** It creates placeholder
employees and stops there. This is a timesheet: once fabricated entries are in
the database they are indistinguishable from real ones, and a demo database
that gets copied, inherited, or pointed at by mistake would then hold invented
records of work nobody did. The employment periods alone still demonstrate the
grid, the weekend shading, the visibility rule and the locked cells, and an
empty grid is the honest starting state. Renamed from `demo-seed` to
`sample-employees` so the name cannot imply more than it does.
*Reverse:* nothing depends on hours existing; the browser e2e types its own.

**Local dev accounts are `admin`/`admin` and `user`/`user`, and they bypass the
password-length rule — for testing only.** The waiver is an explicit
`--allow-weak-password` flag reaching a separately named `CreateUserWeak`,
never a lowered global minimum, so every caller that waives the rule is
greppable and no ordinary call site can waive it by accident. `user add`
without the flag still refuses them, the CLI warns on stderr when the flag is
used, and an *empty* password is refused even with it — "short" is a
deliberate local choice, "absent" is always a mistake. Tests pin both halves.
*Reverse:* drop the flag and the two Weak entry points; the dev script then
needs real passwords.

**`color-scheme` is declared, not implied.** Without it the browser renders
form controls with its light-theme defaults regardless of the page around
them. In dark mode that meant `input.hours` text computed to `rgb(0, 0, 0)` on
a `rgb(21, 24, 28)` cell — about 1.2:1, effectively invisible — and the
placeholder to `rgb(117, 117, 117)`, the reported dark-grey-on-black, repeated
in every empty cell. Declaring it also fixes scrollbars, autofill and the
caret. Confirmed by reading computed styles out of a real browser in both
schemes rather than by eye.
*Reverse:* nothing depends on it; removing it reintroduces the fault.

**Every colour goes through a variable, with none hardcoded outside the two
palettes.** A hardcoded colour is one that exists in exactly one theme. The
first stylesheet left the focused-cell background as `#fff`, so focusing a
cell in dark mode put near-white text in a white box; the save button, the
save flash and the error state had the same problem.
*Reverse:* would reintroduce per-theme drift.

**Grid surface shading is scoped under `table.grid`, not marked `!important`.**
`td.is-locked` is *less* specific than the `table.grid th, table.grid td` base
rule and silently loses to it — which the original `!important` was papering
over. Dropping the `!important` during the theme rewrite made the locked and
total-row shading vanish, caught by reading computed backgrounds rather than
by looking. Specificity, not urgency, is the right tool.
*Reverse:* re-add `!important` and accept that the next base-rule change
breaks silently again.

**The cell placeholder shows on focus only.** With no invented hours the grid
starts empty, so a `0.00` placeholder in every cell was a wall of grey text
that reads as data. As a focus-time format hint it is useful; at rest it is
noise.
*Reverse:* drop the two `::placeholder` rules.

**The manuals are embedded and shown through `glow`, with a raw fallback.**
`wpcalc manual [user|admin]` reads from an `embed.FS` rather than from disk, so
the guides travel with the binary — the WordPress sidecar and a
copied-to-a-server binary have no source tree beside them. glow is used when it
is on PATH *and* stdout is a terminal; otherwise the markdown is printed as-is.
Gating on the terminal matters: piping into a file or another program would
otherwise fill it with ANSI escapes, which is corruption rather than
formatting. glow being absent is a note on stderr, not an error — markdown is
readable on its own.
*Reverse:* the alternative is `charmbracelet/glamour` as a library, which
always renders but adds a substantial dependency to a binary that otherwise
needs none.

**`manuals.go` sits at the module root purely so `//go:embed` can reach
`docs/`.** An embed directive cannot use "..", and moving the manuals under
`internal/` would put them somewhere no reader looks and GitHub does not
present as documentation. One file is a fair price.
*Reverse:* move `docs/` into the package that embeds it.

**A stored language preference beats `Accept-Language`.** It is the more
specific statement: someone whose laptop is English but who wants the German
interface has said so explicitly. An empty value means "follow the browser" and
is the default, so existing accounts behave exactly as before the column
existed. A preference naming a catalog that no longer ships falls through to
negotiation instead of rendering `!!key!!` markers — which is why the read path
checks `Has()` rather than trusting the stored value.
*Reverse:* `printerFor` in `internal/httpx/views.go` is the only place that
decides.

**Under WordPress the app stores no language preference at all.** WordPress
owns the user record there, so the shim sends that user's WordPress profile
locale as `Accept-Language` and the sidecar honours it through the negotiation
it already performs. A second preference on this side could disagree with the
one the site administrator set, with nothing to say which was authoritative.
The selector is hidden when the authenticator cannot persist, rather than
offered and silently ineffective — `auth.LanguageWriter` is the seam, and
`auth.WordPress` deliberately does not implement it.
*Reverse:* implement LanguageWriter on the WordPress adapter and accept two
sources of truth.

**The language form validates against the loaded catalogs and checks its own
redirect.** An unshipped locale is refused rather than stored, or the account
would render in a language that does not exist and could only be fixed from the
database. `return_to` comes from a form field, so it is an open redirect unless
the target is confirmed local — "//evil.example" is scheme-relative and leaves
the site while looking like a path.

**`plugin export` writes the binary alongside the PHP, not just the PHP.** A
plugin directory without a binary looks complete and does not run — the shim's
entire job is to spawn one. Exporting a copy of the running binary also keeps
the two halves at the same version, which shipping them separately does not:
a plugin from one build talking to a sidecar from another is a failure mode
with no good error message.
*Reverse:* `--php-only` already covers the case where the sidecar is installed
system-wide.

**The embed pattern is `wordpress/wpcalc/*.php`, never the directory.** That
directory also holds `bin/wpcalc` during an e2e run, so embedding it wholesale
would embed the binary inside itself — doubling the build and doubling again on
every rebuild. A test asserts the embedded tree contains no directories.

**The WordPress e2e mounts the exported plugin rather than the source tree.**
Otherwise the suite would prove that a directory nobody ships works, while the
artifact a tester actually receives went untested. A separate test asserts the
mount really is the export, because if that ever drifted back every other test
here would keep passing and mean nothing.

**Copying the binary is guarded against copying onto itself.** Exporting into
the directory the binary already occupies would otherwise truncate it to zero
bytes — destroying the very binary running the command. The copy also goes to a
temporary name and is renamed into place, so an interrupted export cannot leave
a half-written binary for WordPress to try to run.

**The version comes from the toolchain, and -ldflags only overrides it for
tagged releases.** Since Go 1.18 a plain `go build` embeds the revision, the
commit time and whether the tree was dirty, so a binary identifies itself even
though nobody remembered to pass a flag — which is the case that actually
happens when handing a build to a tester. Stamping a bare short SHA from the
Makefile would only restate what is already inside, less precisely.

The dirty marker is carried in the version string itself rather than only in
the detail block, because the version string is what gets pasted into a
ticket. A binary built from uncommitted changes cannot be reproduced from its
revision, and a report naming only the commit sends someone to read code that
was never built.

The "a real build identifies itself" check lives in the standalone e2e, not in
a unit test: Go does not stamp VCS information into test binaries, so
currentBuild() reports "unknown" under `go test` while the shipped binary
carries the revision. Asserting it in a unit test failed for a reason that had
nothing to do with the code.
*Reverse:* `cmd/wpcalc/version.go`.
