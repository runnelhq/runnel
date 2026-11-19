<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="design/logo/runnel-logo-inverse.svg">
    <img src="design/logo/runnel-logo.svg" alt="Runnel" height="64">
  </picture>
</p>

<p align="center"><strong>Leicht. Lokal. Im Fluss.</strong></p>

# Runnel

Leichtgewichtige Workflow- und Datenintegrations-Plattform für Windows – eine Alternative zu n8n, Make & Co.,
inspiriert vom Modell von Apache Hop: Workflows und Pipelines aus Actions bzw. Transforms, verbunden durch Links.

- **Editor** – visuelles Designen von Workflows und Pipelines im Browser bzw. WebView2-Fenster
- **Runner** – Ausführung per CLI, als Server mit Scheduler oder als Windows-Dienst
- **Eine `runnel.exe`**, keine Runtime, keine externe Datenbank, Definitionen als YAML (Git-freundlich)

Konzept und technische Architektur: [docs/KONZEPT.md](docs/KONZEPT.md) · Roadmap: [docs/ROADMAP.md](docs/ROADMAP.md) · Testplan: [docs/TESTPLAN.md](docs/TESTPLAN.md) · Mitarbeiten: [CONTRIBUTING.md](CONTRIBUTING.md) · Entscheidungen: [docs/adr](docs/adr/README.md) · Corporate Design: [design/](design/README.md)

## Verwendung

Noch im Aufbau – Workflows lassen sich prüfen und mit den Basis-Actions (`core.start`, `core.log`, `core.wait`,
`core.success`, `core.abort`, `core.dummy`) ausführen; weitere Actions folgen laut [Roadmap](docs/ROADMAP.md).

```bash
runnel run --env prod examples/hello/workflows/hello.workflow.yaml
```

```
23:28:01.493 INFO  workflow "hello" started (environment prod), run 01M7RFT6ENA57HPBJSHRWN0YV6
23:28:01.493 INFO  [greet] Hello, Production! (production on SRV01, files in \\fileserver\exchange\runnel-hello)
23:28:02.494 INFO  workflow "hello" succeeded in 1s
```

`runnel run <datei>` führt einen Workflow aus: `--env` wählt das Environment des Projekts, `--param KEY=VALUE` setzt
Parameter, `-v` zeigt zusätzlich Debug-Meldungen und jeden Action-Start. Exit-Code: `0` erfolgreich, `1` fehlgeschlagen,
`2` nicht gestartet (ungültiger Workflow oder Aufruf), `130` abgebrochen – passend für die Windows-Aufgabenplanung.
Strg+C bricht einen Lauf sauber ab (laufende Actions werden gestoppt); ein zweites Strg+C beendet sofort.

```bash
runnel validate examples/hello
```

`runnel validate [pfad...]` prüft Workflow-Dateien (`*.workflow.yaml`, Ordner werden rekursiv durchsucht) auf
Syntax- und Feldfehler, Links auf unbekannte Actions, Zyklen ohne `loop: true`, unerreichbare Actions, nicht
definierte Variablen sowie – wie vor einem Lauf – unbekannte Action-Typen und ungültige Konfigurationen (`with:`). Das zugehörige Projekt (`project.yaml` im Ordner oder darüber) und seine Environments werden
mitgeprüft; Variablen gegen jedes Environment oder nur gegen eines (`--env prod`). Mit `--param KEY=VALUE` lassen
sich Startparameter angeben. Meldungen erscheinen als `datei:zeile:spalte: error|warning: text`; bei Fehlern ist der
Exit-Code 1 (mit `--strict` auch bei Warnungen). Beispiele liegen unter [`examples/`](examples/).

```bash
runnel serve examples/hello
```

