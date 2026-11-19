# Den Editor entwickeln

Der Editor ist eine React-Anwendung in [`web/`](../../web). Sie wird gebaut, in die `runnel.exe` eingebettet und von
`runnel serve` unter `http://127.0.0.1:7300/` ausgeliefert. Mit dem Server spricht sie nur über die
[API](API.md). Gestaltung: [Corporate Design](../../design/README.md), freigegebene [Mockups](../../design/mockups/README.md).

> Stand: E6a – Kopfzeile, Farbschema, Verbindungsstatus, Explorer, Tabs, Workflows als Canvas mit Verschieben, Löschen, Rückgängig und Speichern. Neue Knoten und Links folgen mit E6b.

## Voraussetzungen

- Go (Version laut `go.mod`)
- [Node.js](https://nodejs.org/) 22 LTS (`web/.node-version`) mit npm

## Befehle

| Zweck | Befehl |
|---|---|
| npm-Pakete installieren (exakt nach `package-lock.json`) | `make web-deps` |
| Editor bauen (nach `internal/webui/dist/`) | `make web` |
| Editor und exe bauen | `make build` |
| Nur die exe bauen (bettet den letzten Editor-Build ein) | `make build-go` |
| Entwickeln mit Live-Reload | `bin/runnel serve examples/hello`, dann in `web/`: `npm run dev` → `http://localhost:5173` (leitet `/api` an Port 7300 weiter) |
| Typen prüfen | in `web/`: `npm run typecheck` |
| End-to-End-Tests (Playwright) | `make e2e` (baut alles; startet `runnel serve examples/hello` auf Port 7399 und für Tests, die speichern, einen zweiten Server auf einer temporären Kopie auf Port 7398 – `web/e2e/serve-copy.mjs`) |

Ohne Node baut `make build-go` eine exe ohne Editor; `runnel serve` zeigt dann unter `/` einen Hinweis. Die Go-Tests
brauchen keinen Editor-Build.

## Aufbau

```
web/
  index.html            # Einstieg
  src/
    main.tsx            # React-Start
    App.tsx             # Gerüst: Kopfzeile, Explorer, Tabs, Datei, Statusleiste
    api.ts              # API-Zugriff, Verbindung (Server-Sent Events)
    files.ts            # Projektbaum, Dateien laden, Kodierung erkennen
    workflow.ts         # /validate, Workflow-Modell, Link-Arten, Zuordnung der Meldungen, Anordnung (dagre)
    catalog.ts          # Descriptors aus /catalog (Kategorie → Kachelfarbe)
    yaml.ts             # Hervorhebung für die Nur-lesen-Ansicht (zeilenweise, ohne Abhängigkeit)
    theme.ts            # Farbschema (System/Light/Dark, gespeichert im Browser)
    index.css           # Tailwind + Design-Tokens + Schriften
    components/         # eine Datei je Komponente
  e2e/                  # Playwright-Tests
internal/webui/         # go:embed des Builds (dist/ ist nicht im Repository, nur .gitkeep)
design/fonts/           # Inter, JetBrains Mono (woff2, OFL) – lokal, kein Schrift-CDN
```

## Regeln

- **Oberfläche nur nach freigegebenem Mockup** ([design/README.md, Kap. 9](../../design/README.md#9-mockups)).
- **Nur Tokens:** Farben über die semantischen Tailwind-Klassen (`bg-background`, `text-muted-foreground`,
  `border-border`, `bg-warning-soft` …), keine Hex-Werte, keine `dark:`-Varianten – das Farbschema wechselt über
  `<html data-theme>` und die Tokens. Schriftgrößen `text-2xs` … `text-xl` entsprechen den Tokens (Basis 13 px).
- **Texte englisch** (ADR 0015), Ton nach Handbuch Kap. 7; Zustände nie nur über Farbe (Symbol oder Text dazu).
- **Keine externen Ressourcen:** Der Server setzt eine Content-Security-Policy, die nur eigene Dateien und die eigene
  API erlaubt. Schriften, Icons und Bilder werden mitgebaut.
- **Abhängigkeiten** nur mit Begründung im PR, Versionen exakt (`--save-exact`), Lizenz MIT-kompatibel (ADR 0004).
  Was in den Build eingeht (`dependencies`), landet automatisch in `THIRD_PARTY_NOTICES.txt` und in der SBOM;
  Werkzeuge gehören in `devDependencies`.
- **Tests:** Jede sichtbare Funktion bekommt einen Playwright-Test gegen den echten Server (`web/e2e/`).

## Verhalten

| Thema | Umsetzung |
|---|---|
| Laden | Projekt, Version und Baum beim Start und nach jeder Wiederverbindung; geöffnete Dateien werden nach einer Wiederverbindung neu geladen |
| Kodierung | wie der Runner: UTF-8 (mit/ohne BOM), UTF-16 mit BOM; sonst Anzeige als Windows-1252. Die Statusleiste nennt die erkannte Kodierung |
| Hervorhebung | nur YAML-Dateien; `${…}` und `{{ … }}` in der Ausdrucksfarbe (Entscheidung zum Mockup E4) |
| Canvas | React Flow (`@xyflow/react`); Knoten `components/ActionNode.tsx`, Links `components/LinkEdge.tsx` (Symbol bei 35 % ab der Quelle). Das Modell kommt aus `/validate` (gesendet wird der angezeigte Text), nicht aus einem YAML-Parser im Browser |
| Anordnung | `layout` der Datei (Pixel, linke obere Ecke, Karte 176 × 52 px); fehlen Positionen, ordnet dagre von links nach rechts an und ein Hinweis erscheint |
| Meldungen | über den YAML-Pfad (`$.actions[i]…`) dem Knoten zugeordnet; Zähler in der Dateileiste; Klick auf den Knoten zeigt sie mit „Show in YAML ›“ |
| Bearbeiten | Jede Änderung (verschieben mit 8-px-Raster, löschen) geht an `POST /workflows/edit`; der Server ändert den Text zeilengenau. Rückgängig/Wiederholen speichern Textstände (bis 100). Ungespeicherte Änderungen bleiben beim Tab-Wechsel erhalten; ein Neuladen nach Wiederverbindung überschreibt sie nie |
| Speichern | `PUT /files/{path}` mit dem ETag des geladenen Stands; `412` öffnet den Konfliktdialog (Cancel, Reload, Overwrite = `If-Match: *`). Speichern wartet auf eine noch laufende Änderung. Gespeichert wird als UTF-8 – auch Dateien, die als UTF-16 oder Windows-1252 geöffnet wurden |
| Schließen | Tab mit ungespeicherten Änderungen: Rückfrage Save / Don’t save / Cancel; beim Verlassen der Seite fragt der Browser |
| Pfad kopieren | vollständiger Pfad mit dem Trennzeichen des Projektordners (`\` unter Windows) |

## Auslieferung

| Thema | Umsetzung |
|---|---|
| Einbettung | `internal/webui` bettet `dist/` per `go:embed` ein |
| Caching | `assets/*` (Dateiname mit Hash) ein Jahr unveränderlich, alles andere `no-cache` |
| Sicherheit | Content-Security-Policy (`default-src 'self'`, kein Einbetten in fremde Seiten), `nosniff`, kein Referrer; Host-/Origin-Prüfung wie bei der API |
| Ansichten ohne Dateiendung | erhalten `index.html` (für spätere Adressen im Editor) |
| Lizenzen und SBOM | `scripts/third-party-notices.sh` ergänzt npm-Pakete und Schriften; `scripts/merge-sbom.mjs` fügt die SBOM des Editors (`cyclonedx-npm`) in die SBOM der exe ein |
