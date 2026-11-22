# ADR 0021: Unter-Pipelines in Pipelines – Mapping und Executor

- **Status:** Entwurf (zur Abstimmung mit dem Maintainer)
- **Datum:** 2026-11-22
- **Bezug:** [ADR 0008 – Ausführungsvarianten](0008-ausfuehrungsvarianten.md) (Executor-Transforms, Mappings,
  Ergebnis-Datensätze, Nachtrag S25b), [ADR 0017 – Sub-Workflows](0017-sub-workflows.md),
  [ADR 0007 – Typsystem](0007-typsystem-pipelines.md), [ADR 0013 – Typnamen](0013-typnamen-namensraeume.md),
  [ADR 0009 – Begriff Link](0009-begriff-link.md), Roadmap S25 (Teil)

## Kontext

ADR 0008 plant zwei Arten, eine Pipeline **aus einer Pipeline** heraus auszuführen:

- **Executor-Transforms** (`core.pipeline-executor`, `core.workflow-executor`, Roadmap S25): eine Unter-Pipeline bzw.
  ein Unter-Workflow **je Datensatz oder Gruppe**, mit den Ausgängen `main`, `results`, `output` und `error`.
- **Mappings** („später“): eine wiederverwendbare Unter-Pipeline mit definiertem Ein- und Ausgang, im Editor als ein
  Transform dargestellt.

Beide berühren dieselben Fragen. Wie erhält eine Unter-Pipeline Datensätze, und wie gibt sie sie zurück? Wie hängt
sie an Engine, Prüfung, Typsystem und Log? Wie erscheint sie im Canvas? Wird der Executor zuerst gebaut, legt er
Antworten fest, die für Mappings nicht passen. Diese ADR entscheidet beides gemeinsam.

Stand heute:

- **Aufrufe aus Workflows:** `core.run-pipeline` ruft eine Pipeline aus einem Workflow auf (ADR 0017, Nachtrag S18).
  Datensätze gehen über `rows` und `core.from-result` hinein und über `core.to-result` und `output.rows` heraus. Beide
  Richtungen sind Listen im Speicher (ADR 0008, Nachtrag S24a).
- **Ziele:** Sie liegen immer im Projektordner und lassen sich mit `allowedTargets` einschränken (ADR 0008, Nachtrag
  S25b).
- **Pipeline-Engine:** eine Goroutine je Transform, gepufferte Kanäle mit Batches und Backpressure.
- **Links:** Es gibt Daten- und Fehler-Links. `info` ist für Lookups (S23) reserviert. Verschiedene Ausgänge eines
  Transforms gibt es bisher nur bei `core.switch`: Felder mit `format: runnel-target` (`to:`) nennen die Transforms,
  zu denen ein Daten-Link führt.

## Entscheidung (Vorschlag)

### 1. Zwei Arten, ein Unterbau

| | Mapping | Pipeline-Executor |
|---|---|---|
| Läufe der Unter-Pipeline | **einer** je Lauf der äußeren Pipeline | **einer je Datensatz oder Gruppe** |
| Datensätze | strömen durch (Backpressure wie zwischen Transforms) | je Lauf eine Liste (Datensatz bzw. Gruppe) |
| Parameter | fest bzw. aus Variablen | je Lauf, auch aus dem Datensatz (`{{ record.kdnr }}`) |
| typischer Zweck | wiederverwendbarer Baustein, z. B. „Adressen bereinigen“ | z. B. je Kunde eine Detail-Pipeline |

Beide nutzen denselben Unterbau in `internal/runs`:

- Das Ziel wird aufgelöst und abgesichert (S25b).
- Die Parameter werden geprüft (wie `CheckGivenParams`).
- Ein Selbstaufruf wird erkannt, auch über andere Pipelines (wie bei Workflows).
- Log und Zähler werden an den Aufrufer weitergereicht.

### 2. Ein- und Ausgang: `core.from-result` und `core.to-result`

