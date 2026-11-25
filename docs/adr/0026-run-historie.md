# ADR 0026: Run-Historie in einer eingebetteten SQLite-Datenbank

- **Status:** Angenommen
- **Datum:** 2026-11-24
- **Bezug:** KONZEPT Kap. 9 (Persistenz), [ADR 0016 – Live-Events per SSE](0016-live-events-sse.md),
  [ADR 0018 – HTTP-Trigger](0018-http-trigger.md) (`history`), [ADR 0022 – Connections und Secrets](0022-connections-secrets.md)
  (`modernc.org/sqlite`, Maskierung); Roadmap S26 (Teilung S26a–S26e), S29 (Resume), S43 (Aufbewahrung, Monitoring),
  S45 (Samples)

## Kontext

Läufe stehen heute nur im Speicher des Prozesses (seit E3): die letzten 100, dazu 100 von HTTP-Triggern, je Lauf
höchstens 5.000 Logzeilen. Nach einem Neustart des Servers oder Dienstes sind sie weg, und Läufe von `runnel run`
erscheinen nirgends. Für den Dauerbetrieb fehlt damit die Antwort auf „Ist der Import heute Nacht gelaufen, und warum
nicht?“. Resume (S29), Aufbewahrung (S43), Samples (S45) und das Nachholen verpasster Cron-Läufe bauen auf einer
dauerhaften Historie auf.

Das Konzept (Kap. 9) sah eine SQLite-Datei `data\runnel.db` vor, die Logs aber als Dateien je Lauf. Offen waren
Speicherort, was gespeichert wird, wann geschrieben wird, gleichzeitiger Zugriff von Server und CLI, Läufe nach einem
Absturz und die Aufbewahrung.

Betrachtete Varianten:

| Variante | Bewertung |
|---|---|
| **SQLite eingebettet** (`modernc.org/sqlite`, reines Go) | Filter, Seiten und Aufbewahrung per SQL; mehrere Prozesse dürfen schreiben (WAL); absturzsicher; viele Läufe kein Problem. Der Treiber ist seit S20a in der exe, also keine neue Abhängigkeit. Gewählt. |
| Key-Value-Datei (`bbolt`) | Neue Abhängigkeit, Filter selbst gebaut, nur ein Prozess darf die Datei öffnen (CLI und Dienst schließen sich aus). |
| Dateien je Lauf (JSON und Log) | Ohne Abhängigkeit und mit jedem Editor lesbar, aber viele kleine Dateien sind unter Windows langsam (Virenscanner), Filter lesen viele Dateien, absturzsicheres Schreiben und Sperren selbst gebaut. Bei HTTP-Triggern mit tausenden Läufen am Tag nicht tragfähig. |
| Tagesdateien, nur angehängt (JSON-Lines) | Lesen heißt nachspielen; Details eines Laufs teuer; gleichzeitiges Anhängen braucht Sperren. |

## Entscheidung

### 1. Eine SQLite-Datei je Projekt im Datenordner

- Gespeichert wird in **einer SQLite-Datei je Projekt**, `runs.db`, mit `modernc.org/sqlite` (kein CGO), im WAL-Modus
  mit einer Wartezeit bei Sperren (`busy_timeout`, 5 s).
- Ort: **Datenordner**, nicht der Projektordner. Laufdaten gehören nicht in Git, und SQLite ist auf Netzlaufwerken
  (SMB) nicht sicher.
  - Dienst: `%ProgramData%\runnel\history\<projekt>-<hash>\runs.db`
  - Benutzer (`runnel serve`, `runnel run`): `%LOCALAPPDATA%\runnel\history\<projekt>-<hash>\runs.db`; unter Linux
    `$XDG_STATE_HOME/runnel/history/…` (sonst `~/.local/state/runnel/history/…`)
  - `<projekt>` ist der Name des Projektordners, `<hash>` die ersten 8 Zeichen von SHA-256 über den absoluten Pfad
    des Projektordners. Lesbar und eindeutig.
  - `--history-dir` (bei `run`, `serve`, `service install`) setzt den Ordner ausdrücklich, z. B. für einen
    portablen Betrieb neben der exe.
- Dienst und Benutzer haben getrennte Historien, wie sie auch getrennte Konten haben. Wer Läufe von `runnel run` im
  Editor des Dienstes sehen will, setzt bei beiden denselben `--history-dir`.
- Ein verschobenes Projekt beginnt mit einer neuen Historie. Die alte Datei bleibt, bis sie gelöscht wird.

