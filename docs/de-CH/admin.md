# wpcalc — Administratorhandbuch

Installation, Betrieb und Wartung von wpcalc.

*English: [../en/admin.md](../en/admin.md)*

---

> **Rollen werden gespeichert, aber noch nicht durchgesetzt.** Jedes
> angemeldete Konto kann Mitarbeitende anlegen, bearbeiten und löschen sowie
> jede Auswertung herunterladen — unabhängig von seiner Rolle. Die
> Unterscheidung `admin` / `user` existiert in der Datenbank und in der
> WordPress-Brücke, aber keine Route prüft sie. Behandeln Sie bis auf Weiteres
> jedes Konto als vollwertigen Administrator und geben Sie keine
> `user`-Konten in der Annahme aus, sie seien eingeschränkt.

## Zwei Betriebsarten

**Standalone** — die Binärdatei bedient HTTP selbst, mit eigenen Konten.
Geeignet hinter einem Reverse Proxy oder im LAN.

**WordPress** — die Binärdatei läuft als Sidecar an einem Unix-Socket, ein
Plugin leitet Admin-Anfragen dorthin weiter. Die Identität kommt von
WordPress; es gibt keine zweite Anmeldung und keine separate Kontenliste.

Beide bedienen exakt dieselbe Anwendung.

## Standalone

```sh
make build                                            # -> bin/wpcalc
./bin/wpcalc migrate --db /var/lib/wpcalc/wpcalc.db   # legt die Datei an
./bin/wpcalc user add alice -role admin --db /var/lib/wpcalc/wpcalc.db
./bin/wpcalc serve --addr :8080 --db /var/lib/wpcalc/wpcalc.db
```

`serve` **startet nicht, solange kein Konto existiert** — statt eine Anmeldung
anzubieten, die nicht gelingen kann. Legen Sie das erste Konto vor dem Start an.

Die Binärdatei ist statisch gelinkt und ohne cgo gebaut, hat also keine
Laufzeitabhängigkeiten: auf den Host kopieren und starten.

### Befehle

| Befehl | Zweck |
|---|---|
| `serve --addr :8080 \| --socket PFAD` | Server starten; genau ein Listener |
| `migrate [up\|down\|status]` | Migrationen anwenden, eine zurückrollen, Status zeigen |
| `user add\|passwd\|list` | Konten verwalten |
| `sample-employees [--month YYYY-MM]` | Platzhalter-Mitarbeitende anlegen; erfasst **keine Stunden** |
| `manual [user\|admin]` | eingebettetes Handbuch anzeigen |
| `version` | Version ausgeben |

Flags funktionieren vor oder nach den Positionsargumenten.

### Handbücher lesen

Beide Handbücher sind in die Binärdatei eingebettet und reisen mit ihr — auch
auf einem Server ohne Quellcode daneben sind sie verfügbar:

```sh
wpcalc manual              # Benutzerhandbuch, in der Sprache Ihrer Shell
wpcalc manual admin        # dieses Dokument
wpcalc manual admin --lang en
wpcalc manual --list       # was verfügbar ist
wpcalc manual admin --raw  # Markdown, zum Weiterleiten
```

