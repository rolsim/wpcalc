# wpcalc

*English: [README.md](README.md)*

Ein monatliches Arbeitszeitraster für Mitarbeitende, ausgeliefert als eine
einzige statische Go-Binärdatei, die entweder als eigenständiger Webserver
oder als Sidecar hinter einem schlanken WordPress-Plugin läuft.

- **y-Achse** — jeder Tag des Monats, Wochenenden schattiert
- **x-Achse** — die Mitarbeitenden, deren Anstellung sich mit diesem Monat
  überschneidet, und nur diese
- **Zellen** — Dezimalstunden ("industrielle Minuten": `7.75` sind 7 Std.
  45 Min.), gesperrt ausserhalb der jeweiligen Anstellungsdauer
- **Summen** — Totale pro Mitarbeitendem am unteren Rand, pro Tag am rechten
  Rand, Gesamttotal in der Ecke
- **Auswertungen** — monatliche Gesamtübersicht als PDF sowie monatliche und
  jährliche PDFs pro Mitarbeitendem

Das Raster funktioniert auch ohne JavaScript. JavaScript entfernt lediglich
den Seiten-Reload.

## Dokumentation

| | Deutsch (CH) | English |
|---|---|---|
| Arbeitszeit erfassen | [docs/de-CH/user.md](docs/de-CH/user.md) | [docs/en/user.md](docs/en/user.md) |
| Betrieb | [docs/de-CH/admin.md](docs/de-CH/admin.md) | [docs/en/admin.md](docs/en/admin.md) |
| Ausprobieren | [docs/de-CH/testing.md](docs/de-CH/testing.md) | [docs/en/testing.md](docs/en/testing.md) |

Das Administratorhandbuch deckt beide Betriebsarten ab: Konten, das
WordPress-Plugin, Datensicherung und Fehlerbehebung.

Beide sind in der Binärdatei eingebettet und im Terminal lesbar:

```sh
wpcalc manual              # Benutzerhandbuch, in der Sprache Ihrer Shell
wpcalc manual admin        # Administratorhandbuch
wpcalc manual testing      # wie man es ausprobiert
wpcalc manual --lang en    # Sprache erzwingen
wpcalc manual --list       # was verfügbar ist
```

