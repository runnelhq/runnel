# ADR 0008: Ausführungsvarianten – Run-Konfigurationen, dynamische Ziele, Ausführung pro Datensatz

- **Status:** Angenommen
- **Datum:** 2026-11-18
- **Bezug:** [Konzept, Kap. 2.8 – Ausführungsvarianten](../KONZEPT.md#28-ausführungsvarianten)

## Kontext

Apache Hop bietet mehrere Wege, Workflows und Pipelines variabel auszuführen:

- **Run Configurations** (Pipeline/Workflow): dieselbe Definition läuft lokal, auf einem Remote-Server oder auf einer anderen Engine.
- **Dynamische Ziele**: Dateinamen von Pipelines/Workflows aus Variablen.
- **Pipeline Executor / Workflow Executor**: Ausführung einer Unter-Pipeline bzw. eines Unter-Workflows pro Datensatz.
- **„Execute for every input row“** an der Pipeline-/Workflow-Action.
- **„Copy rows to result“ / „Get rows from result“**: Weitergabe von Datensätzen zwischen Pipelines im Workflow.
- **Mappings**: wiederverwendbare Unter-Pipelines.

Das Konzept deckte bisher Direktausführung, Parameter, Umgebungen, `core.run-pipeline`/`core.run-workflow` und eine
ForEach-Action ab. Run-Konfigurationen, Ausführung pro Datensatz und Ergebnis-Datensätze fehlten.

## Entscheidung

### 1. Run-Konfigurationen

Neues Metadaten-Objekt `RunConfig` im Projektordner `run-configs/`. Es beschreibt, **wie** ein Workflow oder eine
Pipeline ausgeführt wird – unabhängig davon, **was** ausgeführt wird.

```yaml
# run-configs/local-fast.yaml
kind: RunConfig
apiVersion: runnel/v1
name: local-fast
type: local                  # local (ab 0.4) | remote (später)
with:
  bufferSize: 50000          # Records pro Link-Puffer (überschreibt Pipeline-Einstellung)
  batchSize: 1000
  maxParallelActions: 4      # parallele Actions im Workflow
  sampling: false            # keine Vorschau-Samples speichern
  samplingRows: 100
  metrics: true              # Live-Metriken je Transform
  logLevel: minimal          # minimal | basic | detailed | debug
  timeout: 4h
```

- Es gibt immer eine eingebaute Konfiguration `local` mit Standardwerten; `project.yaml` kann eine andere als Standard setzen.
- **Auswahl** (Vorrang von oben nach unten):
  1. CLI `--run-config local-fast` bzw. Feld `runConfig` in `POST /api/v1/runs`
  2. Trigger (`runConfig:`)
  3. aufrufende Action (`core.run-pipeline` / `core.run-workflow` mit `runConfig:`)
  4. Standard des Projekts
- Im Editor: Auswahlfeld neben „Ausführen“ (zusammen mit Umgebung und Parametern).
- `type: remote` (später, zusammen mit den Remote-Runnern): führt auf einem anderen Runnel-Server aus,
  adressiert über URL + Zugangsschlüssel (siehe [ADR 0005](0005-mehrbenutzerbetrieb.md)).
- Spark/Flink/Beam-Engines wie in Hop sind **nicht** vorgesehen (Nicht-Ziel Big Data).

### 2. Dynamische Ziele

Pfade in `core.run-pipeline`, `core.run-workflow`, den Executor-Transforms und in Triggern dürfen Variablen und
Ausdrücke enthalten:

```yaml
- id: import
  type: core.run-pipeline
  with:
    pipeline: "pipelines/import-${MANDANT}.pipeline.yaml"
```

- Ist das Ziel statisch, prüft der Validator es vollständig (existiert, Parameter passen).
- Ist es dynamisch, gibt der Validator einen **Hinweis** aus; die Prüfung erfolgt beim Start der Action.
  Optional schränkt `allowedTargets: ["pipelines/import-*.pipeline.yaml"]` die erlaubten Ziele ein (Glob).

### 3. Ausführung pro Datensatz

**a) Executor-Transforms in Pipelines**

| Transform | Funktion |
|---|---|
| `core.pipeline-executor` | Startet eine Unter-Pipeline pro Datensatz oder pro Gruppe |
| `core.workflow-executor` | Startet einen Unter-Workflow pro Datensatz oder pro Gruppe |

