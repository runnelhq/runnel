# Changelog

Alle für Anwender sichtbaren Änderungen an Runnel. Aufbau und Regeln nach
[ADR 0029](docs/adr/0029-stabilitaet-versionen.md): Versionen nach SemVer, Zusagen ab `v1.0.0`; je Version die
Abschnitte **Neu**, **Behoben**, **Abgekündigt** (mit der Version, in der das Abgekündigte entfällt) und **Bruch** (nur
in Major-Versionen). Neue Einträge kommen mit dem PR unter „Unveröffentlicht“; beim Release wird daraus die Version.

## Unveröffentlicht

Seit `v0.1.0-alpha.7` (2026-11-21). Vor 1.0 ändern sich Formate und Namen noch ohne Abkündigung.

### Neu

- **Plugins** (ADR 0019): `kind: Plugin` mit Freigabe je Projekt (`runnel plugins trust`), Plugin-Protokoll,
  Actions, Transforms und Connection-Typen aus Plugins; Go-SDK `runnelhq/sdk-go`; Plugin-Verwaltung im Editor.
- **Run-Historie** (ADR 0026): SQLite je Projekt, `runnel runs`, Filter in `GET /api/v1/runs`, Aufbewahrung
  (`settings.history`), Liste der Läufe und Lauf aus der Historie auf dem Canvas im Editor.
- **Fehler-Workflow** (ADR 0027): `settings.onError` in Projekt, Workflow und Pipeline, `runnel run --no-on-error`.
- **JavaScript** (ADR 0028): Action und Transform `core.script` mit Code-Feldern im Editor.
- **Lokaler Secret-Store** (ADR 0022, S39d1): Quelle `local` je Projekt außerhalb des Projektordners, verschlüsselt mit
  AES-256-GCM, Schlüssel aus `RUNNEL_MASTER_KEY` (32 Byte in Base64); `runnel secret set/list/delete` mit
  `--from-file` und `--base64`; `secretStore: local` ist wieder setzbar.
- **Connections und Secrets** (ADR 0022): Secret-Quellen `env` und `inline`, Connection-Typen `sqlite`, `sqlserver`,
  `postgres`, `http`, `smtp` (mit OAuth2), `sftp`, `ftps`, `ftp`; Test von Connections und Formular im Editor.
- **Neue Bausteine:** `db.*` (SQL ausführen, Tabellen lesen und schreiben, Upsert, Lookup, `db.query`), `http.request`
  mit Paginierung und Download, `core.check`, `shell.run` und `shell.powershell` (ADR 0023), `mail.send`,
  `transfer.*`, `archive.create`/`archive.extract`, `file.read`/`file.write`, `file.list` als Transform,
  `json.input`/`json.output`, `excel.input`/`excel.output` (ADR 0024), `xml.input`/`xml.output`, `core.calculate`,
  `core.dedupe`, `core.sequence`, `core.map-values`, `core.split-field`, `core.stream-lookup`, `core.merge-join`;
  Funktionen `concat()`, `basename()`, `dirname()`, `ext()`, `replaceRegex()`; in der Referenz der Ausdrücke neu
  `int()`, `float()`, `string()`, `toJSON()`, `fromJSON()`, `abs()`, `ceil()`, `floor()`, `min()`, `max()`,
  `hasPrefix()`, `hasSuffix()`, `first()`, `last()`, `keys()` und die Listen mit Bedingung (`all`, `any`, `count`,
  `find`, `groupBy`, `sortBy`, `reduce` …).
- **Einheitliche Parameter** der Bausteine (ADR 0025): `ifExists`, `ifMissing`, `format`, `maxRecords` u. a.
- **Schemas der Dateiarten:** `runnel schema <art>` und `GET /api/v1/schemas/{kind}`.
- **Referenz je Version:** `runnel catalog --json` mit Connection-Typen, `runnel catalog --expressions`,
  `runnel-catalog.json` und `runnel-expressions.json` im Release; Ergebnisse, Fehler und Grenzen in den Descriptoren.
- **Betrieb:** `GET /api/v1/health` und `runnel health` für Monitoring; Server-Log als `text` oder `json`.
- **Editor:** Icons aller Bausteine, Listen im Panel, Kopieren und Einfügen, Markdown-Dokumente, Reference und
  Variables, Vervollständigung im YAML-Editor, Formulare für Trigger, Environments und `project.yaml`, viele offene
  Dateien, Link teilen, Hilfe je Baustein mit Suche (Strg+K).
- **Bilder und Binärdateien im Editor:** PNG, JPEG, GIF, WebP und SVG öffnen als Bild mit Abmessungen und
  Dateigröße statt als Text; andere Binärdateien zeigen „Binary file – can't be shown“. Der Server erkennt
  Binärdateien zusätzlich am Inhalt (Nullbytes, weder UTF-8 noch Windows-1252) und antwortet mit `415`.
