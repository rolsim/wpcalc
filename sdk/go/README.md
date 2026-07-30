# wpcalc Go SDK

A typed Go client for wpcalc's [`/api/v1`](../../internal/apiv1/openapi.yaml)
JSON API — generated from the same OpenAPI document the server itself is
generated from, plus one hand-written layer on top: carrying a bearer token
across calls and transparently exchanging it for a new one when it expires.

A separate Go module (its own `go.mod`), so importing it doesn't pull in the
server's own dependencies (SQLite, goose, fpdf, chromedp, ...) — only
`github.com/oapi-codegen/runtime`.

## Install

```sh
go get github.com/rolsim/wpcalc/sdk/go
```

## Use

Get a token pair first — from `wpcalc token create <username>` on the
server, or from a prior `Session.Tokens()` / `WithOnRefresh` callback you
persisted:

```go
package main

import (
	"context"
	"fmt"
	"log"

	wpcalc "github.com/rolsim/wpcalc/sdk/go"
)

func main() {
	sess, err := wpcalc.New("http://localhost:8080/api/v1", wpcalc.TokenPair{
		AccessToken:  "wpat_...",
		RefreshToken: "wprt_...",
	}, wpcalc.WithOnRefresh(func(p wpcalc.TokenPair) {
		// Persist p somewhere durable — the refresh token just used to
		// get here is now spent (single-use) and this is the only copy
		// of its replacement.
	}))
	if err != nil {
		log.Fatal(err)
	}

	resp, err := sess.ListAccessibleTenantsWithResponse(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	if resp.JSON200 == nil {
		log.Fatalf("status %d: %s", resp.StatusCode(), resp.Body)
	}
	for _, t := range *resp.JSON200 {
		fmt.Println(t.Id, t.Name)
	}
}
```

Every `/api/v1` operation is available the same way: `<OperationId>WithResponse(ctx, ...)`,
returning a typed response struct with one field per documented status
code (`JSON200`, `JSON201`, `JSONDefault`, ...) plus the raw `Body` and
`HTTPResponse`. The full list is in
[`/api/v1/openapi.html`](../../internal/apiv1/openapi.yaml) on a running
server, or `client.gen.go` in this directory.

## Access tokens expire; this handles it for you

Access tokens (`wpat_...`) are valid for one hour. Every request made
through a `Session` that gets a `401` automatically exchanges the current
refresh token (`wprt_...`, valid 30 days) for a new pair and retries once
— callers never see the expiry unless the refresh token itself has also
expired or been revoked, in which case the request fails with a normal
`401` same as any other authentication failure.

Refresh tokens are single-use: concurrent requests that all hit `401` at
the same moment coalesce into exactly one refresh, not one per request —
a second, redundant refresh attempt would itself fail, since the first one
already spent the token.

Call `Session.Tokens()` any time to read the current pair — useful if
you're not using `WithOnRefresh` and want to persist state right before a
process exits rather than on every refresh.

## Regenerating

`client.gen.go` is generated from `../../internal/apiv1/openapi.yaml` via
[`oapi-codegen`](https://github.com/oapi-codegen/oapi-codegen). After the
spec changes:

```sh
go generate ./...
```
