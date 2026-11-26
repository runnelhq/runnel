# Mitarbeiten an Runnel

Danke für dein Interesse! Diese Seite beschreibt, wie Beiträge ablaufen. **Die Regeln stehen verbindlich in
[AGENTS.md](AGENTS.md)** – für Menschen wie für KI-Agenten; hier steht nur der Ablauf.

## Voraussetzungen

- [Go](https://go.dev/dl/) in der Version aus `go.mod` (derzeit Go 1.26)
- [golangci-lint](https://golangci-lint.run/) v2.15.0 für `make lint`
- `make` (unter Windows z. B. über Git Bash); alle Befehle gehen auch ohne `make`, siehe [README](README.md#entwicklung)
- [Node.js](https://nodejs.org/) 22 LTS für den Editor (`make build`, `make e2e`); ohne Node baut `make build-go` die exe ohne Editor

## Ablauf

1. **Thema wählen:** einen offenen Schritt aus der [Roadmap](docs/ROADMAP.md) oder ein Issue. Größere Vorhaben vorher in
   einem Issue abstimmen.
2. **Branch anlegen** von `main`.
3. **Umsetzen** nach den Regeln in [AGENTS.md](AGENTS.md) (PR-Größe, Tests, Doku, Sicherheit, Mockup vor Oberfläche).
4. **Prüfen:** `make lint` (Linux- und Windows-Code) und `make test` (Race-Detector); bei Änderungen am Editor
   `npm run typecheck` und die betroffenen Playwright-Tests in `web/` (oder `make e2e`); bei Parsern `make fuzz`, bei
   Pfaden unter Last `make bench`.
5. **Pull Request** nach `main`, Titel und Beschreibung deutsch nach der Vorlage
   ([.github/pull_request_template.md](.github/pull_request_template.md)): Art, Inhalt, Golden-Dateien, Tests,
   Leistung, Sicherheit, Stabilität (ADR 0029), „Prüfen“ (wie man das Ergebnis selbst ausprobiert), Abhängigkeiten,
   Umfang. Für Anwender sichtbare Änderungen bekommen im selben PR einen Eintrag in [CHANGELOG.md](CHANGELOG.md)
   unter „Unveröffentlicht“.
6. Die CI prüft Lint (Linux und Windows), Schwachstellen, Tests unter Linux und Windows, den Editor (Typprüfung,
   Playwright) sowie Build und Packen. Gemergt wird, wenn die CI grün ist und das Review abgeschlossen ist.

Beiträge stehen unter der [MIT-Lizenz](LICENSE) des Projekts (ADR 0004).

## Anleitungen

| Thema | Dokument |
|---|---|
| Neue Action entwickeln | [docs/dev/ACTIONS.md](docs/dev/ACTIONS.md) |
| Server-API | [docs/dev/API.md](docs/dev/API.md) |
| Editor (Frontend) | [docs/dev/EDITOR.md](docs/dev/EDITOR.md) |
| Neue Transforms | [docs/dev/TRANSFORMS.md](docs/dev/TRANSFORMS.md) |
| Plugins | folgt mit Roadmap-Schritt S49–S51 |
| Testen, bekannte Einschränkungen | [docs/TESTPLAN.md](docs/TESTPLAN.md) |
| Release erstellen | [README – Releases](README.md#releases) |

## Fehler melden

Sicherheitslücken bitte nicht als Issue, sondern wie in [SECURITY.md](SECURITY.md) beschrieben. Für andere Fehler bitte ein [Issue](https://github.com/runnelhq/runnel/issues) mit Runnel-Version (`runnel version`), Windows-Version,
dem betroffenen Workflow (ohne Secrets) und der vollständigen Ausgabe anlegen.