```yaml
- id: per-customer
  type: core.pipeline-executor
  copies: 2                          # parallele Ausführungen
  with:
    pipeline: pipelines/customer-detail.pipeline.yaml
    groupBy: none                    # none (pro Datensatz) | size | field
    groupSize: 100                   # bei groupBy: size
    groupField: kdnr                 # bei groupBy: field (Wechsel des Feldwerts)
    params:
      KDNR: "{{ record.kdnr }}"
    passRecords: true                # Gruppe als Eingabe der Unter-Pipeline (über core.from-result)
    runConfig: local
```

Ausgänge des Executors:

| Ausgang | Inhalt |
|---|---|
| `main` | Eingangsdatensätze unverändert (Durchreichen) |
| `results` | Ein Datensatz pro Ausführung: `runId`, `status`, `rows`, `durationMs`, `error`, Parameter |
| `output` | Ergebnis-Datensätze der Unter-Pipeline (aus `core.to-result`) |
| `error` | Datensätze, deren Ausführung fehlgeschlagen ist |

**b) `forEachRow` an Workflow-Actions**

`core.run-pipeline` und `core.run-workflow` erhalten die Option, sich für jeden Ergebnis-Datensatz der vorherigen
Action zu wiederholen:

```yaml
- id: per-file
  type: core.run-pipeline
  with:
    pipeline: pipelines/load-file.pipeline.yaml
    forEachRow: true
    params:
      FILE: "{{ row.path }}"         # 'row' = aktueller Ergebnis-Datensatz
    parallel: 4
    stopOnError: false               # weitermachen, Gesamtergebnis = failure, wenn einer fehlschlägt
```

**c) Ergebnis-Datensätze**

| Transform | Funktion |
|---|---|
| `core.to-result` | Schreibt Datensätze in das Ergebnis der Pipeline (Hop: „Copy rows to result“) |
| `core.from-result` | Liest die Ergebnis-Datensätze der vorherigen Action bzw. die vom Executor übergebene Gruppe (Hop: „Get rows from result“) |

- Ergebnis-Datensätze werden im Speicher gehalten. Ab einer konfigurierbaren Größe (`resultSpillRows`, Standard 100.000)
  werden sie in eine temporäre Datei ausgelagert.
- Im Workflow-Kontext sind sie als `result.rows` (Anzahl) und für `forEachRow` als `row` verfügbar.

### 4. Trigger für Pipelines

Trigger dürfen direkt auf Pipelines zeigen (`target: pipelines/x.pipeline.yaml`), nicht nur auf Workflows.

### 5. Teilausführung im Editor

- **„Ab hier ausführen“** (Workflow): startet bei der gewählten Action. Ergebnisse vorheriger Actions können aus
  dem letzten Lauf übernommen werden.
- **„Nur diese Action ausführen“** (Workflow): Testlauf einer einzelnen Action mit aktuellem Kontext.
- **Pipeline bis Transform X**: besteht bereits als Vorschau.

Technisch: `POST /api/v1/runs` mit `startAt: "<action-id>"` bzw. `only: "<action-id>"` und optional `reuseResultsFrom: "<run-id>"`.

### Später

- **Mappings** (wiederverwendbare Unter-Pipelines mit definiertem Ein-/Ausgang, im Editor als ein Transform dargestellt).
- **Remote-Ausführung** über `RunConfig type: remote`.

## Zeitplan

| Phase | Inhalt |
|---|---|
| 0.2 – Pipelines | Executor-Transforms, `forEachRow`, `core.to-result`/`core.from-result`, dynamische Ziele |
| 0.3 – Server & Editor | Teilausführung im Editor |
| 0.4 – Betrieb | Run-Konfigurationen (`local`), Trigger für Pipelines |
| später | Mappings, `RunConfig type: remote` |

## Konsequenzen

**Positiv**
- Alle wesentlichen Ausführungsmuster von Hop sind abgedeckt; ein späterer Hop-Import kann sie abbilden.
- Klare Trennung von „was“ (Workflow/Pipeline) und „wie“ (RunConfig) – dieselbe Definition läuft in Entwicklung
  mit voller Diagnose und in Produktion schlank.
- Grundlage für Remote-Runner ist gelegt, ohne sie jetzt bauen zu müssen.

**Negativ / zu beachten**
- Viele kleine Unter-Läufe (pro Datensatz) erzeugen viele Einträge in der Run-Historie. Unter-Läufe werden daher
  über `parent_run_id` gruppiert und im Editor eingeklappt dargestellt; die Retention gilt für den gesamten Baum.
- Executor-Transforms mit hoher Parallelität können Datenbanken/APIs überlasten → `copies` und `parallel` sind
  standardmäßig 1.
- Dynamische Ziele sind statisch nur eingeschränkt prüfbar.
- Ergebnis-Datensätze im Speicher brauchen ein Auslagerungskonzept (siehe oben).
