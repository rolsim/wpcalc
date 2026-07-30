# Testing wpcalc

How to try the application out — in both run modes, with or without the
source tree. Budget about fifteen minutes.

There are two starting points. If you only received the binary, follow the
first section; if you have the repository, the second. Everything after that
applies to both.

---

## With only the binary

You have a single file: `wpcalc`. That is all you need. It is statically
built, so it has no dependencies, and creates everything else itself.

Put the file in an empty directory, make it executable, and check briefly
that it runs:

```sh
chmod +x wpcalc
./wpcalc version
```

`version` names the commit and build date. Include that line in any report —
if it says `dirty`, the binary was built from a working tree with unsaved
changes and cannot be reconstructed from the commit alone.

The manuals are embedded, so there is nothing to look up separately:

```sh
./wpcalc manual          # user guide
./wpcalc manual admin    # administrator guide
```

If [glow](https://github.com/charmbracelet/glow) is installed it is rendered;
otherwise plain text. Either way it is readable.

Then four commands, and the application is running:

```sh
./wpcalc sample-employees --db test.db --month 2026-07
./wpcalc user add tester -lang en --db test.db
./wpcalc user grant tester --system -role super_admin --db test.db
./wpcalc serve --addr 127.0.0.1:8090 --db test.db
```

The first creates four sample employment records in the default tenant so the
grid has columns — deliberately no hours recorded. The second asks for a
password twice; it must be at least ten characters. `user add` alone grants no
access — the third command is what makes `tester` an administrator, able to
reach everything. The fourth starts the server.

## With the source tree

```sh
make build
./bin/wpcalc sample-employees --db test.db --month 2026-07
./bin/wpcalc user add tester -lang en --db test.db
./bin/wpcalc user grant tester --system -role super_admin --db test.db
./bin/wpcalc serve --addr 127.0.0.1:8090 --db test.db
```

Same content; only the path to the binary differs.

## Either way

The server deliberately does not start while no account exists — the order
above is therefore required, not a suggestion. If the port is taken, it says
so plainly; 8080 is often already in use.

Then sign in at <http://127.0.0.1:8090> in a browser.

---

## What to look out for

**Totals.** Type a number into a cell and click elsewhere. The totals at the
bottom and on the right must move immediately, without a page reload.

**Input format.** `7.75` and `7,75` are the same thing, namely seven hours and
45 minutes. `7:45`, by contrast, must come back as an error and must not be
stored as `7` — a visible message is better than a silently altered number.

**Locked cells.** The grey cells with a dot lie outside the employment period
and accept nothing. "Sample C" starts on the 15th, where the difference is
easy to see; "Sample D" left in the previous month and so does not appear at
all. Weekends are shaded.

**Crossing months.** Page across a year boundary with the arrows, e.g. from
December to January.

**Reports.** Download the three PDFs and check them — the numbers inside must
match the screen.

**Roles.** Create a tenant and a second, employee-scoped account (`user add
name`, then `user grant name -employee ID -role viewer`) and sign in as it:
the "Employees" nav link is gone, `/employees` and the whole-tenant month PDF
come back forbidden, and only that one employee's cells are visible at all —
every other employee is missing from the grid entirely, not merely locked.
`editor` can write those cells; `viewer` cannot. `/reports` still opens, but
lists only that one employee.

Two things that are easy to miss. The language selector in the top right is
stored with the account, so it also applies when you sign in from a different
browser. And try turning JavaScript off and reloading: every cell then has its
own button and the page reloads on every save, but everything still works.
That is exactly how it should be.

If you want to start over: stop the server, delete `test.db`, run the
commands above again. There are no other traces left in the system.

---

## WordPress

The same single file is enough here too: the plugin is embedded in the binary
and writes itself out. All that is needed is a WordPress installation you
have file access to.

```sh
./wpcalc plugin export /path/to/wp-content/plugins
```

This creates `wpcalc/wpcalc.php` and `wpcalc/bin/wpcalc` — the latter is a
copy of exactly the binary you ran the command with. That keeps the plugin
and the sidecar always in sync. An existing directory is not overwritten
without `--force`.

Then activate the plugin in WordPress and open **Working hours** in the admin
menu. What is most worth checking is that it feels like an ordinary plugin:
no second login, the WordPress session is enough.

Under **wpcalc → Settings** you can see whether the sidecar is running,
whether the binary was found, and what the log last said — that is the first
place to look if something is stuck. The language here follows the locale in
the WordPress profile, so there is deliberately no separate language
selector: otherwise there would be two places that could disagree.

A stress test worth running: rename `bin/wpcalc` in the plugin directory and
reload the page. No blank page should appear — instead a message naming the
missing path.

---

## Automated

Source tree only. Three commands:

```sh
make check     # build, linter and around 130 tests — seconds
make e2e       # a real browser in a container — around 10 seconds
make e2e-wp    # WordPress, MariaDB and the plugin in containers — around 45 seconds
```

`make e2e-wp` uses the *exported* plugin, not the source directory — so what
is tested is exactly what a tester receives. No containers or volumes are
left behind after the run.

---

## One known point

The sample data deliberately contains no working hours, only employment
records. Invented entries could no longer be told apart from real ones later,
which is not something you want in a timesheet. An empty grid is the correct
starting state.
