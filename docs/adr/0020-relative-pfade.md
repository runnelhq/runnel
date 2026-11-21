# ADR 0020: Relative Pfade beziehen sich auf den Projektordner

- **Status:** Angenommen
- **Datum:** 2026-11-21
- **Bezug:** [ADR 0017 – Sub-Workflows](0017-sub-workflows.md) (Pfade relativ zum Projekt),
  [ADR 0019 – Plugin-Protokoll](0019-plugin-protokoll.md), KONZEPT Kap. 2.5 und 12, Roadmap S09, S15, S38, S41, S44

## Kontext

Relative Pfade wurden uneinheitlich aufgelöst:

| Wo | Bezug bisher |
|---|---|
| `core.run-workflow`, `core.run-pipeline`, Trigger-`target`, Ordner von Datei-Triggern | Projektordner |
| `csv.input`, `csv.output`, „Detect fields“, Datenvorschau | Arbeitsverzeichnis von Runnel |
| `file.exists`, `file.wait`, `file.copy`, `file.move`, `file.delete`, `file.mkdir`, `file.list` | Arbeitsverzeichnis von Runnel |

Das Arbeitsverzeichnis ist der Ordner, aus dem `runnel serve` oder `runnel run` gestartet wurde. Dasselbe Projekt las
und schrieb damit je nach Start an anderen Stellen. Als Windows-Dienst (S41) ist das Arbeitsverzeichnis
`C:\Windows\System32` – relative Pfade landeten dort oder scheiterten. Die Beispiele umgingen das mit Pfaden ab dem
Repository (`examples/pipelines/data/…`), die nur von dort aus funktionierten. Beim Entwerfen einer Pipeline im Editor
schrieb `csv.output` damit neben `runnel.exe` statt ins Projekt.

## Entscheidung

**Relative Pfade beziehen sich immer auf den Projektordner** – in allen Actions und Transforms, in Triggern, bei
„Detect fields“ und in der Datenvorschau. Ohne Projekt (`runnel run` auf eine einzelne Datei ohne `project.yaml`
darüber) beziehen sie sich auf den Ordner der Workflow- bzw. Pipeline-Datei, wie schon die Aufrufe in ADR 0017.

- **Absolute Pfade** bleiben unverändert: `C:\…`, UNC-Pfade (`\\server\share\…`), unter Linux `/…`. Ebenso Pfade, die
  unter Windows mit `\` beginnen (relativ zum aktuellen Laufwerk) oder ein Laufwerk ohne `\` nennen (`D:x`) – sie sind
  ungewöhnlich und werden nicht umgedeutet.
- **Aufgerufene Workflows und Pipelines** nutzen denselben Projektordner wie der Aufrufer.
- **Ergebnisse mit Pfaden** (`file.mkdir`, `file.list`, `file.exists`, `file.wait`, `file.copy`, `file.move`,
  `file.delete`) enthalten den aufgelösten, absoluten Pfad. So findet eine spätere Action die Datei unabhängig davon,
  wo sie selbst nachsehen würde. **Meldungen im Log** nennen den Pfad, wie er in der Datei steht.
- **Eingebaute Variable `${PROJECT_DIR}`:** absoluter Pfad des Projektordners, für zusammengesetzte Pfade und
  Ausdrücke (z. B. `{{ … }}`, die einen Pfad an ein anderes Programm geben). Sie ist die unterste Ebene der Variablen:
  Eine Variable gleichen Namens im Projekt, im Environment oder in den Parametern hat Vorrang. Ohne Projekt gibt es sie
  nicht; die Prüfung meldet sie dann als unbekannt.
- **Umsetzung:** Die Engines legen den Bezugsordner in den Kontext eines Laufs (Paket `internal/paths`); Actions und
  Transforms lösen ihre Pfade mit `paths.Resolve` auf, nicht selbst. Neue Bausteine und Plugins (ADR 0019 – der Host
  löst Pfade in der `config` nicht auf, sie stehen dort wie geschrieben; das Plugin erhält den Projektordner in
  `context`) folgen derselben Regel.

## Konsequenzen

- **Positiv:** Ein Projekt verhält sich gleich, egal wo und wie Runnel gestartet wird – auch als Dienst. Projekte
  lassen sich samt Daten verschieben und mit Git weitergeben.
- **Bricht bestehende Projekte**, die sich auf das Arbeitsverzeichnis verlassen. Runnel ist vor 1.0; die Beispiele
  sind umgestellt (`data/…`, Ausgaben unter `out/` im Projekt, in Git ignoriert). Wer weiter ab dem Startordner
  arbeiten will, nutzt absolute Pfade oder einen Parameter.
- **Ausgaben in den Projektordner:** Relative Ausgabepfade schreiben ins Projekt. Für Daten, die nicht in Git
  gehören, empfiehlt sich ein Unterordner wie `out/` mit Eintrag in `.gitignore`, oder ein absoluter Pfad aus einem
  Environment.
- Die API von „Detect fields“ nennt bei einem Lesefehler den Bezugsordner als `baseDir` (bisher `workingDir`).

## Sicherheit

Keine neue Angriffsfläche. Relative Pfade können weiter mit `..` aus dem Projektordner hinausführen und absolute
Pfade überall hin zeigen – Workflows und Pipelines sind Code des Projekts mit den Rechten von Runnel (wie bisher).
Die Datei-API des Editors (`/files`) bleibt davon getrennt auf den Projektordner beschränkt.
