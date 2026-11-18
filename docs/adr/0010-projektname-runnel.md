# ADR 0010: Projektname „Runnel“

- **Status:** Angenommen
- **Datum:** 2026-11-18
- **Bezug:** [ADR 0009 – „Link“ statt „Hop“](0009-begriff-link.md)

## Kontext

Der Arbeitstitel „easy-flow“ war generisch, schwer auffindbar und als Name für Binary, Pfade, Dateiformat
(`apiVersion`) und Go-Modul wenig prägnant. Ein späterer Namenswechsel wäre mit jedem Schritt der Umsetzung teurer
geworden; da noch kein Code existiert, wird der Name jetzt festgelegt.

Anforderungen an den Namen:

- englisch, möglichst kurz, auf Deutsch und Englisch aussprechbar
- passend zum Konzept (leichtgewichtig, Fluss, Ausführung)
- kein gleichnamiges Workflow-/ETL-/Automatisierungs-Projekt
- als CLI-Befehl und Dateiname gut nutzbar

Geprüfte Kandidaten (Websuche und GitHub-Suche, keine Markenrecherche):

| Name | Ergebnis |
|---|---|
| Linkwerk | vergeben (IT-Agentur inkl. GitHub-Organisation) |
| Sluice | ETL-Tool mit YAML-Pipelines und `run`/`validate`-CLI – fast identisches Produkt |
| Floe | Workflow-Engine in Go sowie Workflow-Runner von ManageIQ |
| Weft | mehrere Workflow-/Execution-Engines |
| Spindle | CI-Runner mit YAML-Workflows |
| Millrace | Workflow-Runtime für KI-Agenten |
| Plait | bekanntes Framework für Whiteboards/Flussdiagramme |
| Shunt | frei im Workflow-Umfeld, aber negative Nebenbedeutungen |
| Leat | frei, aber Verwechslung mit „leet“ |
| **Runnel** | kein Workflow-/ETL-Projekt gefunden; nur eine kleinere Python-Bibliothek für Event-Verarbeitung und einzelne Kleinst-Tools |

## Entscheidung

Das Projekt heißt **Runnel** (engl. „Rinnsal, kleiner Wasserlauf“).

| Verwendung | Wert |
|---|---|
| Produktname (Text, UI) | Runnel |
| GitHub-Organisation | `runnelhq` |
| Repository | `runnelhq/runnel` |
| Go-Modul | `github.com/runnelhq/runnel` |
| Binary / CLI | `runnel.exe` – `runnel run`, `runnel serve`, `runnel validate`, `runnel service …` |
| Dateiformat | `apiVersion: runnel/v1` |
| Datenverzeichnisse | `%ProgramData%\runnel\`, `%LOCALAPPDATA%\runnel\` |
| Server-Konfiguration / Datenbank | `runnel.yaml`, `runnel.db` |
| Umgebungsvariablen | Präfix `RUNNEL_` (z. B. `RUNNEL_MASTER_KEY`) |
| Windows-Dienst / EventLog-Quelle | `Runnel` |

Begründung:

- **Bild passt:** ein kleiner, leichter Wasserlauf – Gegenentwurf zu schwergewichtigen Plattformen, trotzdem „Flow“.
- **„run“ im Namen** – passend zu einem Runner; die CLI liest sich natürlich (`runnel run nightly.workflow.yaml`).
- **Kurz, eindeutig, aussprechbar** in Deutsch und Englisch.

## Konsequenzen

- Alle Dokumente (Konzept, Roadmap, ADRs, README, LICENSE) wurden auf den neuen Namen umgestellt.
- Das bisherige Repository wird nach `runnelhq/runnel` übertragen und umbenannt; GitHub leitet alte URLs weiter.
- Vor einer öffentlichen Bekanntmachung sollten Domains (z. B. `runnel.dev`) gesichert und eine kurze Markenrecherche
  (DPMA/EUIPO) durchgeführt werden.
