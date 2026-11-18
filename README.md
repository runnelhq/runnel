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

Konzept und technische Architektur: [docs/KONZEPT.md](docs/KONZEPT.md) · Roadmap: [docs/ROADMAP.md](docs/ROADMAP.md) · Entscheidungen: [docs/adr](docs/adr/README.md) · Corporate Design: [design/](design/README.md)

## Verwendung

Noch im Aufbau – bisher können Workflows geprüft, aber noch nicht ausgeführt werden.

```bash
runnel validate examples/hello
```

`runnel validate [pfad...]` prüft Workflow-Dateien (`*.workflow.yaml`, Ordner werden rekursiv durchsucht) auf
Syntax- und Feldfehler, Links auf unbekannte Actions, Zyklen ohne `loop: true`, unerreichbare Actions und nicht
definierte Variablen. Das zugehörige Projekt (`project.yaml` im Ordner oder darüber) und seine Environments werden
mitgeprüft; Variablen gegen jedes Environment oder nur gegen eines (`--env prod`). Mit `--param KEY=VALUE` lassen
sich Startparameter angeben. Meldungen erscheinen als `datei:zeile:spalte: error|warning: text`; bei Fehlern ist der
Exit-Code 1 (mit `--strict` auch bei Warnungen). Beispiele liegen unter [`examples/`](examples/).

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
