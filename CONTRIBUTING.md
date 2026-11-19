# Mitarbeiten an Runnel

Danke für dein Interesse! Diese Seite beschreibt, wie Beiträge ablaufen. Die Regeln in Kurzform – auch für KI-Agenten –
stehen in [AGENTS.md](AGENTS.md).

## Voraussetzungen

- [Go](https://go.dev/dl/) in der Version aus `go.mod` (derzeit Go 1.26)
- [golangci-lint](https://golangci-lint.run/) v2.15.0 für `make lint`
- `make` (unter Windows z. B. über Git Bash); alle Befehle gehen auch ohne `make`, siehe [README](README.md#entwicklung)

## Ablauf

1. **Thema wählen:** einen offenen Schritt aus der [Roadmap](docs/ROADMAP.md) oder ein Issue. Größere Vorhaben vorher in
   einem Issue abstimmen.
2. **Branch anlegen** von `main`.
3. **Umsetzen** – klein halten: ein Roadmap-Schritt bzw. ein Thema je PR, Richtwert ≤ ca. 1.000 geänderte Zeilen
   inklusive Tests.
4. **Prüfen:** `make lint` und `make test` (Race-Detector). Bei Parser-Änderungen zusätzlich `make fuzz`.
5. **Pull Request** nach `main` mit Beschreibung (deutsch): Zusammenfassung, Änderungen, Entscheidungen fürs Review,
   „Prüfen“ (wie man das Ergebnis selbst ausprobiert).
6. Die CI prüft Lint, Schwachstellen, Tests unter Linux und Windows sowie Build und Packen. Gemergt wird, wenn die CI
   grün ist und das Review abgeschlossen ist.

## Regeln

- **Tests gehören zum PR:** Unit-Tests, Golden-File-Tests für Formate und Ausgaben, `-race` für nebenläufigen Code.
- **Doku gehört zum PR:** Konzept, Roadmap, ADRs, README und Entwickleranleitungen werden mit angepasst.
- **Sprache:** Code, Kommentare und Programmausgaben englisch; Dokumentation deutsch.
- **Entscheidungen:** Grundsätzliches (Formate, Semantik, neue Abhängigkeiten, Namensregeln) als
  [ADR](docs/adr/README.md) festhalten.
- **Lizenz:** Beiträge stehen unter der [MIT-Lizenz](LICENSE) des Projekts (ADR 0004). Abhängigkeiten müssen
  MIT-kompatibel sein.

## Anleitungen

| Thema | Dokument |
|---|---|
| Neue Action entwickeln | [docs/dev/ACTIONS.md](docs/dev/ACTIONS.md) |
| Neue Transforms | folgt mit Roadmap-Schritt S12/S13 |
| Plugins | folgt mit Roadmap-Schritt S49–S51 |
| Testen, bekannte Einschränkungen | [docs/TESTPLAN.md](docs/TESTPLAN.md) |
| Release erstellen | [README – Releases](README.md#releases) |

## Fehler melden

Bitte ein [Issue](https://github.com/runnelhq/runnel/issues) mit Runnel-Version (`runnel version`), Windows-Version,
dem betroffenen Workflow (ohne Secrets) und der vollständigen Ausgabe anlegen.
