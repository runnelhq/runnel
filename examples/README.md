# Beispiele

| Ordner | Inhalt |
|---|---|
| [`hello/`](hello/) | Kleines Projekt mit zwei Environments (`dev`, `prod`); Workflows: `hello` (Variablen, Outputs), `parallel` (parallele Zweige, Join, Fehlerpfad), `collector` (`core.dummy` als Sammelpunkt) |
| [`expressions/`](expressions/) | Ausdrücke `{{ … }}` (ADR 0006): Bedingungs-Links abhängig von einem Parameter und einer Projektvariable, Standardwert per Ausdruck (gestriges Datum), Outputs aus den Ergebnissen der Actions |
| [`files/`](files/) | Datei-Actions (S09): Bestellungen aus `inbox/` (nur `*.csv`) in einen Archivordner je Tag kopieren, Dateiliste mit Größen auswerten, leeren Eingang über einen Fehler-Link melden |
| [`broken/`](broken/) | Absichtlich fehlerhafte Workflows, um die Meldungen von `runnel validate` zu zeigen |

```bash
runnel validate examples/hello
```

```bash
runnel validate --env prod examples/hello
```

```bash
runnel run -v --param ORDERS=25 examples/expressions/workflows/orders.workflow.yaml
```

```bash
runnel run -v examples/files/workflows/archive.workflow.yaml
```

```bash
runnel validate examples/broken
```

Die Beispiele werden in den Tests geprüft (`internal/cli/validate_test.go`), damit sie aktuell bleiben.
