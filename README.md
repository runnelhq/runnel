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

```bash
bin/runnel version
```

Die CI (`.github/workflows/ci.yml`) führt Lint, Tests unter Linux und Windows sowie den Build für beide Plattformen aus.

## Releases

Fertige Builds gibt es unter [Releases](https://github.com/runnelhq/runnel/releases): je Version eine ZIP für Windows,
ein `tar.gz` für Linux (jeweils mit `LICENSE`, `README.md` und `THIRD_PARTY_NOTICES.txt`) sowie `SHA256SUMS`.

Ein Release entsteht automatisch (`.github/workflows/release.yml`), sobald ein Versions-Tag gepusht wird:

```bash
git tag -a v0.1.0 -m "Runnel v0.1.0"
```

```bash
git push origin v0.1.0
```

Tags mit Bindestrich (z. B. `v0.1.0-rc.1`) werden als Vorabversion veröffentlicht. Lokal lassen sich die Archive mit
`make package VERSION=v0.1.0` erzeugen (Ausgabe in `dist/`).

## Lizenz

[MIT](LICENSE)
