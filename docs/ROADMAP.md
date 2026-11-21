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

## Phase 0.6 – Komfort

| Nr. | Schritt | Inhalt | Prüfen |
|---|---|---|---|
| S44 | Datenvorschau | Pipeline bis Transform X mit Limit, Records je Link als Tabelle/JSON | Manuell im Editor |
| S46 ✅ | Felder ermitteln | „Felder ermitteln“ für CSV schreibt `fields:` in die Datei; Mockup [`s46-fields.html`](../design/mockups/s46-fields.html). Die Erkennung ist eine optionale Schnittstelle der Eingabe-Transforms: neue Eingabe-Formate (JSON mit S19, später XML, Excel, Datenbank) setzen sie gleich mit um | Playwright-Test |
| E9 ✅ | YAML bearbeiten | Workflows und Pipelines in der YAML-Ansicht bearbeiten (Editor mit Hervorhebung, Prüfung beim Tippen über `/validate`, Rückgängig gemeinsam mit dem Canvas, Umgang mit Fehlern, die keinen Graph mehr erlauben); Mockup [`e9-yaml.html`](../design/mockups/e9-yaml.html). Bis dahin ist die YAML-Ansicht als „Read-only“ gekennzeichnet | Playwright |
| E10 | Weitere Dateien bearbeiten | `project.yaml`, Environments und Trigger in der YAML-Ansicht bearbeiten wie Workflows und Pipelines in E9 (Prüfung beim Tippen, Rückgängig, Speichern); bis dahin schreibgeschützt | Playwright |

---

## Phase 0.7 – Erweiterbarkeit

| Nr. | Schritt | Inhalt | Prüfen |
|---|---|---|---|
| S49 | Plugin-Protokoll | JSON-RPC über stdin/stdout, `describe`, `action.execute` | Beispiel-Plugin in Go |
| S50 | Plugin-Transforms | `transform.open/batch/close` (Streaming) | Beispiel-Plugin |
| S51 | Go-SDK | `pkg/sdk` mit Beispielen | Beispiel-Plugin baut nur mit SDK |
| S52 | Plugin-Verwaltung | Plugins im Editor anzeigen, Fehler beim Laden sichtbar | Manuell |
| S53 | MCP-Server (vormals S11a) | `runnel mcp` (stdio): Workflows eines Projekts als MCP-Tools; Eingabe-Schema aus `parameters`, Ergebnis aus `outputs`, Log als Fortschrittsmeldungen ([ADR 0012](adr/0012-ki-faehigkeit.md)) | MCP Inspector; Claude Desktop/Claude Code ruft `examples/hello` auf |

---

## Backlog

Werden bei Bedarf als eigener Schritt (je ein PR) eingeplant. Reihenfolge ist offen.

### Zurückgestellt (Tiefe vor Breite)

Geplante Schritte, die vorerst hinter dem Ziel „erst in die Tiefe, dann in die Breite“ zurückstehen. Sie behalten ihre
Nummer und ihren Inhalt; Abhängigkeiten sind angegeben.

