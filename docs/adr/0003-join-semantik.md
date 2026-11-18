# ADR 0003: Join-Semantik in Workflows – konfigurierbar, Standard `all`

- **Status:** Angenommen
- **Datum:** 2026-11-18
- **Bezug:** [Konzept, Kap. 6.1 – Workflow-Engine](../KONZEPT.md#61-workflow-engine)

## Kontext

In einem Workflow kann eine Action mehrere eingehende Hops haben, z. B. wenn parallele Zweige zusammenlaufen
oder mehrere Fehlerpfade auf dieselbe Benachrichtigung zeigen. Zu entscheiden war, **wann** eine solche Action startet:

- Apache Hop startet die Action für jedes eingehende Signal (ggf. mehrfach).
- n8n/Make-Nutzer erwarten eher, dass zusammenlaufende Zweige abgewartet werden.

Beide Verhalten haben legitime Einsatzzwecke. Pipelines sind nicht betroffen – dort werden Datenströme über
Transforms wie „Anhängen“ oder „Merge-Join“ zusammengeführt.

## Entscheidung

Die Join-Semantik ist **pro Action konfigurierbar**, mit einem projektweiten Standard. **Standard ist `all`.**

### Modi

| Modus | Verhalten |
|---|---|
| `all` (Standard) | Startet **einmal**, sobald jeder eingehende Hop entweder `fired` oder `dead` ist und mindestens einer `fired` ist |
| `first` | Startet **einmal** beim ersten eingehenden Signal; weitere Signale werden ignoriert |
| `each` | Startet **für jedes** eingehende Signal (klassisches Hop-Verhalten), kann also mehrfach laufen |

### Konfiguration

```yaml
# project.yaml
workflow:
  defaultJoin: all          # all | first | each
```

```yaml
# Workflow – Überschreiben pro Action
actions:
  - id: build-report
    type: core.run-pipeline
    join: all
    joinTimeout: 30m        # nur bei 'all': maximale Wartezeit, danach Fehlschlag der Action
```

Rangfolge: Action (`join`) > Projekt (`workflow.defaultJoin`) > eingebauter Standard (`all`).

### Dead-Path-Elimination

Damit `all` nicht auf Zweige wartet, die nie ein Signal liefern, verfolgt die Engine den Zustand jedes Hops
(`pending` → `fired` | `dead`):

1. Ist eine Action beendet, werden ausgehende Hops mit erfüllter Bedingung `fired`, alle anderen `dead`.
2. Sind **alle** eingehenden Hops einer Action `dead`, wird die Action selbst `skipped`, und **alle** ihre ausgehenden
   Hops werden `dead` (rekursive Propagation).
3. Eine `all`-Action wird bereit, sobald kein eingehender Hop mehr `pending` ist und mindestens einer `fired` ist.
4. Deaktivierte Hops zählen nicht als Eingang.

Beispiel (Fehlerbehandlung aus dem Konzept): `notify-error` hat zwei eingehende `failure`-Hops von `wait-file` und `load`.
Schlägt `wait-file` fehl, wird `load` übersprungen, sein Hop ist `dead` → `notify-error` startet genau einmal.
Schlägt `load` fehl, ist der `failure`-Hop von `wait-file` bereits `dead` → ebenso genau ein Start.

Übersprungene Actions werden in der Run-Historie mit Status `skipped` geführt und im Editor ausgegraut dargestellt.

### Schleifen

- Hops zurück im Graphen werden als **Schleifen-Hop** markiert (`loop: true`, im Editor automatisch erkannt).
- Schleifen-Hops zählen **nicht** als Eingang für die Join-Auswertung.
- Bei jedem neuen Schleifendurchlauf werden die Hop-Zustände innerhalb des Schleifenkörpers zurückgesetzt.

### Validierung

| Prüfung | Stufe |
|---|---|
| `all` an einem Knoten mit nicht markiertem Rückwärts-Hop | Fehler |
| `each` an einem Knoten hinter parallel laufenden Zweigen | Hinweis („Action kann mehrfach laufen“) |
| `joinTimeout` bei `first`/`each` | Warnung (wird ignoriert) |

### Editor

- Symbol am Knoten bei mehr als einem Eingang: ∧ `all`, ∨ `first`, ⇉ `each`.
- Umschalten im Eigenschaftsfenster; Abweichungen vom Projektstandard werden hervorgehoben.

## Begründung für `all` als Standard

- **Keine versehentliche Mehrfachausführung** (z. B. doppelt versendete Mails, doppelt importierte Daten).
- **Deterministisch**: Ergebnis hängt nicht von der Reihenfolge ab, in der parallele Zweige fertig werden.
- **Intuitiv** für Nutzer von n8n/Make; mit Dead-Path-Elimination funktionieren auch Fehler-Sammelpfade wie erwartet.
- Hop-Verhalten bleibt über `each` verfügbar (relevant für einen späteren Import von Hop-Workflows).

## Konsequenzen

**Positiv**
- Ein Modell deckt Zusammenführung, Fehlerbehandlung und Hop-kompatibles Verhalten ab.
- Status `skipped` macht nachvollziehbar, welche Pfade nicht durchlaufen wurden.

**Negativ / zu beachten**
- Die Engine muss Hop-Zustände je Run verwalten und persistieren (auch für Resume) → zusätzliche Tabelle bzw. Feld in `node_runs`.
- Schleifen benötigen die explizite Markierung von Rückwärts-Hops und das Zurücksetzen von Zuständen.
- Ein späterer Hop-Import muss `join: each` setzen, um das Originalverhalten zu erhalten.
