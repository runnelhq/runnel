# AGENTS.md – Hinweise für KI-Agenten

Kompakte Projektregeln für KI-Coding-Agenten (Claude Code, Copilot, Codex u. a.) und Menschen. **Diese Datei ist die
einzige verbindliche Quelle der Regeln;** [CONTRIBUTING.md](CONTRIBUTING.md) beschreibt den Ablauf, die
[Roadmap](docs/ROADMAP.md) nur Schritt-spezifisches – beide verweisen hierher.

## Projekt

**Runnel** – leichtgewichtige Workflow- und Datenintegrations-Plattform für Windows (Alternative zu n8n/Make, Modell
nach Apache Hop): eine `runnel.exe` in Go, Workflows/Pipelines als YAML. Konzept: [docs/KONZEPT.md](docs/KONZEPT.md),
Entscheidungen: [docs/adr/](docs/adr/README.md), Plan: [docs/ROADMAP.md](docs/ROADMAP.md).

## Befehle

| Zweck | Befehl |
|---|---|
| Bauen | `make build` → `bin/runnel` (mit Editor; Node 22 nötig) · `make build-go` (nur Go) |
| Tests (mit Race-Detector) | `make test` |
| Lint (golangci-lint v2.15.0) | `make lint` (prüft Linux- und Windows-Code, `GOOS=windows`) |
| Formatieren | `make fmt` (oder `golangci-lint fmt ./...`) |
| Fuzzing | `make fuzz FUZZTIME=1m` |
| Benchmarks | `make bench` |
| Editor | `make web` (Build) · `make e2e` (Playwright) · in `web/`: `npm run dev`, `npm run typecheck` |
| Golden-Dateien neu erzeugen | `go test ./internal/<paket> -update` – nur das betroffene Paket, nie `./... -update`; danach Diff prüfen (Pakete mit Golden-Dateien: `cli`, `model`, `scaffold`, `schema`, `server`, `transforms/csv`); die Basis des Vertragstests in `internal/cli/testdata/contract/` ändert nur der Release ([docs/dev/RELEASE.md](docs/dev/RELEASE.md)) |
| Ausprobieren | `bin/runnel init demo` (Ordner außerhalb des Repositorys, z. B. unter `%TEMP%`) · `bin/runnel validate examples` · `bin/runnel run -v examples/hello/workflows/hello.workflow.yaml` · `bin/runnel run examples/pipelines/pipelines/numbers.pipeline.yaml` · `bin/runnel catalog core.log` · `bin/runnel catalog --expressions` · `bin/runnel schema trigger` · `bin/runnel triggers examples/hello` · `bin/runnel runs examples/hello` · `bin/runnel plugins examples/hello` · `bin/runnel serve examples/hello` · `bin/runnel health` (bei laufendem Server) · `RUNNEL_MASTER_KEY=$(openssl rand -base64 32) bin/runnel secret set NAME demo` (Wert über stdin), `bin/runnel secret list demo`, `bin/runnel secret delete NAME demo` |

**Vor jedem Push:** `make lint` und `make test` müssen grün sein. Bei Änderungen am Editor zusätzlich in `web/`
`npm run typecheck` und die betroffenen Playwright-Tests (`npx playwright test e2e/<datei>`); vollständig prüft die CI.
Ist das lokale golangci-lint zu alt für die Go-Version (z. B. in Agent-Umgebungen), mit
`go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run ./...` (für Windows-Code mit `GOOS=windows`).
Temporäre Dateien, Profile und Test-Binaries nicht im Repository ablegen (z. B. `go test -c -o bin/…`).

## Struktur

