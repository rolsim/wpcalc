# wpcalcctl

*English: [README.md](README.md)*

Ein Kommandozeilen-Client zur Fernadministration eines wpcalc-Servers,
vollständig über [`/api/v1`](../../internal/apiv1/openapi.yaml) — aufgebaut
auf [`sdk/go`](../../sdk/go), ohne jede Abhängigkeit von den eigenen Paketen
des Servers (kein SQLite, kein direkter Datenbankzugriff). Läuft auch auf
einer Maschine, die das Dateisystem des Servers nie gesehen hat.

Ein eigenes Go-Modul, getrennt vom Server (`cmd/wpcalc`) und vom SDK, sodass
das Bauen nur `sdk/go` und die Standardbibliothek zieht.

## Build

```sh
cd cmd/wpcalcctl && go build -o ../../bin/wpcalcctl .
```

## Bootstrap, einmalig

`wpcalcctl` kann das erste Konto eines Servers oder dessen erstes Token nicht
selbst anlegen — ein Endpunkt, der ein Bearer-Token voraussetzt, kann nicht
der Weg sein, wie man sein erstes bekommt. Das geschieht auf dem Server
selbst:

```sh
wpcalc user add alice --db wpcalc.db
wpcalc user grant alice --system -role super_admin --db wpcalc.db
wpcalc token create alice --db wpcalc.db
```

Dann die Brücke zu `wpcalcctl` schlagen:

```sh
wpcalcctl login --server http://localhost:8080/api/v1 \
  --access-token wpat_... --refresh-token wprt_...
```

Dies prüft das Paar gegen den Server und speichert es (Modus `0600`) unter
`$XDG_CONFIG_HOME/wpcalcctl/credentials.json` — überschreibbar via
`WPCALCCTL_CREDENTIALS`. Jeder spätere Befehl lädt es automatisch, und die
Erneuerung des Access-Tokens (Tokens laufen nach einer Stunde ab) geschieht
transparent, wobei das erneuerte Paar gespeichert wird, bevor irgendein
Befehl zurückkehrt.

## Alles danach

```sh
wpcalcctl tenant add "Acme Corp"
wpcalcctl user add bob
wpcalcctl user grant bob -tenant 2 -role mandant_admin
wpcalcctl role add auditor -name Auditor -scope tenant
wpcalcctl role permissions auditor -add read
wpcalcctl token list
```

Siehe `wpcalcctl help` für die vollständige Befehlsliste, oder
[`docs/de-CH/admin.md`](../../docs/de-CH/admin.md) für die komplette
Zuordnung von CLI zu API und das dahinterliegende RBAC96-Modell.