Für eine gerenderte, seitenweise Darstellung installieren Sie
[glow](https://github.com/charmbracelet/glow). Ohne glow wird das rohe Markdown
ausgegeben, das ebenfalls lesbar ist. Die Rohform wird auch automatisch
verwendet, wenn die Ausgabe kein Terminal ist — `wpcalc manual admin >
admin.md` füllt die Datei also nicht mit Steuerzeichen.

### Umgebungsvariablen

| Variable | Verwendet von | Bedeutung |
|---|---|---|
| `WPCALC_DB` | alle | Standardpfad der Datenbank |
| `WPCALC_SECRET` | `serve --socket` | mit dem WordPress-Plugin geteiltes Geheimnis |
| `WPCALC_BASE_PATH` | `serve` | URL-Präfix, oder vollständige Basis-URL mit `--link-param` |
| `WPCALC_LINK_PARAM` | `serve` | Anwendungspfad in diesem Query-Parameter übertragen |

### Hinter einem Reverse Proxy

TLS am Proxy terminieren und `--secure-cookies` setzen, damit Sitzungscookies
als `Secure` markiert werden. Standardmässig aus, weil der Standalone-Server
oft über einfaches HTTP im LAN erreicht wird — dort würde ein `Secure`-Cookie
schlicht nie gesendet und die Anmeldung scheinbar grundlos scheitern.

## Konten

```sh
./bin/wpcalc user add alice -role admin --db DB   # fragt nach dem Passwort
./bin/wpcalc user passwd alice --db DB            # widerruft auch alices Sitzungen
./bin/wpcalc user list --db DB
```

Passwörter werden mit bcrypt gehasht und müssen mindestens 10 Zeichen lang
sein. Benutzernamen unterscheiden nicht zwischen Gross- und Kleinschreibung.
Sitzungen liegen serverseitig und gelten 12 Stunden — Abmelden oder eine
Passwortänderung entzieht den Zugriff sofort, statt ein Token bis zum Ablauf
gültig zu lassen.

> `--allow-weak-password` hebt die Mindestlänge auf. Es existiert, damit eine
> lokale Entwicklungsdatenbank mit Wegwerf-Zugangsdaten wie `admin`/`admin`
> bestückt werden kann, und gibt bei jeder Verwendung eine Warnung aus.
> **Niemals auf einem erreichbaren System verwenden.**

Ein Konto lässt sich über die Kommandozeile noch nicht löschen; entfernen Sie
die Zeile nötigenfalls direkt aus der Tabelle `users`. Die zugehörigen
Sitzungen verschwinden mit.

## Mitarbeitende

Verwaltung unter **Mitarbeitende**. Jede Person hat einen Namen, ein
Eintrittsdatum und ein optionales Austrittsdatum; solange jemand angestellt
ist, bleibt das Austrittsdatum leer.

Zwei Verhaltensweisen sind wichtig:

- Eine Person erscheint in einem Monat nur, wenn ihre Anstellung in diesen
  Monat fällt. Ausgetretene verschwinden aus späteren Monaten, statt jedes
  Raster zu verbreitern, durch das Sie blättern.
- **Ein verkürzter Anstellungszeitraum löscht keine bereits erfassten
  Stunden**, die nun ausserhalb liegen. Diese Einträge werden unsichtbar und
  nicht mehr bearbeitbar, bleiben aber in der Datenbank — erfasste Stunden
  wegen eines korrigierten Datums stillschweigend zu vernichten wäre
  schlimmer. Bei erneut erweitertem Zeitraum sind sie wieder da.

Das **Löschen** einer Person löscht dagegen alle ihre erfassten Stunden
kaskadierend mit. Es gibt kein Rückgängig.

## Auswertungen

Unter **Auswertungen**, oder direkt:

```
/report/month/2026-07.pdf                  Monatsübersicht
/report/employee/3/month/2026-07.pdf       eine Person, ein Monat
/report/employee/3/year/2026.pdf           eine Person, ein Jahr
```

Alle Zahlen stammen aus denselben Abfragen, aus denen das Raster gezeichnet
wird — ein Ausdruck kann dem Bildschirm nicht widersprechen.

## WordPress

1. `make build`, dann die Binärdatei nach `wordpress/wpcalc/bin/wpcalc` kopieren
2. `wordpress/wpcalc/` nach `wp-content/plugins/` kopieren
3. **wpcalc** aktivieren
4. Im Admin-Menü **Arbeitszeiten** öffnen

Das Plugin startet die Binärdatei bei Bedarf als Sidecar, überwacht sie und
leitet Anfragen mit einer signierten Aussage über den aktuellen
WordPress-Benutzer weiter. Der Zugriff erfordert die Berechtigung
`manage_options`; schreibende Anfragen tragen eine WordPress-Nonce.

Die Laufzeitdateien liegen in `wp-content/uploads/wpcalc/` — Datenbank, Socket,
PID-Datei und Log. Das Plugin legt dort eine `.htaccess` ab, die den Zugriff
verweigert; **ignoriert Ihr Server `.htaccess`, sperren Sie dieses Verzeichnis
selbst**, sonst ist die Datenbank herunterladbar.

**wpcalc → Einstellungen** zeigt, ob der Dienst läuft, ob die Binärdatei
vorhanden und ausführbar ist, ob `proc_open` und `curl` verfügbar sind, die
Laufzeitpfade sowie die letzten Zeilen des Logs. Dort lässt sich der Dienst
auch neu starten und das geteilte Geheimnis neu erzeugen.

Voraussetzungen: PHP 8.1+, WordPress 6.4+, aktiviertes `proc_open`, die
`curl`-Erweiterung mit Unix-Socket-Unterstützung. Ist `proc_open` deaktiviert,
sagt die Admin-Seite das, statt stillschweigend zu scheitern.

## Datenbank und Sicherung

Eine SQLite-Datei. Im WAL-Modus kommen `-wal` und `-shm` dazu.

```sh
# Konsistente Kopie bei laufendem Server:
sqlite3 /var/lib/wpcalc/wpcalc.db ".backup '/backup/wpcalc-$(date +%F).db'"
```

Kopieren Sie die `.db`-Datei nicht mit `cp`, während der Server läuft — Sie
erhalten unter Umständen eine zerrissene Kopie ohne alles, was noch im WAL
steht. Migrationen laufen beim Start automatisch; sichern Sie vor einem
Upgrade der Binärdatei.

## Fehlersuche

**`address already in use`** — der Port ist belegt. Beachten Sie: Ein Container,
der auf `0.0.0.0:PORT` veröffentlicht, blockiert auch `127.0.0.1:PORT`.

```sh
ss -ltnp | grep :8080
```

**`no accounts exist`** — die Benutzertabelle ist leer. `user add` ausführen.

**`serve: one of --addr or --socket is required`** — Absicht. Ein
Standardwert auf TCP würde die Anwendung auf einem Host veröffentlichen, der
den Socket verwenden wollte.

**Die WordPress-Seite zeigt «wpcalc could not start»** — die Meldung nennt die
Ursache. Prüfen Sie **wpcalc → Einstellungen** und
`wp-content/uploads/wpcalc/wpcalc.log`.

**Stunden in einer Auswertung sehen falsch aus** — Auswertungen umfassen nur
den genannten Zeitraum. Prüfen Sie, ob Sie denselben Monat betrachten wie im
Raster.

## Upgrade

Binärdatei ersetzen und neu starten. Migrationen laufen beim Start.
Unter WordPress `wordpress/wpcalc/bin/wpcalc` ersetzen und auf der
Einstellungsseite **Dienst neu starten** verwenden, oder das Plugin
deaktivieren und wieder aktivieren.

Sichern Sie vorher: Migrationen laufen automatisch vorwärts, ein Rückwärtsschritt
bedeutet, die Datei wiederherzustellen.
