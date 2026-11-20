# ADR 0003: Join-Semantik in Workflows – konfigurierbar, Standard `all`

- **Status:** Angenommen
- **Datum:** 2026-11-18
- **Bezug:** [Konzept, Kap. 6.1 – Workflow-Engine](../KONZEPT.md#61-workflow-engine)

## Kontext

In einem Workflow kann eine Action mehrere eingehende Links haben, z. B. wenn parallele Zweige zusammenlaufen
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
| `all` (Standard) | Startet **einmal**, sobald jeder eingehende Link entweder `fired` oder `dead` ist und mindestens einer `fired` ist |
| `first` | Startet **einmal** beim ersten eingehenden Signal; weitere Signale werden ignoriert |
| `each` | Startet **für jedes** eingehende Signal (Verhalten von Apache Hop), kann also mehrfach laufen |

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

Damit `all` nicht auf Zweige wartet, die nie ein Signal liefern, verfolgt die Engine den Zustand jedes Links
(`pending` → `fired` | `dead`):

1. Ist eine Action beendet, werden ausgehende Links mit erfüllter Bedingung `fired`, alle anderen `dead`.
2. Sind **alle** eingehenden Links einer Action `dead`, wird die Action selbst `skipped`, und **alle** ihre ausgehenden
   Links werden `dead` (rekursive Propagation).
3. Eine `all`-Action wird bereit, sobald kein eingehender Link mehr `pending` ist und mindestens einer `fired` ist.
4. Deaktivierte Links zählen nicht als Eingang.

Beispiel (Fehlerbehandlung aus dem Konzept): `notify-error` hat zwei eingehende `failure`-Links von `wait-file` und `load`.
Schlägt `wait-file` fehl, wird `load` übersprungen, sein Link ist `dead` → `notify-error` startet genau einmal.
Schlägt `load` fehl, ist der `failure`-Link von `wait-file` bereits `dead` → ebenso genau ein Start.

Übersprungene Actions werden in der Run-Historie mit Status `skipped` geführt und im Editor ausgegraut dargestellt.

### Schleifen

- Links zurück im Graphen werden als **Schleifen-Link** markiert (`loop: true`, im Editor automatisch erkannt).
- Schleifen-Links zählen **nicht** als Eingang für die Join-Auswertung.
- Bei jedem neuen Schleifendurchlauf werden die Link-Zustände innerhalb des Schleifenkörpers zurückgesetzt.

### Validierung

| Prüfung | Stufe |
|---|---|
| `all` an einem Knoten mit nicht markiertem Rückwärts-Link | Fehler |
| `each` an einem Knoten hinter parallel laufenden Zweigen | Hinweis („Action kann mehrfach laufen“) |
| `joinTimeout` bei `first`/`each` | Warnung (wird ignoriert) |

### Editor

- Symbol am Knoten bei mehr als einem Eingang: ∧ `all`, ∨ `first`, ⇉ `each`.
- Umschalten im Eigenschaftsfenster; Abweichungen vom Projektstandard werden hervorgehoben.

## Begründung für `all` als Standard

- **Keine versehentliche Mehrfachausführung** (z. B. doppelt versendete Mails, doppelt importierte Daten).
- **Deterministisch**: Ergebnis hängt nicht von der Reihenfolge ab, in der parallele Zweige fertig werden.
- **Intuitiv** für Nutzer von n8n/Make; mit Dead-Path-Elimination funktionieren auch Fehler-Sammelpfade wie erwartet.
- Das Verhalten von Apache Hop bleibt über `each` verfügbar (relevant für einen späteren Import von Hop-Workflows).

## Konsequenzen

**Positiv**
- Ein Modell deckt Zusammenführung, Fehlerbehandlung und Hop-kompatibles Verhalten ab.
- Status `skipped` macht nachvollziehbar, welche Pfade nicht durchlaufen wurden.

**Negativ / zu beachten**
- Die Engine muss Link-Zustände je Run verwalten und persistieren (auch für Resume) → zusätzliche Tabelle bzw. Feld in `node_runs`.
- Schleifen benötigen die explizite Markierung von Rückwärts-Links und das Zurücksetzen von Zuständen.
- Ein späterer Hop-Import muss `join: each` setzen, um das Originalverhalten zu erhalten.

## Umsetzung (S06)

Ergänzende Festlegungen aus der Implementierung (`internal/engine/workflow/scheduler.go`):

- **`all` startet auch, wenn ein Zweig fehlgeschlagen ist** – solange mindestens ein eingehender Link `fired` ist.
  Soll eine Action nur laufen, wenn *alle* Vorgänger erfolgreich waren, gehören die Fehlerpfade auf eigene Links
  (z. B. zu einer Fehlerbehandlung mit `join: first`), und das Gesamtergebnis wird über diese bestimmt.
- **Fehler in einem Zweig:** Die übrigen Zweige laufen zu Ende; danach ist der Lauf fehlgeschlagen, wenn ein Fehler
  von keinem Link (Fehler- oder Immer-Link) behandelt wurde. Mehrere unbehandelte Fehler werden gemeinsam gemeldet.
- **`core.success` / `core.abort` beenden den Lauf sofort** und brechen laufende Zweige ab (Kontext wird abgebrochen).
- **Schleifen:** Ein Schleifen-Link startet sein Ziel als neue Iteration; die Link-Zustände und Join-Markierungen
  aller Actions *hinter* dem Ziel werden zurückgesetzt. Zwischenergebnisse einer Iteration (z. B. ein Fehlschlag vor
  der Wiederholung) wirken dabei schon auf Joins außerhalb der Schleife. Für reine Wiederholungen ist `retry` (S07)
  der einfachere Weg.
- **Projektstandard:** `workflow.defaultJoin` in `project.yaml`.
- **`joinTimeout`** (S07): Die Wartezeit beginnt, sobald der erste eingehende Link gefeuert hat. Läuft sie ab, startet
  die Action nicht, sondern gilt als fehlgeschlagen ("join timeout: still waiting for …"); ihre Fehler-Links werden
  verfolgt, später eintreffende Links ignoriert.