Eine Unter-Pipeline erhält ihre Datensätze über **`core.from-result`** und gibt sie über **`core.to-result`** zurück,
egal ob sie aus einem Workflow, als Mapping oder von einem Executor aufgerufen wird. So bleibt eine Pipeline in allen
drei Fällen dieselbe Datei und lässt sich auch einzeln starten.

- **Als Mapping** sind beide Transforms Stromanschlüsse: Was beim Mapping ankommt, gibt `core.from-result` batchweise
  weiter. Was `core.to-result` erreicht, gibt das Mapping weiter. Die Grenze `maxRows` gilt dabei nicht, denn es wird
  nichts gesammelt.
- **Aus Workflow und Executor** bleibt es bei Listen wie seit S24a.
- Die Beschriftungen werden allgemeiner: „Records from Caller“ und „Records to Caller“ statt „… Workflow“. Typnamen und
  Dateien bleiben unverändert.
- Eine Unter-Pipeline für ein Mapping hat **genau ein** `core.from-result` und **höchstens ein** `core.to-result`.
  Ohne `core.to-result` ist das Mapping ein Endpunkt, z. B. eine Pipeline, die nur schreibt.

### 3. Mapping: Transform `core.run-pipeline`

Das Mapping ist der **Transform `core.run-pipeline`**. ADR 0013 erlaubt denselben Typnamen für Action und Transform.
Die Bedeutung ist dieselbe: Diese Pipeline ausführen, hier mit dem Datenstrom als Ein- und Ausgang.

```yaml
- id: clean
  type: core.run-pipeline
  with:
    pipeline: pipelines/clean-addresses.pipeline.yaml
    params:
      COUNTRY: DE
```

- **Engine:** Die Unter-Pipeline läuft im selben Prozess als eigener Lauf mit eigener Lauf-ID. `parent_run_id` folgt
  mit S26. Die Kanäle des Mappings werden mit `core.from-result` bzw. `core.to-result` der Unter-Pipeline verbunden,
  die Backpressure bleibt durchgehend. Ein Abbruch des äußeren Laufs bricht die Unter-Pipeline ab. Scheitert die
  Unter-Pipeline, scheitert der Transform.
- **Fehler-Link:** Datensätze, die in der Unter-Pipeline auf einen Fehler-Link gehen, bleiben dort. Das Mapping hat
  selbst keinen Fehler-Link; Fehler werden in der Unter-Pipeline behandelt.
- **Typsystem:** Das Schema am Ausgang ist das Schema, das bei `core.to-result` der Unter-Pipeline ankommt. `Init` der
  Unter-Pipeline läuft mit dem eingehenden Schema des Mappings als Schema von `core.from-result`. Damit melden
  Tippfehler in Feldnamen auch durch die Unter-Pipeline hindurch (ADR 0007).
- **Zähler und Log:** Am Knoten stehen die Zähler des Mappings: gelesen, geschrieben. Das Log der Unter-Pipeline
  erscheint mit Präfix `<mapping-id>/<transform>: `, wie bei Aufrufen aus Workflows.

### 4. Pipeline-Executor: `core.pipeline-executor`

Wie in ADR 0008 (Abschnitt 3a), mit diesen Festlegungen:

- `groupBy: none | size | field`, `groupSize`, `groupField`. Je Gruppe läuft die Unter-Pipeline einmal, ihre
  Datensätze kommen als Liste über `core.from-result`.
- `params` werden je Lauf ausgewertet. `record` ist der erste Datensatz der Gruppe.
- **Ausgänge wie bei `core.switch`:** Die Konfigurationsfelder `results` und `rows` (`format: runnel-target`) nennen
  die Transforms, zu denen ein Daten-Link führt. `results` erhält einen Datensatz je Lauf (`runId`, `status`, `rows`,
  `durationMs`, `error` und die Parameter). `rows` erhält die Datensätze aus `core.to-result`. Alle übrigen
  Daten-Links erhalten die Eingangsdatensätze unverändert (`main`). Gruppen, deren Lauf scheitert, gehen auf den
  Fehler-Link.
  - Pipeline-Links brauchen so **keine neue Link-Art und kein neues Feld**.
  - Der Canvas beschriftet die Links mit dem Namen des Ausgangs; dafür ist vorher ein Mockup nötig.
