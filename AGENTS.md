# AGENTS.md – Hinweise für KI-Agenten

Kompakte Projektregeln für KI-Coding-Agenten (Claude Code, Copilot, Codex u. a.). Für Menschen gelten dieselben Regeln,
ausführlicher in [CONTRIBUTING.md](CONTRIBUTING.md).

## Projekt

**Runnel** – leichtgewichtige Workflow- und Datenintegrations-Plattform für Windows (Alternative zu n8n/Make, Modell
nach Apache Hop): eine `runnel.exe` in Go, Workflows/Pipelines als YAML. Konzept: [docs/KONZEPT.md](docs/KONZEPT.md),
Entscheidungen: [docs/adr/](docs/adr/README.md), Plan: [docs/ROADMAP.md](docs/ROADMAP.md).

## Befehle

| Zweck | Befehl |
|---|---|
| Bauen | `make build` → `bin/runnel` (mit Editor; Node 22 nötig) · `make build-go` (nur Go) |
| Tests (mit Race-Detector) | `make test` |
| Lint (golangci-lint v2.15.0) | `make lint` |
| Formatieren | `make fmt` (oder `gofmt -w`) |
| Fuzzing | `make fuzz FUZZTIME=1m` |
| Editor | `make web` (Build) · `make e2e` (Playwright) · in `web/`: `npm run dev`, `npm run typecheck` |
| Golden-Dateien neu erzeugen | `go test ./internal/<paket> -update` – danach Diff prüfen |
| Ausprobieren | `bin/runnel validate examples` · `bin/runnel run -v examples/hello/workflows/hello.workflow.yaml` · `bin/runnel catalog core.log` · `bin/runnel serve examples/hello` |

Vor jedem Commit: `make lint` und `make test` müssen grün sein; bei Änderungen am Editor zusätzlich `make e2e`.

## Struktur

| Pfad | Inhalt |
|---|---|
| `cmd/runnel/` | Einstiegspunkt, Signalbehandlung |
| `internal/cli/` | Befehle (`version`, `validate`, `run`, `catalog`, `serve`) |
| `internal/model/` | Dateiformate (Workflow, Project, Environment), Laden, Feld- und Graph-Prüfung, Namensräume |
| `internal/vars/` | `${…}`-Variablen und Ebenen |
| `internal/schema/` | JSON Schema aus Konfigurations-Structs (Tags `json`, `doc`, `schema`; ADR 0014) |
| `internal/engine/workflow/` | Workflow-Engine (`engine.go`: Prepare/Run, `scheduler.go`: Parallelität, Join, Timeouts) |
| `internal/server/` | HTTP-API für den Editor (`runnel serve`), Datei-API mit ETag, Pfadprüfung, Läufe, Server-Sent Events; Doku in `docs/dev/API.md` |
| `internal/runs/` | Läufe vorbereiten (gemeinsam für `runnel run` und Server), Läufe und Ereignisse im Speicher |
| `internal/webui/` | eingebetteter Editor-Build (`go:embed`; `dist/` nicht im Repository) |
| `internal/action/` | Schnittstelle Engine ↔ Actions, Registry, Descriptors |
| `internal/actions/` | eingebaute Actions: `actions.go` (`Builtin()`), je Namensraum ein Paket, eine Datei je Action |
| `web/` | Editor (React, Vite, TypeScript, Tailwind); Anleitung `docs/dev/EDITOR.md` |
| `examples/` | Beispiele, in Tests mitgeprüft |
| `docs/` | Konzept, Roadmap, ADRs, Testplan, Entwickleranleitungen (`docs/dev/`) |

## Konventionen

- **Sprache:** Code, Kommentare, CLI-Ausgaben, Fehlermeldungen und alle Texte der Oberfläche **englisch** (ADR 0015); Dokumentation, ADRs, PR-Beschreibungen **deutsch**.
- **Neue Action:** genau nach [docs/dev/ACTIONS.md](docs/dev/ACTIONS.md) (eine Datei je Action, `register.go`, Vertragstest).
- **Typnamen:** `<namensraum>.<name>`; Actions und Transforms haben getrennte Namensräume, **kein** `-transform`-Suffix (ADR 0013).
- **Fehlermeldungen in Dateien** mit Position: `datei:zeile:spalte: error|warning: text`; für Anwender verständlich, ohne Go-Typnamen.
- **Tests:** Golden-Files für Formate und Ausgaben, `t.TempDir()` statt echter Pfade, plattformunabhängig (Linux und Windows in CI).
- **Windows-first:** Pfade über `path/filepath`, UNC-Pfade, Kodierungen (BOM, UTF-16), CRLF berücksichtigen.
- **Kein Vorgriff:** nur umsetzen, was der aktuelle Roadmap-Schritt verlangt; Nicht-Unterstütztes klar melden statt still ignorieren.

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

- **Ein Roadmap-Schritt = ein PR**, Richtwert ≤ ca. 1.000 geänderte Zeilen inklusive Tests; größere Schritte vorher teilen.
- **Doku im selben PR:** betroffene Stellen in Konzept, Roadmap (Schritt mit ✅ markieren), ADRs, README, `docs/dev/`.
- **Grundsätzliche Entscheidungen** (Formate, Semantik, Abhängigkeiten, Namensregeln) als neue ADR in `docs/adr/`
  (nächste freie Nummer) und in `docs/adr/README.md` eintragen – vorher mit dem Maintainer abstimmen.
- **Neue Abhängigkeiten** nur mit Begründung im PR; Lizenz muss MIT-kompatibel sein (ADR 0004); Versionen fest pinnen.
- **Nicht tun:** Tests überspringen oder abschwächen, um CI grün zu bekommen; Golden-Dateien ohne Prüfung überschreiben;
  Secrets in Code, Tests oder Logs; Änderungen an Release-/CI-Workflows ohne Auftrag.