| Pfad | Inhalt |
|---|---|
| `cmd/runnel/` | Einstiegspunkt, Signalbehandlung |
| `internal/cli/` | Befehle (`version`, `init`, `validate`, `run`, `runs`, `catalog`, `schema`, `serve`, `triggers`, `service`, `plugins`, `health`, `secret`) |
| `internal/model/` | Dateiformate (Workflow, Pipeline, Project, Environment, Trigger, Connection, Plugin), Laden, Feld- und Graph-Prüfung, Namensräume |
| `internal/vars/` | `${…}`-Variablen und Ebenen |
| `internal/paths/` | Relative Pfade von Actions und Transforms auflösen: Bezugsordner eines Laufs im Kontext (Projektordner, ADR 0020) |
| `internal/expr/` | Ausdrücke `{{ … }}` (expr-lang), Funktionsbibliothek, Dokumentation für den Editor |
| `internal/format/` | Zahlen- und Datumsformate (z. B. `#.##0,00`, `dd.MM.yyyy`), Typvorschlag für „Felder ermitteln“ (`propose.go`) |
| `internal/schema/` | JSON Schema aus Konfigurations-Structs (Tags `json`, `doc`, `schema`; ADR 0014) und aus den Dateiformaten in `internal/model` (`ForFile`, Namen aus `yaml`; S58) |
| `internal/engine/workflow/` | Workflow-Engine (`engine.go`: Prepare/Run, `scheduler.go`: Parallelität, Join, Timeouts) |
| `internal/engine/runid/` | Lauf-IDs (ULID) |
| `internal/server/` | HTTP-API für den Editor (`runnel serve`), Ordner ohne Projekt mit Startseite und Wechsel ins neue Projekt (`host.go`, R10), Datei-API mit ETag, Pfadprüfung, Läufe, Server-Sent Events, Zustand für Monitoring (`health.go`, freier Platz je Betriebssystem in `diskfree_*.go`); Doku in `docs/dev/API.md` |
| `internal/triggers/` | Trigger: Scheduler im Server (Zeitpunkte, Intervalle, Ordner-Überwachung in `files.go`, HTTP-Trigger mit eigenem Listener in `http.go` und Antworten in `respond.go`, `overlap`, Neuladen geänderter Dateien), Prüfung gegen das Projekt in `check.go` |
| `internal/winsvc/` | Windows-Dienst: Installieren, Steuern, Ausführen, Ereignisanzeige (`*_windows.go`; sonst `ErrNotSupported`) |
| `internal/proc/` | Programme starten für `shell.*` (ADR 0023): ohne Shell, Timeout mit Prozessbaum (Job Object/Prozessgruppe), Grenzen der Ausgabe, Kodierung, Prüfung von Batch-Argumenten |
| `internal/script/` | JavaScript für `core.script` (ADR 0028) mit goja: Laufzeit ohne Zugriff auf den Rechner, Werte Go ↔ JS, `console.*` mit Grenzen, `require` nur aus `scripts/` des Projekts, Abbruch über den Kontext, Sitzung für den Transform (`session.go`: Zeitlimit je Aufruf, Rückgabe als Datensätze) |
| `internal/xlsx/` | Excel-Arbeitsmappen (`.xlsx`) lesen für `excel.input` (S67a): Blätter, Zeilen als Strom mit Zelltypen und Datumsformaten, Grenzen für entpackte Teile (Schreiben: `excel.output` mit `excelize`, S67b) |
| `internal/textenc/` | Kodierung von Eingabedateien (UTF-8 mit/ohne BOM, UTF-16, Windows-1252) für `csv.input` und `json.input` |
| `internal/logfile/` | Log-Datei mit Rotation nach Größe und mehreren alten Dateien (Dienst), Handler für das Server-Log im Format `text` oder `json` (S71) |
| `internal/secretstore/` | Lokaler Secret-Store, Quelle `local` (S39d1, ADR 0022): eine JSON-Datei je Projekt im Ordner des Benutzers, Werte einzeln mit AES-256-GCM (an Projekt und Namen gebunden), Schlüssel aus `RUNNEL_MASTER_KEY`, Store des Prozesses (`Default`, in Tests über `SetDefault`) |
| `internal/oauth/` | Tokens nach OAuth 2.0 Client Credentials für Connections (z. B. `smtp` mit Office 365): Abruf, Zwischenspeicher bis kurz vor Ablauf |
| `internal/pagination/` | Seiten einer HTTP-API lesen (`link`, `url`, `cursor`, `page`, `offset`): Liste je Seite, nächste Adresse, Grenzen, gleicher Host (S63c; für `http.request`, später `http.input`) |
| `internal/httpx/` | gemeinsame HTTP-Helfer von Server und Trigger-Listener (`IsLoopback`, JSON-Antworten) |
| `internal/tlscert/` | Zertifikate des Trigger-Listeners: selbst erzeugt und wiederverwendet, eigene PEM-Dateien mit Neuladen |
| `internal/runs/` | Läufe vorbereiten (gemeinsam für `runnel run` und Server), Parameter prüfen, Prüfung für `validate` und Editor (`validate.go`), Texte der Lauf-Ereignisse (`describe.go`), Zwischenspeicher geprüfter Dateien, Läufe und Ereignisse im Speicher |
| `internal/jsonrpc/` | JSON-RPC 2.0 über zwei Ströme, eine Nachricht je Zeile (Plugins, später MCP): Anfragen mit Zuordnung der Antworten, Notifications, `$/cancelRequest`, Grenze 16 MiB |
| `internal/plugin/` | Plugins ausführen (ADR 0019): Prozess mit Prozessbaum, Handshake, `describe`, `action.execute`, Transforms als Strom mit Krediten (`stream.go`), stderr als Log, Maskierung; Test-Plugin in `plugintest/` |
| `internal/plugintrust/` | Freigabe von Plugins je Projekt und Rechner (ADR 0019): Hash über den Plugin-Ordner, Dateien der Freigaben für Benutzer und Dienst |
| `internal/history/` | Run-Historie (ADR 0026): SQLite-Datei je Projekt im Datenordner, Migrationen, Läufe, Status je Knoten, Log, Lebenszeichen und abgebrochene Läufe; geschrieben vom Manager in `internal/runs/record.go` |
| `internal/webui/` | eingebetteter Editor-Build (`go:embed`; `dist/` nicht im Repository) |
| `internal/scaffold/` | Neues Projekt anlegen (`runnel init`, R9; der Editor über `POST /api/v1/project`, R10): Dateien und Ordner, Name aus dem Ordner, nicht leere Ordner ablehnen, vorhandene Dateien nie überschreiben |
| `internal/version/` | Versionsinformationen (`runnel version`, `/api/v1/version`) |
| `internal/action/` | Schnittstelle Engine ↔ Actions, Registry, Descriptors |
| `internal/actions/` | eingebaute Actions: `actions.go` (`Builtin()`), je Namensraum ein Paket, eine Datei je Action |
| `internal/connection/` | Schnittstelle zu den Connection-Typen (ADR 0022): Registry mit Schema je Typ, aufgelöste Connection (`Resolved`) |
| `internal/connections/` | eingebaute Connection-Typen: `connections.go` (`Builtin()`), eine Datei je Typ (z. B. `sqlite.go`) |
| `internal/conntest/` | Connections testen für den Editor (S39c1): Datenbanken Ping und Version, `http`, `smtp`, `sftp`, `ftps` und `ftp` über `Check` der Actions; nichts wird geschrieben oder gesendet |
| `internal/sqldb/` | Datenbanken der Connections öffnen (Pool je Datenbank), Parameter `@name` im SQL erkennen und übergeben |
| `internal/engine/pipeline/` | Pipeline-Engine: Goroutine je Transform, gepufferte Kanäle, Batches, Zähler |
| `internal/record/` | Record: geordnete Felder (ADR 0007) |
| `internal/transform/` | Schnittstelle Engine ↔ Transforms, Registry, Descriptors |
| `internal/transforms/` | eingebaute Transforms: `transforms.go` (`Builtin()`), Aufbau wie `internal/actions/` |
| `web/` | Editor (React, Vite, TypeScript, Tailwind); Anleitung `docs/dev/EDITOR.md` |
| `examples/` | Beispiele, in Tests mitgeprüft |
| `docs/` | Konzept, Roadmap, ADRs, Testplan, Entwickleranleitungen (`docs/dev/`) |

