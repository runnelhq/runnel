# Runnel – Roadmap in kleinen Schritten

Die Phasen aus dem [Konzept (Kap. 15)](KONZEPT.md#15-roadmap) sind hier in **kleine, einzeln prüfbare Schritte** zerlegt.

Erledigte Schritte sind mit ✅ markiert.

## Regeln für jeden Schritt

Es gelten die Regeln aus [AGENTS.md](../AGENTS.md) (PR-Größe und Teilen, Tests, Doku im selben PR, Mockup vor
Oberfläche, Sicherheit). Hier nur, was die Roadmap betrifft:

- **Spalte „Prüfen“:** wie man das Ergebnis des Schritts selbst ausprobiert.
- **Nummern bleiben:** ADRs und PRs verweisen auf sie. Geteilte Schritte bekommen Ziffern (`S38b1`, `S38b2`),
  verschobene Teile behalten ihre Nummer mit „(Teil)“ im Backlog.
- **Umbau-Hinweise** stehen während einer Phase am Kopf und danach im Abschnitt [Verlauf](#verlauf).

Bausteine, die vorerst nicht benötigt werden (z. B. Mail, Shell/PowerShell, Excel), stehen im [Backlog](#backlog) und
werden bei Bedarf als eigener kleiner Schritt eingeplant.

---

## Phase 0.1 – Runner-Basis (Workflows per CLI) ✅

| Nr. | Schritt | Inhalt | Prüfen |
|---|---|---|---|
| S01 ✅ | Projektgerüst | `go.mod`, `cmd/runnel` mit `version`, Ordnerstruktur, Makefile/Taskfile, CI (Lint, Test, Build für Windows + Linux) | `runnel version`; CI grün |
| S01a ✅ | Release-Workflow | Bei Tag `v*`: Tests, Build, ZIP/tar.gz mit `THIRD_PARTY_NOTICES.txt`, `SHA256SUMS`, GitHub-Release (Tags mit `-` als Vorabversion); CI lädt dieselben Archive als Artefakt hoch | `make package VERSION=v0.0.0-test`; Tag pushen |
| S01b ✅ | SBOM, Schwachstellen-Check, Herkunftsnachweis | CycloneDX-SBOM je Plattform (`cyclonedx-gomod`), `govulncheck` in CI (auch wöchentlich), signierte Build-Provenance und SBOM-Attestierung im Release, manueller Release-Start über GitHub; Go 1.26 | CI grün; `make package`; Release über *Run workflow* |
| S02 ✅ | Workflow-Dateiformat | Go-Strukturen für `Workflow`, `Action`, `Link`; YAML laden mit Zeilenangabe bei Fehlern | Golden-Tests mit gültigen/ungültigen Dateien |
| S03 ✅ | Workflow-Validierung | Start vorhanden, IDs eindeutig, Links zeigen auf existierende Actions, unerreichbare Knoten, Rückwärts-Links nur als Schleifen-Link; `runnel validate` | `runnel validate examples/…` mit Fehlerbeispielen |
| S04 ✅ | Projekt & Variablen | `project.yaml`, `environments/*.yaml`, Auflösung von `${VAR}` und `${env:…}` in fester Rangfolge; `--env`, `--param` | Unit-Tests der Rangfolge |
| S05 ✅ | Engine: sequenziell | Ausführung entlang von Erfolgs-/Fehler-/Immer-Links (zunächst max. ein eingehender Link pro Action); Actions `core.start`, `core.success`, `core.abort`, `core.log`, `core.wait`; `runnel run`, Exit-Code, Konsolen-Log | Beispiel-Workflow läuft, Exit-Code 0/1 |
| S05a ✅ | Typisierte Parameter & Workflow-Outputs | Parameter mit `type` (string, integer, number, boolean, json), `enum`, `description`; `outputs` mit `value`, ausgewertet bei erfolgreichem Ende; Prüfung in `validate`, Umwandlung und Prüfung von `--param`, Ausgabe der Outputs bei `runnel run` ([ADR 0012](adr/0012-ki-faehigkeit.md)) | Golden-Tests; `runnel run` zeigt Outputs |
| S06 ✅ | Engine: parallel & Join | Parallele Zweige, `join: all/first/each`, Dead-Path-Elimination, Status `skipped` ([ADR 0003](adr/0003-join-semantik.md)) | Tests mit `-race`; Beispiel mit Fehler-Sammelpfad |
| S07 ✅ | Retry, Timeout, Abbruch | `retry`, `timeout`, `joinTimeout`, Workflow-Timeout, Abbruch per Strg+C (sauberes Beenden) | Tests; Strg+C während `core.wait` |
| Q1 ✅ | Qualitätsrunde | Fuzz-Tests (YAML-Parser, Variablen) inkl. wöchentlichem CI-Lauf; Fehlerbehebungen: Absturz bei bestimmtem fehlerhaftem YAML, UTF-8 mit BOM, UTF-16, ungültige Kodierung, Statusfehler bei spätem Abbruch, Leerzeile bei `core.log`, Meldung bei Ordner statt Datei; Testplan für Windows und Liste bekannter Einschränkungen ([TESTPLAN.md](TESTPLAN.md)) | `make fuzz`; manueller Windows-Test |
| Q2 ✅ | Typnamen & Code-Aufbau | Getrennte Typ-Namensräume für Actions/Transforms, reservierte Namensräume, `provides:` nach Art ([ADR 0013](adr/0013-typnamen-namensraeume.md)); `internal/actions/core` aufgeteilt (eine Datei je Action), zentrale `actions.Builtin()`, Test-Hilfen `actiontest`, Vertragstest für alle eingebauten Actions | `make test` |
| Q3 ✅ | Anleitungen | `docs/dev/ACTIONS.md` (neue Action entwickeln), `AGENTS.md`/`CLAUDE.md` (Regeln für KI-Agenten), `CONTRIBUTING.md`; Regel „Anleitungen aktuell halten“ | – |
| Q4 ✅ | `core.dummy` | Sammelpunkt für Zweige (wie Dummy in Apache Hop); erste Action nach `docs/dev/ACTIONS.md` – Anleitung dabei erprobt und ergänzt; Beispiel `collector.workflow.yaml` | `runnel run -v examples/hello/workflows/collector.workflow.yaml` |

**Ergebnis 0.1:** Workflows mit den Core-Actions laufen per CLI bzw. Windows-Aufgabenplanung – parallel, mit Retry, Timeouts und Abbruch.

---

## Phase 0.2 – Editor-Grundlage ✅

Ziel: Workflows im Browser entwerfen, prüfen, ausführen und beobachten. Die vorhandenen Core-Actions reichen dafür;
neue Actions erscheinen dank E1 später automatisch in Palette und Eigenschaftsdialog.

| Nr. | Schritt | Inhalt | Prüfen |
|---|---|---|---|
| E1 ✅ | Action-Beschreibung (Descriptor) | Jede Action erhält Kategorie, Bezeichnung und ein JSON-Schema ihrer `with:`-Felder (Grundlage für Palette und Eigenschaftsdialog); `/catalog`-taugliche Ausgabe; Vertragstest erzwingt Descriptor und Schema; ADR 0014 | Vertragstest; `runnel catalog [typ] [--json]` |
| E2 ✅ | Server & Datei-API | `runnel serve` (nur `127.0.0.1`, Host-/Origin-Prüfung): Projektbaum, Dateien lesen/speichern/löschen mit ETag (Konfliktschutz), `/catalog`, `/validate` (auch ungespeicherter Inhalt, mit YAML-Pfad je Meldung); `runnel validate` prüft wie `/validate` auch Action-Typen und `with:`; API in `docs/dev/API.md` | Tests mit `httptest`; `curl` |
| E3 ✅ | Run-API & Live-Events | Läufe starten, auflisten, abbrechen; Status je Knoten und Log; Ereignisse live per Server-Sent Events mit Nachholen nach Neuverbindung (ADR 0016); Läufe **zunächst nur im Speicher** (letzte 100), SQLite-Historie folgt mit S26 ohne API-Änderung; `runnel run` und Server nutzen dieselbe Vorbereitung (`internal/runs`) | Tests; `curl -N` |
| E4a ✅ | Editor-Grundlage | Mockup: [`design/mockups/e4-editor-shell.html`](../design/mockups/e4-editor-shell.html). `web/` mit Vite, React, TypeScript, Tailwind und Design-Tokens; Schriften lokal eingebettet; Build in die exe eingebettet und unter `/` ausgeliefert (CSP); Kopfzeile mit Projekt und Farbschema, Statusleiste mit Verbindung; Frontend-Build in CI und Release, SBOM und Lizenzhinweise um npm-Pakete und Schriften ergänzt; Playwright-Grundlage; `docs/dev/EDITOR.md` | `bin/runnel serve examples/hello`, Browser; `make e2e` |
| E4b ✅ | Explorer, Tabs, YAML-Ansicht | nach dem Mockup: Projekt-Explorer (Ordner einklappbar), mehrere Tabs, Datei nur lesend mit Zeilennummern und Hervorhebung (Schlüssel, Kommentare, `${…}`/`{{ … }}`), Kodierung UTF-8/UTF-16/Windows-1252, „Copy path“, Zustände „Datei nicht gefunden“ und „Verbindung verloren“ | Playwright |
| E5 ✅ | Canvas (nur lesen) | Mockup: [`design/mockups/e5-canvas.html`](../design/mockups/e5-canvas.html). Workflows mit React Flow als Graph (Karten-Knoten, Links mit Symbol, Join-Schild), Umschalter Canvas/YAML, Layout aus der Datei, sonst automatisch angeordnet (dagre); Meldungen aus `/validate` an den Knoten mit Sprung in die YAML-Zeile; `/validate` liefert das geparste Modell; Beispiele auseinandergezogen | `examples/` im Editor öffnen; Playwright |
| E6a ✅ | Canvas bearbeiten und speichern | Mockup: [`design/mockups/e6a-edit.html`](../design/mockups/e6a-edit.html). Knoten verschieben, auswählen (auch mehrere), löschen; Rückgängig/Wiederholen; ungespeicherte Änderungen am Tab; Speichern (Strg+S) mit ETag und Konfliktdialog (Neu laden / Überschreiben); der Server ändert das YAML zeilengenau (`POST /workflows/edit`) und erhält Kommentare und Formatierung; Rückfrage beim Schließen mit ungespeicherten Änderungen | Playwright; zwei Browser gleichzeitig |
| E6b ✅ | Knoten und Links anlegen | Mockup: [`design/mockups/e6b-create.html`](../design/mockups/e6b-create.html). Palette aus `/catalog` unter dem Explorer (nach Kategorie, Suche mit `/`), Knoten per Drag & Drop mit automatischer ID (`log-1` …) und sofortigem Umbenennen (auch per Doppelklick), Links zwischen Ports ziehen (Rückwärts-Links mit `loop: true`), Link-Menü für Typ (On success / On failure / Always) und Löschen | Playwright |
| E7a ✅ | Eigenschaften einer Action | Mockup: [`design/mockups/e7-properties.html`](../design/mockups/e7-properties.html). Panel „Properties“ rechts: ID, Description, Konfiguration als Formular aus dem Schema (E1, neues Format `runnel-text` für mehrzeilige Texte), Timeout, Retry, Continue on error, Join; Änderungen nach kurzer Tipp-Pause, je Feld ein Rückgängig-Schritt; Meldungen am Feld; Workflows mit ungültigen Feldern bleiben auf dem Canvas | Playwright |
| E7b ✅ | Links, Workflow, Meldungen | Mockup wie E7a. Eigenschaften eines Links (Bedingung, Loop, Disabled) und des Workflows (Name, Description, Settings; Parameter und Outputs nur anzeigen); Leiste „Problems“ unter dem Canvas mit Sprung zum Feld | Playwright |
| E8a ✅ | Ausführen im Editor | Mockup: [`design/mockups/e8-run.html`](../design/mockups/e8-run.html). Run (F5) mit gemerkten Werten, Dialog für Environment und Parameter (▾, Shift+F5), Rückfrage bei ungespeicherten Änderungen, Abbrechen (Ctrl+.), Live-Status an Actions und Links, Ergebnis in der Statusleiste | Manuell; Playwright |
| E8b ✅ | Log und Ergebnis | Mockup wie E8a. Tab „Log“ unter dem Canvas (folgt dem Lauf, Schalter „Debug“), Ergebnis der gewählten Action im Panel mit Sprung ins Log; Zustand „wartet“ (Join) am Knoten über das neue Ereignis `action.waiting` der Engine | Go-Test der Engine; Playwright |

**Ergebnis 0.2:** Workflows unter `http://localhost:7300` visuell bauen, prüfen, starten und beobachten – auf denselben YAML-Dateien wie die CLI.

---

## Phase 0.3 – Workflow-Ausbau ✅

| Nr. | Schritt | Inhalt | Prüfen |
|---|---|---|---|
| S08a ✅ | Ausdrücke: Sprache und Prüfung | `expr-lang` für `{{ … }}` (Paket `internal/expr`): ganzer Wert behält den Ergebnistyp, sonst Text; Funktionsbibliothek Teil 1 (Datum, Text, Null) ([ADR 0006](adr/0006-ausdruckssprache.md)); Syntax und Namen werden beim Lesen geprüft (`runnel validate`, Editor) – in Link-Bedingungen, `with:`, `outputs` und Standardwerten von Parametern; bis S08b meldet die Engine Ausdrücke als „noch nicht unterstützt“ | Unit-Tests je Funktion; Fuzzing |
| S08b ✅ | Ausdrücke: Auswertung | Kontext `vars`/`params`/`env`/`run`/`result`/`results`; Bedingungs-Links, Ausdrücke in `with:`, `outputs` und Standardwerten; **Ausgabedaten von Actions** (`result.output`) für nachfolgende Actions; Beispiel `examples/expressions` | Tests der Engine; `runnel run -v examples/expressions/workflows/orders.workflow.yaml` |
| S09 ✅ | Datei-Actions | `file.exists`, `file.wait`, `file.copy`, `file.move`, `file.delete`, `file.mkdir`, `file.list` (Ergebnis = Dateiliste) im Namensraum `file` (Kategorie „Files“ der Palette); Platzhalter im Dateinamen, Konfliktprüfung vor dem Kopieren, Beispiel `examples/files` (KONZEPT Kap. 12.1) | Tests mit temporären Ordnern; `runnel run -v examples/files/workflows/archive.workflow.yaml`; UNC-Pfad manuell unter Windows (W29) |
| S11 ✅ | Sub-Workflows | `core.run-workflow` mit Parametern und Ergebnisrückgabe über `outputs` ([ADR 0017](adr/0017-sub-workflows.md)): nur übergebene Parameter, Pfade relativ zum Projekt, Log des Kinds bei der aufrufenden Action, Prüfung fester Aufrufe vorab (Datei, Parameter, Selbstaufruf) | Beispiel `examples/sub-workflows`; `runnel run -v examples/sub-workflows/workflows/nightly.workflow.yaml` |
| S35a ✅ | Ausdruckseditor im Panel | Mockup: [`design/mockups/s35-expressions.html`](../design/mockups/s35-expressions.html). CodeMirror 6 (statt Monaco) für alle Textfelder unter `with:` und Link-Bedingungen: Hervorhebung, Vorschläge (Kontext, Parameter, Ergebnisfelder, Funktionen, Variablen), Signaturhilfe, Fehler an der Stelle mit Korrektur; Referenz vom Server (`GET /expressions`) | Playwright-Test |
| S35b ✅ | Größerer Ausdruckseditor | Mockup wie S35a (Zustand `expanded`): ⤢ (oder Strg+Umschalt+E) öffnet einen Dialog mit Zeilennummern und durchsuchbarer Referenz (Klick fügt ein, außerhalb eines Ausdrucks mit `{{ }}`); Apply mit Strg+Enter, Escape verwirft | Playwright-Test |

**Ergebnis 0.3:** Workflows mit Ausdrücken, Datei-Schritten und Sub-Workflows – in CLI und Editor.

---

## Phase 0.4 – Pipelines (ETL) ✅

| Nr. | Schritt | Inhalt | Prüfen |
|---|---|---|---|
| S12 ✅ | Pipeline-Dateiformat & Validierung | Go-Strukturen für `Pipeline`, `Transform`, Link-Typen; azyklischer Graph; `validate` für Pipelines. Format zunächst minimal (Transforms mit `id`, `type`, `with`; Links mit `from`, `to`; `parameters`), Fehler nur über Links, `error`/`info`-Links als „noch nicht unterstützt“ (KONZEPT Kap. 5.2) | Golden-Tests |
| S13a ✅ | Pipeline-Engine Kern | Transform = Goroutine, Link = gepufferter Channel, Batches (500), Puffer (10.000 Records je Link), Backpressure, Ende-Signal, Abbruch; mehrere Eingänge werden gemischt, mehrere Ausgänge bekommen jeden Record (copy); Transform-Registry (eigener Namensraum, ADR 0013); Testbausteine `core.generate-rows`, `core.dummy`, `core.log`; Zähler je Transform; Record geordnet mit Werten `any` (Typen mit S14) | Tests mit `-race`; 10 Mio. generierte Zeilen bei konstantem Speicher |
| S13b ✅ | Pipelines ausführen und prüfen | `runnel run` für Pipelines (Zähler im Log), Typ- und Konfigurationsprüfung der Transforms in `runnel validate`, `runnel catalog` für Transforms; Anleitung für neue Transforms ([`docs/dev/TRANSFORMS.md`](dev/TRANSFORMS.md)); Beispiel `examples/pipelines` | CLI-Golden-Tests; `runnel run examples/pipelines/pipelines/numbers.pipeline.yaml` |
| S14 ✅ | Record & Typsystem | Geordneter Record, Werttypen inkl. `decimal` (shopspring/decimal) und eigenem Typ `date`, Schema-Ableitung über `Init`, Prüfung verwendeter Felder (Fehler bei geschlossenem, Warnung bei offenem Schema), gezeigt an `fields` von `core.log` ([ADR 0007](adr/0007-typsystem-pipelines.md) mit Nachtrag); strikter Modus mit S17 | Unit-Tests; `runnel validate examples/broken/fields.pipeline.yaml` |
| S15 ✅ | CSV | `csv.input`, `csv.output`; Trennzeichen, Kopfzeile, Encoding (UTF-8 mit/ohne BOM, Windows-1252, beim Lesen UTF-16 per BOM), Felddefinition mit Typ und Format (Zahlenmuster wie `#.##0,00`, Datumsmuster wie `dd.MM.yyyy`), `ifExists` (`replace` über temporäre Datei, `append`, `fail`); Abhängigkeit `golang.org/x/text`; Beispiel `examples/pipelines/pipelines/orders.pipeline.yaml` | Golden-Tests: CSV rein → CSV raus |
| S16a ✅ | Ausdrücke je Datensatz & Filter | `record` im Ausdruckskontext mit `vars`, `params`, `env`, `run`; exakte Dezimalzahlen in Operatoren, `null` wie in SQL ([ADR 0006](adr/0006-ausdruckssprache.md), Nachtrag); `core.filter` (verworfene Datensätze werden gezählt); Beispiel `orders.pipeline.yaml` mit Mindestbetrag | Unit-Tests; `runnel validate` meldet Tippfehler in der Bedingung |
| S16b ✅ | Basis-Transforms | `core.select-fields` (behalten mit Umbenennen oder entfernen), `core.constant` (Werte mit `${…}`/`{{ … }}` einmal je Lauf), `core.convert` (Formate aus `csv` als gemeinsames Paket `internal/format`); Beispiel `customers.pipeline.yaml`; `core.calculate` zurückgestellt | Unit-Tests; CLI-Test mit erzeugter CSV-Datei |
| S17a ✅ | Fehler-Links | `type: error` an Pipeline-Links; `Output.Fail` schickt einen Datensatz mit `_error`, `_errorField`, `_errorTransform` dorthin, sonst Abbruch; genutzt von `csv.input`, `csv.output`, `core.convert`, `core.filter`; Zähler `errors`; Warnung bei Fehler-Links an Transforms ohne Fehler je Datensatz; Beispiel `customers.pipeline.yaml` mit abgelehnten Kunden | Tests mit fehlerhaften Zeilen; CLI-Test |
| S17b ✅ | Switch | `core.switch`: Datensätze nach Bedingungen (`cases` mit `when`/`to`, erster Treffer, `default`) auf genau einen Link verteilen, ohne Treffer verwerfen und zählen; `Output.EmitTo`, `InitContext.Targets`; Prüfung der Ziele in `runnel validate`; Beispiel `segments.pipeline.yaml` | Unit- und Engine-Tests; CLI-Test |
| S17c ✅ | Verteilen & Parallelität | `distribution` (`copy`/`distribute`) am Transform, `copies: n` (parallele Instanzen, nur für Transforms mit `Parallel`), `settings` (`batchSize`, `bufferSize`, `strict` nach ADR 0007); Beispiel `parallel.pipeline.yaml` | Tests mit `-race`; 10 Mio. Zeilen über Kopien |
| S18 ✅ | Workflow ↔ Pipeline | `core.run-pipeline` mit Parametern wie `core.run-workflow` (ADR 0017, Nachtrag); Ergebnis: Zähler je Transform (`output.transforms.<id>.read/written/errors`) und `output.errors`; Prüfung fester Aufrufe in `runnel validate`; Beispiel `examples/pipelines/workflows/import.workflow.yaml` | Beispiel: Workflow ruft CSV-Pipeline; CLI-Test |
| S22 ✅ | Sortieren & Gruppieren | `core.sort` (stabil, `descending`, `ignoreCase`, leere Werte zuletzt) und `core.group-by` (`count`, `countDistinct`, `sum`, `avg`, `min`, `max`, `first`, `last`, `concat`), beide im Speicher; Vergleich und Gruppenschlüssel in `internal/record`; Werte, die nicht passen, auf den Fehler-Link; Beispiel `daily.pipeline.yaml`; `core.dedupe` zurückgestellt | Unit-Tests; CLI-Test mit erzeugter CSV-Datei |
| S24a ✅ | Ergebnis-Datensätze | `core.from-result` und `core.to-result` (`maxRows`, Standard 100.000); `rows` an `core.run-pipeline` hinein, `output.rows` heraus ([ADR 0008](adr/0008-ausfuehrungsvarianten.md), Nachtrag); Beispiel `examples/pipelines/workflows/files.workflow.yaml` | Unit-Tests; CLI-Test |
| S24b ✅ | Wiederholung je Eintrag | `forEach: "{{ … }}"` an jeder Action, mit `row`, `parallel` und `stopOnError`; Ergebnis `items`, `succeeded`, `failed` ([ADR 0008](adr/0008-ausfuehrungsvarianten.md), Nachtrag); Beispiel `examples/pipelines/workflows/each-file.workflow.yaml` | Beispiel: Dateiliste → Pipeline je Datei; Engine- und CLI-Tests |
| S25a1a ✅ | Pipeline-Canvas: Server | `layout` im Pipeline-Format, `/validate` für Pipelines (mit Prüfung der Transforms), `POST /pipelines/edit` (zeilengenau wie bei Workflows; gemeinsamer Editor in `internal/model/edit.go`) | Go-Tests |
| S25a1b ✅ | Pipeline-Canvas: Editor | Pipelines im Canvas anzeigen und bearbeiten: Transform-Palette, Daten- und Fehler-Links, Eigenschaften aus dem Schema mit Execution (`copies`, `distribution`), Problems ([Mockup](../design/mockups/s25a-pipeline.html), Zustände `canvas`, `link`, `problems`) | Playwright |
| S25a2 ✅ | Pipeline-Canvas: Felder | Felder der eingehenden Datensätze und der Links im Panel (Zustände `canvas`, `link`), Endpunkt für die abgeleiteten Schemas | Playwright |
| S25a3 ✅ | Pipeline-Canvas: Ausführen | Pipelines im Editor ausführen, Zähler an Knoten und Links, Log (Zustand `run`) | Playwright |
| E7c ✅ | Parameter, Outputs und Wiederholung im Editor | Mockup: [`design/mockups/e7c-parameters.html`](../design/mockups/e7c-parameters.html) (Parameter und Outputs) und [`s25a-pipeline.html`](../design/mockups/s25a-pipeline.html), Zustand `foreach`. Parameter von Workflows und Pipelines sowie Outputs von Workflows im Panel anlegen, ändern, sortieren und löschen (aufklappbare Zeilen, Server-Bearbeitung zeilengenau); `forEach`, `parallel`, `stopOnError` und das Schlüssel/Wert-Feld für `params` von `core.run-workflow`/`core.run-pipeline` (K10, K11). | Playwright; Go-Tests der Bearbeitung |

**Ergebnis 0.4:** ETL-Fälle mit CSV per CLI und im Editor, von Workflows gesteuert (auch je Datei bzw. Datensatz).

---

## Phase 0.5 – Betrieb ✅

| Nr. | Schritt | Inhalt | Prüfen |
|---|---|---|---|
| S37 ✅ | Cron-Trigger | Trigger-Dateien (`triggers/*.yaml`, Typ `cron` mit Zeitplan und Zeitzone, `params`, `enabled`), Scheduler im Server mit Neuladen geänderter Dateien, `overlap: skip/allow`; Limits gleichzeitiger Läufe (Projekt und Workflow) – ein Start darüber wird abgelehnt (`429`); `runnel triggers`, `GET /triggers`, Prüfung in `runnel validate`; Abhängigkeit `robfig/cron/v3` (MIT) | Tests mit simulierter Uhr; `runnel triggers examples/hello` |
| S38 ✅ | Weitere Trigger | Typ `interval` (`every`, erster Lauf nach einem Abstand) und `file` (Ordner per Polling, auch UNC; `pattern`, Datei bereit bei zwei gleichen Abfragen, `mode: each` mit Pfad als Parameter oder `batch` mit JSON-Liste, `existing: process/ignore`, erneuter Versuch bei übersprungenem oder abgelehntem Start); Prüfung des Ziel-Parameters; Felder anderer Typen unter `with:` als Fehler; Trigger für Workflows und Pipelines | Tests mit temporären Ordnern und simulierter Uhr; `runnel serve examples/files` |
| S41 ✅ | Windows-Dienst | `runnel service install/uninstall/start/stop/status` mit `--name` (mehrere Dienste), Konto (Standard LocalService, Passwortabfrage, gMSA), Starttyp (Standard verzögert), Recovery-Optionen; Log-Datei mit Rotation unter `%ProgramData%\runnel\logs`, Start/Stopp/Warnungen/Fehler in der Ereignisanzeige; geordneter Stopp mit `--stop-timeout` (auch für `runnel serve`); Abhängigkeit `golang.org/x/sys` (BSD-3) | Manuell auf Windows (TESTPLAN W48) |
| S38a ✅ | HTTP-Trigger: asynchron | Typ `http` nach [ADR 0018](adr/0018-http-trigger.md): eigener Listener (zunächst nur lokal), Methode und Pfad mit Platzhaltern, Routing-Prüfung, `auth: token \| none`, Pfad/Query/Body → typisierte Parameter (`400` vor dem Start), `response: accepted` mit `202` und Lauf-ID, `maxConcurrent` (`429`), Body-Limit, `trigger = http:<name>`; für Workflows und Pipelines | Tests; `curl` auf `127.0.0.1` |
| S40a ✅ | Listener und TLS | Adresse des Trigger-Listeners frei wählbar (`--triggers`), im Netz nur mit TLS: selbst erzeugtes Zertifikat (`--triggers-tls auto`, neben den Logs, Fingerabdruck im Log) oder eigenes (`--triggers-cert`/`--triggers-key`, Erneuerung ohne Neustart); reines HTTP nur mit `--triggers-insecure`; Warnung bei `auth: none` im Netz; auch für den Dienst (später auch für den Editor, S40b) | Tests mit TLS-Client; Aufruf von einem zweiten Rechner (TESTPLAN W50) |
| S38b1 ✅ | HTTP-Trigger: Antwort aus dem Workflow | `response: run` mit `timeout` (`504`), Standardantwort aus `outputs` (`200`/`500`), Action `http.respond` (Status, Header, Body; erste Antwort gilt, auch aus Sub-Workflows, ohne Aufrufer wirkungslos), `request` im Ausdruckskontext | Tests; `curl` mit verschiedenen Statuscodes |
| S38b2 ✅ | HTTP-Trigger: Last | `history` (`all`/`errors`/`none`), eigenes Limit für HTTP-Läufe im Lauf-Speicher, Zwischenspeicher geprüfter Workflows/Pipelines, des Projekts und übersetzter Ausdrücke | Tests; Lastprobe `BenchmarkHTTPTrigger` (1,1 → 0,23 ms je Anfrage) |
| S38c ✅ | HTTP-Trigger: Antwort aus der Pipeline | Transform `http.response` (Datensätze als JSON, `single`, `whenEmpty` als Rückfallantwort, Status und Header, `maxRows`), JSON-Array im Body als Datensätze | Tests; REST-artiges Beispiel `customer-api` |

**Ergebnis 0.5:** Dauerbetrieb als Windows-Dienst – zeit- und dateigesteuert und per HTTP von anderen Systemen
ansprechbar, auch als kleiner Dienst mit Antwort aus Workflow oder Pipeline. Der Editor bleibt lokal.

---

## Phase 0.6 – Komfort ✅

| Nr. | Schritt | Inhalt | Prüfen |
|---|---|---|---|
| S44 ✅ | Datenvorschau | Pipeline bis Transform X mit Limit, Records je Link als Tabelle/JSON; Mockup [`s44-preview.html`](../design/mockups/s44-preview.html) | Manuell im Editor |
| S46 ✅ | Felder ermitteln | „Felder ermitteln“ für CSV schreibt `fields:` in die Datei; Mockup [`s46-fields.html`](../design/mockups/s46-fields.html). Die Erkennung ist eine optionale Schnittstelle der Eingabe-Transforms: neue Eingabe-Formate (JSON mit S19, später XML, Excel, Datenbank) setzen sie gleich mit um | Playwright-Test |
| E9 ✅ | YAML bearbeiten | Workflows und Pipelines in der YAML-Ansicht bearbeiten (Editor mit Hervorhebung, Prüfung beim Tippen über `/validate`, Rückgängig gemeinsam mit dem Canvas, Umgang mit Fehlern, die keinen Graph mehr erlauben); Mockup [`e9-yaml.html`](../design/mockups/e9-yaml.html). Bis dahin ist die YAML-Ansicht als „Read-only“ gekennzeichnet | Playwright |
| E10 ✅ | Weitere Dateien bearbeiten | `project.yaml`, Environments und Trigger in der YAML-Ansicht bearbeiten wie Workflows und Pipelines in E9 (Prüfung beim Tippen, Rückgängig, Speichern); bis dahin schreibgeschützt | Playwright |
| E11 ✅ | Dateien anlegen und löschen | Im Explorer: neue Workflows, Pipelines, Trigger und Environments aus einer kleinen gültigen Vorlage, neue Dateien und Ordner, Umbenennen, Löschen mit Rückfrage (mit Hinweis auf Verweise anderer Dateien), Ordner nur leer löschen – damit ein Projekt komplett im Editor entsteht; Mockup [`e11-files.html`](../design/mockups/e11-files.html) | Playwright |
| E12 ✅ | Projekt organisieren | Dateien und Ordner im Explorer verschieben (Ziehen und Ablegen, „Move to…“); beim Umbenennen und Verschieben anbieten, Verweise anderer Dateien anzupassen (Trigger-Ziele, `core.run-workflow`, `core.run-pipeline`, über `/references`); Trigger auch in Unterordnern von `triggers/` (behebt K14); Mockup [`e12-organize.html`](../design/mockups/e12-organize.html) | Playwright |

---

## Phase 0.7 – Erweiterbarkeit

| Nr. | Schritt | Inhalt | Prüfen |
|---|---|---|---|
| E13 ✅ | Icons aller Bausteine | Eigene Icons für die 11 eingebauten Actions und Transforms, die noch das allgemeine Symbol zeigen (`core.run-workflow`, `core.run-pipeline`, `file.*`, `http.respond`, `http.response`), und ein neues für `core.dummy`; Mockup [`e13-icons.html`](../design/mockups/e13-icons.html) | Palette und Canvas im Editor |
| E14a ✅ | Listen im Panel bearbeiten (Editor) | Listen der Konfiguration im Panel aus dem Schema: Tabelle, Karten, Chips; Hinzufügen, Entfernen, Verschieben; Auswahl für feste Werte, Kästchen für true/false, Ausdrücke `{{ … }}`; Eigenschaften nur in bestimmten Fällen (`x-runnel-only`); Mockup [`e14-lists.html`](../design/mockups/e14-lists.html) | Playwright |
| E14b ✅ | Listen im Panel bearbeiten (Felder und Hilfe) | Feldnamen der eingehenden Datensätze zur Auswahl (`format: runnel-field`), Ziele der Daten-Links bei `core.switch` (`runnel-target`), Meldungen an der Zeile mit Vorschlag, Abschnitt „Help“ mit YAML-Beispiel aus dem Descriptor („Show in YAML“, „Copy example“); Mockup wie E14a | Playwright |
| E16 ✅ | Kopieren und Einfügen im Canvas | Ausgewählte Actions bzw. Transforms mit `Strg+C`, `Strg+X`, `Strg+V` und über das Kontextmenü kopieren, ausschneiden, einfügen, auch in eine andere Datei. Links zwischen den kopierten Elementen bleiben erhalten, Links nach außen entfallen. Die Zwischenablage enthält den YAML-Ausschnitt, deshalb funktioniert das Kopieren auch zwischen Browser-Tabs und in den YAML-Editor; eingefügter YAML-Text wird übernommen, wenn er Actions bzw. Transforms enthält. Doppelte IDs bekommen eine Endung (`log-2`), die Position folgt dem Mauszeiger; ein Einfügen ist ein Schritt für Rückgängig. Actions lassen sich nicht in Pipelines einfügen und Transforms nicht in Workflows; das wird gemeldet. Verweise der eingefügten Elemente auf ebenfalls kopierte Elemente folgen deren neuen IDs (`results.<id>` in Ausdrücken, Ziele `to:` von `core.switch`); Mockup [`e16-copy-paste.html`](../design/mockups/e16-copy-paste.html) | Playwright |
| E17 ✅ | Markdown-Dokumente im Projekt | `.md`-Dateien im Editor als formatierte Ansicht mit Umschalter auf den Text; „New → Document“ legt eine an (z. B. `README.md`). Links auf andere Dateien des Projekts öffnen diese im Editor; Bilder nur aus dem Projekt über die Datei-API. Kein eingebettetes HTML und keine Skripte, Links nach außen in einem neuen Tab. Neue Abhängigkeit im Editor für das Rendern (MIT-kompatibel, fest gepinnt, Begründung im PR). Beim Öffnen eines Projekts ohne zuletzt geöffnete Datei zeigt der Editor dessen `README.md`, falls vorhanden; Mockup [`e17-markdown.html`](../design/mockups/e17-markdown.html) | Playwright |
| S16c ✅ | Berechnen & Duplikate (vormals S16 und S22, Teil) | `core.calculate` (Felder je Datensatz per Ausdruck, in Reihenfolge, Fehler-Link mit dem unveränderten Datensatz) und `core.dedupe` (erster Datensatz je Kombination von Feldern, `ignoreCase`, Schlüssel im Speicher); Beispiel `contacts.pipeline.yaml` | Unit-Tests, Benchmarks; CLI-Test mit dem Beispiel |
| S16d ✅ | Sequenz | `core.sequence`: fortlaufende Nummer in der Reihenfolge der Datensätze, `start`, `step`, mit `groupBy` je Gruppe; Icons von `core.calculate`, `core.dedupe` und `core.sequence` nach Mockup [`s16d-icons.html`](../design/mockups/s16d-icons.html); Beispiel `contacts.pipeline.yaml` | Unit-Tests, Benchmark; CLI-Test mit dem Beispiel |
| S16e ✅ | Funktion `concat()` | `concat(trenner, a, b, …)` in der Ausdruckssprache: verbindet Werte als Text und lässt leere weg ([ADR 0006](adr/0006-ausdruckssprache.md), Nachtrag); statt eines eigenen Concat-Transforms | Unit-Tests; Beispiel in `contacts.pipeline.yaml` |
| S16f ✅ | Wertzuordnung & Feld teilen | `core.map-values` (Tabelle `from`/`to`, `target`, `default`, `ignoreCase`) und `core.split-field` (in Felder mit `into` oder in Datensätze mit `rows`, `indexField`, `keepEmpty`, `trim`); Icons nach Vorschlag (Variante A, vom Maintainer ohne eigenes Mockup gewählt); Beispiel `people.pipeline.yaml` | Unit-Tests, Benchmarks; CLI-Test mit dem Beispiel |
| S19 ✅ | JSON | `json.input` (JSON als Strom mit `records` für ein Array im Dokument, NDJSON je Zeile mit Fehler-Link, `fields` mit `from` und Typ, Kodierung wie CSV, „Detect fields“) und `json.output` (Array oder NDJSON, exakte Zahlen, ISO-Daten, `format`); Icons: Seite mit `{ }` (Variante A, vom Maintainer gewählt); Kodierung gemeinsam mit CSV in `internal/textenc`; Beispiel `api-orders.pipeline.yaml` | Unit-Tests, Fuzzing, Benchmarks; CLI-Test mit dem Beispiel |
| S54 ✅ | Archive | `archive.create` und `archive.extract` für ZIP, TAR und TAR.GZ im neuen Namensraum `archive` statt des reservierten `zip` ([ADR 0013](adr/0013-typnamen-namensraeume.md), Nachtrag); Schutz beim Entpacken (Pfade außerhalb des Zielordners, Verknüpfungen, `maxSize`, `maxFiles`), Dateinamen in Codepage 437; Icons: Kiste mit Pfeil (Variante A, vom Maintainer gewählt); Beispiel `examples/files/workflows/bundle.workflow.yaml` | Unit-Tests, Fuzzing, Benchmarks; CLI-Test mit dem Beispiel |
| S10 ✅ | HTTP-Action & Bedingung prüfen (vormals zurückgestellt) | `http.request` (Methode, `query`, Header, Body als Text oder JSON, `timeout`, `maxSize`, `failOnError`; JSON-Antworten als Wert in `result.output.body`, Header nie im Log) und `core.check` (scheitert mit eigener Meldung, wenn eine Bedingung falsch ist); Icons: Raute mit Haken für `core.check`, Pfeile hin und zurück für `http.request` (Vorschlag, vom Maintainer im Chat gewählt); Beispiel `examples/http` | Unit-Tests gegen lokalen Testserver, Benchmark; CLI-Test mit dem Beispiel |
| S55 ✅ | Dateiliste als Transform | `file.list` als Eingabe einer Pipeline: Einträge eines Ordners als Datensätze (`path`, `name`, `extension`, `size`, `modified`, `folder`) mit denselben Optionen wie die Action (`pattern`, `recursive`, `include`); Icon wie die Action (Ordner mit Zeilen, Variante A, vom Maintainer gewählt); Beispiel `inventory.pipeline.yaml` | Unit-Tests, Benchmark; CLI-Test mit dem Beispiel |
| S25b ✅ | Dynamische Ziele absichern (Teil von S25) | `core.run-workflow`/`core.run-pipeline` rufen nur Dateien im Projektordner auf (auch nicht über `..` oder absolute Pfade); `allowedTargets` mit Mustern für Ziele, die erst beim Lauf feststehen; Warnung von `validate` ohne `allowedTargets` ([ADR 0008](adr/0008-ausfuehrungsvarianten.md), Nachtrag mit Sicherheit); Beispiel `nightly.workflow.yaml` mit Parameter `IMPORT` | Unit-Tests der Ablehnungen; CLI-Test mit dem Beispiel |
| S58 ✅ | Schemas der Dateiarten | JSON Schema für `project.yaml`, Environments und Trigger aus den Strukturen in `internal/model` (Beschreibungen per `doc`, Namen aus `yaml`, ADR 0014): `GET /api/v1/schemas/{kind}`, `runnel schema <art>` (z. B. für VS Code); Grundlage für E18 und E19 | Golden-Tests; `runnel schema trigger` |
| E18 (Mockup) ✅ | Mockup: Reference statt Palette ([`e18-reference.html`](../design/mockups/e18-reference.html)) | Seitenleiste je nach Art der Datei: Palette (Workflow, Pipeline), **Reference** (Project, Environment, Trigger: Felder mit Beschreibung, Typ, Standardwert, Einfügen an der Cursor-Position) oder nichts (Markdown, andere Dateien); bei `project.yaml` und Environments **Variables** (genutzte `${…}`, wo gesetzt, wo sie fehlen) | Mockup-PR |
| E18a ✅ | Reference statt Palette: Server | `GET /api/v1/variables`: jede genutzte oder definierte `${…}`-Variable mit Wert und Zeile je Ebene (Projekt, Environments) und Fundstellen (Datei, Zeile; Parameter der Datei gekennzeichnet); Grenzen für Anzahl und Größe der Dateien. E18 ist geteilt (Server vor Editor) | Go-Tests, Benchmark |
| E18b ✅ | Reference statt Palette: Editor | Seitenleiste je nach Art der Datei nach Mockup E18: Palette, Reference (Felder aus S58, Insert, Sprung zur Zeile) oder Variables (Tabelle, „Add to …“) | Playwright |
| E19 ✅ | Vervollständigung und Hover im YAML-Editor | Feldnamen, erlaubte Werte und Beschreibungen aus den Schemas (S58) beim Tippen und Überfahren; auch für Workflows und Pipelines (Schemas der Actions und Transforms) – dort die Typen nach `type:` und die Felder unter `with:`; die übrigen Schlüssel von Workflows und Pipelines (`actions`, `links`, …) noch ohne Schema | Playwright (`e2e/assist.spec.ts`); in einem Trigger unter `with:` Strg+Leertaste |
| S49 | Plugin-Protokoll | JSON-RPC über stdin/stdout ([ADR 0019](adr/0019-plugin-protokoll.md)): `kind: Plugin` unter `plugins/<name>/`, Handshake, Prozess je Lauf, `describe`, `action.execute`; Freigabe je Projekt mit `runnel plugins trust`/`untrust` (Nachtrag) | Beispiel-Plugin in Go; Tests: nicht freigegeben, Freigabe nach Änderung ungültig |
| S50 | Plugin-Transforms | `transform.open/batch/end`, `emit`/`closed` mit Zielen wie bei `core.switch`, Kredite (Streaming, ADR 0019), optional `transform.init` und `transform.detectFields` | Beispiel-Plugin; Benchmark mit Echo-Plugin (Ziel ≥ 300.000 Records/s) |
| S51 | Go-SDK | `pkg/sdk` mit Beispielen | Beispiel-Plugin baut nur mit SDK |
| S52 | Plugin-Verwaltung | Plugins im Editor anzeigen, Fehler beim Laden sichtbar, Hinweis mit „Trust“ für nicht freigegebene Plugins (Mockup vorher) | Manuell |
| S53 | MCP-Server (vormals S11a) | `runnel mcp` (stdio): Workflows eines Projekts als MCP-Tools; Eingabe-Schema aus `parameters`, Ergebnis aus `outputs`, Log als Fortschrittsmeldungen ([ADR 0012](adr/0012-ki-faehigkeit.md)) | MCP Inspector; Claude Desktop/Claude Code ruft `examples/hello` auf |

---

## Backlog

Werden bei Bedarf als eigener Schritt (je ein PR) eingeplant. Reihenfolge ist offen.

### Zurückgestellt (Tiefe vor Breite)

Geplante Schritte, die vorerst hinter dem Ziel „erst in die Tiefe, dann in die Breite“ zurückstehen. Sie behalten ihre
Nummer und ihren Inhalt; Abhängigkeiten sind angegeben.

| Nr. | Schritt | Inhalt | Hängt ab von / Hinweis |
|---|---|---|---|
| S26 | Run-Historie | Ersetzt den Speicher aus E3 ohne API-Änderung; SQLite-Store mit Migrationen, Tabellen `runs`/`node_runs`/Link-Zustände; `runnel runs` listet Läufe | bis dahin die letzten 100 Läufe im Speicher (dazu 100 von HTTP-Triggern, seit S38b2), nach einem Neustart weg |
| S29 | Resume | Fortsetzen ab fehlgeschlagener Action (`/runs/{id}/resume`, CLI `runnel resume`); Voraussetzung für `core.approval` (ADR 0012) | S26 |
| S36 | Teilausführung | „Ab hier ausführen“, „Nur diese Action ausführen“ | – |
| S24 (Teil) | Ergebnis-Datensätze auslagern | `resultSpillRows`: große Ergebnisse in eine temporäre Datei statt in den Speicher | S24a |
| S17 (Teil) | `maxErrors` | Schwelle für Fehler-Links: mehr als n fehlerhafte Datensätze brechen die Pipeline ab | S17a |
| S39a ✅ | Secrets mit der Quelle `env` | `${secret:[quelle/]NAME}` nur in Feldern mit `format: runnel-secret` und als Token von HTTP-Triggern; Standardquelle `secretStore:` im Environment bzw. in `project.yaml`, sonst `local`; eingebaute Quelle `env` (Umgebungsvariablen), `local`/`shared` mit Meldung „not available yet“; fehlendes Secret verhindert den Start (Meldung ohne Wert); Maskierung (`***`) in Log, Fehlern, Ereignissen, Ergebnissen und Datenvorschau ([ADR 0022](adr/0022-connections-secrets.md), Nachtrag S39a) | Go-Tests; HTTP-Trigger mit `token: ${secret:env/NAME}`, `runnel validate` meldet `${secret:…}` in einem gewöhnlichen Feld |
| S39a2 ✅ | Secrets im Klartext (`inline`) | Eingebaute Quelle `inline`: `secrets:` im Environment bzw. in `project.yaml`, gelesen wie jedes Secret (nur in Secret-Feldern, maskiert); `validate` und Editor warnen bei Klartext; für Entwicklung und einfache APIs ([ADR 0022](adr/0022-connections-secrets.md), Nachtrag S39a2) | Go-Tests; `secretStore: inline` mit `secrets:` in `environments/dev.yaml`, HTTP-Trigger mit `${secret:…}`; Beispiel `examples/http` (Trigger `release` mit Token) |
| S39d | Lokaler Secret-Store | Quelle `local` je Projekt außerhalb des Projektordners (AES-256-GCM, Schlüssel per DPAPI bzw. `RUNNEL_MASTER_KEY`), `runnel secret set/list/delete/missing` mit `--from-file`, `--service` ([ADR 0022](adr/0022-connections-secrets.md)) | S39a |
| S39e | Gemeinsamer Store und Pflege | Quelle `shared`, `runnel secret export/import`, Ablaufdatum (`--expires`), `move` für verschobene Projekte, Secret-Quellen als Datei (`kind: SecretStore`) | S39d |
| S39f | Mockup: Secrets im Editor | Felder mit `format: runnel-secret` im Eigenschafts-Panel: zeigt nur die Referenz (`${secret:…}`), schlägt Namen vor, warnt vor Klartext; Übersicht der Secrets je Environment (genutzt, vorhanden in der Quelle des Environments, fehlend; wie `runnel secret missing`) neben den Variablen (E18); Secrets setzen und löschen (nie anzeigen), Hinweis auf bald ablaufende | S39d |
| S39g | Secrets im Editor | Umsetzung nach freigegebenem Mockup S39f; API zum Setzen und Löschen (`PUT`/`DELETE /secrets/{name}`, `GET /secrets` nur mit Namen), im Netzbetrieb standardmäßig aus ([ADR 0022](adr/0022-connections-secrets.md)) | S39f |
| S20a ✅ | Connections & SQL ausführen | `kind: Connection` unter `connections/` (Name = Dateiname) mit Registry der Typen und Schema je Typ (`internal/connection`, `internal/connections`); Prüfung in `validate`, Editor (bearbeitbar, Reference) und vor dem Lauf; Feld `connection:` mit Format `runnel-connection` und erlaubten Typen; Auflösung mit Variablen und Secrets je Environment, Maskierung; Typ `sqlite` (`modernc.org/sqlite`), Action `db.execute` mit Parametern `@name`, SQL ohne `${…}`/`{{ … }}` (Format `runnel-sql`), Timeout, Pool je Datenbank ([ADR 0022](adr/0022-connections-secrets.md), Nachtrag S20a) | Go-Tests, Playwright; Beispiel `examples/database` |
| S20b ✅ | DB-Eingabe | Transform `db.table-input` (Datensätze aus einer Abfrage mit Parametern `@name`, Typen nach ADR 0007 aus dem deklarierten Spaltentyp: `DECIMAL`/`NUMERIC` exakt, `DATE`, `DATETIME`, `BOOLEAN`, `NULL` → leer; offenes Schema), Connections in Pipelines (Auflösung vor dem Start, Prüfung bei `validate`); Verweise auf Connections beim Umbenennen anpassen (`connection:` in Workflows und Pipelines, `name:` in der Datei) | Go-Tests, Benchmark; Pipeline `report` in `examples/database` |
| S20c | SQL Server & PostgreSQL | Typen `sqlserver` (`go-mssqldb`, auch Windows-Anmeldung) und `postgres` (`pgx`, Parameter `@name` übersetzt), TLS mit Zertifikatsprüfung, `trustServerCertificate` nur mit Warnung; Lizenz je Treiber prüfen | S20b; Testplan (eigener Server) |
| S21 | DB-Ausgabe | `db.table-output` (Insert, `commitSize`), `db.upsert` | S20b |
| S23 | Lookups & Join | Info-Link, `core.stream-lookup`, `db.lookup`, `core.merge-join` | `db.lookup`: S20b; danach Info-Links im Pipeline-Canvas |
| S56 | Mapping: Engine und Prüfung | Transform `core.run-pipeline` ([ADR 0021](adr/0021-unter-pipelines.md)): eine Unter-Pipeline je Lauf, Datensätze strömen über `core.from-result`/`core.to-result` hindurch (Backpressure), Schema durch die Unter-Pipeline, Parameter, Selbstaufruf und Verschachtelung (max. 10) als Fehler, Log mit Präfix; Beschriftungen „Records from/to Caller“ | ADR 0021 |
| S56b | Mockup: Mapping-Knoten und Executor-Ausgänge | Mapping im Canvas (Unter-Pipeline öffnen, Zähler), Beschriftung der Ausgänge `results`/`rows` am Executor | S56 |
| S56c | Mapping im Editor | Umsetzung nach freigegebenem Mockup S56b | S56b |
| S25c | Pipeline-Executor je Datensatz (Teil von S25) | `core.pipeline-executor` mit `groupBy: none`, `params` je Datensatz, Durchreichen (`main`), Ausgang `rows` wie `core.switch`, Fehler-Link, `copies` | S56 |
| S25d | Executor: Gruppen und Ergebnisse (Teil von S25) | `groupBy: size/field`, Ausgang `results` (ein Datensatz je Lauf) | S25c |
| S25e | Workflow-Executor (Teil von S25) | `core.workflow-executor` nach demselben Muster, mit den `outputs` des Workflows | S25d |
| S45 | Samples | Erste N Records pro Link je Lauf speichern, in Historie anzeigen | S26 |
| S39b | Mockup: Connections im Editor | Connections im Explorer, Formular aus dem Schema, „Test connection“; Secret-Felder wie in S39f | S20a, S39f |
| S39c | Connections im Editor | Umsetzung nach freigegebenem Mockup S39b; `POST /connections/{name}/test` (15 s, nur gespeicherte Connections) | S39b |
| S57 | Dateitransfer | `transfer.upload/download/list/delete/move/mkdir/exists/wait` wie die `file.*`-Actions; Connection-Typen `sftp` (Hostschlüssel Pflicht), `ftps`, `ftp` (nur mit `insecure: true`) ([ADR 0022](adr/0022-connections-secrets.md), ADR 0013 Nachtrag) | S20 |
| S40b | Netzwerkbetrieb des Editors | `--listen` für Editor und API, gemeinsamer Zugangsschlüssel, Client-Infos im Protokoll ([ADR 0005](adr/0005-mehrbenutzerbetrieb.md)) | S40a (TLS); bis dahin Editor nur auf `127.0.0.1` |
| S38 (Teil) | HMAC für HTTP-Trigger | Signaturprüfung als weitere `auth`-Variante (z. B. GitHub, Shop-Systeme) | S38a |
| S42 | Run-Konfigurationen | `RunConfig type: local`, Auswahl per CLI/Trigger/Action/Editor | – |
| S41 (Teil) | Dienstverwaltung im Editor | Dienste (`runnel service`) im Editor anzeigen und steuern: Zustand, Konto, Starttyp, Port; Starten, Stoppen, Neustarten; Server-Log und Einträge der Ereignisanzeige ansehen; Dienst für das geöffnete Projekt einrichten. Offen: Rechte (Steuern und Einrichten brauchen Administratorrechte, der Dienst selbst läuft als LocalService) und ob es nur im Desktop-Modus angeboten wird | S41; Mockup vorab; bei Zugriff über das Netz S40b |
| S43 | Wartung & Monitoring | Error-Workflow, `/health`, optional `/metrics`, Aufbewahrung der Historie (Retention) | Retention: S26 |
| S37 (Teil) | Run-Queue | Starts über dem Limit warten (Status `queued`) statt abgelehnt zu werden, Prioritäten (manuell > HTTP > zeitgesteuert), `overlap: queue` | S37 |
| S37 (Teil) | Verpasste Läufe nachholen | `catchUp` für Cron-Trigger: verpasste Zeitpunkte beim Start des Servers nachholen | S26 (letzter Lauf je Trigger) |
| S47 | Desktop-Modus | `runnel desktop` mit WebView2-Fenster; vorher Mockup | – |
| S48 | Installer | MSI (WiX) mit Dienst-Option (ZIP und `THIRD_PARTY_NOTICES` bereits mit S01a) | bis dahin ZIP |
| E15 | Referenz der Bausteine | Beschreibung aller Actions und Transforms für Anwender, erzeugt aus den Descriptoren (Felder, Typen, Standardwerte, Beispiele): im Editor (z. B. Hilfe zum ausgewählten Baustein) und als Dokument unter `docs/`. Bis dahin `runnel catalog <typ>` und die Beispiele unter `examples/` | Beispiele im Descriptor (ADR 0014) |

### Fehlerbehebungen

Bekannte Fehler und Einschränkungen ([Testplan, Kap. 3](TESTPLAN.md#3-bekannte-einschränkungen)), je ein Fix-PR.

| Thema | Inhalt | Hinweis |
|---|---|---|
| Kodierung beim Speichern erhalten (K13) | Der Editor speichert eine Datei in der Kodierung, in der sie geöffnet wurde (UTF-8 mit/ohne BOM, UTF-16 LE/BE, Windows-1252); Zeichen, die in Windows-1252 nicht darstellbar sind, werden vor dem Speichern gemeldet statt still ersetzt | betrifft seit E9/E10 alle bearbeitbaren Dateien |

### Weitere Bausteine

| Bereich | Bausteine |
|---|---|
| Kommunikation | `mail.send` (SMTP mit OAuth2), Mail abrufen (IMAP) – Connections nach ADR 0022 |
| Secret-Quellen | `windows-credentials`, `azure-keyvault`, `vault` (REST ohne SDK), Cloud-Tresore, Plugin-Quellen (`secrets.get`) – ADR 0022 |
| Skripte & Programme | `shell.powershell` (Action + Transform), Programm starten, JavaScript (`core.script` als Action und Transform, mit goja) |
| Dateiformate | Excel (Ein-/Ausgabe), XML, Fixed-Width |
| Dateitransfer | siehe S57 (Packen und Entpacken: S54) |
| Netzwerk-Transforms | REST-Eingabe mit Paging, HTTP-Lookup, REST-Ausgabe |
| Datenbanken | Treiber Oracle, ODBC; MySQL/MariaDB vorerst nicht (Lizenz, ADR 0022 – über ODBC oder Plugin) |
| Editor | Schmale Fenster (einklappbarer Explorer, Tablet/Laptop) – der Editor zielt zunächst auf Desktop-PCs; Einstellungsmenü, sobald es mehrere Einstellungen gibt |
| Transforms | Pivot/Unpivot, Feld teilen, Wertzuordnung, String-/Datum-Funktionen als eigene Transforms, Sequenz, externes Sortieren (große Datenmengen) |
| KI ([ADR 0012](adr/0012-ki-faehigkeit.md)) | Connections für Modell-Anbieter (Anthropic, OpenAI-kompatibel, Ollama); `ai.chat` (inkl. strukturierter Ausgabe per JSON Schema); `ai.agent` mit Werkzeugen (Workflows, Actions, MCP-Tools) und Positivliste; `mcp.call-tool` und MCP-Server als Plugin-Quelle; `core.approval` (Freigabe abwarten); Chat-Trigger mit Streaming und Gesprächsgedächtnis; Transforms `ai.chunk`, `ai.embed`, Vektor-Ausgabe (pgvector, Qdrant); MCP über HTTP (Token, später OAuth); Token-/Kosten-Erfassung und Rate-Limit je Connection; Link-Typ `tool` im Editor |
| Plugin-SDKs | Python, C#/.NET, Node |
| Projekte | Bibliotheken: gemeinsame Workflows und Pipelines für mehrere Projekte, z. B. als Eintrag in `project.yaml` mit eigenem Präfix in Aufrufen (`lib:…`); Prüfung, Editor und Explorer sehen sie mit. Bis dahin nur über absolute Pfade; braucht eine ADR |
| Später (Konzept) | Remote-Runner (`RunConfig type: remote`), Git-Integration im Editor, Breakpoints, Import aus Apache Hop, Tray-Icon, `runnel update`, `winget`, Code-Signing |

---

## Verlauf

Umbauten der Roadmap, nach Abschluss der jeweiligen Phase hierher verschoben.

> **Umbau vom 2026-11-19:** Um schneller einen testbaren Editor zu haben, folgt auf die Runner-Basis direkt die
> Editor-Grundlage (E1–E8). Sie ersetzt die bisherigen Schritte S27, S28 und S30–S34. Ausdrücke, weitere Actions,
> Historie und Resume sowie Pipelines folgen danach. Bestehende Schrittnummern bleiben erhalten, weil ADRs und PRs
> darauf verweisen; nur die Phasen sind neu nummeriert.
>
> **Tiefe vor Breite (2026-11-19):** Damit der ganze Anwendungsfall früh bewertbar ist, werden die Phasen zuerst in der
> Tiefe gebaut. Gute, aber nicht vorrangige Schritte stehen mit ihrer Nummer im [Backlog](#zurückgestellt-tiefe-vor-breite):
> S10, S19, S20, S21, S23, S25, S26, S29, S36, S45, S47, S48 sowie `core.calculate` (S16), `maxErrors` (S17), `core.dedupe` (S22) und die
> Aufbewahrung der Historie (S43). E7c war ebenfalls zurückgestellt und ist seit 2026-11-20 wieder eingeplant (Phase 0.4).
>
> **Betrieb verschlankt (2026-11-20):** Aus Phase 0.5 stehen jetzt S39 (Secret-Store), S40b (Netzwerkbetrieb des
> Editors), S42 (Run-Konfigurationen) und S43 (Wartung & Monitoring) im Backlog. Der Webhook-Trigger wurde neu
> besprochen und ist zum HTTP-Trigger geworden ([ADR 0018](adr/0018-http-trigger.md)): S38a–S38c, dazu S40a
> (Listener und TLS) aus S40. Reihenfolge: zuerst die Trigger ohne HTTP und der Windows-Dienst (S37, S38, S41),
> danach alles zum HTTP-Trigger (S38a, S40a, S38b, S38c).