Wird mit [glow](https://github.com/charmbracelet/glow) dargestellt, sofern
installiert und die Ausgabe ein Terminal ist; sonst reines Markdown, damit
das Umleiten in eine Datei oder ein anderes Programm sauber bleibt.

---

## Build

```sh
make build          # -> bin/wpcalc, statisch gelinkt, ohne cgo
make check          # build + vet + lint + Unit-Tests — das Gate, das jeder Commit passieren muss
```

Go 1.26+. Kein npm, kein Bundler, kein Build-Schritt für Assets.

## In VS Code ausführen

**F5** drücken. Prüft, ob Port 8090 frei ist, initialisiert `.dev/wpcalc.db`
beim ersten Lauf, startet den Server unter dem Debugger auf
<http://127.0.0.1:8090> und öffnet einen Browser, sobald der Port tatsächlich
lauscht. Breakpoints funktionieren.

Ist der Port belegt, stoppt der Start, bevor der Debugger anläuft, und nennt,
was ihn belegt. Diese Prüfung existiert, weil der Fehler sonst irreführend
wäre: Der Server beendet sich, der Browser öffnet trotzdem, und was auch immer
den Port belegt, antwortet stattdessen — die Anwendung wirkt dadurch defekt
statt schlicht nicht gestartet.

Dev-Logins, angelegt durch den Prelaunch-Task: **`admin` / `admin`**
(Englisch) und **`user` / `user`** (Deutsch) — absichtlich unterschiedliche
Sprachen, damit die gespeicherte Präferenz sichtbar ist, ohne dass man etwas
ändern muss.

> Diese umgehen die Mindestlänge fürs Passwort über ein explizites Flag
> `--allow-weak-password`, **nur für lokale Tests**. Sie existieren in einer
> gitignorten Datenbank auf Ihrem Rechner. Legen Sie Konten auf diese Weise
> nie auf etwas Erreichbarem an — `user add` verweigert derart kurze
> Passwörter, sofern das Flag nicht gesetzt ist, und das ist Absicht.

Der erste Lauf legt zudem vier Platzhalter-Mitarbeitende an, damit das Raster
Spalten hat. **Es werden keine Arbeitsstunden erfasst.** Dies ist eine
Zeiterfassung: erfundene Einträge sind, einmal in der Datenbank, nicht von
echten zu unterscheiden — die Testdaten beschränken sich deshalb auf die
Anstellungszeiträume, was genügt, um Raster, Wochenendschattierung,
Sichtbarkeitsregel und gesperrte Zellen zu zeigen. Erneutes F5 ändert nichts;
für eine saubere Datenbank den Task **wpcalc: reset dev database** verwenden.

Weitere Konfigurationen: *serve + open in editor* (Simple Browser statt
externem Browser), *serve (WordPress sidecar)* zum Durchsteppen der
Socket- und signierten-Header-Pfade, sowie *debug current test*.

## Eigenständig ausführen

```sh
./bin/wpcalc migrate --db wpcalc.db          # legt die Datei an, falls nicht vorhanden
./bin/wpcalc user add alice --db wpcalc.db
./bin/wpcalc user grant alice --system -role super_admin --db wpcalc.db
./bin/wpcalc serve --addr :8080 --db wpcalc.db
```

Dann <http://localhost:8080> öffnen.

`serve` startet nicht, solange kein Konto die Datenbank verwalten kann — es
weist stattdessen darauf hin, `user add` und `user grant` auszuführen, statt
einen Login anzubieten, der nicht gelingen kann. wpcalc ist mandantenfähig
mit RBAC96-artigen Rollen (mehrere Firmen in einer Datenbank, jedes Konto mit
Rollen im System-, Mandanten- oder Mitarbeitenden-Geltungsbereich); siehe das
[Administratorhandbuch](docs/de-CH/admin.md) für das vollständige Modell.

Damit das Raster überhaupt Spalten zum Darstellen hat:

```sh
./bin/wpcalc sample-employees --db wpcalc.db --month 2026-07
```

Dies legt ausschliesslich Platzhalter-Mitarbeitende an. Es werden absichtlich
keine Stunden erfasst — siehe Hinweis oben.

### Befehle

| Befehl | Zweck |
|---|---|
| `wpcalc serve --addr :8080 \| --socket PATH` | Server starten; genau ein Listener |
| `wpcalc migrate [up\|down\|status]` | Migrationen anwenden, eine zurückrollen oder Status ausgeben |
| `wpcalc user add\|passwd\|lang\|roles\|list` | Standalone-Konten verwalten (`--allow-weak-password` nur für lokale Tests) |
| `wpcalc user grant\|revoke <Name> [-system\|-tenant ID\|-employee ID] [-role ID]` | Rolle zuweisen oder entziehen |
| `wpcalc tenant add\|list\|rename` | Mandanten verwalten |
| `wpcalc role add\|list\|delete\|permissions` | Rollenkatalog verwalten |
| `wpcalc sample-employees [--month YYYY-MM]` | Platzhalter-Mitarbeitende anlegen; erfasst keine Stunden |
| `wpcalc manual [user\|admin] [--lang L] [--raw] [--list]` | ein eingebettetes Handbuch anzeigen, via glow sofern verfügbar |
| `wpcalc plugin export DIR [--force] [--php-only]` | das WordPress-Plugin aus der Binärdatei schreiben |
| `wpcalc version [--short]` | den Build ausgeben: Version, Commit, Datum, Go |

Flags dürfen vor oder nach Positionsargumenten stehen.

### Umgebungsvariablen

| Variable | Verwendet von | Bedeutung |
|---|---|---|
| `WPCALC_DB` | alle | Standard-Datenbankpfad |
| `WPCALC_SECRET` | `serve --socket` | HMAC-Secret, geteilt mit dem WordPress-Plugin |
| `WPCALC_BASE_PATH` | `serve` | URL-Präfix, oder vollständige Basis-URL mit `--link-param` |
| `WPCALC_LINK_PARAM` | `serve` | trägt den App-Pfad in diesem Query-Parameter |

## WordPress-Plugin installieren

Die Binärdatei trägt das Plugin in sich und installiert sich selbst:

```sh
wpcalc plugin export /var/www/html/wp-content/plugins
```

Dies schreibt `wpcalc/wpcalc.php` und `wpcalc/bin/wpcalc` — eine Kopie der
Binärdatei, die es geschrieben hat, sodass Plugin und Sidecar immer dieselbe
Version haben. Anschliessend **wpcalc** aktivieren und **Working hours** im
Admin-Menü öffnen.

Das Plugin startet die Binärdatei bei Bedarf als Sidecar an einem
Unix-Socket unter `wp-content/uploads/wpcalc/`, überwacht sie und leitet
Admin-Anfragen mit einer signierten Bestätigung des aktuellen
WordPress-Benutzers dorthin weiter. Zugriff erfordert `manage_options`;
Schreibvorgänge tragen eine WordPress-Nonce.

Kann sie nicht starten — `proc_open` deaktiviert, Binärdatei fehlt, falsche
Berechtigungen —, zeigt die Admin-Seite an, welcher dieser Fälle vorliegt,
statt einen leeren Bildschirm anzuzeigen.

## Tests

```sh
make test           # Unit- und Handler-Tests, ohne Container, Sekunden
make e2e-wp         # WordPress-Integration: docker compose + wp-cli, Minuten
```

`make e2e-wp` baut eine Linux-Binärdatei ins Plugin, fährt WordPress und
MariaDB hoch, installiert und aktiviert das Plugin und steuert die
Admin-Seite über HTTP an. Fährt den Stack bei jedem Exit-Pfad herunter,
auch im Fehlerfall.

## Releases

Jeder Push nach `main` und jeder Pull Request führt `make check` über
[`.github/workflows/ci.yml`](.github/workflows/ci.yml) aus. Ein Release zu
erstellen ist danach nur noch ein Tag:

```sh
git tag v0.1.0        # oder v0.1.0-alpha, v0.1.0-rc.1, ...
git push origin v0.1.0
```

Das Pushen eines Tags nach dem Muster `v*` führt
[`.github/workflows/release.yml`](.github/workflows/release.yml) aus: führt
zuerst `make check` auf dem getaggten Commit aus — ein Tag auf einem
kaputten Commit wird hier erneut geprüft, statt sich darauf zu verlassen,
dass die CI für genau diesen Commit bereits erfolgreich war — und baut erst
danach
`linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64` und
`windows/amd64` aus einem einzigen Linux-Runner (kein cgo, also kein
Runner pro Betriebssystem nötig), stempelt `-X main.version=<Tag>` und
veröffentlicht ein GitHub-Release mit je einem Archiv pro Plattform sowie
`checksums.txt`.

Ein Tag mit angehängtem Bindestrich-Suffix (`-alpha`, `-beta`, `-rc.1`, ...)
wird automatisch als GitHub-Prerelease veröffentlicht; ein reines `vX.Y.Z`
ist ein stabiles Release.

## Architektur

```
cmd/wpcalc/        Subcommands und beide Listener
internal/domain/   Kalender, Anstellung, Ganzzahlstunden — pur, ohne I/O
internal/store/    SQLite; goose-Migrationen eingebettet; jede Query lebt hier
internal/httpx/    ein Handler-Baum, in beiden Modi identisch bedient
internal/auth/     Authenticator: lokale Konten, oder signierte WordPress-Header
internal/report/   die drei PDFs
internal/i18n/     eingebettete Kataloge; de-CH Standard, en verfügbar
wordpress/wpcalc/  der PHP-Shim
```

Zwei Eigenschaften sind wissenswert, bevor etwas geändert wird:

**Ein Handler-Baum, zwei Modi.** Nichts in `internal/httpx` weiss, ob es
eigenständig oder hinter WordPress läuft. Die Unterschiede sind der
injizierte `Authenticator` und der gebundene Listener. Wenn eine Änderung
verlangt, dass ein Handler das weiss, liegt die Naht am falschen Ort.

**Stunden sind Ganzzahlen.** `domain.Centihours` sind Hundertstelstunden.
Das Raster summiert dieselben Einträge auf zwei Arten, die PDFs auf eine
dritte; diese Totale müssen exakt übereinstimmen, was Floats nicht
garantieren können. Niemals `.Hours()` summieren — `Centihours` summieren
und erst am Ende einmal umrechnen.

Siehe [DECISIONS.md](DECISIONS.md) für die Begründung hinter diesen und den
anderen Entscheidungen, jeweils mit dem, was es bräuchte, um sie
rückgängig zu machen.

## Lizenz

MIT — siehe [LICENSE](LICENSE). Der Header des WordPress-Plugins deklariert
dasselbe.
