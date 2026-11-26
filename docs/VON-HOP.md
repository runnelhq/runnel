# Von Apache Hop zu Runnel

Runnel übernimmt das Modell von Apache Hop: Workflows und Pipelines, verbunden durch Links
([KONZEPT](KONZEPT.md)). Wer Hop kennt, findet sich schnell zurecht. Diese Seite zeigt, wie Begriffe und Bausteine aus
Hop in Runnel heißen und wo Runnel bewusst anders arbeitet. Alle Bausteine listet `runnel catalog`. Im Editor sucht die
Palette jedes Wort für sich in Typ, Name und Beschreibung; „set field value“ findet also `core.calculate`.

## Begriffe

| Apache Hop | Runnel |
|---|---|
| Projekt | Projektordner mit `project.yaml` |
| Environment | Environment (`environments/<name>.yaml`) |
| Workflow, Pipeline (`.hwf`, `.hpl`, XML) | Workflow, Pipeline (`*.workflow.yaml`, `*.pipeline.yaml`, YAML) |
| Action, Transform | Action, Transform; Typnamen `<namensraum>.<name>`, z. B. `core.filter` ([ADR 0013](adr/0013-typnamen-namensraeume.md)) |
| Hop | Link ([ADR 0009](adr/0009-begriff-link.md)) |
| Hop im Workflow: grün, rot, unbedingt | `when: success`, `failure`, `always`; dazu Ausdrücke als Bedingung (`when: "{{ … }}"`) |
| Fehlerbehandlung eines Transforms (Error handling) | Fehler-Link (`type: error`) mit den Feldern `_error` und `_errorField` |
| Info-Stream (z. B. bei Stream lookup) | Info-Link (`type: info`) |
| Change number of copies | `copies: n` |
| Rows distribution / Copy rows | `distribution: distribute` bzw. `copy` |
| Variablen `${NAME}` | `${NAME}` wie in Hop; dazu `${env:…}` und `${secret:…}`, Ausdrücke in `{{ … }}` ([ADR 0006](adr/0006-ausdruckssprache.md)) |
| Parameter von Workflow und Pipeline | `parameters` mit Typ, Standardwert und Pflicht |
| Metadaten: Relational Database Connection | Connection (`connections/<name>.yaml`, [ADR 0022](adr/0022-connections.md)) |
| Hop GUI | Editor im Browser (`runnel serve`) |
| `hop-run` | `runnel run` |
| Hop Server, Scheduler | Trigger (Zeitplan, Intervall, Ordner, HTTP) im Server bzw. Windows-Dienst (`runnel service`) |
| Preview | Datenvorschau im Editor |
| Metrics | Zähler je Link und Transform beim Lauf |
| Plugins (Java) | Plugins als eigener Prozess in beliebiger Sprache ([ADR 0019](adr/0019-plugin-protokoll.md)) |

Die Datentypen heißen in Runnel `string`, `int`, `float`, `decimal`, `date`, `datetime`, `bool`, `binary` und `json`.
Für Hops `Number` gibt es `float`, für `BigNumber` gibt es `decimal`, für `Timestamp` gibt es `datetime`
([ADR 0007](adr/0007-typsystem-pipelines.md)).

## Transforms

