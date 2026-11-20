# Den Editor entwickeln

Der Editor ist eine React-Anwendung in [`web/`](../../web). Sie wird gebaut, in die `runnel.exe` eingebettet und von
`runnel serve` unter `http://127.0.0.1:7300/` ausgeliefert. Mit dem Server spricht sie nur über die
[API](API.md). Gestaltung: [Corporate Design](../../design/README.md), freigegebene [Mockups](../../design/mockups/README.md).

> Stand: S25a2 – Kopfzeile, Farbschema, Verbindungsstatus, Explorer, Palette, Tabs, Workflows als Canvas mit Anlegen, Umbenennen, Verbinden, Verschieben, Löschen, Rückgängig und Speichern; Eigenschaften von Actions, Links und Workflow im Panel; Leiste „Problems“; Ausführen mit Live-Status, Log und Ergebnis im Panel; Pipelines als Canvas mit Transforms, Daten- und Fehler-Links (ohne Ausführen, das folgt mit S25a3). Parameter und Outputs bearbeiten (E7c) ist zurückgestellt (Backlog der Roadmap).

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
    App.tsx             # Gerüst: Kopfzeile, Explorer und Palette, Tabs, Datei, Statusleiste
    api.ts              # API-Zugriff, Verbindung (Server-Sent Events)
    files.ts            # Projektbaum, Dateien laden, Kodierung erkennen
    workflow.ts         # /validate, Workflow- und Pipeline-Modell (graphOf), Link-Arten, Zuordnung der Meldungen, Anordnung (dagre)
    catalog.ts          # Descriptors aus /catalog (Kategorie → Kachelfarbe, Namen der Kategorien)
    expressions.ts      # Ausdrücke: Referenz aus /expressions, Tokens, Vorschläge, Lage von Meldungen
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
| Pipelines | `*.pipeline.yaml` öffnen ebenfalls als Canvas (Mockup `design/mockups/s25a-pipeline.html`). `graphOf` in `workflow.ts` macht aus der Pipeline von `/validate` denselben Graphen wie bei Workflows (`kind: "pipeline"`, Transforms als Knoten, jeder Link mit `type`); Änderungen gehen an `POST /pipelines/edit` (`pipelineEdit`). Transforms der Kategorie Input haben keinen Eingang. Datenlinks sind grau ohne Symbol, Fehlerlinks rot gestrichelt mit ✖. Neue Links sind Datenlinks; Links, die einen Zyklus schließen würden, und doppelte Links werden nicht angelegt. „Run“ fehlt bis S25a3 |
| Anordnung | `layout` der Datei (Pixel, linke obere Ecke, Karte 176 × 52 px); fehlen Positionen, ordnet dagre von links nach rechts an und ein Hinweis erscheint |
| Meldungen | über den YAML-Pfad (`$.actions[i]…`) dem Knoten zugeordnet; Zähler in der Dateileiste; Klick auf den Knoten zeigt sie mit „Show in YAML ›“ |
| Palette | `components/Palette.tsx` unter dem Explorer: Action-Typen aus `/catalog` nach Kategorie, Suche über Typ, Name und Beschreibung (`/` springt hinein, Esc leert). Ziehbar nur, wenn ein Workflow oder eine Pipeline als Canvas offen ist; `core.start` ist gesperrt, wenn es schon eine Start-Action gibt. Bei einer Pipeline zeigt sie die Transforms nach Kategorie (Input, Transform, Flow, Execution, Combine, Output; Farben in `catalog.ts`) |
| Anlegen | Ablegen auf dem Canvas (HTML-Drag-and-drop, Vorschaukarte im 8-px-Raster); die ID ist der Typname mit der kleinsten freien Nummer (`log-1`). Danach ist die ID ein Eingabefeld: Enter benennt um, Esc behält die ID; später per Doppelklick auf die ID. Neue Actions haben noch keine Konfiguration (`with:`), Pflichtfelder erscheinen als Meldung bis zum Eigenschaftsdialog (E7) |
| Verbinden | Vom Ausgang (rechts) zum Eingang (links) ziehen; neue Links sind „On success“. Würde der Link einen Zyklus schließen, erhält er `loop: true` (↻). Links auf sich selbst und doppelte Links werden nicht angelegt |
| Link-Menü | Klick auf das Link-Symbol oder den Link: On success / On failure / Always, Delete link; ein gewählter Link wird auch mit Entf gelöscht. Ausdrücke (ƒ) im Eigenschaften-Panel. Bei Pipelines: Data / Error (Tasten D und E), Delete link |
| Eigenschaften | `components/PropertiesPanel.tsx` rechts neben dem Canvas (mit × ausblendbar, „Show properties“ in der Dateileiste blendet es wieder ein); Workflows haben in jeder Ansicht (Canvas mit oder ohne Panel, YAML) dieselbe kompakte Dateileiste: Dateiname mit Pfad im Tooltip und „Copy path“ als Symbol daneben, ohne Badge „Workflow“, „Save“ ohne Kürzel – beim Wechsel der Ansicht verschiebt sich nichts. Andere Dateien zeigen den Pfad als Ordner / Datei, die Art als Badge und „Copy path“ rechts. Für die gewählte Action: ID, Description, Konfiguration als Formular aus dem Schema des Descriptors (`enum` → Auswahl, Boolean → Kontrollkästchen, Zahlen, `runnel-duration`, `runnel-text` → wachsendes Textfeld; Listen und verschachtelte Werte nur in der YAML-Ansicht), Timeout, Retry, Continue on error, Join (nur bei mehreren eingehenden Links) |
| Übernehmen | Textfelder nach 0,5 s Tipp-Pause, beim Verlassen oder mit Enter; Auswahl, Kontrollkästchen und Schalter sofort. Leere Felder werden aus der Datei entfernt. Mehrere Änderungen desselben Felds hintereinander sind ein Schritt für Rückgängig. Zahlen, die keine sind (z. B. Retry „x“), werden nicht geschrieben, sondern am Feld gemeldet |
| Ausdrücke | `components/ExpressionInput.tsx` (CodeMirror 6) für alle Textfelder unter `with:` und die Link-Bedingung (Mockup `design/mockups/s35-expressions.html`): `${…}` und `{{ … }}` in der Ausdrucksfarbe, darin Kontextnamen, Funktionen, Texte und Zahlen. `{{` ergänzt `}}`, `${` ergänzt `}` und öffnet Vorschläge; sonst nach `.` oder mit Strg+Leertaste: Kontext und Funktionen, Parameter mit Typ, `results.<id>` und die Felder eines Ergebnisses, Felder von `run`, Variablen nach `${`; Datumsmuster in `formatDate`/`parseDate`. Signaturhilfe über der Zeile, Wert einer Variablen beim Zeigen. Meldungen von `/validate` werden in der Zeile markiert (unbekannter Name gewellt unterstrichen, mit „Replace with …“). ⤢ bzw. Strg+Umschalt+E öffnet den größeren Editor (`components/ExpressionDialog.tsx`, S35b): Zeilennummern, Referenz aus Kontext (Parameter, Variablen, `run`, `results.<id>`) und Funktionen nach Gruppen mit Suche; ein Klick fügt am Cursor ein (außerhalb eines Ausdrucks mit `{{ }}`). Apply mit Strg+Enter, Escape schließt erst eine offene Vorschlagsliste, dann den Dialog; keine Taste erreicht die Seite dahinter. Einzeilige Felder übernehmen mit Enter, mehrzeilige (`runnel-text`) beim Verlassen; das Übernehmen beim Verlassen hängt am DOM-Ereignis `blur`, weil CodeMirror den Fokusverlust erst nach einem Klick meldet |
| Link und Workflow | Ein gewählter Link zeigt Von → Nach, Bedingung (Success/Failure/Always/ƒ Expr.; ein Ausdruck ohne `{{ }}` wird eingeklammert), Loop link, Disabled und „Delete link“. Ist nichts gewählt, zeigt das Panel den Workflow: Name, Description, Settings (Timeout, Max. concurrent runs); Parameter und Outputs nur zur Ansicht (Bearbeiten mit E7c, zurückgestellt). Mehrere gewählte Elemente: Hinweis |
| Transform, Pipeline-Link, Pipeline | Ein Transform zeigt ID, Konfiguration aus dem Schema und „Execution“: Copies (Zahl mit − und +; nur bei Transforms mit `parallel` im Descriptor über 1, 1 entfernt das Feld) und Distribution (Copy to all / Distribute, Distribute erst ab zwei ausgehenden Datenlinks). Ein Pipeline-Link zeigt Von → Nach und Type (Data / Error) mit einem Hinweis, ob der Transform Datensätze an Fehlerlinks gibt (`recordErrors`). Ohne Auswahl: Name, Description, Settings (Batch size, Buffer size, Strict) und Parameter zur Ansicht |
| Felder | Aus `schemas` von `/validate` (S25a2): ein Transform zeigt „Incoming fields“ (Name und Typ, `any` für unbekannt; Felder, die die Konfiguration nennt – als `record.<name>` oder als Wert –, sind hinterlegt), ein Pipeline-Link „Fields on this link“ (Datenlink: Ausgabe der Quelle, Fehlerlink: mit den Fehlerfeldern, gedimmt). Ein offenes Schema endet mit „and fields known only when the pipeline runs“. Solange Felder oder Links Fehler haben, erscheint ein Hinweis statt der Liste |
| Problems | `components/ProblemsBar.tsx` unter dem Canvas: eingeklappt mit Zähler, ein Klick darauf oder auf das Fehler-/Warnungs-Badge der Dateileiste klappt die Tabelle auf (Meldung, wo, Zeile); das Badge wechselt dazu auch aus der YAML-Ansicht zum Canvas. Ein Klick auf eine Zeile wählt Action oder Link (oder nichts für Workflow-Felder), öffnet das Panel und setzt den Cursor ins Feld, das kurz aufleuchtet; Meldungen ohne Feld springen in die YAML-Ansicht. Zuordnung über den YAML-Pfad (`problemTarget` in `workflow.ts`) |
| Ausführen | „Run“ (F5) startet die **gespeicherte** Datei (`POST /runs`) mit den je Workflow im Browser gemerkten Werten; beim ersten Lauf eines Workflows mit Parametern oder mehreren Environments – und über ▾ bzw. Shift+F5 – öffnet `components/RunDialog.tsx` (Environment aus `/projects/current`, Parameter mit Standardwert als Platzhalter). Ungespeicherte Änderungen: Rückfrage Save and run / Run saved version / Cancel. Während des Laufs „Cancel“ (Ctrl+.). Startet ein Lauf nicht (Fehler im Workflow), erscheint die Meldung und die Leiste „Problems“ öffnet sich |
| Live-Status | `run.ts` folgt dem Lauf über `/events?run=<id>&since=0`. Knoten: 3-px-Statusleiste und Symbol (läuft, ✔, ✖, ■ abgebrochen, – übersprungen), Laufzeit hinter dem Typ; der Laufstatus hat Vorrang vor Meldungs-Markierungen. Links: der Link zur laufenden Action „strömt“ (aus bei `prefers-reduced-motion`), nicht gefolgte Links werden blass. Der Status bleibt bis zum nächsten Lauf; Verschieben behält ihn, Änderungen an Actions oder Links blenden ihn aus. Die Statusleiste zeigt „Running (dev) · 1.4 s“ bzw. das Ergebnis. ⧗ „wartet“ bei `join: all`, sobald ein eingehender Link gefeuert hat (Ereignis `action.waiting`) |
| Log | Tab „Log“ in `components/BottomPanel.tsx` neben „Problems“; öffnet sich beim Start eines Laufs und folgt ihm, solange ans Ende gescrollt ist. Kopf mit Status, Lauf-ID, Workflow, Environment und Schalter „Debug“ (Standard an, im Browser gemerkt). Zeilen wie bei `runnel run -v` (Zeit, Stufe, Action, Meldung; Fehler hinterlegt); Klick auf die Action wählt sie aus. Gezeigt wird der letzte Lauf der Datei, höchstens 5.000 Zeilen |
| Ergebnis | Die gewählte Action zeigt oben im Panel ihr Ergebnis im letzten Lauf („✖ Failed in the last run after 0 s“ mit Fehlermeldung) und „Show in log“, das die erste Fehlerzeile der Action markiert |
| Meldungen am Feld | aus `/validate` über den YAML-Pfad (`$.actions[i].timeout`, `$.actions[i].with` + Feldname); übrige Meldungen der Action oben im Panel. Solange das Panel offen ist, zeigt der Knoten kein eigenes Meldungsfenster |
| Bearbeiten | Jede Änderung (verschieben mit 8-px-Raster, löschen, anlegen, umbenennen, Links) geht an `POST /workflows/edit` (Pipelines: `POST /pipelines/edit`), nacheinander; der Server ändert den Text zeilengenau. Rückgängig/Wiederholen speichern Textstände (bis 100). Ungespeicherte Änderungen bleiben beim Tab-Wechsel erhalten; ein Neuladen nach Wiederverbindung überschreibt sie nie |
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
