# wpcalc — Administratorhandbuch

Installation, Betrieb und Wartung von wpcalc.

*English: [../en/admin.md](../en/admin.md)*

---

> **Dies ist eine mandantenfähige Anwendung mit durchgesetztem RBAC.**
> Mehrere Firmen («Mandanten») können sich eine Datenbank teilen, vollständig
> voneinander isoliert. Der Zugriff folgt dem NIST-RBAC96-Modell: ein Konto
> hält eine oder mehrere **Rollen**, jeweils gültig für das ganze System, für
> einen Mandanten oder für eine einzelne Person. Siehe
> [Mandanten und Rollen](#mandanten-und-rollen) weiter unten — ein neu
> angelegtes Konto hat **noch keinerlei Zugriff**, bis ihm eine Rolle
> zugewiesen wird.

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
./bin/wpcalc user add alice --db /var/lib/wpcalc/wpcalc.db
./bin/wpcalc user grant alice --system -role super_admin --db /var/lib/wpcalc/wpcalc.db
./bin/wpcalc serve --addr :8080 --db /var/lib/wpcalc/wpcalc.db
```

`serve` **startet nicht, solange kein Konto die Datenbank verwalten kann** —
statt eine Anmeldung anzubieten, die nicht gelingen kann. Legen Sie das erste
Konto an und weisen Sie ihm `super_admin` zu, bevor Sie starten — siehe
[Mandanten und Rollen](#mandanten-und-rollen).

Die Binärdatei ist statisch gelinkt und ohne cgo gebaut, hat also keine
Laufzeitabhängigkeiten: auf den Host kopieren und starten.

### Befehle

| Befehl | Zweck |
|---|---|
| `serve --addr :8080 \| --socket PFAD` | Server starten; genau ein Listener |
| `migrate [up\|down\|status]` | Migrationen anwenden, eine zurückrollen, Status zeigen |
| `user add\|passwd\|lang\|roles\|list` | Konten verwalten |
| `user grant\|revoke <Name> [-system\|-tenant ID\|-employee ID] [-role ID]` | Rolle zuweisen oder entziehen |
| `tenant add\|list\|rename` | Mandanten verwalten |
| `role add\|list\|delete\|permissions` | Rollenkatalog verwalten: was jede Rolle darf |
| `permission list` | den festen Berechtigungskatalog auflisten (nur lesend) |
| `sample-employees [--month YYYY-MM] [--tenant ID]` | Platzhalter-Mitarbeitende anlegen; erfasst **keine Stunden** |
| `manual [user\|admin]` | eingebettetes Handbuch anzeigen |
| `plugin export VERZ` | WordPress-Plugin aus der Binärdatei schreiben |
| `version [--short]` | Build ausgeben: Version, Commit, Datum, Go |

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
./bin/wpcalc user add alice --db DB   # fragt nach dem Passwort; noch kein Zugriff
./bin/wpcalc user passwd alice --db DB            # widerruft auch alices Sitzungen
./bin/wpcalc user list --db DB
```

Passwörter werden mit bcrypt gehasht und müssen mindestens 10 Zeichen lang
sein. Benutzernamen unterscheiden nicht zwischen Gross- und Kleinschreibung.
Sitzungen liegen serverseitig und gelten 12 Stunden — Abmelden oder eine
Passwortänderung entzieht den Zugriff sofort, statt ein Token bis zum Ablauf
gültig zu lassen.

`user add` legt nur die Zugangsdaten an — das Konto kann nichts, bis ihm eine
Rolle zugewiesen wird (siehe nächster Abschnitt). Es gibt keinen Moment, in
dem ein Konto mit einer stillschweigenden Rolle existiert, die niemand
verlangt hat.

> `--allow-weak-password` hebt die Mindestlänge auf. Es existiert, damit eine
> lokale Entwicklungsdatenbank mit Wegwerf-Zugangsdaten wie `admin`/`admin`
> bestückt werden kann, und gibt bei jeder Verwendung eine Warnung aus.
> **Niemals auf einem erreichbaren System verwenden.**

### Sprache der Oberfläche

Jedes Konto speichert eine bevorzugte Sprache, oder einen leeren Wert für «der
Browser entscheidet». Benutzer ändern sie selbst über die Auswahl oben rechts;
Sie können sie beim Anlegen oder später setzen:

```sh
./bin/wpcalc user add alice -lang en --db DB
./bin/wpcalc user lang alice de-CH --db DB    # de_CH wird ebenfalls akzeptiert
./bin/wpcalc user lang alice "" --db DB       # wieder dem Browser folgen
```

Eine gespeicherte Einstellung hat Vorrang vor `Accept-Language` des Browsers,
weil sie die genauere Aussage ist. Verweist eine Einstellung auf eine Sprache,
die nicht mehr ausgeliefert wird, greift wieder die Aushandlung, statt dass das
Konto unbrauchbar wird.

**Unter WordPress gilt das nicht.** Dort gehört der Benutzerdatensatz
WordPress, die Oberfläche folgt also dem Profil-Locale des jeweiligen
WordPress-Benutzers, und die Anwendung blendet ihre eigene Auswahl aus. Eine
zweite Einstellung hier könnte der vom Website-Administrator gesetzten
widersprechen, ohne dass erkennbar wäre, welche massgeblich ist.

Ein Konto lässt sich über die Kommandozeile noch nicht löschen; entfernen Sie
die Zeile nötigenfalls direkt aus der Tabelle `users`. Die zugehörigen
Sitzungen verschwinden mit.

## Mandanten und Rollen

wpcalc folgt [NIST RBAC96](https://csrc.nist.gov/projects/role-based-access-control)
(Sandhu, Coyne, Feinstein, Youman 1996) — demselben Modell wie Kubernetes
RoleBindings oder AWS/GCP-IAM-Policies: eine **Rolle** bündelt
**Berechtigungen**, und eine **Rollenzuweisung** gewährt einem Konto eine
Rolle in einem **Geltungsbereich**. Es gibt drei Geltungsbereiche, vom
weitesten zum engsten:

- **system** — die ganze Datenbank; eine systemweite Rolle deckt jeden
  Mandanten ab.
- **tenant** — ein Mandant; deckt jede Person darin ab.
- **employee** — nur die Zeiterfassung einer einzelnen Person.

Ab Werk existieren fünf Rollen (alle vollständig editierbar — siehe unten):

| Rolle | Geltungsbereich | Darf |
|---|---|---|
| `super_admin` | system | Alles: Mandanten, Rollen, Mitarbeitende und Konten überall verwalten. |
| `mandant_admin` | tenant | Die Mitarbeitenden eines Mandanten und die mitarbeiterbezogenen Rollen seiner Konten verwalten. |
| `viewer` | employee | Das Raster einer Person lesen. |
| `reporter` | employee | Lesen und die PDF-Auswertungen dieser Person herunterladen. |
| `editor` | employee | Lesen, drucken und die Stunden dieser Person erfassen. |

Jede Berechtigungsprüfung in der Anwendung ist eine Abfrage, ob die gehaltene
Rolle in einem Geltungsbereich, der das Ziel abdeckt, die nötige Berechtigung
trägt — nirgends ist ein Rollenname fest verdrahtet. Der Zugriff eines
`mandant_admin` endet an der Grenze des eigenen Mandanten: für den Zugriff auf
die Mitarbeitenden eines anderen Mandanten gibt es `404` (nicht `403`, damit
keine Existenz verraten wird), für systemweite Seiten wie die
Mandantenverwaltung `403`.

### Einrichtung

```sh
./bin/wpcalc tenant add "Acme Corp" --db DB                # -> Mandanten-Id, z. B. 2
./bin/wpcalc user add alice --db DB
./bin/wpcalc user grant alice --system -role super_admin --db DB

./bin/wpcalc user add bob --db DB
./bin/wpcalc user grant bob -tenant 2 -role mandant_admin --db DB

./bin/wpcalc user add carol --db DB
./bin/wpcalc user grant carol -employee 5 -role viewer --db DB

./bin/wpcalc user roles bob --db DB                        # was bob erreichen kann
./bin/wpcalc user revoke carol -employee 5 --db DB          # Rolle ändern: erst entziehen, dann neu zuweisen
```

Ein Konto hält höchstens eine Rolle pro Geltungsbereichs-**Instanz** —
`-employee 5` zweimal mit unterschiedlichen Rollen wird abgelehnt; zuerst
entziehen. `-system`, `-tenant ID` und `-employee ID` schliessen sich
gegenseitig aus; genau eines ist erforderlich.

Ein Konto mit mehreren mandanten- oder mitarbeiterbezogenen Zuweisungen über
verschiedene Mandanten hinweg sieht oben in der Leiste einen
**Mandanten-Wechsler** und beim ersten Login (oder wenn eine Rollenänderung
den bisher aktiven Mandanten unerreichbar macht) eine Auswahlseite — RBAC96
nennt das «Session-Rollenaktivierung»: welche der mehreren
Mandantenmitgliedschaften für diese Browsersitzung aktiv ist.

### Rollen selbst verwalten

Rollen, ihre Berechtigungen und jede Zuweisung sind sowohl über die
Weboberfläche als auch über die Kommandozeile editierbar — nichts davon ist
ein fest verdrahteter Wertebereich:

- **`/tenants`** (`manage_tenants`, systemweit) — Mandanten auflisten und
  anlegen.
- **`/tenants/{id}/access`** (`manage_users`, pro Mandant) — einem Konto eine
  *mitarbeiterbezogene* Rolle für eine Person dieses Mandanten zuweisen oder
  entziehen. Ein Mandant-Admin erreicht diese Seite, kann hier aber keinen
  weiteren Mandant-Admin erzeugen — das ist Absicht, siehe unten.
- **`/roles`** (`manage_roles`, systemweit) — die einzige Seite, die einen
  weiteren `super_admin` oder `mandant_admin` erzeugen kann. Hier werden auch
  Rollen selbst definiert: eine Rolle anlegen, eine löschen (schlägt fehl,
  solange sie noch zugewiesen ist oder Berechtigungen hält), und die
  Berechtigungen einer Rolle umschalten.

```sh
./bin/wpcalc role add auditor -name Auditor -scope tenant --db DB
./bin/wpcalc role permissions auditor -add read --db DB
./bin/wpcalc role permissions auditor -add manage_tenants --db DB   # abgelehnt: min_scope=system
./bin/wpcalc role list --db DB
./bin/wpcalc permission list --db DB
```

Die Berechtigungen selbst (`read`, `print`, `write`, `manage_employees`,
`manage_users`, `manage_tenants`, `manage_roles`) sind der einzig feste Teil
davon: jede entspricht einer tatsächlichen Prüfung im Code, deshalb gibt es
kein `permission add` — eine über die Oberfläche erfundene Berechtigung würde
nichts bewirken. Der `scope` einer Rolle muss breit genug für jede ihrer
Berechtigungen sein (`manage_tenants` braucht `system`; `read` kann so eng wie
`employee` sein) — sowohl die Kommandozeile als auch ein Datenbank-Trigger
setzen das durch.

## Mitarbeitende

Verwaltung unter **Mitarbeitende**, bezogen auf den gerade aktiven Mandanten
(siehe [Mandanten und Rollen](#mandanten-und-rollen) oben) — diese Seite und
ihre Aktionen verlangen alle `manage_employees` in diesem Mandanten. Jede
Person hat einen Namen, ein Eintrittsdatum und ein optionales Austrittsdatum;
solange jemand angestellt ist, bleibt das Austrittsdatum leer.

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

## API

wpcalc dokumentiert seine eigene HTTP-Oberfläche — `GET /openapi.json`,
`/openapi.yaml` und `/openapi.html`, ohne Sitzung erforderlich. Das deckt
die Routen dieser Seite ab: die HTML-Anwendung, ihre Formular-Bodies, ihre
Weiterleitungen.

`/openapi.html` ist eine interaktive Swagger-UI-Seite — jede Operation,
jedes Schema und ein echter "Authorize"- und "Try it out"-Ablauf, nicht nur
eine statische Auflistung. Ihr JS/CSS
(`internal/specdoc/vendor/swagger-ui/`, Apache-2.0) ist in einer fixierten
Version in die Binärdatei eingebettet, nicht von einem CDN geladen — die
Seite rendert also auch ohne ausgehenden Netzwerkzugriff.

Eine zweite, separate Oberfläche, `/api/v1`, spiegelt die meisten
derselben Ressourcen (Mandanten, Mitarbeitende, das Stundenraster,
Tageskommentare, Auswertungen, Rollen, Berechtigungen, Rollenzuweisungen)
als zustandslose JSON-API — für Skripte, nicht für Browser. Sie
dokumentiert sich auf dieselbe Weise, unter `/api/v1/openapi.{json,yaml,html}`.

### Authentifizierung

`/api/v1` akzeptiert nie das `wpcalc_session`-Cookie, und die
HTML-Anwendung akzeptiert nie ein Bearer-Token — zwei getrennte
`Authenticator` per Konstruktion. Ein Token über die CLI ausstellen:

```sh
./bin/wpcalc token create alice -name ci    # gibt das Token einmalig aus — jetzt aufbewahren
./bin/wpcalc token list alice               # id, Name, erstellt, zuletzt verwendet, aktiv/widerrufen
./bin/wpcalc token revoke 3
```

Der Klartext wird genau einmal angezeigt, bei der Erstellung; gespeichert
wird nur sein SHA-256-Hash. Das Widerrufen eines Tokens berührt weder das
Passwort des Kontos noch eine Browser-Sitzung und wirkt bereits bei der
nächsten Anfrage — nichts wird zwischengespeichert.

```sh
curl -H "Authorization: Bearer wpat_..." http://localhost:8080/api/v1/tenants
```

### Unterschiede zur HTML-Anwendung

- **Jede Anfrage nennt ihren Mandanten explizit im Pfad**
  (`/api/v1/tenants/{tenantId}/...`) — ein Bearer-Token hat keine Sitzung,
  in der ein "aktiver Mandant" gehalten werden könnte, also gibt es hier
  keinen Mandanten-Umschalter und keine Auswahlseite.
- **Keine Routen für Login, Logout oder Mandanten-Wechsel** — für einen
  zustandslosen Client bedeutungslos. Eine Spracheinstellung *gibt* es
  (`PUT /api/v1/users/{username}/language`), nennt aber aus demselben
  Grund das Zielkonto explizit, statt "die aktuelle Sitzung" zu ändern.
- Jede Antwort ist JSON mit einem echten Statuscode — keine
  `303`-Weiterleitungen, keine `?err=`-Query-String-Token.

### Konten und Tokens

Alles, was `wpcalc user` und `wpcalc token` auf der CLI können, hat eine
Entsprechung unter `/api/v1` — mit einer notwendigen Ausnahme: das erste
Token eines Kontos kann nur von der CLI kommen (ein Endpunkt, der ein
Bearer-Token verlangt, kann nicht der Weg sein, das erste zu erhalten).

| CLI | API |
|---|---|
| `wpcalc user add` | `POST /api/v1/users` |
| `wpcalc user list` | `GET /api/v1/users` |
| `wpcalc user passwd <Name>` | `PUT /api/v1/users/{username}/password` |
| `wpcalc user lang <Name>` | `PUT /api/v1/users/{username}/language` |
| `wpcalc user roles <Name>` | `GET /api/v1/users/{username}/roles` |
| `wpcalc token create` | `POST /api/v1/tokens` (für sich selbst, sobald man eines hat) |
| `wpcalc token list` | `GET /api/v1/tokens` (nur eigene) |
| `wpcalc token revoke` | `DELETE /api/v1/tokens/{tokenId}` (nur eigene) |

Zwei Zugriffsregeln gelten durchgehend für die `/users/{username}/*`-Routen:
system-weites `manage_users` wirkt auf jedes Konto (der unbeschränkte,
operative Zugriff der CLI), und **ein Konto darf immer auf sich selbst
einwirken** — passend zum eigenen Self-Service-Sprachwechsel der
HTML-Anwendung. Ein Nicht-Admin, der ein fremdes Konto nennt, oder eines,
das nicht existiert, erhält in beiden Fällen dasselbe `403`; dies kann
niemals dazu benutzt werden, herauszufinden, welche Benutzernamen
existieren.

Die `/tokens*`-Routen sind noch enger begrenzt: ausschliesslich
Self-Service. Es gibt keinen Weg — auch nicht für einen
`manage_users`-Admin —, über die API die Tokens eines *anderen* Kontos
aufzulisten oder zu widerrufen; eine `tokenId`, die jemand anderem gehört,
liest sich als `404`, nicht als `403`, sodass sie auch nicht zum
Ausprobieren existierender Token-IDs dienen kann. Die CLI, mit direktem
Datenbankzugriff, ist nicht auf diese Weise begrenzt.

### Autorisierung

Dasselbe RBAC96-Modell wie im Rest der Anwendung (siehe
[Mandanten und Rollen](#mandanten-und-rollen) oben) — ein Bearer-Token löst
sich zur selben Identität auf wie ein Sitzungs-Cookie, also gelten dieselben
Rollen und Berechtigungen. Jede Operation dokumentiert, welche Berechtigung
sie in welchem Geltungsbereich verlangt; einige Beispiele:

| Operation | Berechtigung | Geltungsbereich |
|---|---|---|
| `GET /api/v1/tenants` | `manage_tenants` | system |
| `GET /api/v1/tenants/accessible` | keine — selbstbegrenzend | — |
| `GET /api/v1/tenants/{tenantId}/employees` | `manage_employees` | tenant |
| `GET /api/v1/tenants/{tenantId}/months/{ym}` | `read` | employee |
| `PUT .../months/{ym}/entries` | `write` | employee |
| `GET .../months/{ym}/report` | `print` | tenant oder employee |
| `POST /api/v1/roles` | `manage_roles` | system |

`GET /api/v1/tenants/accessible` ist die Ausnahme von "jede Operation
verlangt eine Berechtigung": Sie braucht keine bestimmte, weil das
Ergebnis bereits auf den Aufrufenden gefiltert ist — jeder über eine Rolle
in irgendeinem Geltungsbereich erreichbare Mandant. Anders als
`GET /api/v1/tenants` (nur system-weites `manage_tenants`, sonst `403`)
ist dies der Weg, wie ein Token mit Mandanten- oder
Mitarbeitenden-Geltungsbereich seine erreichbare `tenantId` entdeckt, ohne
sie bereits von aussen mitgeteilt bekommen zu haben.

Die vollständige, massgebliche Liste — jede Operation, ihre Anfrage- und
Antwortformen sowie ihre Berechtigung — steht in `/api/v1/openapi.html`,
erzeugt aus derselben Spezifikation (`internal/apiv1/openapi.yaml`), aus
der `oapi-codegen` den Server gebaut hat.

### Durchsetzung der Spezifikation

`openapi.yaml` ist nicht nur Dokumentation: Jede `/api/v1`-Anfrage wird
dagegen validiert, bevor irgendein Handler läuft — ein fehlendes
Pflichtfeld, ein Wert ausserhalb eines Enums, eine Zeichenkette, die nicht
zu ihrem deklarierten Muster passt, ein Pfadparameter falschen Typs. Auch
jede Antwort wird dagegen validiert, bevor sie den Client erreicht. Eine
Anfrage, die der Spezifikation widerspricht, erhält ein `400` mit einer
Meldung, was genau fehlgeschlagen ist — kein teilweiser oder
Best-Effort-Versuch, sie trotzdem zu verarbeiten; eine Antwort, die der
Spezifikation widersprechen würde, wird zu einem sauberen `500`, statt
einen Body auszuliefern, den die generierten Typen eines Clients gar nicht
erst parsen könnten.

## WordPress

Die Binärdatei enthält das Plugin und kann sich selbst installieren:

```sh
wpcalc plugin export /var/www/html/wp-content/plugins
```

Das schreibt `wpcalc/wpcalc.php` und `wpcalc/bin/wpcalc` — eine Kopie genau der
Binärdatei, die den Befehl ausgeführt hat, wodurch beide Teile
zusammenpassen. Anschliessend **wpcalc** in WordPress aktivieren und im
Admin-Menü **Arbeitszeiten** öffnen.

Ein bestehendes Plugin-Verzeichnis wird nicht ohne `--force` überschrieben.
`--php-only` schreibt nur die PHP-Datei, für den Fall, dass der Sidecar
systemweit installiert ist und der Pfad in den Einstellungen steht.

Die WordPress-E2E-Tests binden das exportierte Plugin ein, nicht das
Quellverzeichnis — ein kaputter Export lässt also die Tests scheitern, statt
unbemerkt zu bleiben.

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

**`no account can manage this database yet`** — entweder existiert kein Konto,
oder keines hält `super_admin`. `user add` und `user grant --system -role
super_admin` ausführen.

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
