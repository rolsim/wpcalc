# wpcalc Go SDK

*English: [README.md](README.md)*

Ein typisierter Go-Client für die [`/api/v1`](../../internal/apiv1/openapi.yaml)-
JSON-API von wpcalc — generiert aus demselben OpenAPI-Dokument, aus dem auch
der Server selbst generiert wird, plus eine handgeschriebene Schicht darüber:
ein Bearer-Token über Aufrufe hinweg tragen und es transparent gegen ein
neues austauschen, wenn es abläuft.

Ein eigenes Go-Modul (eigenes `go.mod`), sodass das Einbinden nicht die
eigenen Abhängigkeiten des Servers zieht (SQLite, goose, fpdf, chromedp, ...)
— nur `github.com/oapi-codegen/runtime`.

## Installation

```sh
go get github.com/rolsim/wpcalc/sdk/go
```

## Verwendung

Zuerst ein Token-Paar besorgen — von `wpcalc token create <username>` auf
dem Server, oder aus einem zuvor gespeicherten `Session.Tokens()` /
`WithOnRefresh`-Callback:

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
		// p dauerhaft irgendwo speichern — das Refresh-Token, das gerade
		// benutzt wurde, um hierher zu gelangen, ist jetzt verbraucht
		// (einmal verwendbar), und dies ist die einzige Kopie seines
		// Ersatzes.
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

Jede `/api/v1`-Operation ist auf dieselbe Weise verfügbar:
`<OperationId>WithResponse(ctx, ...)`, das eine typisierte Antwortstruktur
mit einem Feld pro dokumentiertem Statuscode zurückgibt (`JSON200`,
`JSON201`, `JSONDefault`, ...) plus dem rohen `Body` und `HTTPResponse`. Die
vollständige Liste steht in
[`/api/v1/openapi.html`](../../internal/apiv1/openapi.yaml) auf einem
laufenden Server, oder in `client.gen.go` in diesem Verzeichnis.

## Access-Tokens laufen ab; das hier übernimmt das für Sie

Access-Tokens (`wpat_...`) sind eine Stunde gültig. Jede Anfrage über eine
`Session`, die einen `401` erhält, tauscht automatisch das aktuelle
Refresh-Token (`wprt_...`, 30 Tage gültig) gegen ein neues Paar und
versucht es einmal erneut — Aufrufende bekommen den Ablauf nie zu sehen,
ausser das Refresh-Token selbst ist ebenfalls abgelaufen oder widerrufen
worden; dann schlägt die Anfrage mit einem gewöhnlichen `401` fehl, wie
jeder andere Authentifizierungsfehler auch.

Refresh-Tokens sind einmal verwendbar: gleichzeitige Anfragen, die alle im
selben Moment einen `401` erhalten, verschmelzen zu genau einer Erneuerung,
nicht einer pro Anfrage — ein zweiter, überflüssiger Erneuerungsversuch
würde selbst fehlschlagen, da der erste das Token bereits verbraucht hat.

`Session.Tokens()` kann jederzeit aufgerufen werden, um das aktuelle Paar
zu lesen — nützlich, wenn `WithOnRefresh` nicht verwendet wird und der
Zustand kurz vor Prozessende statt bei jeder Erneuerung gespeichert werden
soll.

## Neu generieren

`client.gen.go` wird aus `../../internal/apiv1/openapi.yaml` generiert via
[`oapi-codegen`](https://github.com/oapi-codegen/oapi-codegen). Nach einer
Änderung der Spezifikation:

```sh
go generate ./...
```
