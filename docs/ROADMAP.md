# Runnel – Roadmap in kleinen Schritten

Die Phasen aus dem [Konzept (Kap. 15)](KONZEPT.md#15-roadmap) sind hier in **kleine, einzeln prüfbare Schritte** zerlegt.

Erledigte Schritte sind mit ✅ markiert.

## Regeln für jeden Schritt

- **Ein Schritt = ein Pull Request.** Richtwert: höchstens ca. 1.000 geänderte Zeilen inklusive Tests
  (ohne generierte Dateien und Lockfiles). Wird ein Schritt größer, wird er vor der Umsetzung geteilt.
- **Lauffähig nach jedem Schritt:** `main` baut, alle Tests sind grün, nichts ist „halb fertig“ freigeschaltet.
- **Tests sind Teil des Schritts:** Unit-Tests für neue Logik, Golden-File-Tests für Dateiformate und Datentransformationen,
  `-race` für nebenläufigen Code.
- **Manuell prüfbar:** Jeder Schritt nennt unter „Prüfen“, wie man das Ergebnis selbst ausprobiert.
  Dazu gibt es bei Bedarf ein Beispiel unter `examples/`.
- **Nur das Nötige:** Keine Vorab-Implementierung für spätere Schritte. Schnittstellen werden erweitert, wenn sie gebraucht werden.
- **Doku im selben PR:** Betroffene Abschnitte in Konzept/ADRs werden mit angepasst.

Bausteine, die vorerst nicht benötigt werden (z. B. Mail, Shell/PowerShell, Excel), stehen im [Backlog](#backlog) und
werden bei Bedarf als eigener kleiner Schritt eingeplant.

---

## Phase 0.1 – Runner-Kern (Workflows per CLI)

| Nr. | Schritt | Inhalt | Prüfen |
|---|---|---|---|
| S01 ✅ | Projektgerüst | `go.mod`, `cmd/runnel` mit `version`, Ordnerstruktur, Makefile/Taskfile, CI (Lint, Test, Build für Windows + Linux) | `runnel version`; CI grün |
| S01a ✅ | Release-Workflow | Bei Tag `v*`: Tests, Build, ZIP/tar.gz mit `THIRD_PARTY_NOTICES.txt`, `SHA256SUMS`, GitHub-Release (Tags mit `-` als Vorabversion); CI lädt dieselben Archive als Artefakt hoch | `make package VERSION=v0.0.0-test`; Tag pushen |
| S01b ✅ | SBOM, Schwachstellen-Check, Herkunftsnachweis | CycloneDX-SBOM je Plattform (`cyclonedx-gomod`), `govulncheck` in CI (auch wöchentlich), signierte Build-Provenance und SBOM-Attestierung im Release, manueller Release-Start über GitHub; Go 1.26 | CI grün; `make package`; Release über *Run workflow* |
| S02 ✅ | Workflow-Dateiformat | Go-Strukturen für `Workflow`, `Action`, `Link`; YAML laden mit Zeilenangabe bei Fehlern | Golden-Tests mit gültigen/ungültigen Dateien |
| S03 ✅ | Workflow-Validierung | Start vorhanden, IDs eindeutig, Links zeigen auf existierende Actions, unerreichbare Knoten, Rückwärts-Links nur als Schleifen-Link; `runnel validate` | `runnel validate examples/…` mit Fehlerbeispielen |
| S04 ✅ | Projekt & Variablen | `project.yaml`, `environments/*.yaml`, Auflösung von `${VAR}` und `${env:…}` in fester Rangfolge; `--env`, `--param` | Unit-Tests der Rangfolge |
| S05 ✅ | Engine: sequenziell | Ausführung entlang von Erfolgs-/Fehler-/Immer-Links (zunächst max. ein eingehender Link pro Action); Actions `core.start`, `core.success`, `core.abort`, `core.log`, `core.wait`; `runnel run`, Exit-Code, Konsolen-Log | Beispiel-Workflow läuft, Exit-Code 0/1 |
| S06 | Engine: parallel & Join | Parallele Zweige, `join: all/first/each`, Dead-Path-Elimination, Status `skipped` ([ADR 0003](adr/0003-join-semantik.md)) | Tests mit `-race`; Beispiel mit Fehler-Sammelpfad |
| S07 | Retry, Timeout, Abbruch | `retry`, `timeout`, `continueOnError`, Workflow-Timeout, Abbruch per Strg+C (sauberes Beenden) | Tests; Strg+C während `core.wait` |
| S08 | Ausdrücke | `expr-lang` für `{{ … }}`, Kontext `vars`/`params`/`env`/`run`/`result`/`results`, Bedingungs-Links, Funktionsbibliothek Teil 1 (Datum, Text, Null) ([ADR 0006](adr/0006-ausdruckssprache.md)) | Unit-Tests je Funktion; Beispiel mit Bedingungs-Link |
| S09 | Datei-Actions | `file.exists`, `file.wait`, `file.copy`, `file.move`, `file.delete`, `file.mkdir`, `file.list` (Ergebnis = Dateiliste) | Tests mit temporären Ordnern; UNC-Pfad manuell unter Windows |
| S10 | HTTP-Action | `http.request` (Methoden, Header, Body, Timeout, Statuscode → Ergebnis) | Tests gegen lokalen Testserver |
| S11 | Sub-Workflows | `core.run-workflow` mit Parametern und Ergebnisrückgabe | Beispiel Eltern-/Kind-Workflow |

**Ergebnis 0.1:** Workflows mit Datei- und HTTP-Schritten laufen per CLI bzw. Windows-Aufgabenplanung.

---

## Phase 0.2 – Pipelines (ETL per CLI)

| Nr. | Schritt | Inhalt | Prüfen |
|---|---|---|---|
| S12 | Pipeline-Dateiformat & Validierung | Go-Strukturen für `Pipeline`, `Transform`, Link-Typen; azyklischer Graph; `validate` für Pipelines | Golden-Tests |
| S13 | Pipeline-Engine Kern | Transform = Goroutine, Link = gepufferter Channel, Batches, Backpressure, Ende-Signal, Abbruch; Testbausteine `core.generate-rows`, `core.dummy`, `core.log`; Zähler je Transform | Tests mit `-race`; 10 Mio. generierte Zeilen bei konstantem Speicher |
| S14 | Record & Typsystem | Geordneter Record, Werttypen inkl. `decimal`, Schema-Ableitung über `Init`, Validator-Warnungen für unbekannte Felder ([ADR 0007](adr/0007-typsystem-pipelines.md)) | Unit-Tests |
| S15 | CSV | `csv.input`, `csv.output`; Trennzeichen, Kopfzeile, Encoding (UTF-8, Windows-1252), Felddefinition mit Typ/Format | Golden-Tests: CSV rein → CSV raus |
| S16 | Basis-Transforms | `core.filter`, `core.select-fields`, `core.calculate`, `core.constant`, `core.convert` | Golden-Tests |
| S17 | Fehler-Links & Fluss | Fehler-Link, `maxErrors`, `core.switch`, Verteilmodus `copy`/`distribute`, `copies` (parallele Instanzen) | Golden-Tests mit fehlerhaften Zeilen |
| S18 | Workflow ↔ Pipeline | `core.run-pipeline` mit Parametern, Zeilenzahlen im Ergebnis | Beispiel: Workflow ruft CSV-Pipeline |
| S19 | JSON | `json.input`, `json.output` (JSON und NDJSON) | Golden-Tests |
| S20 | Connections & DB-Eingabe | Connection-Metadaten, Passwort zunächst über `${env:…}`; Treiber SQL Server, PostgreSQL, SQLite; `db.table-input`, Action `db.execute` | Tests gegen SQLite; SQL Server/Postgres manuell oder per CI-Container |
| S21 | DB-Ausgabe | `db.table-output` (Insert, `commitSize`), `db.upsert` | Tests gegen SQLite |
| S22 | Sortieren & Gruppieren | `core.sort`, `core.group-by`, `core.dedupe` (zunächst im Speicher) | Golden-Tests |
| S23 | Lookups & Join | Info-Link, `core.stream-lookup`, `db.lookup`, `core.merge-join` | Golden-Tests |
| S24 | Ergebnis-Datensätze | `core.to-result`, `core.from-result`, `forEachRow` an `core.run-pipeline`/`core.run-workflow` ([ADR 0008](adr/0008-ausfuehrungsvarianten.md)) | Beispiel: Dateiliste → Pipeline je Datei |
| S25 | Executor & dynamische Ziele | `core.pipeline-executor`, `core.workflow-executor` (pro Datensatz/Gruppe), Pfade mit Variablen, `allowedTargets` | Beispiel: Pipeline je Kunde |

**Ergebnis 0.2:** ETL-Fälle mit CSV, JSON und Datenbanken laufen per CLI.

---

## Phase 0.3 – Server & Editor

| Nr. | Schritt | Inhalt | Prüfen |
|---|---|---|---|
| S26 | Run-Historie | SQLite-Store mit Migrationen, Tabellen `runs`/`node_runs`/Link-Zustände; `runnel runs` listet Läufe | Unit-Tests; CLI-Ausgabe |
| S27 | Server & Datei-API | `runnel serve` (nur `127.0.0.1`), `/files` mit ETag, `/catalog`, `/validate` | Tests mit `httptest`; `curl` |
| S28 | Run-API & Live-Events | `POST /runs`, Abbrechen, WebSocket-Events (Status, Metriken, Logs), Log-Stream per SSE | Tests; `curl`/`websocat` |
| S29 | Resume | Fortsetzen ab fehlgeschlagener Action (`/runs/{id}/resume`, CLI `runnel resume`) | Test: Fehler beheben, fortsetzen |
| S30 | Editor-Gerüst | Vite + React + TypeScript, in exe eingebettet, Projekt-Explorer, Datei als YAML anzeigen; SBOM um npm-Pakete ergänzen (`cyclonedx-npm`, mit Go-SBOM zusammenführen) | Browser öffnen, Dateien durchklicken |
| S31 | Canvas (nur lesen) | Workflow/Pipeline mit React Flow darstellen, Layout aus Datei, Auto-Layout falls fehlend | Beispiele werden korrekt angezeigt |
| S32 | Canvas bearbeiten | Knoten verschieben/anlegen/löschen, Links ziehen und Typ umschalten, Palette aus `/catalog`, Speichern mit ETag-Konfliktdialog, Undo/Redo | Playwright-Test; zwei Browser gleichzeitig |
| S33 | Eigenschaften | Eigenschaftsfenster aus JSON-Schema der Bausteine, Inline-Validierung, Tab „Probleme“ | Playwright-Test |
| S34 | Ausführen im Editor | Ausführen-Knopf mit Umgebung/Parametern, Live-Status auf Canvas, Logs- und Metriken-Tab, Historie | Manuell: Beispiel starten und beobachten |
| S35 | Ausdruckseditor | Monaco mit Syntaxhervorhebung, Autovervollständigung (Kontext, Felder, Funktionen), Fehleranzeige | Playwright-Test |
| S36 | Teilausführung | „Ab hier ausführen“, „Nur diese Action ausführen“ | Manuell |

**Ergebnis 0.3:** Workflows und Pipelines visuell bauen, starten und beobachten.

---

## Phase 0.4 – Betrieb

| Nr. | Schritt | Inhalt | Prüfen |
|---|---|---|---|
| S37 | Cron-Trigger | Trigger-Dateien, Scheduler im Server, `overlap: skip/queue/allow`, Run-Queue mit Limits | Tests mit simulierter Uhr |
| S38 | Weitere Trigger | Webhook (Token/HMAC), Datei-Überwachung, Intervall; Trigger für Pipelines | Tests; `curl` auf Webhook |
| S39 | Secret-Store | Verschlüsselung (AES-GCM), Schlüssel per DPAPI bzw. Master-Key, `${secret:…}`, Maskierung in Logs | Tests; unter Windows DPAPI manuell |
| S40 | Netzwerkbetrieb | `--listen`, TLS, gemeinsamer Zugangsschlüssel, Client-Infos im Protokoll ([ADR 0005](adr/0005-mehrbenutzerbetrieb.md)) | Zugriff von zweitem Rechner |
| S41 | Windows-Dienst | `runnel service install/start/stop/uninstall`, Ereignisanzeige, Recovery-Optionen | Manuell auf Windows |
| S42 | Run-Konfigurationen | `RunConfig type: local`, Auswahl per CLI/Trigger/Action/Editor | Tests der Rangfolge |
| S43 | Wartung & Monitoring | Retention, Error-Workflow, `/health`, optional `/metrics` | Tests |

**Ergebnis 0.4:** Dauerbetrieb auf einem Windows-Server.

---

## Phase 0.5 – Komfort

| Nr. | Schritt | Inhalt | Prüfen |
|---|---|---|---|
| S44 | Datenvorschau | Pipeline bis Transform X mit Limit, Records je Link als Tabelle/JSON | Manuell im Editor |
| S45 | Samples | Erste N Records pro Link je Lauf speichern, in Historie anzeigen | Manuell |
| S46 | Felder ermitteln | „Felder ermitteln“ für CSV/JSON schreibt `fields:` in die Datei | Playwright-Test |
| S47 | Desktop-Modus | `runnel desktop` mit WebView2-Fenster | Manuell auf Windows |
| S48 | Installer | MSI (WiX) mit Dienst-Option (ZIP und `THIRD_PARTY_NOTICES` bereits mit S01a) | Installation auf frischem Windows |

---

## Phase 0.6 – Erweiterbarkeit

| Nr. | Schritt | Inhalt | Prüfen |
|---|---|---|---|
| S49 | Plugin-Protokoll | JSON-RPC über stdin/stdout, `describe`, `action.execute` | Beispiel-Plugin in Go |
| S50 | Plugin-Transforms | `transform.open/batch/close` (Streaming) | Beispiel-Plugin |
| S51 | Go-SDK | `pkg/sdk` mit Beispielen | Beispiel-Plugin baut nur mit SDK |
| S52 | Plugin-Verwaltung | Plugins im Editor anzeigen, Fehler beim Laden sichtbar | Manuell |

---

## Backlog

Werden bei Bedarf als eigener Schritt (je ein PR) eingeplant. Reihenfolge ist offen.

| Bereich | Bausteine |
|---|---|
| Kommunikation | `mail.send` (SMTP), Mail abrufen (IMAP) |
| Skripte & Programme | `shell.powershell` (Action + Transform), Programm starten, JavaScript (`core.script`, `core.script-transform` mit goja) |
| Dateiformate | Excel (Ein-/Ausgabe), XML, Fixed-Width |
| Dateitransfer | SFTP/FTP Upload/Download, ZIP/Entpacken |
| Netzwerk-Transforms | REST-Eingabe mit Paging, HTTP-Lookup, REST-Ausgabe |
| Datenbanken | Treiber MySQL/MariaDB, Oracle, ODBC |
| Transforms | Pivot/Unpivot, Feld teilen, Wertzuordnung, String-/Datum-Funktionen als eigene Transforms, Sequenz, externes Sortieren (große Datenmengen) |
| Plugin-SDKs | Python, C#/.NET, Node |
| Später (Konzept) | Mappings, Remote-Runner (`RunConfig type: remote`), Git-Integration im Editor, Breakpoints, Import aus Apache Hop, Tray-Icon, `runnel update`, `winget`, Code-Signing |
