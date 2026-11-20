# Mockups

Entwürfe für Oberflächen, die vor der Umsetzung freigegeben werden ([Handbuch, Kap. 9](../README.md#9-mockups)).
Jedes Mockup ist eine statische HTML-Datei mit den Design-Tokens; im Browser direkt öffnen. Eine Leiste oben wechselt
zwischen hellem und dunklem Modus und den Zuständen (sie gehört nicht zum Entwurf).

| Mockup | Schritt | Zustände | Status |
|---|---|---|---|
| [`e4-editor-shell.html`](e4-editor-shell.html) | E4 – Editor-Gerüst | `file`, `empty`, `offline`, `error` | freigegeben mit PR #23, umgesetzt mit E4a/E4b |
| [`e5-canvas.html`](e5-canvas.html) | E5 – Canvas (nur lesen) | `canvas`, `problems`, `invalid`, `auto` | freigegeben mit PR #26, umgesetzt mit E5 |
| [`e6a-edit.html`](e6a-edit.html) | E6a – Canvas bearbeiten und speichern | `edit`, `deleted`, `conflict` | freigegeben mit PR #28, umgesetzt mit E6a |
| [`e6b-create.html`](e6b-create.html) | E6b – Knoten und Links anlegen | `palette`, `rename`, `connect`, `linkmenu` | freigegeben mit PR #30, umgesetzt mit E6b |
| [`e7-properties.html`](e7-properties.html) | E7 – Eigenschaften und Meldungen | `action`, `wait`, `link`, `workflow`, `problems` | freigegeben mit PR #32, umgesetzt mit E7a (`action`, `wait`) und E7b |
| [`e8-run.html`](e8-run.html) | E8 – Ausführen im Editor | `run`, `running`, `done`, `cancelled`, `unsaved` | freigegeben mit PR #35, umgesetzt mit E8a (Run, Live-Status, Rückfrage) und E8b (Log, Ergebnis im Panel, „wartet“) |
| [`s35-expressions.html`](s35-expressions.html) | S35 – Ausdruckseditor | `complete`, `functions`, `error`, `condition`, `expanded` | freigegeben mit PR #43, umgesetzt mit S35a (Editor im Panel, Link-Bedingung) und S35b (`expanded`) |

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
| E6a | Verschieben rastet auf 8 px ein |
| E6a | Löschen ohne Rückfrage, dafür mit Rückgängig (Hinweis „Deleted … · Undo“) |
| E6a | Tab schließen oder Editor verlassen mit ungespeicherten Änderungen: Rückfrage (Save, Don’t save, Cancel) im Stil des Konfliktdialogs, ohne eigenes Mockup |
| E6a | Kein automatisches Speichern; gespeichert wird bewusst (Save, Strg+S) |
| E6b | Link-Typ über ein Menü (On success, On failure, Always, Delete link) statt Durchklicken |
| E6b | Ausdrücke als Link-Bedingung (ƒ) erst mit dem Eigenschaftsdialog (E7) bzw. den Ausdrücken (S08) |
| E6b | Palette in der Seitenleiste unter dem Explorer |
| E6b | Ein gezogener Link, der sonst einen Zyklus schließen würde, erhält automatisch `loop: true` (↻) |
| E7 | Eigenschaften als festes Panel rechts neben dem Canvas statt als Dialog; mit × ausblendbar |
| E7 | Änderungen gelten nach kurzer Tipp-Pause (0,5 s) oder beim Verlassen des Felds, ohne „Apply“; je Feldänderung ein Schritt für Rückgängig; gespeichert wird weiter mit Save/Strg+S |
| E7 | Ausdrücke (ƒ) als Link-Bedingung erst mit S08; das Link-Panel zeigt bis dahin einen Hinweis |
| E7 | Parameter und Outputs des Workflows in E7 nur anzeigen; Bearbeiten wird ein eigener Roadmap-Schritt |
| E7 | Mehrzeilige Texte über das neue Schema-Format `runnel-text` (Ergänzung zu ADR 0014): wachsendes Textfeld, alle anderen Felder einzeilig |
| E7 | Umsetzung in zwei Schritten: E7a Panel für Actions mit Prüfung am Feld, E7b Link- und Workflow-Eigenschaften und die Leiste „Problems“ |
| E8 | **Run** startet mit den je Workflow gemerkten Werten (im Browser); der Dialog öffnet sich beim ersten Lauf eines Workflows mit Parametern oder mehreren Environments, sonst über ▾ |
| E8 | Environment je Lauf im Dialog, nicht in der Kopfzeile; eine globale Auswahl bei Bedarf später |
| E8 | Ungespeicherte Änderungen: Rückfrage (Save and run / Run saved version / Cancel), kein automatisches Speichern |
| E8 | Der Laufstatus bleibt bis zum nächsten Lauf; Verschieben behält ihn, Änderungen an Actions oder Links blenden ihn aus |
| E8 | Debug-Zeilen im Log standardmäßig an; der Schalter wird im Browser gemerkt |
| E8 | Das Log zeigt den letzten Lauf der Datei seit dem Start des Servers; frühere Läufe mit der Lauf-Historie (S26) |
| E7/E8 | Nachtrag nach dem Testen: Workflows zeigen die kompakte Dateileiste in jeder Ansicht (auch YAML und ohne Panel), damit sich beim Wechsel nichts verschiebt; das Fehler-Badge öffnet „Problems“ auch aus der YAML-Ansicht |
| E8 | Umsetzung in zwei Schritten: E8a Run (Button, Dialog, Rückfrage, Cancel) mit Live-Status auf dem Canvas, E8b Log-Tab und Ergebnis im Panel |
| S35 | CodeMirror 6 (MIT) statt Monaco: für viele kleine Felder gebaut, ein Bruchteil der Größe, Vorschläge und Tooltips eingebaut |
| S35 | Ausdruckseditor in allen Textfeldern unter `with:` (auch Zeitangaben) und in Link-Bedingungen; ID und Beschreibung bleiben normale Felder; Parameter-Standardwerte und Outputs mit E7c |
| S35 | Prüfung weiter über `/validate` (nach der Tipp-Pause); Funktionen mit Signatur und Beschreibung liefert der Server (`GET /expressions`), damit Editor und Engine nicht auseinanderlaufen |
| S35 | Vorschau mit Werten des letzten Laufs und Vorschläge für die Felder von `output` später |
| S35 | Umsetzung in zwei Schritten: S35a Editor im Panel und Link-Bedingung, S35b größerer Editor mit Referenz |
| S35 | Die Signaturhilfe erscheint über der Zeile (statt links neben dem Panel), damit die Vorschläge darunter sichtbar bleiben |
| S35 | Größerer Editor: Die Zeile unten zeigt die Meldungen des Felds beim Öffnen; geänderter Text wird erst nach Apply geprüft („Checked when applied“), statt bei jeder Eingabe den ganzen Workflow zu prüfen |

## Screenshots erzeugen

```bash
node design/mockups/screenshots.cjs e4-editor-shell file,empty,offline,error
node design/mockups/screenshots.cjs e5-canvas canvas,problems,invalid,auto
node design/mockups/screenshots.cjs e6a-edit edit,deleted,conflict
node design/mockups/screenshots.cjs e6b-create palette,rename,connect,linkmenu
node design/mockups/screenshots.cjs e7-properties action,wait,link,workflow,problems
node design/mockups/screenshots.cjs e8-run run,running,done,cancelled,unsaved
node design/mockups/screenshots.cjs s35-expressions complete,functions,error,condition,expanded
```

Erzeugt je Zustand `…-hell.png` und `…-dunkel.png` (1280 × 800, doppelte Auflösung). Voraussetzung: Playwright mit
Chromium. Fehlen Inter oder JetBrains Mono auf dem Rechner, nutzen die Screenshots die Ersatzschriften aus den Tokens.
