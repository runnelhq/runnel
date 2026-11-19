# Mockups

Entwürfe für Oberflächen, die vor der Umsetzung freigegeben werden ([Handbuch, Kap. 9](../README.md#9-mockups)).
Jedes Mockup ist eine statische HTML-Datei mit den Design-Tokens; im Browser direkt öffnen. Eine Leiste oben wechselt
zwischen hellem und dunklem Modus und den Zuständen (sie gehört nicht zum Entwurf).

| Mockup | Schritt | Zustände | Status |
|---|---|---|---|
| [`e4-editor-shell.html`](e4-editor-shell.html) | E4 – Editor-Gerüst | `file`, `empty`, `offline`, `error` | zur Freigabe |

## Screenshots erzeugen

```bash
node design/mockups/screenshots.cjs e4-editor-shell file,empty,offline,error
```

Erzeugt je Zustand `…-hell.png` und `…-dunkel.png` (1280 × 800, doppelte Auflösung). Voraussetzung: Playwright mit
Chromium. Fehlen Inter oder JetBrains Mono auf dem Rechner, nutzen die Screenshots die Ersatzschriften aus den Tokens.
