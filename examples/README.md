# Beispiele

| Ordner | Inhalt |
|---|---|
| [`hello/`](hello/) | Kleines Projekt mit zwei Environments (`dev`, `prod`); Workflows: `hello` (Variablen, Outputs), `parallel` (parallele Zweige, Join, Fehlerpfad), `collector` (`core.dummy` als Sammelpunkt) |
| [`expressions/`](expressions/) | Ausdrücke `{{ … }}` (ADR 0006): Bedingungs-Links abhängig von einem Parameter und einer Projektvariable, Standardwert per Ausdruck (gestriges Datum), Outputs aus den Ergebnissen der Actions |
| [`files/`](files/) | Datei-Actions (S09): Bestellungen aus `inbox/` (nur `*.csv`) in einen Archivordner je Tag kopieren, Dateiliste mit Größen auswerten, leeren Eingang über einen Fehler-Link melden |
| [`sub-workflows/`](sub-workflows/) | Sub-Workflows (S11): `nightly` ruft `import-region` je Region mit eigenen Parametern auf und summiert dessen Outputs; zu viele Bestellungen lassen das Kind abbrechen, der Fehler-Link meldet es |
| [`pipelines/`](pipelines/) | Pipelines: `numbers` (S13) erzeugt Datensätze, kopiert sie auf zwei Zweige, führt sie zusammen und schreibt jeden 250. ins Log; `orders` (S15, S16a) liest eine CSV-Datei im deutschen Format (`;`, `1.234,50`, `19.11.2026`), behält Bestellungen ab einem Mindestbetrag (`core.filter`) und schreibt sie mit Komma, ISO-Daten und Punkt als Dezimaltrennzeichen; `customers` (S16b) bereitet Kunden für einen CRM-Import auf: Typen umwandeln (`core.convert`), filtern, Felder auswählen und umbenennen (`core.select-fields`), Quelle und Batch-Nummer ergänzen (`core.constant`); Kunden mit ungültigen Werten gehen über einen Fehler-Link (S17a) in eine eigene Datei; `segments` (S17b) verteilt Kunden mit `core.switch` nach Umsatz auf zwei Dateien; `parallel` (S17c) filtert in vier parallelen Kopien, verteilt reihum (`distribute`) und läuft im strikten Modus mit größeren Batches; der Workflow `import` (S18) ruft `customers` mit `core.run-pipeline` auf und wertet die Zähler aus; `daily` (S22) gruppiert die Bestellungen nach Tag (`core.group-by`) und sortiert nach Umsatz (`core.sort`); der Workflow `files` (S24a) gibt einer Pipeline eine Dateiliste mit (`rows`, `core.from-result`) und bekommt die großen Dateien zurück (`core.to-result`) |
| [`broken/`](broken/) | Absichtlich fehlerhafte Workflows und eine Pipeline (Tippfehler, Zyklus), um die Meldungen von `runnel validate` zu zeigen |

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
runnel run -v examples/sub-workflows/workflows/nightly.workflow.yaml
```

```bash
runnel run --param ROWS=10000000 examples/pipelines/pipelines/numbers.pipeline.yaml
```

```bash
runnel run examples/pipelines/pipelines/orders.pipeline.yaml
```

```bash
runnel run --param BATCH=42 examples/pipelines/pipelines/customers.pipeline.yaml
```

```bash
runnel run examples/pipelines/pipelines/segments.pipeline.yaml
```

```bash
runnel run --param ROWS=10000000 examples/pipelines/pipelines/parallel.pipeline.yaml
```

```bash
runnel run -v examples/pipelines/workflows/import.workflow.yaml
```

```bash
runnel run examples/pipelines/pipelines/daily.pipeline.yaml
```

```bash
runnel run -v examples/pipelines/workflows/files.workflow.yaml
```

```bash
runnel validate examples/broken
```

Die Beispiele werden in den Tests geprüft (`internal/cli/validate_test.go`), damit sie aktuell bleiben.
