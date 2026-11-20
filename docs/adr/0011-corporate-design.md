# ADR 0011: Corporate Design

- **Status:** Angenommen
- **Datum:** 2026-11-18
- **Bezug:** [ADR 0010 – Projektname „Runnel“](0010-projektname-runnel.md), [Konzept, Kap. 4.3 – Frontend](../KONZEPT.md#43-frontend-editor), [Kap. 11 – Editor-UX](../KONZEPT.md#11-editor--ux-konzept)

## Kontext

Mit dem Namen „Runnel“ stehen Editor (S30 ff.), Website, GitHub-Auftritt, Installer und Release-Artefakte an. Ohne
gemeinsames Erscheinungsbild entstehen Farben und Logos ad hoc in jedem Kanal und müssen später angeglichen werden.
Der Editor nutzt React, Tailwind und shadcn/ui; Farben sollten deshalb als CSS-Variablen in deren Schema vorliegen.

Anforderungen:

- ein Logo, das auch als 16-px-Favicon und Windows-Tray-Icon funktioniert
- Farben, die die fachlich festgelegten Link-Farben (Erfolg grün, Fehler rot, Immer neutral, Ausdruck blau) nicht stören
- hell und dunkel gleichwertig (Editor folgt der Windows-Einstellung)
- Schriften frei einbettbar und offline nutzbar (keine Google-Fonts-Abfrage aus dem Editor)
- eine einzige Quelle für alle Kanäle

## Entscheidung

Das Corporate Design liegt unter [`design/`](../../design/README.md) und besteht aus:

| Baustein | Festlegung |
|---|---|
| Bildmarke | Start-Punkt → geschwungener Link → Ziel-Ring (kleinster Workflow, zugleich Wasserlauf); drei getrennte Elemente, dünner Link |
| Wortmarke | „runnel“, Kleinbuchstaben, Inter SemiBold, −2 % Laufweite, als Pfade – nur als Grafik, nie als Text |
| Schreibweise | Produktname **Runnel** in Text, UI und Windows-Metadaten; `runnel` nur als Befehl/technischer Bezeichner in Monospace ([Handbuch, Kap. 7.1](../../design/README.md#71-schreibweise-des-namens)) |
| Markenfarbe | **Strom** (Wasserblau), Hauptton Strom 600 `#0A6DA6` |
| Neutralfarbe | **Tiefe** (kühles Schiefergrau), dunkler Grund Tiefe 950 `#0E151D` |
| Funktionsfarben | Erfolg, Fehler, Warnung, **Ausdruck = Indigo** `#4549C9` (statt Markenblau, damit Auswahl/Fokus und Ausdrucks-Links unterscheidbar bleiben) |
| Schriften | Inter (UI/Text), JetBrains Mono (Code/CLI) – beide SIL OFL 1.1, lokal als WOFF2 eingebettet |
| Tokens | `design/tokens/tokens.json` (DTCG) → `runnel.css` (shadcn/ui-Variablen + Runnel-Rollen) und `tailwind.css` (Tailwind v4) |
| Claim | „Leicht. Lokal. Im Fluss.“ |

## Konsequenzen

- Kleine Wortmarke und großer Produktname stehen bewusst nebeneinander (wie bei vielen Entwicklerwerkzeugen):
  Die Marke zeigt den Befehl, den man tippt; im Text bleibt der Name ein normales Substantiv. Damit das nicht
  verwischt, darf „runnel“ nie als Text gesetzt werden. Erwogen und verworfen: Wortmarke „Runnel“ (förmlicher,
  verliert die Nähe zum Befehl) und durchgehende Kleinschreibung (widerspricht ADR 0010, schwierig im Deutschen).

- Der Editor (S30) bindet `design/tokens/tailwind.css` ein und verwendet ausschließlich die semantischen Rollen;
  Hex-Werte im Frontend-Code sind nicht erlaubt.
- Werden Inter/JetBrains Mono in die exe eingebettet, kommen ihre Lizenztexte in `THIRD_PARTY_NOTICES.txt`.
- `runnel.exe` und der Installer erhalten `design/logo/runnel.ico`; die CLI nutzt für Zustände die ANSI-Standardfarben
  passend zu den Funktionsfarben.
- Änderungen an Farben erfolgen nur in `tokens.json`; Änderungen an Logo oder Markenfarben erfordern eine neue ADR.
- Vor einer öffentlichen Bekanntmachung sollte – zusammen mit der Markenrecherche aus ADR 0010 – geprüft werden, ob die
  Bildmarke mit bestehenden Marken verwechselbar ist.