### 2. Was gespeichert wird

| Tabelle | Inhalt |
|---|---|
| `runs` | was `GET /runs` heute liefert: ID (ULID), Pfad, Art, Name, Trigger, Client, Environment, Parameter, Status, Start, Ende, Dauer, Fehler, Outputs, Zähler je Link; dazu Hash der ausgeführten Datei, Eltern-Lauf und Lebenszeichen (Abschnitt 4) |
| `nodes` | je Action bzw. Transform: Status, Start, Ende, Dauer, Fehler, Anzahl Ausführungen, Zähler der Datensätze (wie `actions` in `GET /runs/{id}`) |
| `log` | Logzeilen eines Laufs: Zeit, Level, Action, Text; wie heute höchstens 5.000 je Lauf, eine Zeile höchstens 8 KB |

- **Keine Datensätze.** Samples sind S45.
- **Secrets** stehen nicht in der Historie: Logzeilen, Fehler und Outputs sind beim Schreiben bereits maskiert
  (ADR 0022), Parameter enthalten keine Secrets.
- **Grenzen:** Outputs und Parameter je Lauf höchstens 1 MB als JSON; was darüber hinausgeht, wird durch einen Hinweis
  ersetzt und im Log gemeldet.
- **Schema:** Version in `PRAGMA user_version`, Migrationen nur vorwärts, eingebettet in die exe, beim Öffnen
  ausgeführt. Eine Datei mit neuerer Version, als die exe kennt, wird nicht geändert; die Historie ist dann aus, mit
  Meldung („the history was written by a newer runnel“).

### 3. Wann geschrieben wird

- Geschrieben wird beim **Start** des Laufs, am **Ende jeder Action bzw. jedes Transforms** und am **Ende des Laufs**.
  Logzeilen werden gesammelt und etwa jede Sekunde oder je 200 Zeilen in einer Transaktion geschrieben.
- Geschrieben wird in einer eigenen Goroutine mit begrenzter Warteschlange. **Die Historie bremst und stoppt keinen
  Lauf:** Ist die Datei gesperrt, voll oder defekt, läuft der Lauf weiter; der Fehler steht einmal im Server-Log bzw.
  auf stderr, und der Lauf ist in der Historie unvollständig.
- Die **Live-Anzeige** bleibt unverändert im Speicher und per Server-Sent Events (ADR 0016). Die Historie ist die
  Quelle für beendete und ältere Läufe.

### 4. Server und CLI gleichzeitig; Läufe nach einem Absturz

- Server, Dienst und `runnel run` dürfen dieselbe Datei gleichzeitig öffnen (WAL).
- Ein laufender Lauf schreibt alle 30 s ein **Lebenszeichen**. Ein Lauf mit Status `running`, dessen Lebenszeichen älter
  als 2 Minuten ist, gilt als abgebrochen: Beim Öffnen und beim Lesen wird er auf `failed` gesetzt, mit dem Fehler
  „runnel stopped while the run was running“. Ein noch laufender Lauf eines anderen Prozesses bleibt `running`.
- Fortsetzen ab der fehlgeschlagenen Action ist S29 und kann dafür einen eigenen Status einführen.

### 5. API und CLI

- `GET /runs` und `GET /runs/{id}` behalten ihre Antworten. Sie lesen laufende Läufe aus dem Speicher und alle
  anderen aus der Historie. Neu sind Filter und Seiten (`status`, `path`, `trigger`, `before`, `limit`).
- `runnel run` schreibt in die Historie, mit Trigger `cli`. Welche beendeten Läufe bleiben, regelt
  `settings.history.cli` im Projekt (Abschnitt 6); `--history all|errors|none` setzt es für einen Aufruf. Vorrang: Schalter,
  dann Projekt, dann Standard `all`.
- `runnel runs` listet Läufe (Filter wie die API) und zeigt einen Lauf mit Knoten und Log (`runnel runs <id>`), auch
  als JSON (`--json`). Damit braucht niemand ein SQLite-Werkzeug.

### 6. Aufbewahrung

- In `project.yaml` unter `settings.history`:
  - `keepDays`: Standard 30
  - `maxRuns`: Standard 10.000 je Projekt
  - `cli`: welche Läufe von `runnel run` bleiben, `all | errors | none` wie `history:` an den Triggern; Standard `all`
  - Ältere bzw. überzählige Läufe werden mit Knoten und Log gelöscht.