- `copies` (Standard 1) begrenzt die gleichzeitigen Läufe.
- `core.workflow-executor` folgt demselben Muster, mit den `outputs` des Workflows statt `rows`.

### 5. Grenzen und Prüfung

- **Verschachtelung:** höchstens 10 Ebenen (Mapping in Mapping in …). Ein Selbstaufruf ist ein Fehler, schon bei
  `validate`, wenn die Pfade fest sind.
- **Feste Ziele** prüft `validate` vollständig: Datei, Parameter, genau ein `core.from-result` bei Mappings, Schema.
  Dynamische Ziele (S25b) prüft der Lauf.
- **Executor:** Die Zahl der Läufe ist nur durch die Eingabe begrenzt. Jeder Lauf hat die Grenzen seiner Pipeline.
  `results` und `rows` liegen nicht im Speicher, sondern strömen weiter.

### Sicherheit

Es entsteht keine neue Netzwerk- oder Sicherheitsgrenze. Ziele folgen S25b: nur Dateien des Projekts, bei dynamischen
Zielen `allowedTargets`. Neu ist, dass Parameter aus **Datensätzen** kommen können (Executor), also aus Dateien oder
Antworten von außen. Dynamische Ziele des Executors ohne `allowedTargets` meldet `validate` deshalb wie bei Actions als
Warnung. Grenzen gegen unbeschränkte Läufe: Verschachtelungstiefe und `copies`.

## Offene Fragen an den Maintainer

1. **Name des Mappings:** Transform `core.run-pipeline` (Vorschlag, parallel zur Action) oder eigener Name
   `core.mapping` (wie in Hop und ADR 0009)?
2. **Ein- und Ausgang:** `core.from-result`/`core.to-result` wiederverwenden (Vorschlag) oder eigene Transforms
   `core.mapping-input`/`core.mapping-output` mit fest deklarierten Feldern (wie Hop)? Eigene wären strenger, aber eine
   Pipeline wäre dann nicht mehr zugleich aus Workflows aufrufbar.
3. **Executor als Option des Mappings?** Statt eines eigenen `core.pipeline-executor` ginge auch
   `core.run-pipeline` mit `per: all | record | group` (ein Transform, weniger Begriffe). ADR 0009 hatte die
   Executor-Namen „bewusst beibehalten“. Vorschlag: getrennt lassen, weil sich Ausgänge und Parallelität deutlich
   unterscheiden.
4. **Reihenfolge:** Vorschlag:
   1. Mapping (Engine und Prüfung)
   2. Mockup für Mapping-Knoten und Executor-Ausgänge
   3. Mapping im Editor
   4. Executor je Datensatz
   5. Gruppen und `results`
   6. Workflow-Executor

## Konsequenzen

**Positiv**
- Eine Pipeline ist an einer Stelle definiert und auf drei Wegen aufrufbar: aus Workflows, als Mapping, vom Executor.
- Keine neue Link-Art: Ausgänge folgen dem bekannten Muster von `core.switch`.
- Feldprüfung und Typen gehen durch Mappings hindurch; Fehler fallen beim Entwerfen auf, nicht erst im Lauf.

**Negativ / zu beachten**
- `core.from-result`/`core.to-result` haben je nach Aufrufer zwei Betriebsarten (Liste bzw. Strom). Das muss in Doku
  und Panel klar erkennbar sein.
- Verschachtelte Läufe machen Log und Zähler tiefer. Bis S26 (Lauf-Historie mit `parent_run_id`) sieht man die
  Unter-Pipeline nur über das Log.
- Mapping und Executor im Canvas brauchen ein Mockup, bevor Editor-Teile umgesetzt werden.