| Nr. | Schritt | Inhalt | Hängt ab von / Hinweis |
|---|---|---|---|
| S10 | HTTP-Action | `http.request` (Methoden, Header, Body, Timeout, Statuscode → Ergebnis); Tests gegen lokalen Testserver | – |
| S26 | Run-Historie | Ersetzt den Speicher aus E3 ohne API-Änderung; SQLite-Store mit Migrationen, Tabellen `runs`/`node_runs`/Link-Zustände; `runnel runs` listet Läufe | bis dahin die letzten 100 Läufe im Speicher (dazu 100 von HTTP-Triggern, seit S38b2), nach einem Neustart weg |
| S29 | Resume | Fortsetzen ab fehlgeschlagener Action (`/runs/{id}/resume`, CLI `runnel resume`); Voraussetzung für `core.approval` (ADR 0012) | S26 |
| S36 | Teilausführung | „Ab hier ausführen“, „Nur diese Action ausführen“ | – |
| S16 (Teil) | `core.calculate` | Felder per Ausdruck berechnen | S08 |
| S24 (Teil) | Ergebnis-Datensätze auslagern | `resultSpillRows`: große Ergebnisse in eine temporäre Datei statt in den Speicher | S24a |
| S17 (Teil) | `maxErrors` | Schwelle für Fehler-Links: mehr als n fehlerhafte Datensätze brechen die Pipeline ab | S17a |
| S19 | JSON | `json.input`, `json.output` (JSON und NDJSON), mit „Felder ermitteln“ (S46) | – |
| S20 | Connections & DB-Eingabe | Connection-Metadaten, Passwort zunächst über `${env:…}`; Treiber SQL Server, PostgreSQL, SQLite; `db.table-input`, Action `db.execute` | – |
| S21 | DB-Ausgabe | `db.table-output` (Insert, `commitSize`), `db.upsert` | S20 |
| S22 (Teil) | `core.dedupe` | Duplikate entfernen (zunächst im Speicher) | – |
| S23 | Lookups & Join | Info-Link, `core.stream-lookup`, `db.lookup`, `core.merge-join` | `db.lookup`: S20; danach Info-Links im Pipeline-Canvas |
| S25 | Executor & dynamische Ziele | `core.pipeline-executor`, `core.workflow-executor` (pro Datensatz/Gruppe), Pfade mit Variablen, `allowedTargets` | S18, S24 |
| S45 | Samples | Erste N Records pro Link je Lauf speichern, in Historie anzeigen | S26 |
| S39 | Secret-Store | Verschlüsselung (AES-GCM), Schlüssel per DPAPI bzw. Master-Key, `${secret:…}`, Maskierung in Logs | bis dahin Umgebungsvariablen (`${env:…}`) |
| S40b | Netzwerkbetrieb des Editors | `--listen` für Editor und API, gemeinsamer Zugangsschlüssel, Client-Infos im Protokoll ([ADR 0005](adr/0005-mehrbenutzerbetrieb.md)) | S40a (TLS); bis dahin Editor nur auf `127.0.0.1` |
| S38 (Teil) | HMAC für HTTP-Trigger | Signaturprüfung als weitere `auth`-Variante (z. B. GitHub, Shop-Systeme) | S38a |
| S42 | Run-Konfigurationen | `RunConfig type: local`, Auswahl per CLI/Trigger/Action/Editor | – |
| S41 (Teil) | Dienstverwaltung im Editor | Dienste (`runnel service`) im Editor anzeigen und steuern: Zustand, Konto, Starttyp, Port; Starten, Stoppen, Neustarten; Server-Log und Einträge der Ereignisanzeige ansehen; Dienst für das geöffnete Projekt einrichten. Offen: Rechte (Steuern und Einrichten brauchen Administratorrechte, der Dienst selbst läuft als LocalService) und ob es nur im Desktop-Modus angeboten wird | S41; Mockup vorab; bei Zugriff über das Netz S40b |
| S43 | Wartung & Monitoring | Error-Workflow, `/health`, optional `/metrics`, Aufbewahrung der Historie (Retention) | Retention: S26 |
| S37 (Teil) | Run-Queue | Starts über dem Limit warten (Status `queued`) statt abgelehnt zu werden, Prioritäten (manuell > HTTP > zeitgesteuert), `overlap: queue` | S37 |
| S37 (Teil) | Verpasste Läufe nachholen | `catchUp` für Cron-Trigger: verpasste Zeitpunkte beim Start des Servers nachholen | S26 (letzter Lauf je Trigger) |
| S47 | Desktop-Modus | `runnel desktop` mit WebView2-Fenster; vorher Mockup | – |
| S48 | Installer | MSI (WiX) mit Dienst-Option (ZIP und `THIRD_PARTY_NOTICES` bereits mit S01a) | bis dahin ZIP |

### Weitere Bausteine

| Bereich | Bausteine |
|---|---|
| Kommunikation | `mail.send` (SMTP), Mail abrufen (IMAP) |
| Skripte & Programme | `shell.powershell` (Action + Transform), Programm starten, JavaScript (`core.script` als Action und Transform, mit goja) |
| Dateiformate | Excel (Ein-/Ausgabe), XML, Fixed-Width |
| Dateitransfer | SFTP/FTP Upload/Download, ZIP/Entpacken |
| Netzwerk-Transforms | REST-Eingabe mit Paging, HTTP-Lookup, REST-Ausgabe |
| Datenbanken | Treiber MySQL/MariaDB, Oracle, ODBC |
| Editor | Schmale Fenster (einklappbarer Explorer, Tablet/Laptop) – der Editor zielt zunächst auf Desktop-PCs; Einstellungsmenü, sobald es mehrere Einstellungen gibt |
| Transforms | Pivot/Unpivot, Feld teilen, Wertzuordnung, String-/Datum-Funktionen als eigene Transforms, Sequenz, externes Sortieren (große Datenmengen) |
| KI ([ADR 0012](adr/0012-ki-faehigkeit.md)) | Connections für Modell-Anbieter (Anthropic, OpenAI-kompatibel, Ollama); `ai.chat` (inkl. strukturierter Ausgabe per JSON Schema); `ai.agent` mit Werkzeugen (Workflows, Actions, MCP-Tools) und Positivliste; `mcp.call-tool` und MCP-Server als Plugin-Quelle; `core.approval` (Freigabe abwarten); Chat-Trigger mit Streaming und Gesprächsgedächtnis; Transforms `ai.chunk`, `ai.embed`, Vektor-Ausgabe (pgvector, Qdrant); MCP über HTTP (Token, später OAuth); Token-/Kosten-Erfassung und Rate-Limit je Connection; Link-Typ `tool` im Editor |
| Plugin-SDKs | Python, C#/.NET, Node |
| Später (Konzept) | Mappings, Remote-Runner (`RunConfig type: remote`), Git-Integration im Editor, Breakpoints, Import aus Apache Hop, Tray-Icon, `runnel update`, `winget`, Code-Signing |

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
