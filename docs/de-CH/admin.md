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

`wpcalc` (diese Binärdatei) ist der Server: direkter Datenbankzugriff, nur
das, was ein API-Client grundsätzlich nicht selbst leisten kann — den
Dienst betreiben, Migrationen anwenden, das erste Konto samt erstem Token
bootstrappen. Alles andere wird ferngesteuert, über `/api/v1`, mit der
separaten Binärdatei `wpcalcctl` — die vollständige Befehlszuordnung
steht unten unter [Konten und Tokens](#konten-und-tokens).

| Befehl | Zweck |
|---|---|
| `serve --addr :8080 \| --socket PFAD` | Server starten; genau ein Listener |
| `migrate [up\|down\|status]` | Migrationen anwenden, eine zurückrollen, Status zeigen |
| `user add` | ein leeres Konto anlegen (Bootstrap) |
| `user grant\|revoke <Name> [-system\|-tenant ID\|-employee ID] [-role ID]` | Rolle zuweisen oder entziehen (Bootstrap: ohne geht nichts) |
| `token create <Name>` | ein Access-/Refresh-Token-Paar ausstellen (Bootstrap: ein API-Client kann sein erstes Token auf keinem anderen Weg erhalten) |
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
./bin/wpcalc user add alice --db DB   # fragt nach dem Passwort; noch kein Zugriff — nur Bootstrap
```

Jedes weitere Konto legt man am einfachsten remote an, sobald ein Token für
ein Konto mit `manage_users` vorliegt:

```sh
wpcalcctl user add bob                          # fragt nach dem Passwort
wpcalcctl user passwd bob                       # widerruft auch bobs Sitzungen
wpcalcctl user list
```

Passwörter werden mit bcrypt gehasht und müssen mindestens 10 Zeichen lang
sein. Benutzernamen unterscheiden nicht zwischen Gross- und Kleinschreibung.
Sitzungen liegen serverseitig und gelten 12 Stunden — Abmelden oder eine
Passwortänderung entzieht den Zugriff sofort, statt ein Token bis zum Ablauf
gültig zu lassen; eine Passwortänderung widerruft zudem jedes Refresh-Token
dieses Kontos (bereits ausgestellte Access-Tokens laufen unabhängig davon
innert der Stunde von selbst ab).

`user add` legt nur die Zugangsdaten an — das Konto kann nichts, bis ihm eine
Rolle zugewiesen wird (siehe nächster Abschnitt). Es gibt keinen Moment, in
dem ein Konto mit einer stillschweigenden Rolle existiert, die niemand
verlangt hat.

> `--allow-weak-password` (nur Server-Binärdatei) hebt die Mindestlänge auf.
> Es existiert, damit eine lokale Entwicklungsdatenbank mit
> Wegwerf-Zugangsdaten wie `admin`/`admin` bestückt werden kann, und gibt bei
> jeder Verwendung eine Warnung aus. **Niemals auf einem erreichbaren System
> verwenden.**

### Sprache der Oberfläche

Jedes Konto speichert eine bevorzugte Sprache, oder einen leeren Wert für «der
Browser entscheidet». Benutzer ändern sie selbst über die Auswahl oben rechts;
Sie können sie beim Anlegen oder später setzen:

```sh
./bin/wpcalc user add alice -lang en --db DB   # Server-Binärdatei, nur Bootstrap
wpcalcctl user lang bob de-CH                 # de_CH wird ebenfalls akzeptiert
wpcalcctl user lang bob ""                    # wieder dem Browser folgen
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
| `mandant_admin` | tenant | Alles innerhalb eines Mandanten: Stunden **aller** Mitarbeitenden lesen, drucken und erfassen, deren Stammdaten verwalten und seinen Konten mitarbeiterbezogene Rollen erteilen. |
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

Das allererste Konto und seine `super_admin`-Zuweisung müssen auf dem
Server selbst geschehen — nichts anderes kann sie bootstrappen:

```sh
./bin/wpcalc user add alice --db DB
./bin/wpcalc user grant alice --system -role super_admin --db DB
./bin/wpcalc token create alice --db DB
```

Alles Weitere geschieht remote, über `wpcalcctl` (nach `wpcalcctl
login` mit dem gerade von `token create` ausgegebenen Paar):

```sh
wpcalcctl tenant add "Acme Corp"                           # -> Mandanten-Id, z. B. 2

wpcalcctl user add bob
wpcalcctl user grant bob -tenant 8f14e45f-ceea-4c2b-9b1a-1d7f3a6c50e2 -role mandant_admin

wpcalcctl user add carol
wpcalcctl user grant carol \
  -tenant 8f14e45f-ceea-4c2b-9b1a-1d7f3a6c50e2 \
  -employee 3c9a70b1-2d84-4f6e-8a15-b7c2e9d40f83 -role viewer

wpcalcctl user roles bob                                   # was bob erreichen kann
# Rolle ändern: erst entziehen, dann neu zuweisen
wpcalcctl user revoke carol \
  -tenant 8f14e45f-ceea-4c2b-9b1a-1d7f3a6c50e2 \
  -employee 3c9a70b1-2d84-4f6e-8a15-b7c2e9d40f83
```

Ein Konto hält höchstens eine Rolle pro Geltungsbereichs-**Instanz** —
`-employee <id>` zweimal mit unterschiedlichen Rollen wird abgelehnt; zuerst
entziehen. `-system`, `-tenant ID` allein, oder `-tenant ID -employee ID`
zusammen verwenden — eine mitarbeiterbezogene Zuweisung braucht weiterhin
ihren Mandanten, da `/api/v1` mitarbeiterbezogene Rollenzuweisungen unter
einem Mandanten verschachtelt (`wpcalc user grant` auf der
Server-Binärdatei behandelt die drei Flags stattdessen als streng
gegenseitig ausschliessend, da sie direkt mit der Datenbank spricht statt
mit einer verschachtelten Route).

Ein Konto mit mehreren mandanten- oder mitarbeiterbezogenen Zuweisungen über
verschiedene Mandanten hinweg sieht oben in der Leiste einen
**Mandanten-Wechsler** und beim ersten Login (oder wenn eine Rollenänderung
den bisher aktiven Mandanten unerreichbar macht) eine Auswahlseite — RBAC96
nennt das «Session-Rollenaktivierung»: welche der mehreren
Mandantenmitgliedschaften für diese Browsersitzung aktiv ist.

### Rollen selbst verwalten

Rollen, ihre Berechtigungen und jede Zuweisung sind über die Weboberfläche,
das entfernte `wpcalcctl` und direkt über `/api/v1` editierbar — nichts
davon ist ein fest verdrahteter Wertebereich:

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
wpcalcctl role add auditor -name Auditor -scope tenant
wpcalcctl role permissions auditor -add read
wpcalcctl role permissions auditor -add manage_tenants   # abgelehnt: min_scope=system
wpcalcctl role list
wpcalcctl permission list
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
`Authenticator` per Konstruktion. Ein Token-Paar über die CLI ausstellen:

```sh
./bin/wpcalc token create alice -name ci    # gibt ein Access-/Refresh-Token-Paar einmalig aus
./bin/wpcalc token list alice               # id, Name, erstellt, Ablauf, zuletzt verwendet, aktiv/abgelaufen/widerrufen
./bin/wpcalc token revoke 3
./bin/wpcalc token revoke-all alice         # jedes Access- und Refresh-Token des Kontos
```

Beide Klartexte werden genau einmal angezeigt, bei der Erstellung;
gespeichert wird nur ihr SHA-256-Hash. Das Widerrufen eines Tokens berührt
weder das Passwort des Kontos noch eine Browser-Sitzung und wirkt bereits
bei der nächsten Anfrage — nichts wird zwischengespeichert.

```sh
curl -H "Authorization: Bearer wpat_..." http://localhost:8080/api/v1/tenants
```

**Access-Tokens laufen nach einer Stunde ab** (`domain.AccessTokenTTL`) —
absichtlich kurz, damit ein geleaktes Token von selbst aufhört zu
funktionieren, lange bevor es jemand bemerken und von Hand widerrufen
würde. Das zugehörige **Refresh-Token** (`wprt_...`) ist 30 Tage gültig
(`domain.RefreshTokenTTL`) und tauscht sich gegen ein brandneues Paar ein
— ein neues Access-Token *und* ein neues, rotiertes Refresh-Token — ohne
erneut über `wpcalc token create` zu gehen:

```sh
./bin/wpcalc token refresh wprt_...          # über die CLI, meist zum Testen
curl -X POST -H "Content-Type: application/json" \
  -d '{"refreshToken":"wprt_..."}' http://localhost:8080/api/v1/tokens/refresh
```

`POST /api/v1/tokens/refresh` ist die einzige `/api/v1`-Operation, die
keinen Bearer-Header benötigt (das Refresh-Token im Body *ist* das
Credential) — mit Absicht, denn der ganze Sinn ist Erreichbarkeit genau
dann, wenn das Access-Token bereits abgelaufen ist. Refresh-Tokens sind
**einmal verwendbar**: der Austausch eines Tokens macht es sofort ungültig,
unabhängig davon, ob das neu rotierte behalten wird. Ein bereits
eingetauschtes, abgelaufenes, widerrufenes oder unbekanntes Refresh-Token
liest sich in allen Fällen identisch (`401`), sodass eine Antwort niemals
dazu benutzt werden kann, zu prüfen, ob ein bestimmtes Secret jemals gültig
war. Ein Passwortwechsel (`wpcalc user passwd` /
`PUT /api/v1/users/{username}/password`) widerruft zusätzlich zu
Browser-Sitzungen auch jedes Refresh-Token dieses Kontos — bereits
ausgestellte Access-Tokens laufen unabhängig davon von selbst innert der
Stunde ab.

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

`wpcalcctl` *ist* ein `/api/v1`-Client — jeder seiner Befehle bildet
direkt auf eine Operation ab:

| `wpcalcctl` | API |
|---|---|
| `user add` | `POST /api/v1/users` |
| `user list` | `GET /api/v1/users` |
| `user passwd <Name>` | `PUT /api/v1/users/{username}/password` |
| `user lang <Name>` | `PUT /api/v1/users/{username}/language` |
| `user roles <Name>` | `GET /api/v1/users/{username}/roles` |
| `token create` | `POST /api/v1/tokens` (für sich selbst, sobald man eines hat) |
| `token list` | `GET /api/v1/tokens` (nur eigene) |
| `token revoke` | `DELETE /api/v1/tokens/{tokenId}` (nur eigene) |
| `token revoke-all` | `DELETE /api/v1/tokens` (nur eigene) |

`POST /api/v1/tokens/refresh` hat überhaupt keinen `wpcalcctl`-Befehl —
`wpcalcctl` ruft es automatisch und transparent auf, sobald ein
Access-Token abgelaufen ist, und speichert das rotierte Paar, bevor es
zurückkehrt.

Das einzige ohne jeden API-Pfad: `wpcalc token create` auf der
**Server**-Binärdatei, direkt gegen die Datenbank, ohne Bearer-Credential.
Ein Endpunkt, der selbst ein Bearer-Token verlangt, kann nicht der Weg
sein, wie ein Konto sein erstes erhält — das ist diese Ausnahme, zusammen
mit `wpcalc user add`/`grant` für das Konto und seine erste Rolle.

Zwei Zugriffsregeln gelten durchgehend für die `/users/{username}/*`-Routen:
system-weites `manage_users` wirkt auf jedes Konto, und **ein Konto darf
immer auf sich selbst einwirken** — passend zum eigenen
Self-Service-Sprachwechsel der HTML-Anwendung. Ein Nicht-Admin, der ein
fremdes Konto nennt, oder eines, das nicht existiert, erhält in beiden
Fällen dasselbe `403`; dies kann niemals dazu benutzt werden,
herauszufinden, welche Benutzernamen existieren.

Die `/tokens*`-Routen sind noch enger begrenzt: ausschliesslich
Self-Service. Es gibt keinen Weg — auch nicht für einen
`manage_users`-Admin —, über die API die Tokens eines *anderen* Kontos
aufzulisten oder zu widerrufen (und damit auch nicht über `wpcalcctl`);
eine `tokenId`, die jemand anderem gehört, liest sich als `404`, nicht als
`403`, sodass sie auch nicht zum Ausprobieren existierender Token-IDs
dienen kann. Nur die Server-Binärdatei, mit direktem Datenbankzugriff, ist
nicht auf diese Weise begrenzt — und selbst sie kann nur Access-Tokens
widerrufen (`wpcalc token create` stellt aus; einen serverseitigen
Widerruf-Befehl gibt es mit Absicht nicht: dafür `wpcalcctl token
revoke`, oder im echten Notfall die Datenbank direkt bearbeiten).

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

### Was die Integration ist

WordPress führt die Logik von wpcalc nie selbst aus. Das Plugin ist ein Proxy:
Es startet die wpcalc-Binärdatei als **Sidecar**, spricht über einen
**Unix-Socket** mit ihr und übergibt eine **signierte Aussage darüber, wer der
aktuelle WordPress-Benutzer ist**. wpcalc antwortet mit demselben HTML, das
auch der eigenständige Server erzeugen würde, und das Plugin reicht es in der
Admin-Seite durch.

```
Browser ──► WordPress (PHP)  ──Unix-Socket──►  wpcalc-Sidecar  ──►  SQLite
                │                                     ▲
                │  X-Wpcalc-User / -Roles /           │
                └─ -Timestamp / -Scope / -Signature ──┘
                   (HMAC-SHA256, geteiltes Geheimnis)
```

Die Folge, die man vor allem anderen verstehen sollte: **Es gibt keine zweite
Anmeldung.** Eine WordPress-Administratorin, die **Arbeitszeiten** öffnet, ist
für wpcalc bereits authentifiziert — PHP hat ihre Berechtigung geprüft und
bürgt mit einer Signatur für sie, die wpcalc überprüfen kann.

### Das Plugin installieren

Die Binärdatei enthält das Plugin und kann sich selbst installieren:

```sh
wpcalc plugin export /var/www/html/wp-content/plugins
```

Das schreibt `wpcalc/wpcalc.php` und `wpcalc/bin/wpcalc` — eine Kopie genau der
Binärdatei, die den Befehl ausgeführt hat, wodurch beide Teile zusammenpassen.
Anschliessend **wpcalc** in WordPress aktivieren und im Admin-Menü
**Arbeitszeiten** öffnen.

- `--force` überschreibt ein bestehendes Plugin-Verzeichnis; ohne diese Option
  bricht der Export ab, statt es zu überschreiben.
- `--php-only` schreibt nur die PHP-Datei, für den Fall, dass der Sidecar
  systemweit installiert ist und sein Pfad in den Einstellungen steht.

Die WordPress-E2E-Tests binden das exportierte Plugin ein, nicht das
Quellverzeichnis — ein kaputter Export lässt also die Tests scheitern, statt
unbemerkt zu bleiben.

Ohne Shell-Zugang packen Sie das exportierte Verzeichnis `wpcalc/` in ein ZIP
und verwenden **Plugins → Installieren → Plugin hochladen**. PHP's eigener
ZIP-Entpacker stellt das Unix-Ausführungs-Bit nicht zuverlässig wieder her,
deshalb setzt das Plugin beim ersten Start selbst `chmod +x` auf die
Binärdatei — dieser Weg ist vorgesehen und abgedeckt.

### Laufzeitdateien

Alles, was dem Sidecar gehört, liegt in `wp-content/uploads/wpcalc/`:

| Datei | Was sie ist |
|---|---|
| `wpcalc.db` | die SQLite-Datenbank — **Ihre gesamten Daten** |
| `wpcalc.sock` | der Unix-Socket, über den das Plugin spricht |
| `wpcalc.pid` | die Prozess-ID des überwachten Diensts |
| `wpcalc.log` | das Log des Sidecars, sichtbar in den Einstellungen |

Das Plugin legt dort beim ersten Start eine `.htaccess` (`Require all denied`)
und eine `index.php` ab. **Ignoriert Ihr Server `.htaccess` — nginx tut das —,
sperren Sie dieses Verzeichnis selbst.** Sonst ist `wpcalc.db` eine
herunterladbare URL, und sie enthält jede Stunde und jeden Passwort-Hash.

Unter nginx:

```nginx
location ^~ /wp-content/uploads/wpcalc/ { deny all; return 404; }
```

### Wie angemeldete WordPress-Benutzer zugeordnet werden

Das ist der Teil, der entscheidet, was jede Person sieht — und er funktioniert
für die beiden Zugänge unterschiedlich.

#### Die zwei Türen

| | **wp-admin** (`Arbeitszeiten`) | **Shortcode `[wpcalc]`** |
|---|---|---|
| Wer hindurchkommt | wer `manage_options` hat | jede angemeldete WordPress-Person |
| Behaupteter Scope | `admin` | `self` |
| wpcalc-Konto nötig | **nein** | **ja** — muss verknüpft sein |
| Resultierender Zugriff | Vollzugriff auf alles | genau das, was die Rollen des verknüpften Kontos gewähren |
| Wenn nicht verknüpft | entfällt | fällt auf wpcalcs eigenes Anmeldeformular zurück |

**Die Admin-Tür ordnet überhaupt nichts zu.** PHP prüft vor dem Weiterleiten
`current_user_can('manage_options')`, und wpcalc behandelt das Ergebnis als
Identität mit Vollzugriff. Jede WordPress-Rolle mit dieser Berechtigung zählt —
eine eigene Rolle, der `manage_options` erteilt wurde, gilt genauso viel wie
`administrator`. Der Benutzername wird für Anzeige und Protokollierung
mitgeführt; die WordPress-Rollenliste ebenfalls, aber wpcalc zieht sie auf
diesem Weg **nicht** zur Autorisierung heran. Nichts zu konfigurieren, nichts
zu verknüpfen.

**Die Shortcode-Tür ordnet über den Benutzernamen zu.** Das ist die ganze
Regel:

> wpcalc sucht ein Konto, dessen `username` dem WordPress-`user_login`
> entspricht — verglichen **ohne Beachtung von Gross-/Kleinschreibung** und mit
> abgeschnittenen Leerzeichen. Welche RBAC-Rollen dieses wpcalc-Konto hält, ist
> genau das, was die Person sieht. Gibt es kein solches Konto — oder existiert
> es, hält aber gar keine Rolle —, gilt die Anfrage als nicht verknüpft.

Beachten Sie, was *nicht* verwendet wird: Die WordPress-**Rolle** der Person
(`subscriber`, `editor`, `author`, …) hat keinerlei Einfluss darauf, was sie in
wpcalc sieht. Die signierten Header führen sie mit, aber nur damit sie nicht
manipuliert werden kann; die Autorisierung stammt vollständig aus wpcalcs
eigenen Rollen und Zuweisungen (siehe
[Mandanten und Rollen](#mandanten-und-rollen)). Eine WordPress-Administratorin,
die *nicht* verknüpft ist, sieht auf der Shortcode-Seite das
Rückfall-Anmeldeformular — obwohl dieselbe Person über wp-admin Vollzugriff
hat.

Und es ist ausdrücklich `user_login` — der Name, den jemand zum Anmelden
eintippt — nicht der Anzeigename, nicht der Spitzname und nicht die
E-Mail-Adresse.

#### Jemanden verknüpfen, von Anfang bis Ende

Angenommen, eine Person meldet sich in WordPress als `alice` an und ist die
Mitarbeiterin «Alice Müller».

Zuerst brauchen Sie die ID der mitarbeitenden Person, eine UUID. Einen Befehl
`wpcalcctl employee` gibt es nicht — Mitarbeitende werden in der Oberfläche
verwaltet —, lesen Sie die ID also aus einer dieser Quellen:

- **Bildschirm «Mitarbeitende»**: Der *Bearbeiten*-Link enthält sie als
  `…&wpcalc_path=%2Femployees%2F<uuid>%2Fedit`.
- **Die API**, falls Sie ein Token haben:
  `curl -H "Authorization: Bearer $TOKEN" \`
  `  https://…/api/v1/tenants/<mandant-uuid>/employees`
- **Die Datenbank**, auf dem Server:
  `sqlite3 wpcalc.db "SELECT id, display_name FROM employees;"`

Dann das Konto verknüpfen:

```sh
# 1. wpcalc-Konto anlegen, dessen Benutzername dem WordPress-user_login entspricht.
wpcalcctl user add alice

# 2. Ihm eine Rolle auf genau diese mitarbeitende Person erteilen.
wpcalcctl user grant alice \
  -tenant 8f14e45f-ceea-4c2b-9b1a-1d7f3a6c50e2 \
  -employee 3c9a70b1-2d84-4f6e-8a15-b7c2e9d40f83 \
  -role viewer          # oder «editor», damit sie ihre Stunden selbst erfasst
```

(Eine mitarbeiterbezogene Zuweisung nennt weiterhin ihren Mandanten — siehe
[Einrichtung](#einrichtung).)

Setzen Sie nun `[wpcalc]` auf eine beliebige Seite. Alice meldet sich wie
gewohnt in WordPress an, öffnet diese Seite und sieht ihre eigene Spalte — ohne
wp-admin, ohne `manage_options`, ohne zweites Passwort.

`user add` fragt nach einem Passwort. Es wird ausschliesslich vom unten
beschriebenen Rückfall-Anmeldeformular verwendet; wer verknüpft über den
Shortcode kommt, tippt es nie. Setzen Sie etwas Langes und vergessen Sie es —
oder geben Sie es heraus, wenn sich die Person zusätzlich direkt bei wpcalc
anmelden können soll.

Was die Rolle des Kontos abdeckt, ist das, was der Shortcode zeigt — eine
einzelne Spalte bei einer mitarbeiterbezogenen Rolle, ein ganzer Mandant bei
einer mandantenweiten — dieselbe RBAC96-Abgrenzung, die auch das Admin-Raster
durchsetzt.

#### Was passiert, wenn jemand nicht verknüpft ist

Der Shortcode zeigt weder eine leere Seite noch einen Fehler. Er fällt auf
**wpcalcs eigenes Anmeldeformular** zurück, eingebettet in Ihre Seite: ein
zweites, getrenntes Kontosystem. Das ist ein Ausweichweg für Konten, die noch
niemand verknüpft hat, und für Installationen, in denen die wpcalc-Benutzerliste
bewusst nicht die WordPress-Benutzerliste ist. Es ist nicht der vorgesehene
Weg — sehen Sie es dort, wo Sie es nicht erwarten, stimmt mit grosser
Wahrscheinlichkeit der Benutzername nicht überein.

So prüfen Sie eine Zuordnung schnell:

```sh
wpcalcctl user roles alice   # was alice erreicht; leer heisst faktisch «nicht verknüpft»
```

Ein Konto ohne Rollen wird genau wie ein fehlendes Konto behandelt, denn das
Ergebnis wäre dasselbe: nichts Sichtbares.

#### Die Vertrauensgrenze

Der Sidecar glaubt der Aussage des Plugins nur, wenn **beides** zutrifft:

1. **Die Anfrage kam über den Unix-Socket.** Signierte Header, die über einen
   TCP-Listener eintreffen, werden rundweg abgelehnt, egal wie gültig die
   Signatur ist — sonst würde ein kurzzeitig offener Port genügen, damit
   irgendwer im Netz behauptet, Administrator zu sein. Das ist ein eigener,
   protokollierter Fehlerfall, keine Weiterleitung zur Anmeldung.
2. **Die Header tragen einen frischen HMAC-SHA256** über `user`, `roles`,
   `timestamp` und `scope`, mit einem Geheimnis, das Plugin und Sidecar teilen.
   Der Zeitstempel muss innerhalb von **5 Minuten** um die Uhr des Sidecars
   liegen, damit ein mitgeschnittener Headersatz nicht unbegrenzt
   wiederverwendet werden kann.

Der Scope wird mitsigniert. Deshalb lässt sich eine `self`-Anfrage nicht durch
Ändern eines Headers in eine `admin`-Anfrage verwandeln — die Signatur würde
nicht mehr stimmen.

Das geteilte Geheimnis wird bei der ersten Verwendung erzeugt (24 zufällige
Bytes, hexadezimal) und in der WordPress-Option `wpcalc_shared_secret`
gespeichert. Es wird nie mit einem Standardwert ausgeliefert, denn ein
ausgelieferter Standardwert wäre auf jeder Installation weltweit derselbe. In
den Einstellungen können Sie es neu erzeugen; der Sidecar startet dann mit dem
neuen neu.

### Was die Administration bereitstellen muss

| Voraussetzung | Wozu sie nötig ist | Wo sie sichtbar wird |
|---|---|---|
| PHP **8.1+** | Sprachniveau des Plugins | lässt sich darunter nicht aktivieren |
| WordPress **6.4+** | genutzte Admin-APIs | — |
| `proc_open()` **aktiviert** | Starten und Überwachen des Sidecars | Zeile in den Einstellungen |
| **curl** mit Unix-Socket-Unterstützung | jede Anfrage an den Sidecar (curl 7.40+, `CURLOPT_UNIX_SOCKET_PATH`) | Zeile in den Einstellungen |
| Beschreibbares `wp-content/uploads/` | Datenbank, Socket, PID und Log liegen dort | Zeile in den Einstellungen |
| Ein **dauerhaftes** Dateisystem | die Datenbank muss Deployments und Neustarts überleben | siehe Grundtypen unten |
| Erlaubnis für einen **langlebigen Prozess** | der Sidecar überlebt die Anfrage, die ihn startete | siehe Grundtypen unten |
| Setzbares **Ausführungs-Bit** | die Binärdatei muss startbar sein | meist automatisch erledigt |

`manage_options` ist die Berechtigung, die die Admin-Seiten absichert;
schreibende Anfragen tragen zusätzlich eine WordPress-Nonce.

**wpcalc → Einstellungen** zeigt den Live-Zustand des meisten davon: ob der
Dienst antwortet, ob die Binärdatei vorhanden und ausführbar ist, ob
`proc_open` und `curl` verfügbar sind, die Laufzeitpfade sowie die letzten
Zeilen des Logs. Dort lässt sich der Dienst auch neu starten und das geteilte
Geheimnis neu erzeugen. Ist `proc_open` deaktiviert, sagt die Seite das
deutlich, statt stillschweigend zu scheitern.

### Vorabprüfung: fragen Sie Ihren Hoster, raten Sie nicht

Hosting-Richtlinien unterscheiden sich je nach Anbieter, je nach Tarif und über
die Zeit, und veröffentlichte Dokumentation ist oft veraltet. Verlassen Sie
sich deshalb auf keine Tabelle — auch nicht auf die unten stehende —, sondern
**fragen Sie den Host, auf dem Sie tatsächlich sind.**

Speichern Sie dies als `wpcalc-preflight.php`:

```php
<?php
// Ausführen mit:  wp eval-file wpcalc-preflight.php
// oder nach wp-content/mu-plugins/ legen und eine Admin-Seite einmal laden.
$out = [];
$out['php'] = PHP_VERSION . (version_compare(PHP_VERSION, '8.1', '>=') ? ' OK' : ' ZU ALT');

$disabled = array_map('trim', explode(',', (string) ini_get('disable_functions')));
$out['proc_open'] = function_exists('proc_open') ? 'OK'
    : 'FEHLT' . (in_array('proc_open', $disabled, true) ? ' (in disable_functions)' : '');

$cv = function_exists('curl_version') ? curl_version() : null;
$out['curl'] = $cv ? $cv['version'] : 'FEHLT';
$out['curl_unix_socket'] = ($cv && defined('CURLOPT_UNIX_SOCKET_PATH')
    && version_compare($cv['version'], '7.40', '>=')) ? 'OK' : 'FEHLT';

$up  = wp_upload_dir();
$dir = trailingslashit($up['basedir']) . 'wpcalc';
if (!is_dir($dir)) { @wp_mkdir_p($dir); }
$out['uploads_writable'] = is_writable($dir) ? "OK ($dir)" : "NICHT BESCHREIBBAR ($dir)";

// Kann dieser Host eine Datei ausführen, die wir selbst abgelegt haben?
$probe = $dir . '/probe.sh';
@file_put_contents($probe, "#!/bin/sh\necho alive\n");
@chmod($probe, 0755);
$out['exec_bit'] = is_executable($probe) ? 'OK' : 'AUSFUEHRUNGS-BIT NICHT SETZBAR';
if (function_exists('proc_open')) {
    $p = @proc_open([$probe], [1 => ['pipe','w'], 2 => ['pipe','w']], $pipes, $dir);
    if (is_resource($p)) {
        $said = trim((string) stream_get_contents($pipes[1]));
        foreach ($pipes as $pipe) { @fclose($pipe); }
        proc_close($p);
        $out['can_spawn'] = ($said === 'alive') ? 'OK' : "UNERWARTETE AUSGABE: $said";
    } else {
        $out['can_spawn'] = 'proc_open VERWEIGERT';
    }
}
@unlink($probe);

$out['open_basedir'] = ini_get('open_basedir') ?: '(keines)';
foreach ($out as $k => $v) { echo str_pad($k, 20) . ": $v\n"; }
```

So lesen Sie das Ergebnis:

- **Alles `OK`** → das mitgelieferte Sidecar-Modell funktioniert. Normal
  installieren.
- **`proc_open FEHLT` oder `can_spawn` verweigert** → das Plugin kann den
  Sidecar nicht *starten*, aber einen von jemand anderem gestarteten weiterhin
  *nutzen*. Siehe [Den Sidecar selbst betreiben](#den-sidecar-selbst-betreiben).
- **`curl_unix_socket FEHLT`** → dafür gibt es im Plugin keinen Ausweg; das
  Transportmittel selbst fehlt. Betreiben Sie wpcalc stattdessen eigenständig
  und verlinken Sie darauf.
- **`uploads_writable NICHT BESCHREIBBAR`** oder ein einschränkendes
  `open_basedir` → die Datenbank hat unter der aktuellen Konfiguration keinen
  Platz.
- **`exec_bit AUSFUEHRUNGS-BIT NICHT SETZBAR`** → typischerweise eine
  `noexec`-Einhängung auf `wp-content/uploads`. Legen Sie die Binärdatei an
  einen ausführbaren Ort und tragen Sie den Pfad in den Einstellungen ein.

### Hosting-Grundtypen

Die unten genannten Produkte sind **Beispiele für eine Bauform**, keine
Kompatibilitätstabelle — Tarife unterscheiden sich, Voreinstellungen ändern
sich, und jedes davon lässt sich abweichend konfigurieren. Die verlässliche
Antwort liefert die Vorabprüfung.

**1. Eigener Server oder VPS** — Hetzner, DigitalOcean, Linode, Vultr, AWS
EC2/Lightsail, eine Maschine im Schrank. Sie haben root, `proc_open` ist aktiv,
das Dateisystem ist dauerhaft. Alles funktioniert unverändert, und Sie haben
zusätzlich die Möglichkeit, den Sidecar unter systemd zu betreiben — die
robustere Anordnung für alles Langlebige.

**2. Klassisches Shared Hosting (cPanel-/Plesk-Linie)** — SiteGround,
Hostinger, IONOS, Bluehost, HostGator, DreamHost, Namecheap, A2. Das
Dateisystem ist dauerhaft und `wp-content/uploads` beschreibbar, die Datenseite
ist also unproblematisch. Die Variable ist PHPs `disable_functions`:
`proc_open` und `exec` sind in solchen Tarifen häufig standardmässig
deaktiviert — und ebenso häufig wieder aktivierbar, entweder selbst über den
**MultiPHP INI Editor** in cPanel oder eine `.user.ini`, oder auf Anfrage beim
Support. Diese Bitte ist angemessen und wird für eine benannte Funktion auf
einem bestimmten Konto meist gewährt. Wird sie abgelehnt, haben Sie auch kein
systemd; dann bleibt praktisch Grundtyp 4.

**3. Managed-WordPress-Plattformen** — WP Engine, Kinsta, Pressable, Flywheel,
WordPress.com (ab Business, wo Plugins überhaupt erlaubt sind). Diese betreiben
WordPress in einem kontrollierten Container mit bewusst gehärtetem PHP, und das
ganze Produktversprechen lautet, dass Sie auf deren Infrastruktur keine
beliebigen Prozesse ausführen. Rechnen Sie damit, dass langlebige Sidecars
nicht verfügbar sind und dass das Dateisystem ausserhalb von `uploads/` beim
Deployment zurückgesetzt wird. Der Support wird das Starten von Prozessen in
aller Regel nicht freischalten, denn genau dessen Abwesenheit wird verkauft.
**Kämpfen Sie nicht dagegen an** — betreiben Sie wpcalc eigenständig anderswo
(Grundtyp 1 ist dafür günstig) und behandeln Sie WordPress als blossen Ort, an
dem Leute auf einen Link klicken.

**4. Container und PaaS** — Platform.sh, Docker-/Kubernetes-Images, DDEV und
Lando lokal. Das Dateisystem ist ausserhalb deklarierter Mounts meist flüchtig
oder nur lesbar, und der PHP-Container ist nicht der Ort für einen zweiten
Daemon. Naheliegend ist, wpcalc als **eigenen Container/Dienst** zu betreiben
und den Socket über ein Volume zu teilen — oder im eigenständigen Modus über
das Netz mit ihm zu sprechen. Was Sie in `uploads/` ablegen, muss auf einem
deklarierten dauerhaften Mount liegen, sonst ist Ihre Datenbank beim nächsten
Deployment weg.

### Den Sidecar selbst betreiben

Kann PHP den Sidecar nicht starten, aber weiterhin einen Socket erreichen, dann
betreiben Sie wpcalc als Systemdienst und lassen das Plugin ihn einfach finden.
Das funktioniert, weil das Plugin zuerst prüft, ob der Socket **bereits
antwortet**, bevor es überhaupt etwas zu starten versucht — ein gesunder Socket
überspringt den `proc_open`-Weg vollständig.

Drei Dinge müssen exakt zusammenpassen:

1. **Der Socket-Pfad.** Das Plugin schaut immer auf
   `<uploads>/wpcalc/wpcalc.sock`, und das ist nicht konfigurierbar — richten
   Sie Ihren Dienst genau auf diesen Pfad aus.
2. **Das geteilte Geheimnis.** Der Sidecar braucht `WPCALC_SECRET` mit dem Wert
   aus der WordPress-Option `wpcalc_shared_secret`, sonst scheitert jede
   weitergeleitete Anfrage an der Prüfung.
3. **Der Datenbankpfad**, damit beide Hälften dieselbe Datei als Daten ansehen.

```sh
# Das von WordPress erzeugte Geheimnis:
wp option get wpcalc_shared_secret
```

```ini
# /etc/systemd/system/wpcalc.service
[Unit]
Description=wpcalc sidecar
After=network.target

[Service]
User=www-data
Group=www-data
Environment=WPCALC_SECRET=<Wert aus wp option get>
Environment=WPCALC_BASE_PATH=https://example.com/wp-admin/admin.php?page=wpcalc
Environment=WPCALC_LINK_PARAM=wpcalc_path
ExecStart=/usr/local/bin/wpcalc serve \
  --socket /var/www/html/wp-content/uploads/wpcalc/wpcalc.sock \
  --db     /var/www/html/wp-content/uploads/wpcalc/wpcalc.db
Restart=on-failure
UMask=0007

[Install]
WantedBy=multi-user.target
```

```sh
sudo systemctl enable --now wpcalc
```

Der Socket muss für den PHP-Prozess les- und schreibbar sein (oben `www-data` —
passen Sie das an den Benutzer Ihres PHP-FPM-Pools an). `WPCALC_BASE_PATH` und
`WPCALC_LINK_PARAM` sorgen dafür, dass Links und Weiterleitungen innerhalb der
Anwendung wieder auf die WordPress-Admin-Seite zeigen statt auf blosse Pfade;
der Frontend-Shortcode sendet seinen Basispfad pro Anfrage mit und hängt nicht
davon ab.

Läuft der Dienst, setzen Sie **wpcalc → Einstellungen → Pfad zur Binärdatei**
auf dieselbe Binärdatei, damit die dort angezeigte Version zu dem passt, was
tatsächlich ausliefert.

### Einen noch nicht dokumentierten Hoster angehen

1. **Führen Sie die Vorabprüfung aus.** Sie beantwortet auf der Maschine, auf
   die es ankommt, die fünf Fragen, auf die sich jeder Host reduzieren lässt:
   PHP-Version, kann er Prozesse starten, kann er über einen Unix-Socket
   sprechen, kann er Dateien schreiben und behalten, kann er ein
   Ausführungs-Bit setzen.
2. **Ordnen Sie sich anhand der Ausgabe einer von drei Bauformen zu**:
   mitgelieferter Sidecar (alles OK), fremdverwalteter Sidecar (`proc_open`
   blockiert, Socket in Ordnung) oder vollständig eigenständig
   (Socket-Transport fehlt, oder das Dateisystem ist nicht dauerhaft).
3. **Brauchen Sie `proc_open`, fragen Sie präzise.** Ein Ticket mit «Bitte
   entfernen Sie `proc_open` für dieses Konto aus `disable_functions`» bekommt
   ein Ja oder ein Nein. «Mein Plugin geht nicht» bekommt keines. Fragen Sie im
   selben Zug nach `open_basedir` und nach `noexec` auf dem
   uploads-Einhängepunkt.
4. **Prüfen Sie die Dauerhaftigkeit, bevor Sie ihr trauen.** Deployen oder
   starten Sie einmal neu, mit einem Datensatz in der Datenbank, und
   kontrollieren Sie, ob er noch da ist. Auf jeder Plattform, die Container neu
   baut, ist das der Fehler, der echte Daten kostet — und er bleibt still, bis
   er es nicht mehr ist.
5. **Bestätigen Sie, dass das Verzeichnis nicht über das Web lesbar ist.** Rufen
   Sie `https://ihre-seite/wp-content/uploads/wpcalc/wpcalc.db` in einem
   abgemeldeten Browser auf. Erwartet wird 403 oder 404. Alles andere heisst,
   dass `.htaccess` ignoriert wird und Sie im Webserver sperren müssen.
6. **Im Zweifel trennen Sie die Installation.** wpcalc eigenständig auf einem
   kleinen Server mit einem Reverse-Proxy davor ist ein vollwertiger,
   unterstützter Betriebsweg — das WordPress-Plugin ist eine Bequemlichkeit,
   keine Voraussetzung. Nichts in `internal/httpx` weiss, in welchem Modus es
   läuft.

### Fehlersuche zu WordPress

| Symptom | Wahrscheinliche Ursache |
|---|---|
| «proc_open() is disabled on this host» | Grundtyp 2 oder 3; Support fragen oder den Sidecar selbst betreiben |
| 503 auf der Admin-Seite | Sidecar startet nicht — `wpcalc.log` in den Einstellungen lesen |
| 502 «service is not responding» | Socket existiert, aber niemand lauscht; veraltete `wpcalc.sock` nach einem Absturz |
| Shortcode zeigt ein Anmeldeformular | zum WordPress-`user_login` gibt es kein passendes wpcalc-Konto, oder es hält keine Rollen |
| Shortcode zeigt verknüpften Personen nichts Brauchbares | Konto ist verknüpft, aber seine Rolle gewährt nichts — `wpcalcctl user roles <name>` prüfen |
| Anfrage mit signierten Headern abgelehnt, laut protokolliert | Header erreichten einen TCP-Listener; der Sidecar muss auf einem Unix-Socket liegen |
| Lief, hörte nach einem Deployment auf | nicht dauerhaftes Dateisystem — Grundtyp 3 oder 4 |
| Datenverlust / leeres Raster nach Hoster-Wechsel | `wpcalc.db` wurde nicht mitkopiert; sie ist die gesamte Datenbank |

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
