# ADR 0029: Stabilität und Versionen ab 1.0

- **Status:** Angenommen
- **Datum:** 2026-11-26
- **Bezug:** [ADR 0014 – Descriptor und Schema](0014-action-descriptor-schema.md), [ADR 0018 – HTTP-Trigger](0018-http-trigger.md),
  [ADR 0019 – Plugin-Protokoll](0019-plugin-protokoll.md), [ADR 0025 – Einheitliche Parameter](0025-einheitliche-parameter.md),
  [ADR 0026 – Run-Historie](0026-run-historie.md); Roadmap Phase 1.0, E15a

## Kontext

Bis 1.0 ändert Runnel Formate und Namen frei, zuletzt mit ADR 0025 (`overwrite` → `ifExists`, `mustExist` →
`ifMissing`, `as` → `format`). Die Hinweise auf die alten Namen wurden wenige Tage später wieder entfernt. Ab dem
ersten stabilen Release hängen andere davon ab:

- **Anwender:** Workflows, Pipelines, Trigger und Skripte der Aufgabenplanung, die Exit-Codes auswerten.
- **Plugin-Autoren:** das Plugin-Protokoll, das Go- und das Rust-SDK, die offiziellen Plugins.
- **Website und Werkzeuge:** `runnel-catalog.json` und `runnel-expressions.json` aus jedem Release (E15a).
- **Systeme, die Runnel aufrufen:** HTTP-Trigger, Überwachung über `GET /api/v1/health` und `runnel health` (S43a).

Gesucht ist eine klare Grenze: was zugesagt ist, was sich in 1.x ändern darf und wie etwas wegfällt.

## Entscheidung

### 1. Versionen nach SemVer

Die Zusagen gelten ab `v1.0.0`. Vorabversionen (`-alpha`, `-beta`, `-rc`) sagen nichts zu.

| Version | Darf enthalten |
|---|---|
| Patch (`1.4.1` → `1.4.2`) | nur Fehlerbehebungen; kein neues Feld, kein neues Verhalten |
| Minor (`1.4` → `1.5`) | Neues, das nichts Bestehendes ändert (Abschnitt 3); Abkündigungen (Abschnitt 4) |
| Major (`1.x` → `2.0`) | Brüche an einer Zusage aus Abschnitt 2, mit Hinweisen zur Umstellung |

### 2. Was zugesagt ist

| Bereich | Zusage |
|---|---|
| **Dateiformate** `apiVersion: runnel/v1` | Workflow, Pipeline, Project, Environment, Trigger, Connection, Plugin (`plugin.yaml`): Felder, Werte, Standardwerte und ihre Bedeutung; die Typnamen der eingebauten Actions, Transforms und Connection-Typen mit ihren Feldern und Ergebnissen (`result.output`, gesetzte Felder) |
| **Ausdrücke** `{{ … }}`, Variablen `${…}` | Namen im Kontext (`params`, `results`, `record`, `run` …), Funktionen mit Namen, Parametern und Ergebnis; Formate von Zahlen und Datum (`#.##0,00`, `dd.MM.yyyy`) |
| **Ausführung** | Semantik von Links, Join, Retry, Timeout, Fehler-Links, Fehler-Workflow; Typen der Datensätze (ADR 0007) |
| **Plugin-Protokoll** v1 (ADR 0019) | Methoden, Nachrichten, Felder und Wertetypen; Kredite; Fehlercodes. `protocolVersion: 1` bleibt für alle 1.x |
| **CLI** | Befehle, Flags und ihre Bedeutung; Exit-Codes (`runnel run`: 0 Erfolg, 1 Lauf fehlgeschlagen, 2 nicht gestartet, 130 abgebrochen; `runnel health`: 0–3); Ausgaben mit `--json` |
| **HTTP-Trigger** (ADR 0018) | Adressen, Authentifizierung, Statuscodes und Antworten (`response`) |
| **Öffentliche HTTP-API** unter `/api/v1` | Abschnitt 5 |
| **Release-Dateien** | Aufbau von `runnel-catalog.json` und `runnel-expressions.json` |

### 3. Was in einer Minor-Version erlaubt ist

- Neue Actions, Transforms, Connection-Typen, Trigger-Arten und Funktionen in Ausdrücken.
- Neue **optionale** Felder mit einem Standardwert, der das bisherige Verhalten beibehält.
- Neue Werte in einer Auswahl (z. B. `ifExists: rename`), wenn die bisherigen gleich bleiben.
- Neue Schlüssel in JSON-Ausgaben, Antworten der API, Nachrichten des Plugin-Protokolls und den Release-Dateien.
  Leser müssen unbekannte Schlüssel ignorieren, so wie schon im Plugin-Protokoll.
- Neue Befehle, Flags, Endpunkte und Methoden des Protokolls; neue `validate`-Warnungen.
- Neue Exit-Codes nur für neue Befehle oder für Fälle, die vorher kein Ergebnis hatten.

**Nicht** in 1.x:
- ein Feld, einen Wert, einen Befehl, ein Flag, einen Endpunkt oder eine Funktion umbenennen oder entfernen;
- einen Standardwert oder die Bedeutung eines Werts ändern;
- ein optionales Feld zur Pflicht machen;
- einen Typ in einer Ausgabe ändern (z. B. `decimal` von Text zu Zahl);
- einen Exit-Code umdeuten.

Ein Fehler darf behoben werden, auch wenn sich dadurch Verhalten ändert. Das gilt aber nur, wenn das bisherige
Verhalten der Doku widersprach. Der Changelog nennt das unter „Behoben“.

