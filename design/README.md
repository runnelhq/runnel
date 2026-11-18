<p align="center"><img src="logo/runnel-logo.svg" alt="Runnel" height="64"></p>

# Runnel – Corporate Design

Stand: 2026-11-18 · Entscheidung: [ADR 0011](../docs/adr/0011-corporate-design.md)

Dieses Verzeichnis ist die **einzige Quelle** für das Erscheinungsbild von Runnel – im Editor, auf der Website, in
GitHub, im Installer, in der CLI und in Präsentationen. Wer etwas gestaltet, nimmt Farben, Schriften und Logos von hier
und nicht aus Screenshots oder Kopien.

```
design/
├─ README.md                 # dieses Handbuch
├─ logo/                     # Logos, Icons, Favicons (SVG = Quelle, PNG/ICO = Export)
│  └─ png/                   # Rastergrafiken 16–512 px, Logo als PNG
├─ tokens/
│  ├─ tokens.json            # Design Tokens (Quelle, DTCG-Format)
│  ├─ build.mjs              # erzeugt runnel.css und tailwind.css
│  ├─ runnel.css             # CSS-Variablen, hell/dunkel, shadcn/ui-kompatibel
│  └─ tailwind.css           # Tailwind-v4-Theme (bg-primary, text-strom-600 …)
└─ social/                   # Social-/Open-Graph-Bild (1280 × 640) inkl. HTML-Vorlage
```

---

## 1. Markenkern

**Runnel** (engl. „Rinnsal, kleiner Wasserlauf“) ist der Gegenentwurf zu schwergewichtigen Automatisierungs-Plattformen:
eine einzige exe, die Daten zuverlässig von A nach B fließen lässt.

| | |
|---|---|
| **Claim** | **Leicht. Lokal. Im Fluss.** |
| Claim (EN) | Lightweight. Local. Flowing. |
| Beschreibung | Workflows und Datenintegration für Windows – eine einzige exe, keine Runtime, keine Datenbank. |
| Beschreibung (EN) | Workflows and data integration for Windows – one executable, no runtime, no database. |

Drei Eigenschaften leiten jede Gestaltungsentscheidung:

1. **Leicht** – wenig Elemente, viel Luft, keine Effekte ohne Funktion. Was nicht hilft, fliegt raus.
2. **Präzise** – Werkzeug für Technikerinnen und Techniker: klare Zustände, exakte Begriffe, Monospace für alles, was man tippen kann.
3. **Im Fluss** – Bewegung entsteht durch Linien und Verbindungen (Links), nicht durch Verläufe oder Dekoration.

---

## 2. Logo

### 2.1 Bildmarke

Die Bildmarke zeigt den kleinsten möglichen Runnel-Workflow: **Start → Link → Ziel**.
Der gefüllte Punkt ist der Start, die geschwungene Linie der Link (wie im Editor-Canvas), der Ring das Ziel.
Die drei Elemente stehen getrennt: Der Link ist deutlich dünner als die Knoten (3,5 : 13 Einheiten) und hält zu Start
und Ziel denselben Abstand – wie ein Link zwischen zwei Ports im Editor.
Gleichzeitig liest sie sich als Wasserlauf, der von oben links nach unten rechts fließt.

| Datei | Verwendung |
|---|---|
| `logo/runnel-icon.svg` | **Standard.** App-Icon auf Strom-600-Fläche: Website-Header, GitHub-Avatar, Installer, Taskleiste |
| `logo/runnel-icon-dark.svg` | App-Icon auf Tiefe-950-Fläche (dunkle Umgebungen, Splash) |
| `logo/runnel-icon-small.svg` | Für 16–24 px optimiert (dickere Linien) – Basis für `favicon.svg`/`favicon.ico` |
| `logo/runnel-mark.svg` | Bildmarke ohne Fläche, für helle Hintergründe |
| `logo/runnel-mark-inverse.svg` | Bildmarke ohne Fläche, für dunkle Hintergründe |
| `logo/runnel-mark-mono.svg` | Einfarbig mit `currentColor` – übernimmt die Textfarbe (UI, Stempel, Gravur) |
| `logo/runnel-tray.svg` | Windows-Tray-Icon, 16 px, einfarbig (`currentColor`) |
| `logo/runnel.ico` | Windows-Icon (16–256 px) für `runnel.exe`, MSI, Startmenü |
| `logo/favicon.svg`, `logo/favicon.ico` | Browser-Tab (Editor und Website) |
| `logo/png/runnel-icon-*.png` | 16, 24, 32, 48, 64, 128, 180 (Apple Touch), 256, 512 px |

