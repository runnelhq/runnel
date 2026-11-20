# Testplan und bekannte Einschränkungen

Stand: nach S07 (Version `v0.1.0-alpha.2`). Dieses Dokument beschreibt, wie der aktuelle Stand geprüft wird – automatisch
und manuell unter Windows – und welche Einschränkungen bekannt sind.

## 1. Automatische Prüfungen

| Prüfung | Wann | Befehl |
|---|---|---|
| Lint (golangci-lint) | jeder PR, `main` | `make lint` |
| Unit-, Golden- und CLI-Tests mit Race-Detector | jeder PR (Linux), ohne Race-Detector (Windows) | `make test` |
| Schwachstellen (`govulncheck`) | jeder PR, wöchentlich | `make vulncheck` |
| Fuzzing von YAML-Parser und Variablen | wöchentlich (5 min je Ziel), lokal bei Bedarf | `make fuzz FUZZTIME=2m` |
| Build, Packen, SBOM | jeder PR | `make package` |

Testabdeckung (Anweisungen): `vars` 98 %, `model` 93 %, `actions/core` 94 %, `cli` 92 %, `engine/workflow` 92 %.

Vom Fuzzing gefundene Eingaben werden unter `internal/model/testdata/fuzz/` gespeichert und laufen danach bei jedem
`go test` als Regressionstest mit.

## 2. Manueller Test unter Windows

Vorbereitung: Release `v0.1.0-alpha.2` (oder neuer) herunterladen, ZIP entpacken, PowerShell im entpackten Ordner öffnen.
Für die Beispiele zusätzlich das Repository als ZIP herunterladen (*Code → Download ZIP*) und den Ordner `examples`
daneben legen.

