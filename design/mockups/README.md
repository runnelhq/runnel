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
| [`s25a-pipeline.html`](s25a-pipeline.html) | S25a – Pipeline-Canvas (dazu K10/K11 im Workflow-Panel) | `canvas`, `link`, `run`, `problems`, `foreach` | freigegeben mit PR #62; Umsetzung in S25a1a (Server), S25a1b, S25a2, S25a3; `foreach` mit E7c ✅ |
| [`e7c-parameters.html`](e7c-parameters.html) | E7c – Parameter und Outputs im Editor | `parameters`, `add`, `outputs`, `problems` | freigegeben mit PR #69, umgesetzt mit E7c (zusammen mit `foreach` aus S25a) |
| [`e9-yaml.html`](e9-yaml.html) | E9 – YAML bearbeiten | `edit`, `problem`, `syntax`, `canvas` | freigegeben mit PR #90, umgesetzt mit E9 |
| [`e11-files.html`](e11-files.html) | E11 – Dateien anlegen und löschen | `new`, `name`, `rename`, `delete`, `folder` | freigegeben mit PR #96, umgesetzt mit E11 |
| [`e12-organize.html`](e12-organize.html) | E12 – Projekt organisieren | `drag`, `moveto`, `refs`, `triggers` | freigegeben mit PR #99, umgesetzt mit E12 |
| [`e13-icons.html`](e13-icons.html) | E13 – Icons aller Bausteine | `overview`, `sizes`, `canvas` | freigegeben vom Maintainer, umgesetzt mit E13 (im selben PR) |
| [`e14-lists.html`](e14-lists.html) | E14 – Listen im Panel bearbeiten | `constant`, `convert`, `group`, `switch`, `help` | freigegeben mit PR #109; umgesetzt mit E14a (Listen-Editor) und E14b (Feldauswahl, Meldungen an der Zeile, Help) |
| [`e16-copy-paste.html`](e16-copy-paste.html) | E16 – Kopieren und Einfügen im Canvas | `copy`, `paste`, `refs`, `wrongkind` | freigegeben mit PR #115; umgesetzt mit E16 |
| [`e17-markdown.html`](e17-markdown.html) | E17 – Markdown-Dokumente im Projekt | `open`, `text`, `new`, `links` | freigegeben mit PR #117; umgesetzt mit E17 |
| [`s16d-icons.html`](s16d-icons.html) | S16d – Icons für Berechnen, Duplikate entfernen und Sequenz | `choice`, `canvas` | freigegeben mit PR #120 (Variante A); umgesetzt mit S16d |
| [`s44-preview.html`](s44-preview.html) | S44 – Datenvorschau | `preview`, `json`, `link`, `error` | freigegeben mit PR #94, umgesetzt mit S44 |
| [`s46-fields.html`](s46-fields.html) | S46 – Felder ermitteln | `fields`, `detect`, `written`, `update`, `error` | freigegeben mit PR #88, umgesetzt mit S46 |
| [`s23b-info-link.html`](s23b-info-link.html) | S23b – Info-Links und Stream Lookup | `canvas`, `link`, `run`, `problems` | freigegeben mit PR #157, umgesetzt mit S23b1 (Engine) und S23b2 (Editor) |
| [`s39-connections.html`](s39-connections.html) | S39b/S39f – Connections und Secrets im Editor | `form`, `test`, `new`, `field`, `plaintext`, `secrets`, `set` | freigegeben mit PR #173; `form` und `test` umgesetzt mit S39c2a, `new`, `field` und `plaintext` (ohne „Store as secret…“) mit S39c2b; `secrets` und `set` folgen mit S39g |
| [`e20-forms.html`](e20-forms.html) | E20 – Formulare für Trigger, Environments und `project.yaml` | `cron`, `http`, `type`, `environment`, `project`, `new` | freigegeben mit PR #178; `cron`, `http`, `type` und `new` umgesetzt mit E20a, `environment` und `project` mit E20b |
| [`e21-tabs.html`](e21-tabs.html) | E21 – Viele offene Dateien: Liste und Menü der Tabs | `many`, `list`, `search`, `menu` | freigegeben mit PR #184, umgesetzt mit E21 |
| [`s26d-runs.html`](s26d-runs.html) | S26d – Liste der Läufe im Editor | `list`, `filter`, `run`, `changed`, `empty` | freigegeben mit PR #213; `list`, `filter` und `empty` umgesetzt mit S26e1, `run` und `changed` mit S26e2 |

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
| E7c | Parameter und Outputs als aufklappbare Zeilen im Panel (kein Dialog): ein Klick öffnet das Formular, „+ Add parameter“/„+ Add output“ legt eine Zeile an (Name sofort zum Ändern markiert), „Delete“ löscht mit Rückgängig, ↑/↓ ändern die Reihenfolge (sie gilt im Run-Dialog). Änderungen wie in E7 nach der Tipp-Pause, ohne Apply |
| E7c | Felder eines Parameters: Name, Type, Required, Default (Ausdruckseditor aus S35; bei Required ohne Wirkung), Allowed values (`enum`, als Chips), Description. Outputs: Name, Type, Value (Ausdruckseditor), Description |
| E7c | Umbenennen eines Parameters ändert keine Verweise (`${NAME}`, `params.NAME`) – der Hinweis am Feld sagt das; Meldungen zu fehlenden Namen erscheinen danach über `/validate` |
| E7c | Pipelines haben denselben Abschnitt „Parameters“, ohne Outputs. `forEach`/`parallel`/`stopOnError` und das Schlüssel/Wert-Feld für `params` folgen dem freigegebenen Mockup S25a (Zustand `foreach`) |
| E9 | Bearbeitet wird in der YAML-Ansicht von Workflows und Pipelines mit CodeMirror 6 (schon mit S35 im Editor); Hervorhebung, Zeilennummern und Farben wie bisher (E4), über den vorhandenen Tokenizer, ohne neues Sprachpaket. Neu: `@codemirror/search` (MIT, freigegeben) für Suchen und Ersetzen (Strg+F, Strg+H). Tab rückt um zwei Leerzeichen ein, Enter behält die Einrückung; keine Vorschläge in E9 |
| E9 | Geprüft wird nach einer Tipp-Pause (0,5 s wie in E7) über `/validate` mit dem Text im Editor; „Checking…“ in der Dateileiste, solange die Prüfung läuft. Meldungen: Wellenlinie an der Stelle, Zeichen in der Randspalte, Text beim Darüberfahren oder wenn der Cursor dort steht, und wie bisher unter „Problems“ (ein Klick springt an die Stelle) |
| E9 | Ein Rückgängig für alles: Jede Tipp-Pause ist ein Schritt im selben Verlauf wie Änderungen auf dem Canvas und im Panel; Strg+Z und Strg+Y wirken in beiden Ansichten (der eigene Verlauf von CodeMirror ist aus). Speichern wie bisher mit Save, kein automatisches Speichern |
| E9 | Ist der Text kein lesbarer Workflow (Syntaxfehler), bleibt der Wechsel zum Canvas möglich; er zeigt den Hinweis aus E5 („can’t be shown as a graph“) mit „Show YAML at line n“, die Änderungen bleiben erhalten. Der Knopf „Canvas“ trägt dann einen roten Punkt. Fehler in Feldern verhindern den Canvas wie bisher nicht |
| E9 | Der Hinweis „Read-only“ entfällt bei Workflows und Pipelines; andere Dateien (`project.yaml`, Environments, Trigger) bleiben in E9 schreibgeschützt und kommen mit einem eigenen Schritt (E10). Die Hinweise im Panel „in your text editor for now“ verweisen danach auf die YAML-Ansicht |
| E11 | „+“ im Kopf des Explorers öffnet „New“: Workflow, Pipeline, Trigger, Environment (jeweils in ihrem Ordner) und Folder; ein Rechtsklick oder „⋯“ an einem Ordner bietet dasselbe für diesen Ordner und zusätzlich „New file…“ (leere Datei, z. B. CSV), „Rename“ (F2), „Delete“ und „Copy path“; an einer Datei „Rename“, „Delete“ und „Copy path“ |
| E11 | Der Name wird direkt im Baum eingegeben (wie das Umbenennen auf dem Canvas, E6b): vorgeschlagen und markiert, die Endung (`.workflow.yaml` …) steht fest daneben; Enter legt an, Esc bricht ab; ein vorhandener oder ungültiger Name wird am Feld gemeldet |
| E11 | Vorlagen sind klein und gültig: Workflow mit `core.start`, Pipeline mit `core.generate-rows`, Trigger `interval` mit dem ersten Workflow als Ziel, Environment mit leeren `variables`; `name` aus dem Dateinamen. Die neue Datei öffnet sich im Tab |
| E11 | Löschen mit Rückfrage; die Rückfrage nennt Dateien, die auf die Datei verweisen (Trigger-Ziel, `core.run-workflow`, `core.run-pipeline`); gelöscht wird endgültig (kein Papierkorb), ein offener Tab wird geschlossen. Ordner lassen sich nur leer löschen |
| E11 | Umbenennen ändert keine Verweise in anderen Dateien; die Prüfung meldet sie danach (wie beim Umbenennen von Parametern, E7c). Ein offener Tab zeigt danach den neuen Namen |
| S44 | „Preview data“ im Panel eines Transforms führt die Pipeline bis zu diesem Transform aus (nur die Transforms davor und er selbst); Ausgaben (`csv.output` …) laufen nie, es wird nichts geschrieben. Ein Hinweis oben im Canvas („Preview up to …“, × beendet die Vorschau); Transforms und Links außerhalb der Vorschau sind blass |
| S44 | Jede Eingabe liest höchstens die ersten N Datensätze (100, wählbar 1.000); Werte wie beim Run ohne Dialog (gemerkte Parameter und Environment); gelesen wird der Text im Editor, auch ungespeichert. Die Vorschau ist kein Lauf: nicht in der Liste der Läufe und nicht im Log |
| S44 | Die Datensätze stehen im neuen Tab „Preview“ unter dem Canvas, als Tabelle (Spalten mit Typ, Zahlen rechtsbündig, Fehlerfelder `_error…` blass) oder als JSON; die Auswahl links wählt Ausgabe des Transforms oder einen Link (auch Fehler-Links); ein Klick auf einen Link im Canvas zeigt dessen Datensätze. Die Zähler an Links und Knoten wie nach einem Run (S25a), gestrichelt umrandet |
| S44 | Die Tabelle bleibt ohne neue Abhängigkeit (höchstens 1.000 Zeilen, keine Virtualisierung nötig); das Konzept nannte `@tanstack/react-table` – erst bei Bedarf |
| S44 | „Refresh“ führt die Vorschau nach Änderungen erneut aus; ein Fehler zeigt, an welchem Transform die Vorschau stehen blieb, mit der Meldung |
| S23b | Info-Links (`type: info`) haben die Farbe der Daten-Links (`--link-always`) und unterscheiden sich durch die Linienform (gepunktet) und das Symbol „i“ bei 35 % des Wegs; keine neue Funktionsfarbe (Strom bleibt für Auswahl und Fokus). Ein falscher Info-Link wird rot gepunktet |
| S23b | Ein Info-Link endet in einem eigenen Anschluss oben in der Mitte des Knotens; den Anschluss haben nur Transforms, die Nachschlagedaten annehmen (`core.stream-lookup`). Ein Link, der dort abgelegt wird, ist sofort ein Info-Link; im Link-Menü (E6b) kommt „Info“ (Taste I) zu Data und Error hinzu, nur bei solchen Zielen. Höchstens ein Info-Link je Transform |
| S23b | Das Panel von `core.stream-lookup` zeigt oben „Lookup data“ (Quelle des Info-Links oder rot „No info link“) und `maxRecords` (Standard 100.000; mehr hält die Pipeline an, Hinweis auf Database Lookup). Schlüssel als Paare Feld = Lookup-Feld, Rückgabe mit Lookup-Feld, As und Default, dazu `notFound` (Keep/Drop/Error) und `multiple` (First/Error) wie bei `db.lookup`. Die Feldauswahl bietet für „Lookup field“ die Felder des Info-Links an; „Incoming fields“ zeigt beide Eingänge getrennt |
| S23b | Nach einem Lauf trägt der Info-Link die Zahl der Nachschlage-Datensätze (gepunktete Pille); das Panel nennt zusätzlich „lookup records“. Das Log meldet das Warten auf den Info-Link (Debug) und „loaded N lookup records“ |
| S39b | Connection-Dateien öffnen als **Formular** aus dem Schema ihres Typs (`x-runnel-variants`), mit „Form \| YAML“ wie „Canvas \| YAML“; Felder in Gruppen (z. B. Server, Sign-in, Security, eingeklappt „More“), nur die Felder der gewählten Anmeldung (`x-runnel-only`), Warnungen von `validate` am Feld. Kopf: Typ mit Symbol, `description` und wo die Connection genutzt wird |
| S39b | „Test connection“ in der Dateileiste mit dem Environment daneben (wie Run); Ergebnis über dem Formular: ✓ mit Dauer und Version des Servers, ✕ mit Meldung, Environment, Adresse und Hinweisen; bei falscher Anmeldung direkt „Set password for prod…“. Getestet wird nur die gespeicherte Datei (`POST /connections/{name}/test`, 15 s) |
| S39b | Im Explorer tragen Connections das Symbol ihrer Art (Zylinder für Datenbanken wie `db.*`, Globus für `http`, Umschlag für `smtp`) und rechts den Typ; „New → Connection“ fragt Name und Typ (Karten mit Beschreibung aus der Registry) und legt die Datei mit den Pflichtfeldern an |
| S39f | Felder mit `format: runnel-secret` zeigen nur die Referenz `${secret:…}` (Schloss, Farbe der Ausdrücke) und je Environment, ob das Secret gesetzt ist (● Quelle, ⚠ fehlt, ⏱ läuft bald ab); „Change…“ öffnet die Auswahl der Namen: Secrets des Projekts mit ihrem Stand, Umgebungsvariablen (`env/…`), „New secret …“ |
| S39f | Klartext in einem Secret-Feld wird gelb unterstrichen und erklärt; „Store as secret…“ legt das Secret mit dem Wert an und setzt die Referenz ein |
| S39f | „Secrets“ ist der dritte Reiter neben „Fields“ und „Variables“ (project.yaml, Environments): Tabelle je Environment mit Quelle (`local`, `env`, `inline`), fehlenden und bald ablaufenden Secrets, Fundstellen und „Set for prod…“; Klartext aus `inline` rot markiert. Werte werden nie angezeigt |
| S39f | „Set secret“: Environment und Quelle stehen fest (aus `secretStore`), Wert nur verdeckt (Anzeigen nur beim Tippen), optional Ablaufdatum mit Warnung 14 Tage vorher. Setzen geht nur bei schreibbaren Quellen (`local`, später `shared`); bei `env` erklärt der Editor, wo die Variable zu setzen ist |
| S39b/S39f | Abgestimmt mit dem Maintainer: Connection-Dateien öffnen standardmäßig als Formular (YAML per Umschalter); „Secrets“ ist der dritte Reiter neben „Fields“ und „Variables“; das Ablaufdatum bleibt im Mockup, umgesetzt wird es erst mit S39e. Umgesetzt wird zunächst S39c (Formular, Test, Anzeige der Secret-Felder mit dem Stand aus `env` und `inline`); S39d (lokaler Store) und S39g (Secrets setzen und löschen) sind vorerst zurückgestellt |
| E20 | Trigger, Environments und `project.yaml` öffnen wie Connections standardmäßig als **Formular** aus ihrem Schema (S58) mit „Form \| YAML“; eine Änderung schreibt nur ihre Zeile, Kommentare bleiben (wie S39c2a) |
| E20 | Trigger: der Typ als vier Karten (Schedule, Interval, Folder, HTTP call); darunter nur die Felder des Typs (Varianten je `type` im Trigger-Schema wie `x-runnel-variants` der Connections). Ein Wechsel des Typs entfernt die Felder unter `with:`, die der neue Typ nicht kennt, mit Hinweis und „Undo“; gemeinsame Felder (z. B. `overlap`) bleiben |
| E20 | Kopf eines Triggers: Schalter für `enabled`, darunter der nächste und der letzte Lauf aus `GET /triggers` (bei ungespeicherten Änderungen „Next run after saving“), bei HTTP-Triggern die Adresse des Listeners; „Run target now“ in der Dateileiste startet das Ziel mit Environment und `params` des Triggers |
| E20 | Zeitplan (`cron`): der Ausdruck in Worten und die nächsten drei Zeitpunkte unter dem Feld, berechnet vom Server mit demselben Parser wie der Scheduler; die Zeitzone als Auswahl |
| E20 | Ziel als Auswahl der Workflows und Pipelines mit „Open“, Environment als Auswahl (leer: Standard); `params` als Tabelle mit den Parametern des Ziels (Typ, Pflicht, Standard), fehlende Pflichtparameter gelb. Bei HTTP-Triggern zeigt die Tabelle, welche Parameter aus dem Pfad und dem Body kommen |
| E20 | HTTP-Trigger: Methode und Pfad, darunter die fertige Adresse mit „Copy“; `auth` als Auswahl (none, Token), das Token als Secret-Feld nach S39f mit Warnung, wenn es für ein Environment fehlt |
| E20 | Environments und `project.yaml`: Variablen als Tabelle (Name mit Anzahl der Fundstellen, Wert, daneben der Wert der anderen Ebene bzw. die Environments, die ihn überschreiben); genutzte, aber nirgends gesetzte Variablen gelb mit „Set for …“; `name` eines Environments nur lesbar (Umbenennen im Explorer); `secretStore` als Auswahl mit Erklärung |
| E20 | `secrets:` (Quelle `inline`) zeigt im Formular nur die Namen, nie die Werte, mit roter Warnung zu Klartext; die Werte bleiben in der YAML-Ansicht sichtbar wie bisher |
| E20 | „New → Trigger“ fragt Name, Typ und Ziel; der neue Trigger startet deaktiviert (wie bisher in E11) |
| E20 | Umsetzung in zwei Schritten: E20a Server (allgemeiner Edit-Endpunkt für Dateien mit Metadaten, Varianten und Gruppen im Trigger-Schema, Zeitplan in Worten) mit dem Trigger-Formular, E20b Environments und `project.yaml` |
| E21 | Baut auf dem Fix der Tab-Leiste auf (PR #183): Scrollen ohne Scrollleiste, Mausrad waagerecht, aktiver Tab immer sichtbar, Verlauf am Rand. Abweichend vom Fix werden Tabs nur bis 160 px schmaler (höchstens 240 px, längere Namen mit „…“), damit etwa 12 Zeichen des Namens lesbar bleiben – bei 112 px blieben nur drei; die übrigen Tabs erreicht man über Scrollen und „▾“. Umgesetzt mit E21 |
| E21 | Am rechten Ende der Leiste fest „▾ 14“ (Zahl der offenen Dateien; Strg+Umschalt+L, siehe Nachtrag): Liste aller offenen Dateien in der Reihenfolge der Tabs mit Symbol, Name, Ordner, ● für ungespeichert und ×; Suchfeld oben (Tippen filtert nach Name und Pfad, ↑↓ wählen, Enter öffnet, Esc schließt); unten „Close saved files“ und „Close all…“ |
| E21 | Rechtsklick auf einen Tab: Close, Close others, Close to the right, Close saved (mit Zahl), Close all; Copy path, Reveal in explorer. Schließen mehrerer Dateien mit ungespeicherten Änderungen fragt wie bisher je Datei (E6a); „Close saved“ lässt sie offen |
| E21 | Kein Vorschau-Tab (wie in VS Code): vom Maintainer verworfen |
| E21 | Nachtrag bei der Umsetzung: Die Liste öffnet mit Strg+Umschalt+L statt Strg+Umschalt+E – das ist schon der größere Ausdruckseditor (S35b). „Close“ im Menü ohne den Hinweis Strg+W: Browser geben dieses Kürzel nicht an die Seite weiter (es schließt den Browser-Tab) |
| S46 | „Detect fields…“ im Abschnitt „Fields“ des Panels bei Eingabe-Transforms, die Felder ermitteln können (zuerst `csv.input`); der Server meldet das je Typ, der Editor zeigt den Knopf nur dort |
| E14 | Listen in Konfigurationen werden im Panel bearbeitet; der Editor ergibt sich aus dem Schema (ADR 0014), ohne eigenen Code je Transform. Statt „Lists and nested values are changed in the YAML view.“ |
| E14 | Darstellung: Listen mit bis zu drei kurzen, einfachen Eigenschaften als **Tabelle** (eine Zeile je Eintrag, Spaltenköpfe mit `*` für Pflicht, z. B. `core.constant`, `core.convert`, `core.select-fields`, `core.sort`); längere oder mehr Eigenschaften als **Karten** (z. B. `aggregates` von `core.group-by`, `cases` von `core.switch`): zugeklappt mit einer Zusammenfassung (`total = sum(Betrag)`, Bedingung → Ziel), aufgeklappt die Felder. Listen von Texten (Feldnamen wie `groupBy`, `remove`) als **Chips** |
| E14 | Jede Zeile/Karte: Griff zum Verschieben (Reihenfolge zählt, z. B. bei `cases`), × zum Entfernen; „Add field“, „Add aggregate“, „Add case“ am Ende. Neue Einträge sind leer, der Fokus steht im ersten Feld. Jede Änderung ist ein Schritt für Rückgängig wie andere Panel-Änderungen |
| E14 | Feste Werte (`enum`) als Auswahl, `bool` als Kästchen, Ausdrücke `{{ … }}` mit dem Ausdruckseditor (S35, Knopf `{ }`); Eigenschaften, die nur in einem Fall gelten (z. B. `separator` nur bei `concat`), erscheinen nur dann |
| E14 | **Feldnamen zur Auswahl:** Wo ein vorhandenes Feld gemeint ist (z. B. `name` bei `core.convert`, `core.select-fields`, `core.sort`, `field` bei Aggregaten, `groupBy`), bietet eine Auswahl die Felder der eingehenden Datensätze mit Typ an; bereits verwendete sind markiert, ein anderer Name lässt sich tippen. Neue Namen (`name` bei `core.constant`, Aggregaten) sind freie Eingaben. Welche Eigenschaft ein Feld meint, sagt der Descriptor (`format: runnel-field` im Schema), ebenso Ziele von Links (`to` bei `core.switch`: `format: runnel-target`, Auswahl der Ziele der Daten-Links) |
| E14 | Meldungen an der Zeile: ein unbekanntes Feld wird als Warnung unter der Zeile gezeigt, mit Vorschlag („did you mean umsatz?“), wie in Problems |
| E14 | Abschnitt **Help** je Transform (zugeklappt): kleines YAML-Beispiel aus dem Descriptor, „Show in YAML“ (springt zum Transform) und „Copy example“, Hinweis auf `runnel catalog <typ>` |
| E13 | Jeder eingebaute Baustein hat ein eigenes Icon; das allgemeine Symbol (Quadrat mit Strich) bleibt nur für Typen ohne eigenes Icon (z. B. aus Plugins, bis der Descriptor `icon` liefert) |
| E13 | Linien-Icons wie bisher (24 × 24, Strich 1,6, `currentColor`, weiß auf der Kachel in der Kategoriefarbe); lesbar ab 13 px (Palette) |
| E13 | `core.run-workflow` und `core.run-pipeline` zeigen die Symbole der Workflow- und Pipeline-Dateien aus dem Explorer – derselbe Begriff, dasselbe Zeichen |
| E13 | Datei-Actions: ein Blatt mit Zeichen unten rechts für Dateien (`file.copy` zwei Blätter, `file.delete` ✕, `file.exists` ✓, `file.wait` Uhr), ein Ordner für Ordner und Listen (`file.mkdir` +, `file.move` →, wie „Move to…“ im Explorer, `file.list` Zeilen) |
| E13 | `http.respond` (Action) und `http.response` (Transform) teilen sich einen Antwortpfeil, wie `core.log` und `core.dummy` in Workflow und Pipeline |
| E13 | `core.dummy` bekommt ein neues Icon (bisher dem Pipeline-Symbol zu ähnlich): eine leere, gestrichelte Stelle auf der Linie – ein Schritt, der nichts tut |
| E12 | Ordner sind frei: nach Art (`workflows/`, `pipelines/`) oder nach Thema (z. B. `import/` mit den Workflows und der Pipeline, die nur sie nutzen; `shared/` für gemeinsam genutzte Pipelines). Ob eine Datei Workflow oder Pipeline ist, entscheidet die Endung; `workflows/` und `pipelines/` sind nur die Vorgabe für „+“ im Kopf des Explorers. Nur Trigger (`triggers/`) und Environments (`environments/`) haben feste Ordner |
| E12 | Verschieben auf zwei Wegen: Ziehen und Ablegen im Explorer (der Zielordner wird hervorgehoben, ein Hinweis nennt den Zielordner) und „Move to…“ im Menü einer Datei oder eines Ordners mit einer Ordnerauswahl (aktueller Ordner ausgegraut, „New folder“ im Dialog) |
| E12 | Beim Verschieben und Umbenennen fragt ein Dialog „Update the references?“, wenn andere Dateien auf den alten Pfad verweisen (Trigger-Ziel, `core.run-workflow`, `core.run-pipeline` mit festem Pfad, wie die Rückfrage beim Löschen in E11): je Verweis Datei, Stelle und alter → neuer Pfad, standardmäßig ausgewählt; „Move and update n references“, „Move only“ oder „Cancel“. Ohne Verweise kein Dialog |
| E12 | Angepasst wird nur der Pfad in der betroffenen Zeile; die Dateien werden gespeichert. Verweise mit Variablen oder Ausdrücken werden nicht erkannt und nicht geändert (die Prüfung meldet sie danach). Ändert sich eine geöffnete Datei mit ungespeicherten Änderungen, wird sie nicht angepasst und im Dialog als nicht änderbar genannt. Damit ändert sich die Entscheidung aus E11 („Umbenennen ändert keine Verweise“) |
| E12 | Trigger werden auch in Unterordnern von `triggers/` gelesen (Server, `runnel triggers`, Prüfung); Namen bleiben im ganzen Projekt eindeutig. Behebt K14 |
| S46 | Vorschlag in einem Dialog (das Panel ist für eine Tabelle zu schmal): je Spalte Auswahl, Name, Type, Format, gelesene Werte und eine Begründung; geschrieben wird erst mit „Write n fields“ |
| S46 | Gelesen wird mit der aktuellen (auch ungespeicherten) Konfiguration des Transforms; Variablen im Pfad mit den Standardwerten der Parameter und dem Standard-Environment (wie Run ohne Dialog); höchstens die ersten 1.000 Zeilen |
| S46 | Ein Typ wird nur vorgeschlagen, wenn jeder gelesene Wert passt; sonst `string` mit Begründung (z. B. „3 of 4 values are decimal“). Zahlen mit führender Null bleiben Text; `ja`/`nein` ist kein `bool` (das Format kennt true/false, 1/0, yes/no) |
| S46 | Schreiben ist ein Schritt für Rückgängig (Hinweis „Wrote n fields · Undo“ wie in E6a); gespeichert wird weiter mit Save |
| S46 | Erneut ermitteln bei vorhandenen Feldern: aufgeführte Felder behalten Typ und Format („listed“), neue Spalten sind nicht ausgewählt („new“), aufgeführte Felder, die in der Datei fehlen, werden markiert und lassen sich abwählen |
| S46 | Das Panel zeigt die Felder als Liste (Name, Typ, Format); einzelne Felder ändert man bis E9 in der Datei mit einem Texteditor (die YAML-Ansicht ist bis dahin nur zum Lesen; gleicher Hinweis wie heute bei Listen im Panel) |
| S46 | Datei nicht lesbar: Meldung im Dialog mit dem Ordner, auf den sich relative Pfade beziehen; keine Meldung am Feld, weil `/validate` keine Dateien liest |
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
node design/mockups/screenshots.cjs s25a-pipeline canvas,link,run,problems,foreach
node design/mockups/screenshots.cjs e7c-parameters parameters,add,outputs,problems
node design/mockups/screenshots.cjs s46-fields fields,detect,written,update,error
node design/mockups/screenshots.cjs e9-yaml edit,problem,syntax,canvas
node design/mockups/screenshots.cjs s44-preview preview,json,link,error
node design/mockups/screenshots.cjs e11-files new,name,rename,delete,folder
node design/mockups/screenshots.cjs e12-organize drag,moveto,refs,triggers
node design/mockups/screenshots.cjs e13-icons overview,sizes,canvas
node design/mockups/screenshots.cjs e14-lists constant,convert,group,switch,help
node design/mockups/screenshots.cjs e16-copy-paste copy,paste,refs,wrongkind
node design/mockups/screenshots.cjs e17-markdown open,text,new,links
node design/mockups/screenshots.cjs s16d-icons choice,canvas
node design/mockups/screenshots.cjs s39-connections form,test,new,field,plaintext,secrets,set
```

Erzeugt je Zustand `…-hell.png` und `…-dunkel.png` (1280 × 800, doppelte Auflösung). Voraussetzung: Playwright mit
Chromium. Fehlen Inter oder JetBrains Mono auf dem Rechner, nutzen die Screenshots die Ersatzschriften aus den Tokens.