### 2.2 Wortmarke und Logo

Die Wortmarke ist **„runnel“ in Kleinbuchstaben** – so, wie man das Programm in der Konsole tippt. Gesetzt in
Inter SemiBold mit −2 % Laufweite, als Pfade gespeichert (keine Schrift nötig).

| Datei | Verwendung |
|---|---|
| `logo/runnel-logo.svg` | **Standard-Logo:** App-Icon + Wortmarke, für helle Hintergründe |
| `logo/runnel-logo-inverse.svg` | dto. für dunkle Hintergründe |
| `logo/runnel-logo-flat.svg` | Bildmarke ohne Fläche + Wortmarke (ruhiger, z. B. Dokumentation, Briefkopf) |
| `logo/runnel-logo-mono.svg` | Einfarbig (`currentColor`) |
| `logo/runnel-wordmark.svg`, `…-inverse.svg` | Nur Wortmarke (wenn das Icon bereits daneben steht) |
| `logo/png/runnel-logo.png`, `…-inverse.png` | 1020 × 320 px für Office, Präsentationen, E-Mail-Signatur |

Im Fließtext heißt das Produkt immer **Runnel** (großes R). Die Kleinschreibung gibt es nur in der Wortmarke und
als Befehl (`runnel`).

### 2.3 Regeln

- **Schutzraum:** rundum mindestens ¼ der Icon-Höhe frei (bei 64 px Icon = 16 px).
- **Mindestgröße:** App-Icon 16 px (dann `runnel-icon-small.svg`), Logo mit Wortmarke 96 px breit (Bildschirm) bzw. 25 mm (Druck).
- **Hintergründe:** Standard-Logo auf Weiß bis Tiefe-100; Inverse-Logo auf Tiefe-800 bis Tiefe-950 und Strom-700 bis Strom-950.
  Auf Fotos nur das App-Icon (mit eigener Fläche).
- **Nicht erlaubt:** verzerren, drehen, spiegeln (der Fluss geht immer von oben links nach unten rechts), Farben
  austauschen, Link verdicken oder an Start/Ziel anschließen, Schatten/Verläufe/Konturen hinzufügen, Wortmarke in einer anderen Schrift nachsetzen, Ring und Punkt vertauschen.

---

## 3. Farben

Alle Werte stammen aus `tokens/tokens.json`. Die Namen sind Teil der Marke und werden im Code genauso verwendet
(`--rn-strom-600`, `bg-strom-600`).

### 3.1 Markenfarben

| Name | Hex | Rolle |
|---|---|---|
| **Strom 600** | `#0A6DA6` | **Hauptfarbe.** Logo-Fläche, Primär-Buttons, Links im Text (hell) |
| Strom 500 | `#1688C6` | Fokus-Ring, Auswahl auf dem Canvas, laufende Knoten |
| Strom 400 | `#3FA6DD` | Primärfarbe im dunklen Modus |
| Strom 300 | `#7CC5EB` | Ziel-Ring im Logo, Hervorhebungen auf dunklem Grund |
| Strom 50 | `#EEF8FD` | Hover-/Akzentflächen (hell) |
| **Tiefe 950** | `#0E151D` | Dunkler Hintergrund (Editor dunkel, Website-Hero, Social) |
| **Tiefe 900** | `#19222E` | Text (hell), Karten (dunkel) |
| Tiefe 500 | `#67778A` | Sekundärtext (hell) |
| Tiefe 200 | `#DDE3EA` | Rahmen und Trennlinien (hell) |
| Tiefe 50 | `#F6F8FA` | Canvas, Seitenleisten (hell) |
| Weiß | `#FFFFFF` | Hintergrund (hell) |

Vollständige Skalen (50–950) für **Strom** und **Tiefe** stehen in `tokens.json`.

**Mengenverhältnis:** ca. 70 % Neutral (Weiß/Tiefe), 20 % Tiefe-Dunkeltöne für Text und Flächen, höchstens 10 % Strom.
Strom markiert, was wichtig oder klickbar ist – nicht ganze Flächen.

### 3.2 Funktionsfarben (Zustände und Links)

