# ADR 0009: „Link“ statt „Hop“ für Verbindungen zwischen Knoten

- **Status:** Angenommen
- **Datum:** 2026-11-18
- **Bezug:** [Konzept, Kap. 2.4 – Link](../KONZEPT.md#24-link)

## Kontext

Die Verbindungen zwischen Actions (Workflow) bzw. Transforms (Pipeline) hießen im Konzept bisher **„Hop“** –
übernommen von Apache Hop. Anders als „Workflow“, „Pipeline“, „Action“ oder „Transform“, die in vielen ETL- und
Orchestrierungswerkzeugen üblich sind, ist „Hop“ der **Name des Apache-Projekts selbst**. Die Übernahme könnte als
Kopie wahrgenommen werden und in der Community schlecht ankommen.

Betrachtete Alternativen:

| Begriff | Bewertung |
|---|---|
| **Link** | Neutral, kurz, auf Deutsch und Englisch verständlich, keinem Projekt zugeordnet, passt für Workflow und Pipeline |
| Edge | Graphentheoretisch korrekt, für Fachanwender zu technisch |
| Wire | Anschaulich, aber von Node-RED geprägt – gleiches Problem |
| Route | Passt zu bedingten Wegen im Workflow, weniger zu Datenströmen; ähnlich bei Make |
| Flow | Passt zum Produktnamen, aber Verwechslung mit „Workflow“ |
| Connection | Kollidiert mit den Connection-Metadaten (DB, HTTP, SMTP) |
| Transition / Channel | Jeweils nur für eine Graph-Art passend |

## Entscheidung

Verbindungen zwischen Knoten heißen **Link**.

| Bisher | Neu |
|---|---|
| Hop | Link |
| YAML `hops:` | `links:` |
| Daten-Hop / Error-Hop / Info-Hop | Daten-Link / Fehler-Link / Info-Link |
| Schleifen-Hop | Schleifen-Link |
| Hop-Bedingung, Hop-Zustand | Link-Bedingung, Link-Zustand |
| `EmitTo(hop, …)`, `emitTo('hop', …)` | `EmitTo(link, …)`, `emitTo('link', …)` |
| `onError: error-hop` | `onError: error-link` |
| Spalte `samples.hop` | `samples.link` |

Im Workflow werden Links nach ihrer Bedingung benannt: **Erfolgs-Link**, **Fehler-Link**, **Immer-Link**, **Bedingungs-Link**.

**Unverändert bleiben**:

- Allgemein übliche Begriffe: Workflow, Pipeline, Action, Transform, Mapping, Run-Konfiguration.
- **Pipeline Executor** und **Workflow Executor** (`core.pipeline-executor`, `core.workflow-executor`) – bewusst beibehalten.
- Verweise auf **Apache Hop als Projekt** (Inspiration, Vergleich, „kompatibel zu Apache Hop“, späterer Import aus Apache Hop).
  Diese Nennung ist fair und erwünscht.

## Konsequenzen

**Positiv**
- Eigenständige, neutrale Begrifflichkeit; kein Eindruck einer Kopie.
- Der Begriff ist auch für Einsteiger ohne Hop-Hintergrund sofort verständlich.

**Negativ / zu beachten**
- Nutzer mit Hop-Erfahrung müssen umdenken; die Doku enthält dafür eine kurze Begriffsgegenüberstellung
  (Hop ↔ Runnel).
- Ein späterer Import aus Apache Hop bildet dessen Hops auf Links ab.

## Umsetzung

Da noch kein Code existiert, wurden Konzept und ADRs 0002, 0003, 0006, 0007 und 0008 terminologisch angepasst
(ohne inhaltliche Änderung der dortigen Entscheidungen).
