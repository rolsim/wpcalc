# wpcalc — Benutzerhandbuch

Arbeitszeiten im Monatsraster erfassen.

*English: [../en/user.md](../en/user.md)*

---

## Anmelden

Öffnen Sie die Adresse, die Sie von Ihrem Administrator erhalten haben, und
geben Sie Benutzername und Passwort ein. Ohne Konto können Sie sich nicht
selbst registrieren — Ihr Administrator legt es an.

Unter WordPress gibt es keine separate Anmeldung: Öffnen Sie im Admin-Menü
**Arbeitszeiten**, Ihre WordPress-Sitzung wird verwendet.

## Das Raster lesen

Jede Zeile ist ein Tag des Monats, jede Spalte eine Person.

| | |
|---|---|
| **Schattierte Zeilen** | Samstag und Sonntag |
| **Hervorgehobene Zeile** | heute |
| **Graue Zelle mit `·`** | ausserhalb des Anstellungszeitraums — nicht bearbeitbar |
| **Leere Zelle** | für diesen Tag ist nichts erfasst |

Angezeigt werden nur Mitarbeitende, deren Anstellung in den dargestellten Monat
fällt. Wer früher ausgetreten ist, erscheint gar nicht — und nicht als Spalte
voller gesperrter Zellen.

## Stunden erfassen

Klicken Sie in eine Zelle und geben Sie die Stunden als Dezimalzahl ein —
**Industrieminuten**, eine Viertelstunde ist also `0.25`:

| Gearbeitet | Eingabe |
|---|---|
| 7 Stunden 45 Minuten | `7.75` oder `7,75` |
| 8 Stunden | `8` oder `8.00` |
| 30 Minuten | `0.5` oder `0,5` |
| 7 Stunden 3 Minuten | `7.05` |

Komma und Punkt werden beide akzeptiert — es spielt keine Rolle, ob Sie den
Zehnerblock oder die Tastatur verwenden.

**Was abgelehnt statt geraten wird:**

- `7:45` — dieses Feld nimmt Dezimalstunden, nicht Stunden und Minuten
- `7.755` — mehr als zwei Nachkommastellen
- mehr als `24.00` an einem Tag, oder eine negative Zahl
- Text wie `7h30`

Eine abgelehnte Eingabe wird gemeldet und nichts gespeichert. Es wird nie
stillschweigend gerundet oder umgedeutet: Eine falsche Zahl in einer
Zeiterfassung ist schlimmer als eine sichtbare Fehlermeldung.

**Zum Löschen** einer Zelle deren Inhalt entfernen und speichern. Der Eintrag
wird gelöscht — das ist nicht dasselbe wie eine erfasste Null.

## Speichern

Ihre Eingabe wird gespeichert, sobald Sie die Zelle verlassen, oder wenn Sie
**Enter** drücken. Ein kurzes grünes Aufleuchten bestätigt dies, ein roter
Rahmen bedeutet, dass die Eingabe abgelehnt wurde.

Ohne JavaScript hat jede Zelle eine eigene Schaltfläche **Speichern**, und die
Seite lädt bei jedem Speichern neu. Es funktioniert genau gleich, nur langsamer.

### Tastatur

| Taste | Wirkung |
|---|---|
| **Enter** | speichern und einen Tag nach unten |
| **↓** / **↑** | in derselben Spalte nach unten oder oben, gesperrte Zellen werden übersprungen |
| **Esc** | Eingabe verwerfen und gespeicherten Wert wiederherstellen |

Eine Spalte von oben nach unten durchzugehen ist der schnellste Weg, einen
Monat für eine Person zu erfassen.

## Bemerkungen

Die Spalte **Bemerkung** rechts nimmt eine Notiz pro Tag auf. Sie gehört zum
Tag, nicht zu einer einzelnen Person — geeignet etwa für einen
Betriebsausflug oder einen Feiertag.

## Totale

Drei Totale, alle vom Server berechnet:

- **unterste Zeile** — Total pro Mitarbeiter für den Monat
- **rechte Spalte** — Total pro Tag über alle Personen
- **Ecke** — Gesamttotal des Monats

Sie aktualisieren sich während der Eingabe. Dieselben Zahlen erscheinen in den
PDF-Auswertungen, Bildschirm und Ausdruck können sich also nicht widersprechen.

## Zwischen Monaten wechseln

Mit **← Vorheriger Monat** und **Nächster Monat →**, oder **Aktueller Monat**,
um zu heute zurückzuspringen. In beide Richtungen unbegrenzt.

Die Adresszeile zeigt den Monat (`…/m/2026-07`) — Sie können also ein Lesezeichen
setzen oder einer Kollegin einen Link auf genau diesen Monat schicken.

## Auswertungen

Unter **Auswertungen** entstehen PDFs:

- Monatsübersicht — die Totale aller Personen für einen Monat
- eine Person, ein Monat — Tag für Tag, inklusive der Tagesbemerkungen
- eine Person, ein Jahr — Monat für Monat

## Sprache

Die Oberfläche richtet sich nach der Spracheinstellung Ihres Browsers. Verfügbar
sind Deutsch (Schweiz) und Englisch; alles andere fällt auf Deutsch zurück. Es
gibt keine Sprachumschaltung in der Seite — stellen Sie die bevorzugte Sprache
im Browser um.

## Dieses Handbuch offline lesen

Wenn Ihnen der Befehl `wpcalc` zur Verfügung steht, zeigt `wpcalc manual` diese
Anleitung im Terminal an; `wpcalc manual --lang de-CH` erzwingt die Sprache.

## Wenn etwas nicht stimmt

- **Eine Zelle nimmt nichts an.** Sie ist grau mit einem `·`: Das Datum liegt
  ausserhalb des Anstellungszeitraums. Ihr Administrator kann Eintritt oder
  Austritt anpassen.
- **Jemand fehlt im Raster.** Die Anstellung fällt nicht in diesen Monat.
- **Ein Total sieht falsch aus.** Die Totale zählen nur den angezeigten Monat.
  Einträge im Nachbarmonat sind nicht enthalten.
