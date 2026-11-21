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

Konzept und technische Architektur: [docs/KONZEPT.md](docs/KONZEPT.md) · Roadmap: [docs/ROADMAP.md](docs/ROADMAP.md) · Testplan: [docs/TESTPLAN.md](docs/TESTPLAN.md) · Mitarbeiten: [CONTRIBUTING.md](CONTRIBUTING.md) · Entscheidungen: [docs/adr](docs/adr/README.md) · Corporate Design: [design/](design/README.md) · Sicherheit: [SECURITY.md](SECURITY.md)

## Verwendung

Noch vor Version 1.0 (Stand: Phase 0.5 abgeschlossen, siehe [Roadmap](docs/ROADMAP.md)):

- **Workflows** mit den Basis-Actions (`core.start`, `core.log`, `core.wait`, `core.success`, `core.abort`,
  `core.dummy`), Sub-Workflows und Pipelines als Schritt (`core.run-workflow`, `core.run-pipeline`), Datei-Actions
  (`file.exists`, `file.wait`, `file.copy`, `file.move`, `file.delete`, `file.mkdir`, `file.list`) und der Antwort an
  HTTP-Aufrufer (`http.respond`); Ausdrücke `{{ … }}`, `forEach`, Retry und Timeouts.
- **Pipelines** mit CSV (`csv.input`, `csv.output`), Filtern, Umwandeln, Verteilen, Sortieren und Gruppieren,
  Datensätzen von und zu Workflows sowie der Antwort an HTTP-Aufrufer (`http.response`).
- **Editor** im Browser für Workflows und Pipelines, **Trigger** (Zeitplan, Intervall, Ordner, HTTP) und der Betrieb
  als **Windows-Dienst**.

Die vollständige Liste der Bausteine zeigt `runnel catalog`.

```bash
runnel run --env prod examples/hello/workflows/hello.workflow.yaml
```

```
23:28:01.493 INFO  workflow "hello" started (environment prod), run 01M7RFT6ENA57HPBJSHRWN0YV6
23:28:01.493 INFO  [greet] Hello, Production! (production on SRV01, files in \\fileserver\exchange\runnel-hello)
23:28:02.494 INFO  workflow "hello" succeeded in 1s
```

`runnel run <datei>` führt einen Workflow oder eine Pipeline aus (bei Pipelines mit den Zählern je Transform): `--env` wählt das Environment des Projekts, `--param KEY=VALUE` setzt
Parameter, `-v` zeigt zusätzlich Debug-Meldungen und jeden Action-Start. Exit-Code: `0` erfolgreich, `1` fehlgeschlagen,
`2` nicht gestartet (ungültiger Workflow oder Aufruf), `130` abgebrochen – passend für die Windows-Aufgabenplanung.
Strg+C bricht einen Lauf sauber ab (laufende Actions werden gestoppt); ein zweites Strg+C beendet sofort.

```bash
runnel validate examples/hello
```

`runnel validate [pfad...]` prüft Workflows, Pipelines und Trigger (`*.workflow.yaml`, `*.pipeline.yaml`,
`triggers/**/*.yaml`; Ordner werden rekursiv durchsucht) auf
Syntax- und Feldfehler, Links auf unbekannte Actions, Zyklen ohne `loop: true`, unerreichbare Actions, nicht
definierte Variablen sowie – wie vor einem Lauf – unbekannte Action-Typen und ungültige Konfigurationen (`with:`). Das zugehörige Projekt (`project.yaml` im Ordner oder darüber) und seine Environments werden
mitgeprüft; Variablen gegen jedes Environment oder nur gegen eines (`--env prod`). Mit `--param KEY=VALUE` lassen
sich Startparameter angeben. Meldungen erscheinen als `datei:zeile:spalte: error|warning: text`; bei Fehlern ist der
Exit-Code 1 (mit `--strict` auch bei Warnungen). Beispiele liegen unter [`examples/`](examples/).

```bash
runnel serve examples/hello
```

`runnel serve [projektordner]` startet den Server für ein Projekt; der Editor ist im Browser unter
`http://127.0.0.1:7300` erreichbar (anderer Port mit `--port`). Er ist nur vom eigenen Rechner aus erreichbar und bietet
die API für den Editor: Projektbaum, Dateien lesen und speichern mit Konfliktschutz, Katalog der Actions und
Transforms, Prüfung von Workflows und Pipelines sowie Läufe starten, abbrechen und live verfolgen – beschrieben in
[docs/dev/API.md](docs/dev/API.md). Mit `--stop-timeout` bekommen laufende Runs beim Beenden Zeit, fertig zu werden.

```bash
runnel triggers examples/hello
```