| Apache Hop | Runnel | Hinweis |
|---|---|---|
| Generate rows, Data grid | `core.generate-rows` | Feste Werte; Werte je Zeile danach mit `core.calculate` |
| CSV file input, Text file input | `csv.input` | Trennzeichen, Kodierung (auch UTF-16, Windows-1252), „Detect fields“ |
| Microsoft Excel input | `excel.input` | `.xlsx` |
| JSON input | `json.input` | auch NDJSON |
| Get data from XML | `xml.input` | |
| Get file names | `file.list` | |
| Table input | `db.table-input` | |
| Get rows from result | `core.from-result` | |
| Set field value | `core.calculate` | `{ name: ziel, value: "{{ record.quelle }}" }`; der Typ des Quellfelds bleibt |
| Set field value to a constant, Add constants | `core.constant` oder `core.calculate` mit festem Wert | `core.constant` wertet einmal je Lauf aus |
| If field value is null | `core.calculate` | `{{ record.ort ?? 'unbekannt' }}` |
| Calculator, Formula, String operations | `core.calculate` | Ausdrücke mit Funktionen wie `upper()`, `round()`, `concat()`, `replaceRegex()` |
| Concat fields | `core.calculate` | `{{ concat(' ', record.vorname, record.nachname) }}` |
| Replace in string | `core.calculate` | `replace()` bzw. `replaceRegex()` |
| Select values | `core.select-fields`, `core.convert` | Auswählen, Ordnen, Umbenennen, Entfernen; Typen und Formate mit `core.convert` |
| Filter rows | `core.filter` | Bedingung als Ausdruck; „Send false data to“ heißt `else: <id>`, ohne `else` fallen die übrigen Datensätze weg |
| Switch / case | `core.switch` | |
| Value mapper | `core.map-values` | |
| Split fields | `core.split-field` | mit `into` |
| Split field to rows | `core.split-field` | mit `rows` |
| Add sequence | `core.sequence` | auch je Gruppe (`groupBy`) |
| Sort rows | `core.sort` | |
| Unique rows, Unique rows (HashSet) | `core.dedupe` | ohne vorheriges Sortieren |
| Group by, Memory group by | `core.group-by` | |
| Merge join | `core.merge-join` | Eingänge nach denselben Schlüsseln sortiert |
| Stream lookup | `core.stream-lookup` | Nachschlagedaten über einen Info-Link |
| Database lookup | `db.lookup` | mit Cache |
| Dummy (do nothing) | `core.dummy` | |
| Write to log | `core.log` | |
| Table output | `db.table-output` | |
| Insert / update | `db.upsert` | |
| Text file output | `csv.output` | |
| Microsoft Excel writer | `excel.output` | |
| JSON output | `json.output` | |
| XML output | `xml.output` | |
| Copy rows to result | `core.to-result` | |
| REST client | `http.request` (Action) | Als Transform je Datensatz noch nicht vorhanden |

Noch ohne Entsprechung, teils im [Backlog](ROADMAP.md#backlog):

- Pipeline executor und Workflow executor je Datensatz (geplant, [ADR 0021](adr/0021-unter-pipelines.md))
- Row normaliser und denormaliser
- Cartesian product
- Database join
- Mapping (Unter-Pipelines als Baustein)

## Workflow-Actions

| Apache Hop | Runnel |
|---|---|
| Start | `core.start` |
| Success, Abort | `core.success`, `core.abort` |
| Dummy | `core.dummy` |
| Write to log | `core.log` |
| Wait for | `core.wait` |
| Simple evaluation, Evaluate … | `core.check` oder eine Bedingung am Link |
| Pipeline, Workflow | `core.run-pipeline`, `core.run-workflow` |
| Shell | `shell.run` (ohne Shell, Argumente als Liste), `shell.powershell` |
| SQL | `db.execute`; Ergebnisse lesen mit `db.query` |
| HTTP | `http.request` |
| Mail | `mail.send` |
| File exists, Wait for file | `file.exists`, `file.wait` |
| Copy files, Move files, Delete files, Create a folder | `file.copy`, `file.move`, `file.delete`, `file.mkdir` |
| Zip file, Unzip file | `archive.create`, `archive.extract` |
| Get a file with SFTP, Put a file with SFTP, FTP delete | `transfer.download`, `transfer.upload`, `transfer.delete`; dazu `transfer.list`, `transfer.move`, `transfer.mkdir`, `transfer.exists`, `transfer.wait` wie die `file.*`-Actions (Server, Anmeldung und Hostschlüssel in einer `sftp`-Connection; FTPS/FTP mit S57c) |

## Bewusst anders

- **Dateien:** YAML statt XML; lesbare Diffs in Git, Layout getrennt von der Logik am Ende der Datei.
- **Ausdrücke statt vieler Spezial-Transforms:** Was in Hop Calculator, Formula, Set field value und If field value
  is null erledigen, ist in Runnel ein Transform mit Ausdrücken (`core.calculate`).
- **Ein Programm:** `runnel.exe` ohne Java, mit Editor, Server, Triggern und Windows-Dienst.
- **Kein Big-Data-Backend:** Pipelines laufen im Prozess (Apache Beam, Spark und Flink entfallen), ausgelegt auf
  Datenmengen bis einige Millionen Zeilen pro Lauf.
- **Sicherheit:** Der Server lauscht nur lokal; Plugins laufen erst nach Freigabe. Secrets stehen nie in Dateien des
  Projekts.
