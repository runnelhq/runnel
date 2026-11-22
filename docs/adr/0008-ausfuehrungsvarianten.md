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
  Präzisiert mit S25b (siehe Nachtrag): Ziele liegen immer im Projektordner, der Hinweis ist eine Warnung.

### 3. Ausführung pro Datensatz

**a) Executor-Transforms in Pipelines** (präzisiert durch [ADR 0021](0021-unter-pipelines.md))

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
  Festgelegt gemeinsam mit den Executor-Transforms in [ADR 0021](0021-unter-pipelines.md).
- **Remote-Ausführung** über `RunConfig type: remote`.

## Zeitplan

| Phase | Inhalt |
|---|---|
| 0.4 – Pipelines | Executor-Transforms, `forEachRow`, `core.to-result`/`core.from-result`, dynamische Ziele |
| 0.3 – Workflow-Ausbau | Teilausführung im Editor |
| 0.5 – Betrieb | Run-Konfigurationen (`local`), Trigger für Pipelines |
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

## Nachtrag 2026-11-20 (S24a): Ergebnis-Datensätze

Abstimmung mit dem Maintainer zur Umsetzung von 3c:

- **Übergabe ausdrücklich:** Eine Pipeline erhält Datensätze über das Feld `rows` von `core.run-pipeline`, eine Liste
  aus einem Ausdruck (z. B. `rows: "{{ results.list.output }}"`); `core.from-result` gibt sie weiter. Ein implizites
  Lesen des Ergebnisses der vorherigen Action (wie „Get rows from result“ in Hop) gibt es nicht – es hinge davon ab,
  über welchen Link die Action erreicht wurde. Objekte werden zu Datensätzen (Felder nach Namen sortiert, da Objekte
  keine Reihenfolge haben), andere Einträge zu `{value: …}`.
- **Rückgabe:** `core.to-result` gibt die Datensätze weiter und sammelt sie; `core.run-pipeline` liefert sie als
  `output.rows` (Liste von Objekten, Datumsfelder wie in Ausdrücken als Zeitpunkt). Ohne Aufrufer bleibt das Ergebnis
  ungenutzt; ohne `rows` liefert `core.from-result` keine Datensätze und sagt das im Log.
- **Speicher:** Ergebnis-Datensätze liegen im Speicher, höchstens `maxRows` (Standard 100.000) je `core.to-result`;
  mehr bricht die Pipeline mit klarer Meldung ab. Das Auslagern in eine temporäre Datei (`resultSpillRows`) ist
  zurückgestellt (Backlog).
- **Wiederholung je Datensatz** folgt mit S24b als `forEach: "{{ … }}"` (ausdrückliche Liste, aktueller Eintrag `row`)
  statt `forEachRow: true`.

## Nachtrag 2026-11-20 (S24b): Wiederholung je Eintrag

- **An der Action, für alle Actions:** `forEach`, `parallel` und `stopOnError` stehen neben `retry` und `timeout`, nicht
  unter `with` von `core.run-pipeline`/`core.run-workflow` – so lässt sich z. B. auch `file.copy` je Datei wiederholen.
  Die Engine wertet `with` je Eintrag mit `row` aus; `retry` und `timeout` gelten je Eintrag.
- **Ergebnis:** `output.items` (je Eintrag `row`, `success`, `output`, `error`, in der Reihenfolge der Liste), dazu
  `output.succeeded` und `output.failed`. `stopOnError` (Standard `true`) beendet beim ersten Fehler die übrigen
  Einträge; mit `false` scheitert die Action erst am Ende, wenn ein Eintrag fehlschlug (wie oben unter 3b).
- `parallel` ist standardmäßig 1, höchstens 64.

## Nachtrag 2026-11-22 (S25b): Dynamische Ziele absichern

- **Nur Dateien des Projekts:** `core.run-workflow` und `core.run-pipeline` rufen nur Dateien im Projektordner auf, auch
  über `..` oder einen absoluten Pfad nicht andere. Das gilt für feste Pfade (Fehler bei `runnel validate`) und für
  Pfade, die erst beim Lauf feststehen (die Action scheitert). Ohne Projekt (einzelne Datei) bleibt es bei Pfaden
  relativ zur aufrufenden Datei ohne Einschränkung.
- **`allowedTargets`:** Liste von Mustern relativ zum Projektordner (`*`, `?`, `[abc]`; `*` überschreitet keine
  Ordnergrenze; unter Windows ohne Unterscheidung der Groß- und Kleinschreibung). Ist sie gesetzt, muss das Ziel
  (bereinigt, mit `/`) einem Muster entsprechen, sonst scheitert die Action mit „… is not one of the allowed targets“.
  Ungültige feste Muster meldet schon `runnel validate`.
- **Warnung statt Hinweis:** Ein Ziel mit `${…}` oder `{{ … }}` ohne `allowedTargets` meldet `runnel validate` (und
  der Editor) als Warnung; der Lauf startet trotzdem.

### Sicherheit

Werte aus Parametern können von außen kommen, etwa aus Pfad, Query oder Body eines HTTP-Triggers. Ohne Einschränkung
könnte ein Aufrufer mit `params.TYPE = "../../anderes-projekt/x"` beliebige Workflow- oder Pipeline-Dateien des
Rechners starten. Die Grenze „nur Dateien des Projekts“ gilt deshalb immer; `allowedTargets` schränkt innerhalb des
Projekts weiter ein. Ablehnungen sind getestet (`..`, absolute Pfade, Ziele außerhalb der Muster).
