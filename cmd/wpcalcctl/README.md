# wpcalcctl

A command-line client for administering a wpcalc server remotely, entirely
over [`/api/v1`](../../internal/apiv1/openapi.yaml) — built on
[`sdk/go`](../../sdk/go), with no dependency on the server's own packages
(no SQLite, no direct database access). It can run from a machine that has
never seen the server's filesystem.

A separate Go module from both the server (`cmd/wpcalc`) and the SDK, so
building it only ever pulls in `sdk/go` and the standard library.

## Build

```sh
cd cmd/wpcalcctl && go build -o ../../bin/wpcalcctl .
```

## Bootstrap, once

`wpcalcctl` cannot create a server's first account or its first token —
an endpoint that requires a bearer token can't be how you get your first
one. That happens on the server itself:

```sh
wpcalc user add alice --db wpcalc.db
wpcalc user grant alice --system -role super_admin --db wpcalc.db
wpcalc token create alice --db wpcalc.db
```

Then bridge into `wpcalcctl`:

```sh
wpcalcctl login --server http://localhost:8080/api/v1 \
  --access-token wpat_... --refresh-token wprt_...
```

This validates the pair against the server and stores it (mode `0600`) at
`$XDG_CONFIG_HOME/wpcalcctl/credentials.json` — overridable via
`WPCALCCTL_CREDENTIALS`. Every later command loads it automatically, and
access-token refresh (tokens expire after an hour) happens transparently,
with the rotated pair saved back before any command returns.

## Everything after that

```sh
wpcalcctl tenant add "Acme Corp"
wpcalcctl user add bob
wpcalcctl user grant bob -tenant 2 -role mandant_admin
wpcalcctl role add auditor -name Auditor -scope tenant
wpcalcctl role permissions auditor -add read
wpcalcctl token list
```

See `wpcalcctl help` for the full command list, or
[`docs/en/admin.md`](../../docs/en/admin.md) for the complete
CLI-to-API mapping and the RBAC96 model behind it.