- Bereinigt wird beim Öffnen, danach im Server stündlich und nach einem `runnel run`, in kleinen Schritten, damit kein
  Lauf wartet. `auto_vacuum = INCREMENTAL` gibt den Platz frei.
- `history: all | errors | none` gibt es heute nur bei HTTP-Triggern (ADR 0018). Es gilt künftig für **alle
  Trigger-Arten** mit derselben Bedeutung: welche beendeten Läufe in der Historie bleiben. Standard `all`, bei
  HTTP-Triggern mit `response: run` weiter `errors`.

### 7. Aussage „keine Datenbank“

Runnel braucht weiterhin keinen Datenbank-Server, nichts zu installieren und nichts zu verwalten. Die Beschreibung im
Corporate Design (`design/README.md`, Social Card) sagt künftig „kein Datenbank-Server“ bzw. „no database server“
statt „keine Datenbank“. „Keine externe Datenbank“ in README und Konzept bleibt richtig.

### 8. Umsetzung in Schritten

| Schritt | Inhalt |
|---|---|
| S26a | Paket `internal/history` (Öffnen, Migrationen, Schreiben, Lesen), Manager schreibt mit, `GET /runs` und `GET /runs/{id}` lesen aus der Historie, Lebenszeichen und abgebrochene Läufe, `--history-dir` |
| S26b | `runnel run` schreibt mit (`settings.history.cli`, `--history`), `runnel runs`, Filter und Seiten in `GET /runs` |
| S26c | Aufbewahrung (`settings.history`), `history:` für alle Trigger-Arten |
| S26d | Mockup: Liste der Läufe im Editor (Filter, alter Lauf auf dem Canvas mit Status und Log) |
| S26e | Umsetzung nach freigegebenem Mockup, geteilt in S26e1 (Liste) und S26e2 (Lauf auf dem Canvas) |

## Sicherheit

- Die Historie kann Geschäftsdaten enthalten (Outputs, Parameter, Fehlertexte mit Werten). Sie liegt deshalb nicht im
  Projektordner und wird nicht über die Datei-API ausgeliefert.
- **Rechte:** Der Ordner des Dienstes wird geschützt wie der Ordner seines TLS-Zertifikats (`prepareTLSDir`, S40a):
  nur das Dienstkonto, Administratoren und SYSTEM; der geerbte Lesezugriff aller Benutzer auf `%ProgramData%` wird
  entfernt. Der Ordner eines
  Benutzers liegt in dessen Profil.
- Secrets sind maskiert, bevor sie die Historie erreichen (Abschnitt 2). Ein Test sichert das ab.
- SQL nur mit Parametern, keine Werte im SQL-Text. Die Grenzen aus Abschnitt 2 verhindern, dass ein Lauf die Datei
  unbegrenzt füllt.
- Neue Netzwerkgrenzen entstehen nicht: Die API bleibt wie bisher lokal bzw. hinter den bestehenden Regeln.

## Konsequenzen

**Positiv**
- Läufe überstehen Neustarts; Läufe von `runnel run` und der Aufgabenplanung werden sichtbar.
- Grundlage für Resume (S29), Monitoring und Aufbewahrung (S43), Samples (S45) und verpasste Cron-Läufe.
- Keine neue Abhängigkeit, kein Server, nichts zu installieren.

**Negativ / zu beachten**
- Die Historie ist eine Binärdatei; Ansehen geht über Editor, API oder `runnel runs`.
- Dienst und Benutzer haben getrennte Historien, solange nicht derselbe `--history-dir` gesetzt ist.
- Ein verschobenes Projekt beginnt mit einer neuen Historie.
- Schreiben kostet etwas Zeit je Action. Der Nachweis kommt mit einem Benchmark in S26a (HTTP-Trigger mit und ohne
  Historie).

## Nachtrag 2026-11-24 (S26a)

- `--history-dir` nennt den Ordner **für alle Projekte**; die Historie eines Projekts liegt darin im Unterordner
  `<projekt>-<hash>`, wie im Standardordner. Gilt für `runnel serve`, `runnel service install` (wird an den Dienst
  weitergegeben) und später `runnel run`.
- Rechte: Der Standardordner des Dienstes wird wie der TLS-Ordner geschützt. Ein eigener Ordner (`--history-dir`) kann
  mit `runnel serve` eines Benutzers geteilt sein; dort bekommt das Dienstkonto nur Schreibrechte, die übrigen Rechte
  bleiben. Der Ordner eines Benutzers wird mit Rechten nur für ihn angelegt.