`runnel serve [projektordner]` startet den Server für ein Projekt unter `http://127.0.0.1:7300` (anderer Port mit
`--port`). Er ist nur vom eigenen Rechner aus erreichbar und bietet die API für den Editor: Projektbaum, Dateien lesen
und speichern mit Konfliktschutz, Katalog der Actions und Prüfung von Workflows – beschrieben in
[docs/dev/API.md](docs/dev/API.md). Der Editor selbst folgt mit den nächsten Schritten.

```bash
runnel catalog core.wait
```

`runnel catalog` listet die verfügbaren Actions nach Kategorien, `runnel catalog <typ>` beschreibt eine Action mit
ihren Feldern (Typ, Pflichtfeld, erlaubte Werte, Standardwert, Beispiel). `--json` gibt die Descriptors mit
JSON Schema aus – dieselben Daten, aus denen der Editor Palette und Eigenschaftsdialog erzeugt.

## Entwicklung

Voraussetzung: [Go](https://go.dev/dl/) (Version laut `go.mod`). Für `make lint` zusätzlich
[golangci-lint](https://golangci-lint.run/) v2.

| Aufgabe | Mit `make` | Ohne `make` (z. B. Windows/PowerShell) |
|---|---|---|
| Bauen | `make build` → `bin/runnel` | `go build -o bin/runnel.exe ./cmd/runnel` |
| Windows-/Linux-Binaries | `make dist` | `$env:GOOS="windows"; go build -o bin/runnel-windows-amd64.exe ./cmd/runnel` |
| Tests | `make test` (mit Race-Detector) | `go test ./...` |
| Lint | `make lint` | `golangci-lint run ./...` |
| Formatieren | `make fmt` | `golangci-lint fmt ./...` |
| Fuzz-Tests (Parser, Variablen) | `make fuzz` | – |
| Bekannte Sicherheitslücken prüfen | `make vulncheck` | `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...` |
| SBOMs erzeugen | `make sbom` | – |

```bash
bin/runnel version
```

Die CI (`.github/workflows/ci.yml`) führt Lint, `govulncheck`, Tests unter Linux und Windows sowie Build und Packen
(inkl. SBOMs) für beide Plattformen aus – bei jedem PR, auf `main` und wöchentlich, damit neu bekannt gewordene
Sicherheitslücken auffallen.

## Releases

Fertige Builds gibt es unter [Releases](https://github.com/runnelhq/runnel/releases). Je Version:

| Datei | Inhalt |
|---|---|
| `runnel_<version>_windows_amd64.zip` | `runnel.exe`, `LICENSE`, `README.md`, `THIRD_PARTY_NOTICES.txt` |
| `runnel_<version>_linux_amd64.tar.gz` | `runnel` und dieselben Begleitdateien |
| `runnel_<version>_<os>_amd64.cdx.json` | SBOM (Software-Stückliste) im Format CycloneDX 1.6 |
| `SHA256SUMS` | Prüfsummen aller Dateien |

### Release erstellen

**Über GitHub (ohne lokales Git):** *Actions → Release → Run workflow*, Branch `main` lassen, Version eintragen
(z. B. `v0.1.0`) und starten. Der Workflow testet, baut, erzeugt das Tag und veröffentlicht das Release.

**Per Tag:**

```bash
git tag -a v0.1.0 -m "Runnel v0.1.0"
```

```bash
git push origin v0.1.0
```

Versionen mit Bindestrich (z. B. `v0.1.0-rc.1`) werden als Vorabversion veröffentlicht. Lokal lassen sich die Archive
mit `make package VERSION=v0.1.0` erzeugen (Ausgabe in `dist/`).

### Herkunft prüfen

Jedes Release-Archiv ist mit einer signierten Herkunftsbestätigung (SLSA Build Provenance) und seiner SBOM verknüpft.
Prüfen mit der [GitHub CLI](https://cli.github.com/):

```bash
gh attestation verify runnel_0.1.0_windows_amd64.zip --repo runnelhq/runnel
```

## Lizenz

[MIT](LICENSE)
