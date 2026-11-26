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
- **Connections und Secrets** (ADR 0022): Secret-Quellen `env` und `inline`, Connection-Typen `sqlite`, `sqlserver`,
  `postgres`, `http`, `smtp` (mit OAuth2), `sftp`, `ftps`, `ftp`; Test von Connections und Formular im Editor.
- **Neue Bausteine:** `db.*` (SQL ausführen, Tabellen lesen und schreiben, Upsert, Lookup, `db.query`), `http.request`
  mit Paginierung und Download, `core.check`, `shell.run` und `shell.powershell` (ADR 0023), `mail.send`,
  `transfer.*`, `archive.create`/`archive.extract`, `file.read`/`file.write`, `file.list` als Transform,
  `json.input`/`json.output`, `excel.input`/`excel.output` (ADR 0024), `xml.input`/`xml.output`, `core.calculate`,
  `core.dedupe`, `core.sequence`, `core.map-values`, `core.split-field`, `core.stream-lookup`, `core.merge-join`;
  Funktionen `concat()`, `basename()`, `dirname()`, `ext()`, `replaceRegex()`.
- **Einheitliche Parameter** der Bausteine (ADR 0025): `ifExists`, `ifMissing`, `format`, `maxRecords` u. a.
- **Schemas der Dateiarten:** `runnel schema <art>` und `GET /api/v1/schemas/{kind}`.
- **Referenz je Version:** `runnel catalog --json` mit Connection-Typen, `runnel catalog --expressions`,
  `runnel-catalog.json` und `runnel-expressions.json` im Release; Ergebnisse, Fehler und Grenzen in den Descriptoren.
- **Betrieb:** `GET /api/v1/health` und `runnel health` für Monitoring; Server-Log als `text` oder `json`.
- **Editor:** Icons aller Bausteine, Listen im Panel, Kopieren und Einfügen, Markdown-Dokumente, Reference und
  Variables, Vervollständigung im YAML-Editor, Formulare für Trigger, Environments und `project.yaml`, viele offene
  Dateien, Link teilen, Hilfe je Baustein mit Suche (Strg+K).
- Relative Pfade beziehen sich auf den Projektordner (ADR 0020); dynamische Ziele mit `allowedTargets`.

### Behoben

- Lauf-IDs sind monoton, auch in derselben Millisekunde.
- Kodierung einer Datei bleibt beim Speichern im Editor erhalten; Standardwerte in Variablen.
- Exakte Zahlen und Datumswerte in Body, Params und Rows; `transform.init` schickt leere Listen als `[]`.
- Mehrere Fehler im Editor: Umbenennen und Einfügen von Transforms ändern alle Ziele, `core.filter` mit `else`,
  Binärdateien werden nicht geöffnet, Tab-Leiste mit vielen Dateien, Lesbarkeit im dunklen Farbschema.

## Frühere Versionen

Die Vorabversionen bis `v0.1.0-alpha.7` sind mit ihren Hinweisen unter
[GitHub Releases](https://github.com/runnelhq/runnel/releases) beschrieben. Dieser Changelog beginnt danach.
