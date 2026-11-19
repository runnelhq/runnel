# Mockups

Entwürfe für Oberflächen, die vor der Umsetzung freigegeben werden ([Handbuch, Kap. 9](../README.md#9-mockups)).
Jedes Mockup ist eine statische HTML-Datei mit den Design-Tokens; im Browser direkt öffnen. Eine Leiste oben wechselt
zwischen hellem und dunklem Modus und den Zuständen (sie gehört nicht zum Entwurf).

| Mockup | Schritt | Zustände | Status |
|---|---|---|---|
| [`e4-editor-shell.html`](e4-editor-shell.html) | E4 – Editor-Gerüst | `file`, `empty`, `offline`, `error` | freigegeben mit PR #23, umgesetzt mit E4a/E4b |
| [`e5-canvas.html`](e5-canvas.html) | E5 – Canvas (nur lesen) | `canvas`, `problems`, `invalid`, `auto` | freigegeben mit PR #26 |

## Entscheidungen

| Mockup | Entscheidung |
|---|---|
| E4 | `${…}`-Variablen in der Ausdrucksfarbe (Indigo), wie `{{ … }}`-Ausdrücke |
| E4 | Mehrere geöffnete Dateien als Tabs schon in E4 |
| E4 | Umschalter für das Farbschema in der Kopfzeile (Standard: System); ein Einstellungsmenü erst, wenn es mehrere Einstellungen gibt |
| E4 | Zielgerät Desktop-PC; schmale Fenster stehen im Backlog |
| E5 | Knoten als Karten (176 × 52 px); `layout`-Koordinaten sind Pixel der linken oberen Ecke; die Beispiele werden mit E5 auseinandergezogen |
| E5 | Link-Symbol bei 35 % des Wegs ab der Quelle, damit sich kreuzende Links nicht überdecken |
| E5 | Markierung oben rechts am Knoten: beim Ausführen (E8) hat der Laufstatus Vorrang; Meldungen bleiben über Rahmen und Zähler sichtbar |
| E5 | Kachelfarbe nach Kategorie (Core-Actions: `general`, grau), Unterscheidung über das Icon je Typ |
| E5 | Keine Minimap; bei Bedarf später |
| E5 | Abhängigkeiten: `@xyflow/react` (MIT) für den Canvas, `@dagrejs/dagre` (MIT) für das automatische Anordnen statt `elkjs` (EPL-2.0/GPL-3.0, nicht MIT-kompatibel) |

## Screenshots erzeugen

```bash
node design/mockups/screenshots.cjs e4-editor-shell file,empty,offline,error
node design/mockups/screenshots.cjs e5-canvas canvas,problems,invalid,auto
```

Erzeugt je Zustand `…-hell.png` und `…-dunkel.png` (1280 × 800, doppelte Auflösung). Voraussetzung: Playwright mit
Chromium. Fehlen Inter oder JetBrains Mono auf dem Rechner, nutzen die Screenshots die Ersatzschriften aus den Tokens.
