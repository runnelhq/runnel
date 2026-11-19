# Den Editor entwickeln

Der Editor ist eine React-Anwendung in [`web/`](../../web). Sie wird gebaut, in die `runnel.exe` eingebettet und von
`runnel serve` unter `http://127.0.0.1:7300/` ausgeliefert. Mit dem Server spricht sie nur über die
[API](API.md). Gestaltung: [Corporate Design](../../design/README.md), freigegebene [Mockups](../../design/mockups/README.md).

> Stand: E4a (Gerüst: Kopfzeile, Farbschema, Verbindungsstatus). Explorer, Tabs und YAML-Ansicht folgen mit E4b.

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
| End-to-End-Tests (Playwright) | `make e2e` (baut alles und startet `runnel serve examples/hello` auf Port 7399) |

Ohne Node baut `make build-go` eine exe ohne Editor; `runnel serve` zeigt dann unter `/` einen Hinweis. Die Go-Tests
brauchen keinen Editor-Build.

## Aufbau

```
web/
  index.html            # Einstieg
  src/
    main.tsx            # React-Start
    App.tsx             # Gerüst: Kopfzeile, Hauptbereich, Statusleiste
    api.ts              # API-Zugriff, Verbindung (Server-Sent Events)
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

## Auslieferung

| Thema | Umsetzung |
|---|---|
| Einbettung | `internal/webui` bettet `dist/` per `go:embed` ein |
| Caching | `assets/*` (Dateiname mit Hash) ein Jahr unveränderlich, alles andere `no-cache` |
| Sicherheit | Content-Security-Policy (`default-src 'self'`, kein Einbetten in fremde Seiten), `nosniff`, kein Referrer; Host-/Origin-Prüfung wie bei der API |
| Ansichten ohne Dateiendung | erhalten `index.html` (für spätere Adressen im Editor) |
| Lizenzen und SBOM | `scripts/third-party-notices.sh` ergänzt npm-Pakete und Schriften; `scripts/merge-sbom.mjs` fügt die SBOM des Editors (`cyclonedx-npm`) in die SBOM der exe ein |