- **Neues Projekt:** `runnel init [ordner] [--name] [--force]` legt `project.yaml`, die Ordner `workflows/`,
  `pipelines/`, `triggers/`, `environments/` und `connections/`, das Environment `dev`, einen Beispiel-Workflow und
  `.gitignore` an. Ein nicht leerer Ordner wird abgelehnt (Exit-Code 2); mit `--force` kommen nur die fehlenden
  Dateien hinzu, vorhandene bleiben unverändert.
- **Neues Projekt im Editor:** `runnel serve` in einem Ordner ohne Projekt startet trotzdem; der Editor zeigt „No
  project here“ und legt mit „Create project“ ein Projekt wie `runnel init` im Ordner des Servers an (einen nicht
  leeren Ordner nur mit „Add to the existing files“), danach öffnet er den Beispiel-Workflow – ohne Neustart des
  Servers. Bis dahin meldet `GET /api/v1/health` den Status `degraded` mit der Prüfung `project`.
- **Abkündigen** (ADR 0029): umbenannte Felder gelten unter beiden Namen, `validate` warnt mit Ersatz; im Katalog
  tragen sie `deprecated` und `x-runnel-replacement`. Abgekündigt ist noch nichts.
- Relative Pfade beziehen sich auf den Projektordner (ADR 0020); dynamische Ziele mit `allowedTargets`.
- **Zeitlimit für `core.script`:** `timeout` in der Action (Standard 1 min) und im Transform für `init` und `end`
  (Standard 1 min je Teil; ADR 0028, Nachtrag R5b).
- **Zahlenfelder mit Variablen:** Alle Zahlenfelder der Bausteine nehmen eine Zahl oder `${…}` an, in Actions auch
  `{{ … }}` – z. B. `commitSize`, `cache`, `every`, `maxFiles`, `paginate.maxPages`, `retryAfter.maxRetries`
  (ADR 0014, Nachtrag R5b2). Ein Text, der keine Zahl ist, meldet `field "commitSize": expected a whole number, got
  "abc"`: als Zahl in der Datei schon `runnel validate` (auch bei `count`, `start`/`step`, `status`, `maxRows` und
  `maxRecords`, bisher erst im Lauf), aus einer Variablen der Start des Laufs (Pipeline: Exit-Code 2 statt 1).
- **Ausgaben:** `shell.run`/`shell.powershell` liefern `output.durationMs` (Laufzeit als Zahl in Millisekunden),
  `archive.create` `output.count` (beide ersetzen bisherige Felder, siehe „Bruch“).

### Behoben

- Lauf-IDs sind monoton, auch in derselben Millisekunde.
- `runnel run` meldet einen erfolgreichen Lauf mit `--history errors` oder `none` (bzw. `settings.history.cli`)
  nicht mehr gelegentlich als fehlgeschlagen (Exit-Code 1, mit Start des Fehler-Workflows), wenn er sehr schnell endet.
- HTTP-Trigger mit `response: run` und `history: errors` oder `none` antworten bei einem sehr schnell beendeten Lauf
  ohne `http.response` nicht mehr gelegentlich erst nach dem Timeout mit 504, sondern mit der Antwort des Laufs.
- `GET /api/v1/runs?status=…` und die Zahl laufender Läufe im Editor zählen einen gerade beendeten Lauf nicht mehr
  als laufend, solange die Run-Historie noch nicht geschrieben ist.
- Kodierung einer Datei bleibt beim Speichern im Editor erhalten; Standardwerte in Variablen.
- Exakte Zahlen und Datumswerte in Body, Params und Rows; `transform.init` schickt leere Listen als `[]`.
- Mehrere Fehler im Editor: Umbenennen und Einfügen von Transforms ändern alle Ziele, `core.filter` mit `else`,
  Binärdateien werden nicht geöffnet, Tab-Leiste mit vielen Dateien, Lesbarkeit im dunklen Farbschema.
- `runnel run` endet bei einem falschen Aufruf (unbekanntes Flag, fehlende Datei als Argument, `--param` ohne Wert)
  mit Exit-Code 2 („nicht gestartet“) wie in der Hilfe beschrieben, nicht mit 1 („fehlgeschlagen“).
- `core.stream-lookup` nimmt bei `maxRecords` eine Variable `${…}` an wie `core.to-result`.

### Bruch

Bewusste Änderungen aus der Durchsicht der Namen und Standardwerte vor 1.0 (R5a, R5b, R5c, ADR 0029). Alte Namen sind
unbekannte Felder (`unknown field "…"`) bzw. Flags (`unknown flag`), alte Werte ungültige Werte mit den erlaubten; einen
Hinweis auf den neuen Namen gibt es nicht:

- **Exit-Codes:** Ein falscher Aufruf endet bei allen Befehlen mit 2 statt 1, bei `runnel health` mit 3.
  `runnel validate` und `runnel triggers` enden mit 2, wenn nichts geprüft werden konnte (Pfad fehlt, kein Projekt,
  ungültiges `--param`/`--environment`); 1 heißt nur noch „Fehler gefunden“.