## Konventionen

- **Sprache:** Code, Kommentare, CLI-Ausgaben, Fehlermeldungen und alle Texte der Oberfläche **englisch** (ADR 0015); Dokumentation, ADRs, PR-Beschreibungen **deutsch**.
- **Neue Action:** genau nach [docs/dev/ACTIONS.md](docs/dev/ACTIONS.md) (eine Datei je Action, `register.go`, Vertragstest).
- **Neuer Transform:** nach [docs/dev/TRANSFORMS.md](docs/dev/TRANSFORMS.md) (ergänzt ACTIONS.md um `Process`, Records, Backpressure).
- **Typnamen:** `<namensraum>.<name>`; Actions und Transforms haben getrennte Namensräume, **kein** `-transform`-Suffix (ADR 0013).
- **Fehlermeldungen in Dateien** mit Position: `datei:zeile:spalte: error|warning: text`; für Anwender verständlich, ohne Go-Typnamen.
- **Tests:** Golden-Files für Formate und Ausgaben, `t.TempDir()` statt echter Pfade, plattformunabhängig (Linux und Windows in CI).
  Geänderte Golden-Dateien im PR-Text begründen (welche Datei, warum).
- **Fuzzing:** Ein neuer Parser für Dateien oder Netzwerkeingaben bekommt im selben PR ein `Fuzz…`-Ziel, eingetragen in
  `make fuzz` (läuft wöchentlich in der CI).
