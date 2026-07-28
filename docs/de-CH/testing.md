# wpcalc testen

Wie Sie die Anwendung ausprobieren — in beiden Betriebsarten, mit oder ohne
Quellcode. Rechnen Sie mit einer Viertelstunde.

Es gibt zwei Ausgangslagen. Wenn Sie nur die Binärdatei erhalten haben, folgen
Sie dem ersten Abschnitt; wenn Sie das Repository haben, dem zweiten. Alles
danach gilt für beide.

---

## Nur mit der Binärdatei

Sie haben eine einzige Datei: `wpcalc`. Mehr braucht es nicht. Sie ist statisch
gebaut, hat also keine Abhängigkeiten, und legt alles Weitere selbst an.

Legen Sie die Datei in ein leeres Verzeichnis, machen Sie sie ausführbar und
prüfen Sie kurz, dass sie läuft:

```sh
chmod +x wpcalc
./wpcalc version
```

`version` nennt Commit und Datum des Builds. Geben Sie diese Zeile bei einer
Rückmeldung mit an — steht dort `dirty`, wurde die Datei aus einem
Arbeitsstand mit ungespeicherten Änderungen gebaut und lässt sich nicht allein
aus dem Commit rekonstruieren.

Die Handbücher stecken mit drin, Sie müssen also nichts nachschlagen:

```sh
./wpcalc manual          # Anleitung für Anwender
./wpcalc manual admin    # Anleitung für den Betrieb
```

Ist [glow](https://github.com/charmbracelet/glow) installiert, wird gesetzt
dargestellt, sonst als reiner Text. Beides ist lesbar.

Dann drei Befehle, und die Anwendung läuft:

```sh
./wpcalc sample-employees --db test.db --month 2026-07
./wpcalc user add tester -role admin -lang de-CH --db test.db
./wpcalc serve --addr 127.0.0.1:8090 --db test.db
```

Der erste legt vier Beispiel-Anstellungen an, damit das Raster Spalten hat —
Stunden erfasst er bewusst keine. Der zweite fragt zweimal nach einem Passwort;
es muss mindestens zehn Zeichen haben. Der dritte startet den Server.

## Mit dem Quellcode

```sh
make build
./bin/wpcalc sample-employees --db test.db --month 2026-07
./bin/wpcalc user add tester -role admin -lang de-CH --db test.db
./bin/wpcalc serve --addr 127.0.0.1:8090 --db test.db
```

Inhaltlich dasselbe; nur der Pfad zur Binärdatei unterscheidet sich.

## Beides

Der Server startet absichtlich nicht, solange kein Konto existiert — die
Reihenfolge oben ist also nötig und keine Empfehlung. Ist der Port belegt, sagt
er das im Klartext; 8080 ist erfahrungsgemäss oft schon vergeben.

Dann im Browser auf <http://127.0.0.1:8090> anmelden.

---

## Worauf zu achten ist

**Totale.** Tippen Sie eine Zahl in eine Zelle und klicken Sie daneben. Die
Totale unten und rechts müssen sich sofort mitbewegen, ohne dass die Seite neu
lädt.

**Eingabeformat.** `7.75` und `7,75` sind dasselbe, nämlich sieben Stunden und
45 Minuten. `7:45` muss dagegen als Fehler zurückkommen und darf nicht als `7`
gespeichert werden — lieber eine sichtbare Meldung als eine stillschweigend
veränderte Zahl.

**Gesperrte Zellen.** Die grauen Zellen mit einem Punkt liegen ausserhalb des
Anstellungszeitraums und nehmen nichts an. «Muster C» tritt am 15. ein, dort
sieht man den Unterschied gut; «Muster D» ist im Vormonat ausgetreten und
taucht deshalb gar nicht auf. Wochenenden sind schattiert.

**Monatswechsel.** Blättern Sie mit den Pfeilen über eine Jahresgrenze, etwa
von Dezember auf Januar.

**Auswertungen.** Laden Sie die drei PDFs herunter und schauen Sie hinein — die
Zahlen darin müssen mit dem Bildschirm übereinstimmen.

Zwei Dinge, die leicht übersehen werden. Die Sprachauswahl oben rechts bleibt
beim Konto gespeichert, gilt also auch, wenn Sie sich in einem anderen Browser
anmelden. Und schalten Sie einmal JavaScript ab und laden Sie neu: Dann hat
jede Zelle eine eigene Schaltfläche und die Seite lädt bei jedem Speichern neu,
aber es funktioniert alles. Genau so soll es sein.

Wenn Sie von vorn anfangen wollen: Server beenden, `test.db` löschen, die
Befehle von oben nochmals. Es gibt keine weiteren Spuren im System.

---

## WordPress

Auch dafür reicht die eine Datei: Das Plugin steckt mit in der Binärdatei und
schreibt sich selbst heraus. Nötig ist nur eine WordPress-Installation, auf die
Sie Dateizugriff haben.

```sh
./wpcalc plugin export /pfad/zu/wp-content/plugins
```

Das legt `wpcalc/wpcalc.php` und `wpcalc/bin/wpcalc` an — Letzteres ist eine
Kopie genau der Binärdatei, mit der Sie den Befehl ausgeführt haben. Dadurch
passen Plugin und Dienst immer zusammen. Ein bereits vorhandenes Verzeichnis
wird nicht ohne `--force` überschrieben.

Danach das Plugin in WordPress aktivieren und im Admin-Menü **Arbeitszeiten**
öffnen. Prüfenswert ist vor allem, dass es sich wie ein normales Plugin
anfühlt: kein zweiter Login, die WordPress-Sitzung genügt.

Unter **wpcalc → Einstellungen** steht, ob der Dienst läuft, ob die Binärdatei
gefunden wurde und was zuletzt im Log stand — das ist die erste Anlaufstelle,
wenn etwas klemmt. Die Sprache folgt hier dem Locale im WordPress-Profil,
deshalb fehlt die eigene Sprachauswahl absichtlich: sonst gäbe es zwei Stellen,
die sich widersprechen könnten.

Ein Härtetest, der sich lohnt: `bin/wpcalc` im Plugin-Verzeichnis umbenennen
und die Seite neu laden. Es darf keine weisse Seite erscheinen, sondern ein
Hinweis, der den fehlenden Pfad nennt.

---

## Automatisiert

Nur mit dem Quellcode. Drei Befehle:

```sh
make check     # Bau, Linter und rund 130 Tests — Sekunden
make e2e       # echter Browser in einem Container — rund 10 Sekunden
make e2e-wp    # WordPress, MariaDB und das Plugin in Containern — rund 45 Sekunden
```

`make e2e-wp` bindet dabei das *exportierte* Plugin ein, nicht das
Quellverzeichnis — geprüft wird also genau das, was ein Tester erhält. Nach dem
Lauf bleiben weder Container noch Volumes stehen.

---

## Zwei bekannte Punkte

Die Rollen `admin` und `user` werden zwar gespeichert, aber noch nirgends
ausgewertet: Jedes angemeldete Konto darf zurzeit alles. Das ist bekannt und
kein Fund.

Und die Beispieldaten enthalten absichtlich keine Arbeitsstunden, nur
Anstellungen. Erfundene Einträge liessen sich später nicht mehr von echten
unterscheiden, und das will man in einer Zeiterfassung nicht. Ein leeres Raster
ist der richtige Ausgangszustand.