| # | Test | Erwartung |
|---|---|---|
| W1 | `.\runnel.exe version` | Version `v0.1.0-alpha.2`, `windows/amd64`; ggf. SmartScreen-Warnung beim ersten Start (exe noch nicht signiert) |
| W2 | `.\runnel.exe validate examples\hello` | `3 workflow files checked: 0 errors, 0 warnings` |
| W3 | `.\runnel.exe validate examples\broken` | 4 Fehler, 2 Warnungen; Exit-Code 1 (`$LASTEXITCODE`) |
| W4 | `.\runnel.exe run examples\hello\workflows\hello.workflow.yaml` | Gruß mit Rechnername und `%TEMP%`-Pfad, `output MESSAGE = …`, Exit-Code 0 |
| W5 | wie W4 mit `--env prod --param GREETING=Moin` | „Moin, Production! … \\fileserver\exchange\runnel-hello“ |
| W6 | wie W4 mit `--param GREETING=Servus` | Exit-Code 2, Meldung zu erlaubten Werten |
| W7 | `.\runnel.exe run -v examples\hello\workflows\parallel.workflow.yaml` | `fetch-a` und `fetch-b` starten gleichzeitig, `combine` nach ca. 2 s, `report-error` übersprungen |
| W8 | wie W7 mit `--param DELAY_B=oops` | `[fetch-b] failed`, `[report-error] A branch failed`, Workflow trotzdem erfolgreich (Fehler behandelt) |
| W9 | Workflow mit `core.wait` von `1h` starten, dann **Strg+C** | „Cancelling…“, `cancelled after …`, Exit-Code 130 |
| W10 | wie W9, Strg+C **zweimal** | Programm endet sofort |
| W11 | Workflow in Notepad öffnen, als „UTF-8 mit BOM“ bzw. „Unicode“ speichern, `validate` | keine Fehler |
| W12 | Workflow-Datei auf einem Netzlaufwerk (UNC-Pfad `\\server\share\…`) prüfen und ausführen | wie lokal |
| W13 | Pfad mit Leerzeichen, z. B. `"C:\Mein Ordner\test.workflow.yaml"` | wie lokal |
| W14 | Ausführung über die Windows-Aufgabenplanung (Programm `runnel.exe`, Argumente `run C:\…\hello.workflow.yaml`) | Aufgabe zeigt Ergebnis 0 (bzw. 0x1 bei Fehler) |
| W15 | Herkunft prüfen: `gh attestation verify runnel_0.1.0-alpha.2_windows_amd64.zip --repo runnelhq/runnel` | „Verification succeeded“ |
| W16 | ab dem ersten Release nach E1: `.\runnel.exe catalog`, dann `.\runnel.exe catalog core.log` und `.\runnel.exe catalog --json > catalog.json` | Liste der Core-Actions unter `general`; Felder `message` (required) und `level` (debug\|info\|warn\|error, default info); gültiges JSON, Umlaute/Gedankenstrich korrekt dargestellt |
| W17 | ab dem ersten Release nach E2: `.\runnel.exe serve examples\hello`, im Browser `http://localhost:7300/api/v1/projects/current/tree` und `…/api/v1/files/workflows/hello.workflow.yaml` öffnen; Windows-Firewall-Abfrage beobachten; Strg+C | Baum und Datei werden angezeigt; **keine** Firewall-Abfrage (nur lokal); `Server stopped` |
| W18 | ab dem ersten Release nach E3: Server wie W17 starten; in einer zweiten PowerShell `curl.exe -N http://localhost:7300/api/v1/events`, in einer dritten `curl.exe -X POST http://localhost:7300/api/v1/runs -d '{\"path\":\"workflows/parallel.workflow.yaml\"}'`; danach Strg+C im Server während eines Laufs mit `DELAY_B=1h` | Ereignisse erscheinen live im zweiten Fenster; `GET /api/v1/runs` zeigt den Lauf; beim Beenden wird der Lauf abgebrochen, `Server stopped` |
| W19 | ab dem ersten Release nach E4a: `.\runnel.exe serve examples\hello`, im Browser (Edge, Chrome, Firefox) `http://localhost:7300` öffnen; Farbschema umschalten; Windows auf dunklen Modus stellen; Server mit Strg+C beenden | Kopfzeile mit Projekt „hello“, Schriften Inter/JetBrains Mono, „● Connected“; „System“ folgt Windows; nach Beenden Hinweis „Connection to the Runnel server lost“ und „⚠ Disconnected“, nach Neustart wieder verbunden |
| W20 | ab dem ersten Release nach E4b: wie W19; im Explorer `hello.workflow.yaml` und `environments\dev.yaml` öffnen, Tabs wechseln und schließen, „Copy path“ und in Notepad einfügen; `dev.yaml` in Notepad als „Unicode“ speichern und im Editor neu öffnen; eine geöffnete Datei im Windows-Explorer umbenennen und den Tab erneut anklicken | Tabs wie im Mockup, `${…}` hervorgehoben; Pfad mit `\` (z. B. `C:\…\hello\workflows\hello.workflow.yaml`); Statusleiste „YAML · UTF-16 LE · … lines“; nach Umbenennen beim nächsten Laden „File not found“ |
| W21 | ab dem ersten Release nach E5: wie W19; `parallel.workflow.yaml` öffnen; zoomen, Fläche verschieben, Knoten anklicken; in Notepad bei `fetch-b` den Typ in `core.wiat` ändern, speichern, Tab schließen und neu öffnen; „Show in YAML ›“; danach den `layout`-Block löschen und neu öffnen; Änderungen rückgängig machen | Graph wie im Mockup E5 (✔/✖ an den Links, `join: first`); Fehler-Markierung an `fetch-b`, Zähler ✖ 1, Sprung in die richtige Zeile; ohne Layout automatisch angeordnet mit Hinweis |
| W22 | ab dem ersten Release nach E6a: wie W19 auf einer **Kopie** von `examples\hello`; in `parallel.workflow.yaml` Knoten verschieben, zwei mit Strg+Klick wählen und mit Entf löschen, „Undo“, Strg+S; Datei in Notepad öffnen; Datei in Notepad ändern und speichern, im Editor erneut ändern und Strg+S; Tab mit ungespeicherten Änderungen schließen | ● am Tab, „Unsaved changes“; nach Strg+S nur die `layout`-Zeilen bzw. gelöschten Blöcke geändert, Kommentare und CRLF erhalten; Konfliktdialog mit Reload/Overwrite; Rückfrage beim Schließen |
| W23 | ab dem ersten Release nach E6b: wie W22 auf einer **Kopie** von `examples\hello`; in `parallel.workflow.yaml` „Log“ aus der Palette auf den Canvas ziehen, als `notify` benennen; Link von `report-error` zu `notify` ziehen; Link-Symbol anklicken und „On failure“ wählen; Link von `combine` zu `fetch-a` ziehen; `combine` per Doppelklick in `merge` umbenennen; Strg+S und Datei in Notepad öffnen | neue Action mit ID `log-1`, Eingabefeld zum Umbenennen; neue Links „On success“, Rückwärts-Link mit ↻; in der Datei `- id: notify`, `{ from: report-error, to: notify, when: failure }`, `loop: true`, `merge` in Action, Links und `layout`; Kommentare und CRLF erhalten |
| W24 | ab dem ersten Release nach E7a: wie W22 auf einer **Kopie** von `examples\hello`; in `parallel.workflow.yaml` `combine` anklicken, „Message“ auf zwei Zeilen ändern, „Level“ auf `warn`; bei `fetch-a` Timeout `5 mins` eingeben, dann `5m`; Retry 3 und `10s`, „Exponential“; Strg+Z; Strg+S und Datei in Notepad öffnen | Panel „Properties“ wie im Mockup; `5 mins` rot mit Meldung am Feld, der Canvas bleibt sichtbar; in der Datei `message: \|-` mit zwei Zeilen, `level: warn`, `timeout: 5m`, `retry:`-Block; Kommentare und CRLF erhalten |
| W25 | ab dem ersten Release nach E7b: wie W24; ins Leere klicken und Description sowie „Max. concurrent runs“ des Workflows setzen; einen Link anklicken, Esc, „Always“ und „Disabled“ wählen; bei `combine` die Message leeren, auf das Fehler-Badge in der Dateileiste klicken und die Zeile in „Problems“ anklicken; Strg+S und Datei in Notepad öffnen | Workflow- und Link-Eigenschaften wie im Mockup; Link gestrichelt und grau; Klick auf die Meldung wählt `combine` und setzt den Cursor ins Feld „Message“; in der Datei `description:` nach `name:`, `settings:` vor `actions:`, `when: always, disabled: true` am Link |
| W26 | ab dem ersten Release nach E8a: wie W22 auf einer **Kopie** von `examples\hello`; `parallel.workflow.yaml` öffnen, „Run“, im Dialog `DELAY_B` = `oops`, „Run“; danach F5; über ▾ `DELAY_A` = `30s` und während des Laufs „Cancel“; einen Knoten verschieben und „Run“ | Dialog nur beim ersten Mal; Status an den Knoten (läuft, ✔, ✖, ■, –), aktiver Link „strömt“, nicht gefolgte Links blass; Statusleiste „Running (dev) · …“, danach „✔ Last run succeeded“ bzw. „■ Last run cancelled“; bei ungespeicherten Änderungen die Rückfrage „Save before running?“ |
| W27 | ab dem ersten Release nach E8b: wie W26; `parallel.workflow.yaml` mit `DELAY_A` = `5s` und `DELAY_B` = `0s` laufen lassen; „Debug“ aus- und einschalten; danach mit `DELAY_B` = `oops`, `fetch-b` anklicken und „Show in log“; in der Konsole `runnel run -v --param DELAY_A=5s --param DELAY_B=0s examples\hello\workflows\parallel.workflow.yaml` | Log öffnet sich beim Start und folgt dem Lauf; `combine` zeigt ⧗ „wartet“, im Log „waiting for 1 more incoming link (join: all)“ (auch in der Konsole); ohne Debug nur INFO/WARN/ERROR; Panel „✖ Failed in the last run … invalid duration "oops"“, Sprung markiert die Fehlerzeile |

Ergebnisse bitte als GitHub-Issue mit Testnummer, Windows-Version und Ausgabe festhalten.

### Ergebnisse

| Datum | Version | Tests | Ergebnis | Hinweise |
|---|---|---|---|---|
| 2026-11-19 | `v0.1.0-alpha.3` | W1–W8, W16, W19 | ✅ bestanden | gemeldet vom Maintainer; noch offen: W9–W15, W17, W18 und die Editor-Tests W20–W27 |

## 3. Bekannte Einschränkungen

| # | Einschränkung | Umgehung / Plan |
|---|---|---|
| K1 | Keine Standardwerte in Variablen (`${env:X:-fallback}`); `examples/hello` braucht `COMPUTERNAME` und `TEMP` und schlägt daher unter Linux fehl | Variablen im Projekt setzen; Standardwert-Syntax als Kandidat für eine spätere Erweiterung |
| K2 | Zeitangaben (`timeout`, `joinTimeout`, `retry.backoff`, `settings.timeout`) können keine Variablen enthalten | feste Werte; Variablen erst, wenn Felder zur Laufzeit ausgewertet werden (mit S08 prüfen) |
| K3 | `join: all` startet auch, wenn ein Zweig fehlgeschlagen ist (so in ADR 0003 festgelegt) | Fehlerpfade separat modellieren; ggf. später Modus „alle erfolgreich“ |
| K4 | In Schleifen wirken Zwischenergebnisse bereits auf Joins außerhalb der Schleife | für Wiederholungen `retry` statt Schleife verwenden (ADR 0003, „Umsetzung“) |
| K5 | Typfehler nennen das betroffene Feld nicht („expected an integer, got a string“) – die Zeile/Spalte zeigt es | Grenze der YAML-Bibliothek; eigene Meldungen in einem späteren Schritt |
| K6 | Keine Korrekturvorschläge bei unbekannten Feldern (`tpye` → „did you mean type?“); nur bei Action-IDs in Links | späterer Schritt |
| K7 | Ausdrücke `{{ … }}` werden noch nicht ausgewertet (in Bedingungen und Outputs vor dem Start abgelehnt, sonst als Text) | S08 |
| K8 | Die YAML-Bibliothek stürzt bei bestimmten fehlerhaften Eingaben ab; Runnel fängt das ab und meldet „the file could not be read“ | Fehlerbericht an `goccy/go-yaml` (Eingabe in `internal/model/testdata/fuzz/`) |
| K9 | `runnel.exe` ist nicht signiert (SmartScreen-Warnung) | Code-Signing vor 1.0 (Roadmap) |
| K10 | Strg+C und Exit-Code 130 sind unter Windows nur über die CI (Build) abgedeckt, nicht automatisch getestet | manueller Test W9/W10 |