- **Leistung:** Für Pfade mit Last je Datensatz oder je Anfrage (Engine, Transforms, Server, Trigger) ein Benchmark
  (`Benchmark…`, `-benchmem`) im selben PR; Werte vorher/nachher im PR-Text. Kein CI-Grenzwert, nur Nachweis.
- **Windows-first:** Pfade über `path/filepath`, UNC-Pfade, Kodierungen (BOM, UTF-16), CRLF berücksichtigen.
- **Kein Vorgriff:** nur umsetzen, was der aktuelle Roadmap-Schritt verlangt; Nicht-Unterstütztes klar melden statt still ignorieren.

## Sicherheit

Gilt für alles, was auf Anfragen von außen reagiert (Server, HTTP-Trigger, später Plugins und MCP):

- **Standard nur lokal** (`127.0.0.1`); im Netz nur mit TLS und Authentifizierung – Ausnahmen nur mit ausdrücklichem
  Schalter und Warnung (z. B. `--triggers-insecure`). Anfragen aus Webseiten (`Origin`) und DNS-Rebinding abwehren.
- **Grenzen für jede Eingabe:** Größe (Body, Zeilen, Datensätze im Speicher), Zeit (Timeouts) und gleichzeitige Läufe.
- **Geheimnisse:** Tokens zeitkonstant vergleichen, nie loggen, nie in Dateien des Projekts (nur `${env:…}` bzw. später
  Secret-Store); nicht an Läufe weitergeben, die sie nicht brauchen.
- **Fehlerantworten** ohne interne Details (Pfade, Stacktraces, Go-Typen); die Details stehen im Server-Log.
- **Tests für Ablehnungen** (z. B. `401`, `403`, `413`, Timeout) gehören zum PR.
- Neue Angriffsfläche → Abschnitt „Sicherheit“ in der ADR. Schwachstellen meldet man nach [SECURITY.md](SECURITY.md).

## Oberfläche (UI)

Gilt für alles Sichtbare: Editor, Website, Installer, Tray, Grafiken, Dokumente – und sinngemäß für CLI-Ausgaben.

- **Corporate Design anwenden** ([ADR 0011](docs/adr/0011-corporate-design.md), [design/README.md](design/README.md)):
  Farben, Schriften, Radien und Abstände nur über die Tokens (`design/tokens/`), **keine Hex-Werte** im Code; Logos aus
  `design/logo/`; Funktionsfarben nur mit fester Bedeutung und nie ohne Symbol; Texte nach Kap. 7 (Sprache und Ton).
- **Erst Mockup, dann Umsetzung:** Vor jeder neuen oder deutlich geänderten Oberfläche ein Mockup unter
  `design/mockups/` anlegen (statisches HTML mit `design/tokens/runnel.css`, hell und dunkel, dazu PNG-Screenshots für
  den PR) und als **eigenen PR** vorlegen. Umgesetzt wird erst, wenn der Maintainer das Mockup freigegeben (gemergt) hat.
- **Umsetzung folgt dem Mockup;** Abweichungen erst im Mockup ändern und erneut freigeben lassen. Der Umsetzungs-PR
  verweist auf das freigegebene Mockup.