### 4. Abkündigen

Was wegfallen soll, wird **mindestens eine Minor-Version** vorher abgekündigt und erst mit der nächsten Major-Version
entfernt:

- `validate` und der Editor melden eine Warnung mit Ersatz: `field "overwrite" is deprecated: use ifExists: replace
  (removed in 2.0)`. Das ist der Mechanismus aus ADR 0025 (`Renamed` in den Definitionen), dauerhaft statt nur bis 1.0.
- Ein umbenanntes Feld gilt in der ganzen 1.x unter beiden Namen. Stehen beide in einer Datei, ist das ein Fehler.
- Im Katalog tragen abgekündigte Felder, Typen und Funktionen `deprecated` mit Ersatz. Website und Hilfe im Editor
  zeigen es an.
- Der Changelog führt die Abkündigung unter „Abgekündigt“ mit der Version, in der sie kommt.

### 5. Öffentliche und interne HTTP-API

Die API des Servers dient vor allem dem Editor, der eingebettet ist und immer zur Version passt. Seine Endpunkte
bleiben **intern** und dürfen sich in jeder Version ändern. **Öffentlich** und zugesagt sind nur:

| Endpunkt | Zweck |
|---|---|
| `GET /api/v1/version` | Version des Servers |
| `GET /api/v1/health` | Zustand für Überwachung (S43a) |
| `GET /api/v1/catalog` | Bausteine wie `runnel catalog --json` |
| `GET /api/v1/expressions` | Referenz der Ausdrücke |
| `POST /api/v1/runs`, `GET /api/v1/runs`, `GET /api/v1/runs/{id}`, `POST /api/v1/runs/{id}/cancel` | Läufe starten, auflisten, abfragen, abbrechen |

- Alle anderen Endpunkte sind intern: Dateien, Ordner, Umbenennen, Verweise, Schemas, Variablen, Connections, Secrets,
  `validate`, Bearbeiten, Kopieren, Felder, Vorschau, Plugins, Trigger, Projekt und `/events`.
- `docs/dev/API.md` kennzeichnet jeden Endpunkt als öffentlich oder intern.
- Ein interner Endpunkt wird öffentlich nur durch einen Nachtrag zu dieser ADR.
- Die Prüfungen von Host und Origin gelten für alle Endpunkte gleich; „öffentlich“ heißt zugesagt, nicht im Netz
  erreichbar.

### 6. Nicht zugesagt

- Go-Pakete unter `internal/` (Runnel ist kein Go-Modul zum Einbinden; Plugins nutzen das Protokoll).
- Oberfläche und Bedienung des Editors, menschenlesbare Ausgaben der CLI (Text, Spalten, Farben) und Log-Texte.
- Aufbau der Datei der Run-Historie (Runnel migriert sie selbst, ADR 0026) und der Dateien der Plugin-Freigaben.
- Leistung, Speicherbedarf und Grenzwerte, solange sie nicht als Feld zugesagt sind (z. B. `maxSize`).
- Eingebaute Plugins oder Beispiele unter `examples/`. Sie dienen aber als Prüfung der Zusagen (Abschnitt 7).

### 7. Prüfung im Projekt

- **Golden-Dateien als Vertrag:** Für `catalog.golden.json`, `catalog-expressions.golden.json`, die Schemas der
  Dateiarten und die JSON-Ausgaben der CLI dürfen in 1.x nur Zeilen hinzukommen. Ein Test vergleicht sie dafür mit
  dem Stand des letzten Release-Tags.
- **Beispiele von 1.0:** Die Beispiele aus `v1.0.0` müssen mit jeder 1.x durch `validate` gehen und gleich laufen.
- **Plugin-Protokoll:** Feste Transkripte unter `testdata/protocol/`, dieselben wie in den SDKs.
- **CHANGELOG.md** mit den Abschnitten „Neu“, „Behoben“, „Abgekündigt“ und „Bruch“ (nur in Major-Versionen); jede
  Version ab dem ersten Release Candidate.
- **PRs:** Die Vorlage fragt „Ändert dieser PR eine Zusage aus ADR 0029?“. Ein Ja braucht einen Nachtrag oder eine
  neue ADR und wartet auf die nächste Major-Version, außer es ist eine Abkündigung.
- **Plugins:** `requires` in `plugin.yaml` (Mindestversion von Runnel, bisher Backlog) wird mit dem ersten
  Minor-Release nach 1.0 nötig, damit Plugins neue Felder voraussetzen können.

## Konsequenzen

- Anwender und Plugin-Autoren können auf 1.x aktualisieren, ohne Dateien oder Skripte anzupassen.
- Namen und Standardwerte sind ab 1.0 schwer zu ändern. Deshalb sehen wir sie vor dem Release Candidate ein letztes Mal
  durch (eigener Qualitäts-PR).
- Neues kommt nur additiv dazu. Abgekündigtes bleibt bis 2.0 im Code; das kostet etwas Pflege, wie bei ADR 0025.
- Der Editor bleibt frei, weil seine Endpunkte intern sind. Wer Runnel automatisieren will, nutzt CLI, HTTP-Trigger
  oder die öffentlichen Endpunkte.
- Die Prüfungen aus Abschnitt 7 kommen als eigene Schritte vor bzw. mit dem Release Candidate.

Umsetzung (Roadmap Phase 1.0): Kennzeichnung öffentlich/intern in `docs/dev/API.md`; `deprecated` im Katalog und
dauerhafter Mechanismus für umbenannte Felder; Test für Golden-Dateien gegen den letzten Tag; CHANGELOG und PR-Vorlage.