`runnel triggers [projektordner]` listet die Trigger des Projekts (`triggers/`, auch in Unterordnern) mit Zeitplan, Ziel und nächstem
Zeitpunkt. Solange `runnel serve` läuft, starten sie ihre Workflows bzw. Pipelines: zu festen Zeiten (Cron-Ausdruck mit
Zeitzone), in einem festen Abstand (`interval`), für neue Dateien in einem Ordner (`file`, z. B.
`runnel serve examples/files`) oder auf Anfrage per HTTP (`http`, eigener Listener auf `127.0.0.1:7301`, z. B.
`curl -X POST http://127.0.0.1:7301/greet/Moin` bei `runnel serve examples/hello`); das Server-Log zeigt jeden Start.
Aus dem Netz erreichbar werden HTTP-Trigger mit HTTPS, z. B. `runnel serve --triggers 0.0.0.0:7301 --triggers-tls auto`
(selbst erzeugtes Zertifikat, Fingerabdruck im Log) oder mit eigenem Zertifikat (`--triggers-cert`, `--triggers-key`).
Mit `response: run` wartet der Aufrufer auf die Antwort des Workflows (Action `http.respond` oder die `outputs`), z. B.
`curl http://127.0.0.1:7301/answer/Anna` bei `runnel serve examples/hello`. Pipelines antworten mit Datensätzen
(Transform `http.response`), z. B. `curl http://127.0.0.1:7301/api/customers/4711` bei `runnel serve examples/pipelines`.

```powershell
runnel service install D:\flows
runnel service start
```

`runnel service install [projektordner]` richtet unter Windows einen Dienst ein, der das Projekt wie `runnel serve`
bedient und seine Trigger auch ohne angemeldeten Benutzer ausführt (Konsole „als Administrator“). Standard: Konto
LocalService, verzögerter Autostart, Neustart nach Fehlern, laufende Runs bekommen beim Stoppen 30 s
(`--stop-timeout`). `runnel service status|stop|uninstall` steuern ihn, `--name` unterscheidet mehrere Dienste. Das Log
steht in `%ProgramData%\runnel\logs\<name>.log`, Start, Stopp, Warnungen und Fehler zusätzlich in der Ereignisanzeige.

```bash
runnel catalog core.wait
```

`runnel catalog` listet die verfügbaren Actions und Transforms nach Kategorien, `runnel catalog <typ>` beschreibt einen Baustein mit
ihren Feldern (Typ, Pflichtfeld, erlaubte Werte, Standardwert, Beispiel). `--json` gibt die Descriptors mit
JSON Schema aus – dieselben Daten, aus denen der Editor Palette und Eigenschaftsdialog erzeugt.

## Entwicklung

Voraussetzung: [Go](https://go.dev/dl/) (Version laut `go.mod`) und für den Editor [Node.js](https://nodejs.org/)
22 LTS. Für `make lint` zusätzlich [golangci-lint](https://golangci-lint.run/) v2.15.0. Details zum Editor:
[docs/dev/EDITOR.md](docs/dev/EDITOR.md).

| Aufgabe | Mit `make` | Ohne `make` (z. B. Windows/PowerShell) |
|---|---|---|
| Bauen (mit Editor) | `make build` → `bin/runnel` | `cd web; npm ci; npm run build; cd ..; go build -o bin/runnel.exe ./cmd/runnel` |
| Nur Go (ohne Editor-Build) | `make build-go` | `go build -o bin/runnel.exe ./cmd/runnel` |
| Editor-Tests (Playwright) | `make e2e` | – |
| Windows-/Linux-Binaries | `make dist` | `$env:GOOS="windows"; go build -o bin/runnel-windows-amd64.exe ./cmd/runnel` |
| Tests | `make test` (mit Race-Detector) | `go test ./...` |
| Lint (Linux- und Windows-Code) | `make lint` | `golangci-lint run ./...` und `$env:GOOS="windows"; golangci-lint run ./...` |
| Formatieren | `make fmt` | `golangci-lint fmt ./...` |
| Fuzz-Tests (Dateien, Variablen, Ausdrücke, Formate, CSV, HTTP-Anfragen) | `make fuzz` | – |
| Benchmarks | `make bench` | `go test ./... -run '^$' -bench . -benchmem` |
| Bekannte Sicherheitslücken prüfen | `make vulncheck` | `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...` |
| SBOMs erzeugen | `make sbom` | – |

```bash
bin/runnel version
```

Die CI (`.github/workflows/ci.yml`) führt Lint (Linux- und Windows-Code), `govulncheck`, Tests unter Linux und Windows,
die Editor-Prüfung (Typprüfung, Playwright) sowie Build und Packen
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
