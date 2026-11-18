# Runnel

Leichtgewichtige Workflow- und Datenintegrations-Plattform für Windows – eine Alternative zu n8n, Make & Co.,
inspiriert vom Modell von Apache Hop: Workflows und Pipelines aus Actions bzw. Transforms, verbunden durch Links.

- **Editor** – visuelles Designen von Workflows und Pipelines im Browser bzw. WebView2-Fenster
- **Runner** – Ausführung per CLI, als Server mit Scheduler oder als Windows-Dienst
- **Eine `runnel.exe`**, keine Runtime, keine externe Datenbank, Definitionen als YAML (Git-freundlich)

Konzept und technische Architektur: [docs/KONZEPT.md](docs/KONZEPT.md) · Roadmap: [docs/ROADMAP.md](docs/ROADMAP.md) · Entscheidungen: [docs/adr](docs/adr/README.md)

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

## Lizenz

[MIT](LICENSE)