## Arbeitsweise

- **Arten von PRs:** Schritt (ein Roadmap-Schritt), Mockup (vor einer Oberfläche), ADR, Roadmap/Backlog, Fix,
  Qualität (Doku, Regeln, Refactoring ohne neue Funktion). Jeder PR hat genau eine Art.
- **Ein Roadmap-Schritt = ein PR.** Richtwert: **≤ ca. 800 geänderte Zeilen Produktivcode** (Go, TS/TSX); Tests,
  Golden-Dateien, Doku, Beispiele, Mockups, generierte Dateien und Lockfiles zählen nicht mit. Gesamt-Diff über ca.
  2.000 Zeilen nur nach Absprache. Zeichnet sich ein größerer Schritt ab, wird er **vor** der Umsetzung geteilt
  (`S38b1`, `S38b2` …; Kern vor Erweiterungen, Server vor Editor) und die Teilung in der Roadmap vermerkt.
- **Lauffähig nach jedem PR:** `main` baut, alle Tests sind grün, nichts ist „halb fertig“ freigeschaltet; jeder Schritt
  nennt unter „Prüfen“, wie man das Ergebnis ausprobiert (bei Bedarf mit Beispiel unter `examples/`).
- **Doku im selben PR:** betroffene Stellen in Konzept, Roadmap (Schritt mit ✅ markieren), ADRs, README, `docs/dev/`;
  für Anwender sichtbare Änderungen ein Eintrag in [CHANGELOG.md](CHANGELOG.md) unter „Unveröffentlicht“ (Neu,
  Behoben, Abgekündigt, Bruch; ADR 0029). Neue Pakete, Befehle oder Make-Ziele → Struktur- und Befehlstabelle in
  dieser Datei.
- **Stabilität (ADR 0029):** Ab `v1.0.0` nur additive Änderungen an den Zusagen (Dateiformate, Ausdrücke, CLI mit
  Exit-Codes und `--json`, Plugin-Protokoll, HTTP-Trigger, öffentliche Endpunkte in `docs/dev/API.md`); Umbenennen
  oder Entfernen nur über Abkündigung bis zur nächsten Major-Version. Die PR-Vorlage fragt danach.
- **ADR nötig** bei: neuem Dateityp oder `kind`, geänderter Ausführungssemantik, neuer Laufzeitabhängigkeit, neuer
  Netzwerk- oder Sicherheitsgrenze, Namens- oder Sprachregeln, Abkehr von einer bestehenden ADR. **Nicht nötig** bei
  neuen Actions/Transforms nach bestehenden Regeln, neuen Optionen und Editor-Funktionen nach freigegebenem Mockup.
  Neue ADR: nächste freie Nummer, in `docs/adr/README.md` eintragen, vorher mit dem Maintainer abstimmen. Präzisierungen
  bei der Umsetzung als Nachtrag nach [docs/adr/README.md](docs/adr/README.md#nachträge).
- **Neue Abhängigkeiten** nur mit Begründung im PR; Lizenz muss MIT-kompatibel sein (ADR 0004); Versionen fest pinnen.
- **Manuelle Tests** ([TESTPLAN](docs/TESTPLAN.md)) nur für das, was sich nicht automatisieren lässt (Windows-Dienst,
  Netzlaufwerk, Excel, zweiter Rechner, Browser-Vielfalt); dann mit Begründung im Bereich des Testplans.
- **Roadmap:** Verschobene Teile behalten ihre Nummer mit „(Teil)“ im Backlog. Umbau-Hinweise am Kopf der Roadmap
  wandern nach Abschluss der Phase in den Abschnitt „Verlauf“ am Ende.
- **Commit-Nachrichten und PR-Titel deutsch**, Format `<Art oder Schritt>: <Inhalt>`, z. B. `S38c: …`, `Mockup E7c: …`,
  `ADR 0019: …`, `Backlog: …`, `Fix: …`, `Q-Doku: …`.
- **Nicht tun:** Tests überspringen oder abschwächen, um CI grün zu bekommen; Golden-Dateien ohne Prüfung überschreiben;
  Secrets in Code, Tests oder Logs; Änderungen an Release-/CI-Workflows ohne Auftrag.
