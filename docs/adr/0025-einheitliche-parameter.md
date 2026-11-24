# ADR 0025: Einheitliche Parameter der Bausteine

- **Status:** Angenommen
- **Datum:** 2026-11-24
- **Bezug:** [ADR 0013 – Typnamen und Aufbau der Bausteine](0013-typnamen-namensraeume.md),
  [ADR 0014 – Descriptor und Schema](0014-action-descriptor-schema.md),
  [ADR 0007 – Typsystem in Pipelines](0007-typsystem-pipelines.md); `docs/dev/ACTIONS.md`, `docs/dev/TRANSFORMS.md`

## Kontext

Runnel hat 27 Actions und 33 Transforms. Sie sind Schritt für Schritt entstanden, und einige Parameter heißen für
dieselbe Sache unterschiedlich oder verhalten sich unter demselben Namen unterschiedlich. Eine Prüfung aller Schemas
(Katalog) ergab:

| Sache | Heute |
|---|---|
| Ziel existiert schon | `ifExists` bei 8 Bausteinen; `overwrite: true/false` bei `file.copy` und `file.move` |
| Quelle fehlt | `ifMissing: fail\|empty` bei `file.read`; `mustExist: true/false` bei `file.delete` |
| Grenze für Datensätze | `maxRows` bei `core.to-result` und `http.response` (Transform); `maxRecords` bei `core.stream-lookup` |
| Grenze für Größen | `maxSize` als Text (`"10MB"`); `maxOutput` bei `shell.run`/`shell.powershell` als Zahl in Bytes |
| Lesart einer Datei | `format` bei `json.input`, `json.output` und den Archiven; `as: text\|lines\|json` bei `file.read`/`file.write` |
| Neuer Name eines Feldes | `rename` bei `core.select-fields`; `as` bei `db.lookup` und `core.stream-lookup`; `target` bei `core.map-values` |
| Spalte ≠ Feldname | `from` nur bei `json.input`/`xml.input`, `as` (Ort beim Schreiben) nur bei `xml.output` |
| Zahlen- und Datumsformat | `fields[].format` ist überall ein Runnel-Muster (`#.##0,00`, `dd.MM.yyyy`), bei `excel.output` aber ein Excel-Code (`#,##0.00`, `dd.mm.yyyy`) |
| Pfad in der Ausgabe | `path` bei `file.write`, `file.mkdir` und `archive.create`; `file` bei `http.request` mit `saveTo` |

Für Anwender wirkt das uneinheitlich. Wer einen Baustein kennt, kann den nächsten nicht ohne Nachschlagen
konfigurieren, und kopierte Werte (z. B. ein Format aus `csv.output` nach `excel.output`) verhalten sich anders.
Namensregeln brauchen nach AGENTS.md eine ADR; die Umbenennungen brechen außerdem bestehende Dateien.

## Entscheidung

### 1. Regeln für Namen

Diese Regeln gelten für alle bestehenden und neuen Actions und Transforms. Neue Bausteine folgen ihnen; Abweichungen
brauchen eine Begründung im PR.

| Regel | Name | Werte |
|---|---|---|
| Ziel existiert schon | `ifExists` | aus `replace`, `append`, `skip`, `fail` und eigenen wie `sheet`, in dieser Reihenfolge |
| Quelle fehlt | `ifMissing` | aus `fail`, `skip`, `empty` |
| Kein passender Datensatz (Lookup) | `notFound` | `keep`, `drop`, `error` (unverändert) |
| Höchstzahl Datensätze in Pipelines | `maxRecords` | Text mit Zahl, `${…}` erlaubt |
| Höchstzahl Zeilen einer Datenbank | `maxRows` | Text mit Zahl (nur `db.*` und Actions, die Zeilen lesen) |
| Höchstgröße | `max…` mit Größe | Text wie `500KB`, `10MB`, `1GB` (z. B. `maxSize`, `maxOutput`) |
| Höchstdauer | `timeout` | Dauer wie `30s`, `5m` (unverändert) |
| Lesart einer Datei | `format` | z. B. `auto`, `json`, `ndjson`, `text`, `lines` |
| Neuer Name eines Feldes | `as` | Name; leer: der Name bleibt |
| Herkunft beim Lesen | `fields[].from` | Spalte, Schlüssel oder Pfad in der Datei; leer: wie `name` |
| Ort beim Schreiben | `fields[].as` | Spalte, Schlüssel oder Pfad in der Datei; leer: wie `name` |
| Zahlen- und Datumsformat eines Wertes | `fields[].format` | immer ein Runnel-Muster (`#.##0,00`, `dd.MM.yyyy`) |
| Pfad in der Ausgabe einer Action | `path` | bei mehreren Dateien eine Liste von Pfaden |

Weiterhin gilt, was schon einheitlich ist: `path` für eine Datei, `source`/`target` für Kopieren, Verschieben und
Archive, `folder` für Ordner, `connection`, `sql`, `params`, `header`, `message`, `level`, `fields[].type` mit derselben
Werteliste.

### 2. Standardwert von `ifExists`

- **Eine Zieldatei**, die der Baustein vollständig neu schreibt (`file.write`, `*.output`, `archive.create`,
  `http.request` mit `saveTo`): Standard `replace`, geschrieben über eine temporäre Datei, sodass bei einem Fehler die
  alte Datei bleibt.
- **Viele Dateien in einen Ordner** (`archive.extract`, `file.copy`, `file.move`): Standard `fail`, geprüft bevor
  etwas geschrieben wird, damit vorhandene Dateien nicht unbemerkt ersetzt werden.

### 3. Formate in `excel.output`