- **Ausdrücke:** Nur die Funktionen der Referenz (`runnel catalog --expressions`) sind aufrufbar; andere Built-ins von
  expr-lang wie `date()`, `duration()`, `type()` oder `repeat()` sind ein Fehler mit ähnlichen Funktionen
  (ADR 0006, Nachtrag R5a).
- **HTTP-Trigger:** `with.maxConcurrent` hat den Standard 4 statt „keine Grenze“; `maxConcurrent: 0` hebt die eigene
  Grenze ausdrücklich auf. Die Antwort `202` enthält nur noch `runId`, das Feld `status` mit dem Pfad des Laufs
  entfällt (ADR 0018, Nachtrag R5a).
- **Datei-Trigger:** Der überwachte Ordner heißt `with.folder` (wie bei `file.list`) statt `with.path`.
- **forEach:** `stopOnError` heißt `continueOnEntryError` mit umgekehrter Bedeutung (Standard `false`):
  `stopOnError: false` wird zu `continueOnEntryError: true` (ADR 0008, Nachtrag R5a).
- **`file.write`:** Die Kodierung `utf-16le` heißt `utf-16` wie in `csv.output` und `file.read`.
- **`core.stream-lookup`:** `maxRecords` ist Text (wie bei `core.to-result`); im Katalog Typ `string`.
- **Secret-Quellen:** `secretStore` nimmt nur noch `env` und `inline` an; `shared` entfällt, `local` kommt mit dem
  lokalen Secret-Store (S39d) zurück (ADR 0022, Nachtrag R5a).
- **HTTP-Trigger:** `with.maxConcurrent` heißt `with.maxConcurrentRuns` wie `settings.maxConcurrentRuns`
  (ADR 0018, Nachtrag R5b).
- **`project.yaml`:** `workflow.defaultJoin` heißt `settings.defaultJoin`; den Block `workflow:` gibt es nicht mehr
  (ADR 0003, Nachtrag R5b).
- **`file.exists`, `transfer.exists`:** `include: all|files|folders` (Standard `all`) wie bei `file.list` statt
  `kind: any|file|folder`.
- **Connections `postgres`, `sqlserver`:** `connectTimeout` heißt `timeout` wie bei den übrigen Typen.
  **`smtp`:** `auth: password` heißt `auth: basic` wie bei `http`; unbekannte Werte von `auth` sind ein Fehler
  (ADR 0022, Nachtrag R5b).
- **`shell.run`, `shell.powershell`:** `json: true` heißt `format: json`, der Wert steht in `output.value` statt
  `output.json`; `output.duration` (Text) ist `output.durationMs` (Zahl) (ADR 0023, Nachtrag R5b).
- **`core.generate-rows`:** `rows` heißt `count`, `rowNumberField` heißt `numberField`. **`core.split-field`:**
  `rows: true` heißt `asRecords: true`, `indexField` heißt `numberField`. **`core.run-pipeline`:** `rows` heißt `records`,
  die zurückgegebenen Datensätze stehen in `output.records` statt `output.rows` (ADR 0025, Nachtrag R5b).
- **`core.switch`:** `default` heißt `else` wie bei `core.filter`.
- **Pipelines:** `distribution: distribute` heißt `distribution: roundRobin`.
- **`archive.create`:** `output.files` heißt `output.count`.
- **Standardwerte:** `maxWait` von `file.wait` und `transfer.wait` ist 1 h statt „unbegrenzt“ (`0`: bis zum Timeout der
  Action); `core.script` endet nach 1 min (`timeout`); `transfer.download` lädt Dateien bis 1 GB (`maxSize`) statt ohne
  Grenze.
- **CLI:** `--env` heißt `--environment` (`runnel run`, `runnel validate`) wie `environment` in Triggern und der API;
  `runnel health --service <name>` heißt `--service-name <name>`, damit `--service` überall ein Schalter für das
  Dienstkonto ist (wie bei `runnel plugins trust --service`).
- **API und `runnel runs --json`:** Ein Lauf (`GET /api/v1/runs`, `GET /api/v1/runs/{id}`) nennt Start und Ende
  `startedAt` und `finishedAt` statt `started` und `finished`, ebenso je Action; der Name des Workflows bzw. der
  Pipeline heißt `name` statt `workflow`. `GET /api/v1/health` (und `runnel health --json`): `uptime` heißt
  `uptimeSeconds`, `runs.lastFailed.file` heißt `path`.
- **Typen von Parametern und Outputs:** dieselben wie bei Feldern in Pipelines – `string`, `int`, `decimal`, `float`,
  `bool`, `date`, `datetime`, `json` – statt `integer`, `number`, `boolean` (ADR 0007, Nachtrag R5c); neu sind damit
  `decimal` (exakt), `date` und `datetime`. `bool` nimmt auch `1`/`0` und `yes`/`no` an.

## Frühere Versionen

Die Vorabversionen bis `v0.1.0-alpha.7` sind mit ihren Hinweisen unter
[GitHub Releases](https://github.com/runnelhq/runnel/releases) beschrieben. Dieser Changelog beginnt danach.