- Schreiben: Was in der Warteschlange liegt (höchstens 16.384 Einträge), wird in einer Transaktion geschrieben. Endet
  ein Lauf im selben Schub, wird er nur einmal mit seinem Endstand geschrieben. Ist die Warteschlange voll, entfallen
  Einträge (einmal gemeldet), der Lauf geht weiter. Schreibfehler stehen höchstens einmal je Minute im Server-Log, mit
  der Zahl der Fehler seitdem.
- Kann die Historie beim Start nicht geöffnet werden (z. B. kein Zugriff, Datei einer neueren Version), läuft der Server
  ohne sie (Läufe nur im Speicher) und sagt das im Server-Log.

## Nachtrag 2026-11-24 (S26b)

- `runnel runs [projektordner] [lauf-id]`: ohne Lauf-ID die Liste (Tabelle oder `--json` wie die API), mit Lauf-ID ein
  Lauf mit Knoten und Log. Eine Lauf-ID allein (26 Zeichen, kein Ordner dieses Namens) gilt für das Projekt im aktuellen
  Ordner. Gibt es noch keine Historie, legt `runnel runs` nichts an.
- `runnel run` startet seinen Lauf über denselben Manager wie der Server, mit Trigger `cli`; ohne Projekt gibt es keine
  Historie. Läufe aus dem Editor haben weiterhin keinen Trigger (`runnel runs` zeigt `editor`).
- `limit` der API und von `runnel runs`: höchstens 1.000.

## Nachtrag 2026-11-24 (S26c)

- `keepDays` und `maxRuns` unter `settings.history`: `0` oder fehlend bedeutet den Standard (30 bzw. 10.000), erlaubt
  sind höchstens 3.650 Tage bzw. 1.000.000 Läufe. Es zählen die Startzeit (`keepDays`) und die Reihenfolge der Lauf-IDs
  (`maxRuns`); laufende Läufe werden nie entfernt.
- Der Server liest die Grenzen bei jeder Bereinigung neu aus `project.yaml` (geänderte Werte gelten ohne Neustart) und
  meldet entfernte Läufe im Server-Log; Fehler der Bereinigung sind Warnungen, kein Lauf wartet darauf.
- `history:` an Cron-, Intervall- und Datei-Triggern hat dieselbe Bedeutung wie an HTTP-Triggern; im Trigger-Formular
  steht es unter „More“.

## Nachtrag 2026-11-24 (S26e1)

- Filter `trigger=editor` (API und `runnel runs --trigger editor`) wählt die Läufe ohne Trigger, also die aus dem Editor
  und mit `POST /runs`. Trigger heißen `<typ>:<name>` oder `cli`, ein Trigger `editor` kann es nicht geben.
- `GET /projects/current` nennt die Aufbewahrung (`history.keepDays`, `history.maxRuns`, mit Standardwerten), damit die
  Liste der Läufe sie zeigen kann.
- Der Editor lädt die Liste neu, wenn ein Lauf startet oder endet (Ereignisse `run.started`, `run.finished`, `reset` des
  Stroms `/events`, höchstens alle 0,5 s), nicht auf jedes Ereignis eines Laufs.

## Nachtrag 2026-11-24 (S26e2)

- Ein Lauf aus der Historie erscheint auf dem Canvas der **aktuellen** Datei; die Historie speichert keine Kopie der
  Datei. Weichen die Actions ab (IDs), nennt der Editor sie und lässt die Zähler an Links weg, weil deren Reihenfolge
  nicht mehr stimmen muss. Geänderte Links bei gleichen Actions erkennt er nicht.
- Ein noch laufender Lauf aus der Liste wird wie ein im Editor gestarteter über `/events?run=<id>&since=0` verfolgt.

## Nachtrag 2026-11-25 (Fix): Monotone Lauf-IDs

Die Historie sortiert Läufe nach ID (ULID). Bisher war der Zufallsteil einer ULID unabhängig: Zwei Läufe, die in
derselben Millisekunde starteten, konnten in `runnel runs` und im Editor vertauscht erscheinen (gefunden durch einen
Test, der unter Windows gelegentlich scheiterte). `runid.New` erzeugt jetzt monotone ULIDs nach der Spezifikation:
In derselben Millisekunde wie die letzte ID (oder wenn die Uhr zurückspringt) ist die neue ID die letzte plus eins.
Die Reihenfolge gilt je Prozess; Läufe verschiedener Prozesse (z. B. `runnel run` neben dem Server) sind innerhalb
einer Millisekunde weiterhin nicht geordnet.