`fields[].format` nimmt in `excel.output` dasselbe Runnel-Muster wie die anderen Ausgaben (`#.##0,00`, `dd.MM.yyyy`,
`dd.MM.yyyy HH:mm`). Runnel übersetzt es in den Zahlenformat-Code von Excel. Der Wert bleibt in der Zelle eine Zahl
bzw. ein Datum; Excel zeigt ihn mit dem Format an (Tausender- und Dezimaltrenner nach der Sprache des Benutzers). Wer
einen eigenen Excel-Code braucht (Farben, Bedingungen, Einheiten wie `0.0 "kg"`), setzt ihn in `fields[].excelFormat`.

### 4. Übergang

Runnel ist vor Version 1.0; die alten Namen werden **nicht** weiter angenommen, damit es für jede Sache nur einen Namen
gibt. Damit bestehende Dateien leicht anzupassen sind:

- Eine Definition kann umbenannte Felder nennen. `runnel validate`, der Editor und der Lauf melden ein altes Feld mit
  Position und neuem Namen, z. B.
  `error: action "copy" (file.copy): unknown field "overwrite" (renamed to ifExists: replace or fail, S70a)`.
- Wo sich mit dem Namen auch die Werte ändern (`overwrite: true` → `ifExists: replace`, `mustExist: true` →
  `ifMissing: fail`), nennt der Hinweis die neue Schreibweise.
- Die Hinweise bleiben bis Version 1.0. Die Beispiele unter `examples/` werden im selben PR angepasst.

### 5. Umsetzung in drei Schritten

| Schritt | Inhalt |
|---|---|
| S70a | Datei-Actions: `file.copy`/`file.move` `ifExists`, `file.delete` `ifMissing`, `file.read`/`file.write` `format`, Ausgabe `path` bei `http.request`; Hinweise für umbenannte Felder (Mechanismus aus Abschnitt 4); Reihenfolge der Werte und Wortlaut der Beschreibungen von `ifExists` |
| S70b | Grenzen und Felder: `maxRecords` bei `core.to-result` und `http.response`, `maxOutput` als Größe, `as` bei `core.select-fields` und `core.map-values`, `fields[].from` bei `csv.input`/`excel.input`, `fields[].as` bei `csv.output`, `excel.output`, `json.output`; `utf-16` für `csv.output` (siehe Nachtrag) |
| S70c | `excel.output`: Runnel-Muster in `format`, `excelFormat` für eigene Excel-Codes |

`docs/dev/ACTIONS.md` und `docs/dev/TRANSFORMS.md` verweisen auf die Regeln dieser ADR.

## Konsequenzen

**Positiv**
- Ein Name je Sache: Wer `ifExists`, `format` oder `from`/`as` einmal kennt, findet es bei jedem Baustein wieder.
- Kopierte Formate verhalten sich in allen Ausgaben gleich.
- Neue Bausteine haben eine Vorgabe; Reviews können sich auf die Tabelle berufen.

**Negativ / zu beachten**
- Bestehende Workflows und Pipelines mit den alten Namen laufen nach S70a/S70b nicht mehr, bis sie angepasst sind;
  `runnel validate` zeigt jede Stelle mit dem neuen Namen. Für Projekte vor 1.0 ist das vertretbar.
- `excel.output` braucht eine Übersetzung der Runnel-Muster in Excel-Codes; nicht jedes Muster hat eine genaue
  Entsprechung (z. B. Wochentage in anderer Sprache). Was sich nicht übersetzen lässt, meldet die Prüfung.
- Der Mechanismus für umbenannte Felder ist zusätzlicher Code in `action`/`transform`, der bis 1.0 gepflegt wird.

## Nachtrag 2026-11-24 (S70b)

- Die Kodierung UTF-16 heißt in `csv.output` wie in `csv.input` `utf-16` (Little Endian, mit BOM), nicht `utf-16le`:
  ein Name für dieselbe Sache in Lesen und Schreiben.
- `fields[].from` setzt eine Kopfzeile voraus; ohne Kopfzeile (`header: false`) werden Spalten über die Position
  gelesen, und `from` ist ein Fehler.
- `fields[].as` bei `json.output` ist ein Pfad mit Punkten wie `from` bei `json.input`; Felder mit gleichem Anfang
  teilen sich ein Objekt. Zwei Felder auf demselben Schlüssel oder ein Schlüssel, der zugleich Wert und Objekt wäre,
  sind ein Fehler.

## Nachtrag 2026-11-24 (S70c)

- Übersetzung in `excel.output`: Ein Zahlenmuster wird `0` bzw. `#,##0` (mit Tausendertrennung) mit so vielen
  Nachkommastellen wie Nullen; welche Zeichen das Muster als Trenner nennt, bestimmt nur die Bedeutung, Excel zeigt die
  Trenner des Benutzers. Datumsbuchstaben werden zu den Codes von Excel (`MM` → `mm`, `HH` → `hh`, `EEEE` → `dddd`,
  `a` → `AM/PM`, `ss.SSS` → `ss.000`), Text in Anführungszeichen zeichenweise mit `\`.
- Was Excel anders zeigen würde, ist ein Fehler mit Hinweis auf `excelFormat`: Zeitzonen (`XXX`, `Z`), Minuten ohne
  Stunden davor oder Sekunden danach (Excel liest `mm` sonst als Monat) und umgekehrt, `hh` ohne `a` (Excel zeigt
  24 Stunden), `HH` mit `a`, Millisekunden außer als `ss.SSS`.
- `format` und `excelFormat` zusammen in einem Feld sind ein Fehler. Excel-Codes, die zugleich gültige Runnel-Muster
  sind (`#,##0.00`, `0.00`), ergeben denselben Code; andere (`0.0%`, `dd.mm.yyyy`) meldet die Prüfung mit dem Hinweis.
