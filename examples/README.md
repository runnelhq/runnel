# Beispiele

Relative Pfade in den Beispielen beziehen sich auf den jeweiligen Projektordner ([ADR 0020](../docs/adr/0020-relative-pfade.md)); Ausgaben landen unter `out/` im Projekt (in Git ignoriert). Die Befehle unten funktionieren deshalb aus jedem Ordner, sofern der Pfad zur Datei stimmt.

| Ordner | Inhalt |
|---|---|
| [`hello/`](hello/) | Kleines Projekt mit zwei Environments (`dev`, `prod`); Workflows: `hello` (Variablen, Outputs), `parallel` (parallele Zweige, Join, Fehlerpfad), `collector` (`core.dummy` als Sammelpunkt); Trigger `morning` (Cron, S37), `hourly` (Intervall, S38), `greet` und `greet-default` (HTTP, S38a: `curl -X POST http://127.0.0.1:7301/greet/Moin`), `answer` mit dem Workflow `answer` (HTTP mit Antwort aus dem Workflow, S38b1: `curl -i http://127.0.0.1:7301/answer/Anna`) |
| [`expressions/`](expressions/) | Ausdrücke `{{ … }}` (ADR 0006): Bedingungs-Links abhängig von einem Parameter und einer Projektvariable, Standardwert per Ausdruck (gestriges Datum), Outputs aus den Ergebnissen der Actions |
| [`files/`](files/) | Datei-Actions (S09): Bestellungen aus `inbox/` (nur `*.csv`) in einen Archivordner je Tag kopieren, Dateiliste mit Größen auswerten, leeren Eingang über einen Fehler-Link melden; Trigger `orders` (S38) startet mit `runnel serve examples/files` den Workflow `arrived` für jede neue Datei `orders-*.csv` in `inbox/`; `bundle` (S54) packt den Eingang in ein ZIP (`archive.create`) und entpackt daraus nur die CSV-Dateien (`archive.extract`) |
| [`http/`](http/) | HTTP-Request (S10): `release` fragt die GitHub-API nach der neuesten Version eines Repositorys (`http.request`, JSON-Antwort als Wert), prüft mit `core.check`, ob sie Dateien hat, und meldet Fehler über Fehler-Links; Trigger `release` (HTTP mit Token `${secret:release-token}`, Secret im Klartext unter `secrets:` in `project.yaml`, S39a2) |
| [`database/`](database/) | Connections und SQL (S20a): `connections/shop.yaml` ist eine SQLite-Datenbank unter `out/`; der Workflow `orders` legt die Tabelle an, fügt eine Bestellung mit Parametern `@name` ein (`db.execute`) und behält die letzten fünf; die Pipeline `report` (S20b) liest die Bestellungen eines Kunden mit `db.table-input` und schreibt sie als CSV; die Pipeline `totals` (S21) summiert je Kunde und schreibt mit `db.upsert` in die Tabelle `totals`; die Pipeline `enrich` (S23a) hängt mit `db.lookup` die Summen an Bestellungen an |
| [`sub-workflows/`](sub-workflows/) | Sub-Workflows (S11): `nightly` ruft `import-region` je Region mit eigenen Parametern auf (für den Süden über den Parameter `IMPORT`, eingeschränkt mit `allowedTargets`, S25b) und summiert dessen Outputs; zu viele Bestellungen lassen das Kind abbrechen, der Fehler-Link meldet es |
| [`pipelines/`](pipelines/) | Mit einer `README.md` (öffnet im Editor beim Start, E17) und dem Bild `docs/customers.png`. Pipelines: `numbers` (S13) erzeugt Datensätze, kopiert sie auf zwei Zweige, führt sie zusammen und schreibt jeden 250. ins Log; `orders` (S15, S16a) liest eine CSV-Datei im deutschen Format (`;`, `1.234,50`, `19.11.2026`), behält Bestellungen ab einem Mindestbetrag (`core.filter`) und schreibt sie mit Komma, ISO-Daten und Punkt als Dezimaltrennzeichen; `customers` (S16b) bereitet Kunden für einen CRM-Import auf: Typen umwandeln (`core.convert`), filtern, Felder auswählen und umbenennen (`core.select-fields`), Quelle und Batch-Nummer ergänzen (`core.constant`); Kunden mit ungültigen Werten gehen über einen Fehler-Link (S17a) in eine eigene Datei; `segments` (S17b) verteilt Kunden mit `core.switch` nach Umsatz auf zwei Dateien; `parallel` (S17c) filtert in vier parallelen Kopien, verteilt reihum (`distribute`) und läuft im strikten Modus mit größeren Batches; der Workflow `import` (S18) ruft `customers` mit `core.run-pipeline` auf und wertet die Zähler aus; `daily` (S22) gruppiert die Bestellungen nach Tag (`core.group-by`) und sortiert nach Umsatz (`core.sort`); der Workflow `files` (S24a) gibt einer Pipeline eine Dateiliste mit (`rows`, `core.from-result`) und bekommt die großen Dateien zurück (`core.to-result`); der Workflow `each-file` (S24b) startet für jede Datei aus `inbox/` eine Pipeline (`forEach`, `parallel: 2`) und fasst die Summen zusammen; `contacts` (S16c) bereinigt eine Kontaktliste aus `crm/`: E-Mail-Adressen vereinheitlichen und Bruttobeträge berechnen (`core.calculate`), doppelte Kontakte entfernen (`core.dedupe`) und durchnummerieren (`core.sequence`, S16d); `api-orders` (S19) liest Bestellungen aus der JSON-Antwort einer API (`api/orders.json`, `records: data.items`, Felder aus verschachtelten Objekten) und schreibt die bezahlten als NDJSON (`json.output`); `people` (S16f) ersetzt Status-Codes durch Namen (`core.map-values`), teilt Namen in Nach- und Vorname und Schlagworte in je einen Datensatz (`core.split-field`); `inventory` (S55) liest die Dateien eines Ordners als Datensätze (`file.list`) und zählt Dateien und Bytes je Endung; `customer-api` mit dem Trigger `customer` (S38c) ist ein kleiner REST-Dienst: `GET /api/customers/{KDNR}` antwortet mit dem Kunden als JSON oder `404` (`http.response`, `runnel serve examples/pipelines`) |
| [`broken/`](broken/) | Absichtlich fehlerhafte Workflows und zwei Pipelines (Tippfehler, Zyklus, unbekanntes Feld), um die Meldungen von `runnel validate` zu zeigen |

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
runnel run -v examples/files/workflows/bundle.workflow.yaml
```

```bash
runnel run -v examples/http/workflows/release.workflow.yaml
```

`examples/http` hat außerdem einen HTTP-Trigger mit Token aus einem Secret (`secretStore: inline`, S39a2): nach
`runnel serve examples/http` mit Token aufrufen (ohne Token oder mit einem anderen kommt `401`):

```bash
curl -X POST -H "Authorization: Bearer example-token" "http://127.0.0.1:7301/release?REPO=cli/cli"
```

```bash
runnel run -v --param CUSTOMER=Ada examples/database/workflows/orders.workflow.yaml
```

```bash
runnel run -v --param CUSTOMER=Ada examples/database/pipelines/report.pipeline.yaml
```

```bash
runnel run -v examples/database/pipelines/totals.pipeline.yaml
```

```bash
runnel run -v examples/database/pipelines/enrich.pipeline.yaml
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
runnel run -v examples/pipelines/workflows/each-file.workflow.yaml
```

```bash
runnel validate examples/broken
```

Die Beispiele werden in den Tests geprüft (`internal/cli/validate_test.go`), damit sie aktuell bleiben.