Die Farben der Workflow-Links sind fachlich festgelegt ([Konzept, Kap. 2.2](../docs/KONZEPT.md#22-workflow-ablaufsteuerung))
und gelten überall gleich: im Editor, in Doku-Grafiken, in der CLI-Ausgabe und auf der Website.

| Bedeutung | Hell | Dunkel | Link-Symbol |
|---|---|---|---|
| Erfolg (`success`) | Erfolg 600 `#198348` | Erfolg 400 `#4CC47F` | ✔ |
| Fehler (`failure`) | Fehler 600 `#C53030` | Fehler 400 `#F07272` | ✖ |
| Immer (`always`) | Tiefe 700 `#3A4758` | Tiefe 300 `#C3CDD8` | ∞ |
| Ausdruck (`expression`) | Ausdruck 600 `#4549C9` | Ausdruck 400 `#8489F0` | ƒ |
| Warnung / wartet | Warnung 700 `#925C08` (Text), 600 `#B87507` (Fläche) | Warnung 400 `#F2B13D` | ⚠ |
| Läuft | Strom 500 `#1688C6` | Strom 400 `#3FA6DD` | – |
| Übersprungen | Tiefe 400 `#8F9DAC` (gestrichelt) | Tiefe 500 | – |
| Deaktiviert | Tiefe 300 (gestrichelt) | Tiefe 700 (gestrichelt) | – |

Regeln:

- **Farbe nie allein:** Jeder Zustand hat zusätzlich ein Symbol oder eine Linienform (gestrichelt, Icon, Text) –
  wichtig für Farbsehschwächen (Rot/Grün).
- **Ausdruck ist Indigo, nicht Markenblau.** So bleibt Strom frei für Auswahl und Fokus, ohne mit Ausdrucks-Links verwechselt zu werden.
- Funktionsfarben werden nie dekorativ eingesetzt (kein grüner Button, nur weil er „positiv“ wirkt).

### 3.3 Kategoriefarben (Bausteine)

Kennfarben für die Icon-Kachel der Actions/Transforms in Palette und Knoten. Nur als kleine Kachel hinter einem weißen
Icon, nie für Flächen, Text oder Zustände.

| Kategorie | Hex | | Kategorie | Hex |
|---|---|---|---|---|
| Allgemein | `#67778A` | | Eingabe | `#2F7D4F` |
| Ausführung | `#0A6DA6` | | Transformation | `#B5487F` |
| Dateien | `#C2700E` | | Fluss | `#4D5C6F` |
| Netzwerk | `#0E8A8A` | | Ausgabe | `#9A5B2E` |
| Datenbank | `#7B4FC9` | | | |

Plugins ohne eigene Kategorie nutzen „Allgemein“.

### 3.4 Kontrast

Alle Text-Kombinationen der semantischen Rollen erfüllen WCAG 2.2 AA (≥ 4,5 : 1), z. B. Strom 600 auf Weiß 5,6 : 1,
Tiefe 500 auf Weiß 4,6 : 1, Strom 400 auf Tiefe 950 6,7 : 1. Strom 500 (3,9 : 1) und Warnung 600 (3,8 : 1) sind nur
für Flächen, Linien und großen Text (≥ 18 px fett) zugelassen.

---

## 4. Typografie

| Rolle | Schrift | Fallback (Windows zuerst) |
|---|---|---|
| UI, Text, Überschriften | **Inter** (400, 500, 600, 700) | Segoe UI Variable, Segoe UI, system-ui |
| Code, YAML, Ausdrücke, CLI, Zahlenspalten | **JetBrains Mono** (400, 500) | Cascadia Mono, Consolas, ui-monospace |

Beide Schriften stehen unter der SIL Open Font License 1.1 und dürfen eingebettet werden. Der Editor liefert sie als
WOFF2 (nur Latin-Subset) **lokal** aus – keine Google-Fonts-Abfrage, weil Runnel offline und im Firmennetz laufen muss.
Lizenztexte gehören dann in `THIRD_PARTY_NOTICES.txt`.

### 4.1 Größen

| Stufe | Editor | Website | Gewicht | Einsatz |
|---|---|---|---|---|
| Display | – | 48–64 px, Zeilenhöhe 1,05, −2,5 % | 700 | Hero |
| H1 | 22 px | 36 px | 600 | Seitentitel |
| H2 | 16 px | 28 px | 600 | Abschnitte |
| H3 | 14 px | 22 px | 600 | Panels, Karten |
| Text | **13 px** / 1,5 | **16 px** / 1,6 | 400 | Fließtext, Formulare |
| Klein | 12 px | 14 px | 400/500 | Hilfetexte, Metadaten |
| Label | 11 px, Großbuchstaben, +6 % | 12 px | 600 | Panel-Titel, Tabellenköpfe |

Der Editor ist ein dichtes Werkzeug (wie eine IDE) und nutzt deshalb 13 px als Basis.

### 4.2 Regeln

- Zahlen in Tabellen, Zählern und Metriken: `font-variant-numeric: tabular-nums` (Inter) oder Monospace.
- Alles, was man tippen oder kopieren kann (Befehle, Dateinamen, Feldnamen, `${VAR}`, `{{ … }}`), steht in Monospace.
- Überschriften in Satzschreibweise („Pipeline ausführen“), keine Großschreibung ganzer Wörter außer bei Labels.

---

## 5. Form, Raster und Bewegung

| Token | Wert | Einsatz |
|---|---|---|
| Raster | 4 px | Alle Abstände sind Vielfache von 4 |
| Radius sm / md / lg / xl | 4 / 6 / 10 / 14 px | Badges / Buttons, Inputs, Knoten / Karten, Dialoge / App-Icon-nahe Flächen |
| Schatten sm / md / lg | siehe Tokens | Knoten / Popover / Dialoge – im dunklen Modus Flächen statt Schatten |
| Bewegung fast / normal | 120 / 200 ms, `cubic-bezier(0.2, 0, 0, 1)` | Hover, Panels |
| Bewegung flow | 1200 ms, linear, Endlosschleife | „Strömung“ auf aktiven Links (laufende Strich-Animation) |

Bewegung respektiert `prefers-reduced-motion`: dann keine Strömungsanimation, nur Farbwechsel.

**Gestaltungselement „Strömungslinien“:** Zwei bis drei parallele, geschwungene Linien in Strom 400–700 mit
abnehmender Deckkraft, optional mit einem Ziel-Ring – als Hintergrundgrafik für Website-Hero, Social-Bilder, Titelfolien.
Nie hinter Fließtext, nie mehr als eine Gruppe pro Fläche. Vorlage: `social/social-card.html`.

---

## 6. Anwendung

### 6.1 Editor (React + Tailwind + shadcn/ui)

Die Tokens sind auf das Variablenschema von shadcn/ui abgestimmt (`--background`, `--primary`, `--sidebar-*`, `--chart-*`)
und erweitern es um Runnel-Rollen (`--canvas`, `--node`, `--link-success`, `--status-running` …).

```css
/* web/src/index.css */
@import "tailwindcss";
@import "../../design/tokens/tailwind.css";   /* bindet runnel.css mit ein */
```

Danach stehen u. a. zur Verfügung: `bg-background`, `text-muted-foreground`, `bg-primary`, `border-border`,
`stroke-link-success`, `bg-status-running`, `bg-strom-600`, `text-tiefe-500`, `font-mono`, `rounded-lg`, `shadow-md`.

Dunkler Modus: Klasse `dark` oder `data-theme="dark"` auf `<html>`; mit `data-theme="system"` folgt der Editor der
Windows-Einstellung. Standard im Editor ist **System**.

Canvas-Konventionen ([Konzept, Kap. 11](../docs/KONZEPT.md#11-editor--ux-konzept)):

| Element | Gestaltung |
|---|---|
| Canvas | `--canvas`, Punktraster in `--canvas-grid`, 16 px Abstand |
| Knoten | `--node`, 1 px `--node-border`, Radius lg, Schatten sm; links eine 28-px-Kachel in Kategoriefarbe mit weißem Icon |
| Ausgewählt | 2 px Ring `--node-selected` |
| Zustand | 3 px Statusleiste am unteren Knotenrand in `--status-*` + Symbol oben rechts |
| Links | 2 px, Bézier wie in der Bildmarke; Farbe nach Typ (`--link-*`), Symbol ✔/✖/∞/ƒ in der Mitte; aktiv = „Strömung“ |
| Zeilenzähler an Links | Monospace 11 px, `tabular-nums`, Pille auf `--card` |

Der Editor zeigt das Logo nur einmal: App-Icon (20 px) + Wortmarke oben links. Favicon und Fenster-Icon (WebView2) = `favicon.svg` bzw. `runnel.ico`.

### 6.2 Website

- Hell als Standard, dunkler Modus über `prefers-color-scheme`. Hero darf dauerhaft dunkel sein (Tiefe 950 + Strömungslinien).
- Basisschrift 16 px, Textbreite max. 68 Zeichen, Inhaltsbreite max. 1120 px.
- Code-Beispiele (`runnel run …`, YAML) prominent und kopierbar – sie sind das stärkste Verkaufsargument.
- Meta-Bilder: `social/social-card.png` als `og:image`/`twitter:image`; Favicons aus `logo/`.
- Primärer Call-to-Action: „Herunterladen“ (Strom 600), sekundär: „Auf GitHub ansehen“ (Outline).

```html
<link rel="icon" href="/favicon.svg" type="image/svg+xml">
<link rel="icon" href="/favicon.ico" sizes="any">
<link rel="apple-touch-icon" href="/runnel-icon-180.png">
<meta name="theme-color" content="#0A6DA6">
<meta property="og:image" content="https://…/social-card.png">
```

### 6.3 GitHub

- Organisations- und Repo-Avatar: `logo/png/runnel-icon-512.png`.
- Social Preview des Repos (Settings → Social preview): `social/social-card.png` (1280 × 640).
- README-Kopf: `design/logo/runnel-logo.svg` (mit `<picture>` und `runnel-logo-inverse.svg` für den dunklen GitHub-Modus).
- Labels in Funktionsfarben: `bug` Fehler 600, `enhancement` Strom 600, `docs` Tiefe 500, `good first issue` Erfolg 600.

### 6.4 CLI (`runnel run`, `runnel validate` …)

Die Konsole nutzt die **16 ANSI-Standardfarben**, damit Ausgaben in jedem Terminal-Theme lesbar bleiben:

| Bedeutung | ANSI | Beispiel |
|---|---|---|
| Erfolg | Grün | `✔ load   12.430 Zeilen  1,8 s` |
| Fehler | Rot | `✖ upload  HTTP 500` |
| Warnung | Gelb | `⚠ Feld "plz" ist nicht deklariert` |
| Überschrift, Name des Laufs | Cyan, fett | `runnel run nightly.workflow.yaml` |
| Nebeninfos (Zeit, IDs) | Grau (dim) | `14:02:11` |

Farben nur bei TTY und ohne `NO_COLOR`; Symbole (✔ ✖ ⚠) immer, damit Logs auch ohne Farbe lesbar sind.

### 6.5 Windows

- `runnel.exe`, MSI-Installer, Startmenü und Dienst: `logo/runnel.ico`.
- Tray-Icon: `logo/runnel-tray.svg` in Weiß (dunkle Taskleiste) bzw. Tiefe 900 (helle Taskleiste); Fehlerzustand mit
  kleinem Punkt in Fehler 500 unten rechts.
- WiX-Installer-Banner: Tiefe 950 mit Strömungslinien rechts, Inverse-Logo links.

### 6.6 Dokumente und Präsentationen

- Titelfolie: Tiefe 950, Inverse-Logo, Claim in Weiß/Strom 300, Strömungslinien rechts (wie Social Card).
- Inhaltsfolien: Weiß, Text Tiefe 900, Akzente Strom 600, Schrift Inter (Fallback Segoe UI).
- Diagramme: Reihenfolge `--chart-1` … `--chart-5` (Strom, Indigo, Petrol, Bernstein, Beere).

---

## 7. Sprache und Ton

- **Sachlich, knapp, konkret.** Runnel spricht wie eine erfahrene Kollegin, nicht wie ein Werbeprospekt.
- UI-Texte im Infinitiv bzw. Imperativ ohne Anrede: „Pipeline ausführen“, „Änderungen speichern“. Wo eine Anrede nötig ist: **du**.
- Fehlermeldungen sagen, **was** passiert ist und **was zu tun ist**: „Datei `customers.csv` nicht gefunden. Pfad in ‚CSV-Eingabe‘ prüfen.“
- Fachbegriffe sind fest und werden nicht übersetzt oder variiert: **Workflow, Pipeline, Action, Transform, Link, Trigger,
  Environment, Connection, Run, Runner, Editor** ([ADR 0009](../docs/adr/0009-begriff-link.md)).
- Keine Emojis in Produkt und Doku; Symbole nur mit fester Bedeutung (✔ ✖ ∞ ƒ ⚠).
- Vergleiche mit n8n, Make und Apache Hop fair und faktenbasiert, ohne Herabsetzung.

---

## 8. Pflege

- **Farben, Schriften, Radien ändern:** nur in `tokens/tokens.json`, danach `node design/tokens/build.mjs` ausführen und
  `runnel.css`/`tailwind.css` mit committen.
- **Logo ändern:** SVG ist die Quelle; PNG/ICO werden daraus exportiert (Chromium/Inkscape, transparente Ränder beibehalten).
  Größere Änderungen an Logo oder Farben brauchen eine neue ADR.
- Neue Kanäle (z. B. Merch, Messestand) leiten sich aus Kapitel 2–5 ab und werden hier ergänzt.
