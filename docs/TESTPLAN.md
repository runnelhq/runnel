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
| W2 | `.\runnel.exe validate examples\hello` | `2 workflow files checked: 0 errors, 0 warnings` |
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

Ergebnisse bitte als GitHub-Issue mit Testnummer, Windows-Version und Ausgabe festhalten.

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
