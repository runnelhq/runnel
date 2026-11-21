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
| Golden-Dateien neu erzeugen | `go test ./internal/<paket> -update` – nur das betroffene Paket, nie `./... -update`; danach Diff prüfen (Pakete mit Golden-Dateien: `cli`, `model`, `schema`, `server`, `transforms/csv`) |
| Ausprobieren | `bin/runnel validate examples` · `bin/runnel run -v examples/hello/workflows/hello.workflow.yaml` · `bin/runnel run examples/pipelines/pipelines/numbers.pipeline.yaml` · `bin/runnel catalog core.log` · `bin/runnel triggers examples/hello` · `bin/runnel serve examples/hello` |

**Vor jedem Push:** `make lint` und `make test` müssen grün sein. Bei Änderungen am Editor zusätzlich in `web/`
`npm run typecheck` und die betroffenen Playwright-Tests (`npx playwright test e2e/<datei>`); vollständig prüft die CI.
Ist das lokale golangci-lint zu alt für die Go-Version (z. B. in Agent-Umgebungen), mit
`go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run ./...` (für Windows-Code mit `GOOS=windows`).
Temporäre Dateien, Profile und Test-Binaries nicht im Repository ablegen (z. B. `go test -c -o bin/…`).

## Struktur

| Pfad | Inhalt |
|---|---|
| `cmd/runnel/` | Einstiegspunkt, Signalbehandlung |
| `internal/cli/` | Befehle (`version`, `validate`, `run`, `catalog`, `serve`, `triggers`, `service`) |
| `internal/model/` | Dateiformate (Workflow, Pipeline, Project, Environment, Trigger), Laden, Feld- und Graph-Prüfung, Namensräume |
| `internal/vars/` | `${…}`-Variablen und Ebenen |
| `internal/expr/` | Ausdrücke `{{ … }}` (expr-lang), Funktionsbibliothek, Dokumentation für den Editor |
| `internal/format/` | Zahlen- und Datumsformate (z. B. `#.##0,00`, `dd.MM.yyyy`), Typvorschlag für „Felder ermitteln“ (`propose.go`) |
| `internal/schema/` | JSON Schema aus Konfigurations-Structs (Tags `json`, `doc`, `schema`; ADR 0014) |
| `internal/engine/workflow/` | Workflow-Engine (`engine.go`: Prepare/Run, `scheduler.go`: Parallelität, Join, Timeouts) |
| `internal/engine/runid/` | Lauf-IDs (ULID) |
| `internal/server/` | HTTP-API für den Editor (`runnel serve`), Datei-API mit ETag, Pfadprüfung, Läufe, Server-Sent Events; Doku in `docs/dev/API.md` |
| `internal/triggers/` | Trigger: Scheduler im Server (Zeitpunkte, Intervalle, Ordner-Überwachung in `files.go`, HTTP-Trigger mit eigenem Listener in `http.go` und Antworten in `respond.go`, `overlap`, Neuladen geänderter Dateien), Prüfung gegen das Projekt in `check.go` |
| `internal/winsvc/` | Windows-Dienst: Installieren, Steuern, Ausführen, Ereignisanzeige (`*_windows.go`; sonst `ErrNotSupported`) |
| `internal/logfile/` | Log-Datei mit Rotation nach Größe (Dienst) |
| `internal/httpx/` | gemeinsame HTTP-Helfer von Server und Trigger-Listener (`IsLoopback`, JSON-Antworten) |
| `internal/tlscert/` | Zertifikate des Trigger-Listeners: selbst erzeugt und wiederverwendet, eigene PEM-Dateien mit Neuladen |
| `internal/runs/` | Läufe vorbereiten (gemeinsam für `runnel run` und Server), Parameter prüfen, Prüfung für `validate` und Editor (`validate.go`), Texte der Lauf-Ereignisse (`describe.go`), Zwischenspeicher geprüfter Dateien, Läufe und Ereignisse im Speicher |
| `internal/webui/` | eingebetteter Editor-Build (`go:embed`; `dist/` nicht im Repository) |
| `internal/version/` | Versionsinformationen (`runnel version`, `/api/v1/version`) |
| `internal/action/` | Schnittstelle Engine ↔ Actions, Registry, Descriptors |
| `internal/actions/` | eingebaute Actions: `actions.go` (`Builtin()`), je Namensraum ein Paket, eine Datei je Action |
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
- **Doku im selben PR:** betroffene Stellen in Konzept, Roadmap (Schritt mit ✅ markieren), ADRs, README, `docs/dev/`.
  Neue Pakete, Befehle oder Make-Ziele → Struktur- und Befehlstabelle in dieser Datei.
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
