# ADR 0023: Programme und PowerShell starten – `shell.run` und `shell.powershell`

- **Status:** Angenommen
- **Datum:** 2026-11-23
- **Bezug:** KONZEPT Kap. 7.2 und 12.1; [ADR 0005 – Mehrbenutzerbetrieb](0005-mehrbenutzerbetrieb.md),
  [ADR 0013 – Typnamen](0013-typnamen-namensraeume.md), [ADR 0014 – Descriptor](0014-action-descriptor-schema.md),
  [ADR 0018 – HTTP-Trigger](0018-http-trigger.md), [ADR 0020 – Relative Pfade](0020-relative-pfade.md),
  [ADR 0022 – Connections und Secrets](0022-connections-secrets.md)

## Kontext

In Windows-Umgebungen hängt viel Automatisierung an vorhandenen Programmen und Skripten:
- Kopieren mit `robocopy`;
- Exporte aus dem ERP, Aufrufe von `sqlcmd` oder `7z`;
- eigene `.exe`- und `.bat`-Dateien;
- PowerShell-Skripte für Active Directory, Dienste, Freigaben oder Office.

Runnel kann sie bisher nicht starten. Das Konzept nennt `shell.powershell` (Kap. 7.2) und „Programm starten“ (Kap. 12.1).
Der Namensraum `shell` ist reserviert (ADR 0013). Wie ausgeführt wird, was der Lauf zurückbekommt und welche Grenzen
gelten, ist noch offen.

Ein Programm zu starten ist eine neue Sicherheitsgrenze: Runnel führt Code aus, der nicht von Runnel stammt. Werte aus
einem Lauf erreichen dabei ein anderes Programm, z. B. Parameter eines HTTP-Triggers. Deshalb ist eine ADR nötig.

Betrachtete Alternativen:

| Variante | Bewertung |
|---|---|
| **Zwei Actions, `shell.run` und `shell.powershell`** | `shell.run` startet ein Programm mit Argumenten ohne Shell. `shell.powershell` kümmert sich zusätzlich um Parameter mit Typen, JSON-Ergebnis, Execution Policy und Kodierung. Beide teilen sich die Ausführung. |
| Eine Action `shell.run`, PowerShell als Programm | Möglich (`program: pwsh`), aber Parameter, JSON und Execution Policy müsste jeder Anwender selbst richtig zusammensetzen. Fehleranfällig, vor allem beim Quoting. |
| Kommandozeile als Text (`command: "robocopy a b /MIR"`) über `cmd.exe` oder `sh` | Bequem, aber jeder Wert im Text wird von der Shell gedeutet (`&`, `|`, `%`). Mit Werten aus einem HTTP-Trigger ist das eine Einladung zur Command-Injection. Abgelehnt. |
| Schalter in `project.yaml`, der `shell.*` erst freigibt | Mit dem Maintainer besprochen und **nicht** gewählt. Wer Dateien des Projekts ändern kann, kann den Schalter auch setzen; die Grenze liegt beim Zugriff auf den Projektordner und den Server (ADR 0005), nicht bei einem Feld in einer Datei. |

## Entscheidung

### 1. Zwei Actions mit gemeinsamer Ausführung

- **`shell.run`** startet ein Programm mit einer Liste von Argumenten, **ohne Shell dazwischen**.
- **`shell.powershell`** führt ein PowerShell-Skript aus, inline oder als Datei, mit benannten Parametern und optional
  JSON als Ergebnis.
- Beide nutzen dieselbe Ausführung (Abschnitt 4): Prozess, Arbeitsordner, Umgebung, Timeout, Grenzen und Kodierung.
- Transforms (`shell.powershell` mit Datensätzen als NDJSON über stdin/stdout, KONZEPT 7.2) folgen als eigener Schritt.
  Diese ADR gilt für sie sinngemäß.

### 2. `shell.run`

```yaml
- id: mirror
  type: shell.run
  with:
    program: robocopy                    # über PATH oder relativ zum Projektordner
    args: [ "data/out", "\\\\server\\share\\in", "*.csv", "/MIR", "/R:2" ]
    okCodes: [0, 1, 2, 3]                # robocopy: 0–7 sind Erfolg; Standard [0]
    env:
      API_TOKEN: ${secret:ERP_TOKEN}
    timeout: 10m
```

| Feld | Bedeutung |
|---|---|
| `program` | Programm über `PATH` oder als Pfad, relativ zum Projektordner (ADR 0020). Erlaubt `${…}`-Variablen des Projekts und der Environments sowie `${env:…}`, aber keine Parameter des Workflows und keine Ausdrücke `{{ … }}`; das prüft `validate`. |
| `args` | Liste. Jedes Element ist genau ein Argument und darf `${…}` und `{{ … }}` enthalten. Runnel quotet es für das Zielsystem; nichts wird von einer Shell gedeutet. |
| `okCodes` | Exit-Codes, die als Erfolg gelten (Standard `[0]`). |
| `workdir` | Arbeitsordner, Standard der Projektordner. |
| `env` | Zusätzliche Umgebungsvariablen. Die Werte sind Felder für Secrets (`${secret:…}` erlaubt, ADR 0022). Die Umgebung des Runnel-Prozesses wird geerbt. |
| `input` | Optionaler Text für stdin. Ohne `input` ist stdin sofort geschlossen, damit ein Programm, das auf eine Eingabe wartet, nicht hängt. |
| `encoding` | Kodierung von stdout und stderr: `auto` (Standard), `utf-8`, `oem`, `windows-1252`. Siehe Abschnitt 4. |
| `json` | `true`: stdout wird als JSON gelesen und als `output.json` weitergegeben. Seit R5b `format: json` → `output.value` (Nachtrag). |
| `timeout` | Standard 10m. |

**Batch-Dateien:** `.bat` und `.cmd` laufen unter Windows immer über `cmd.exe`. Dort kann ein Argument mit `&`, `|`,
`<`, `>`, `^`, `%`, `!` oder `"` den Befehl verändern. Solche Argumente sind für Batch-Dateien ein Fehler, mit Hinweis
auf eine `.ps1` oder eine Umgebungsvariable. Das gilt auch dann, wenn Go sie maskieren könnte, denn die Regeln von
`cmd.exe` sind nicht vollständig zu beherrschen.

### 3. `shell.powershell`

```yaml
- id: cleanup
  type: shell.powershell
  with:
    script: |
      param([string]$Folder, [int]$Days)
      Get-ChildItem $Folder -File |
        Where-Object LastWriteTime -lt (Get-Date).AddDays(-$Days) |
        Remove-Item -PassThru | Select-Object Name, Length | ConvertTo-Json
    # oder: file: scripts/cleanup.ps1
    params:
      Folder: ${ARCHIVE_DIR}
      Days: "{{ params.KEEP_DAYS }}"
    format: json                         # bis R5b: json: true
    edition: auto                        # auto | pwsh | windows
```

- **Skript:**
  - Entweder `script` (Text) oder `file` (Pfad relativ zum Projektordner, KONZEPT: `scripts/`).
  - `script` und `file` dürfen **keine** `${…}` und `{{ … }}` enthalten (wie SQL, ADR 0022). Werte kommen nur über
    `params` in das Skript.
- **Parameter:**
  - `params` gehen als JSON über stdin an ein kleines Startskript von Runnel. Das Startskript übergibt sie per
    Splatting (`& $script @params`) als benannte Parameter.
  - Typen bleiben so erhalten: Zahlen, Wahrheitswerte, Listen und Objekte.
  - Kein Wert wird je in Skripttext eingesetzt.
- **Ausführung:** `-NoProfile -NonInteractive -ExecutionPolicy Bypass`.
  - Die Execution Policy gilt nur für diesen Prozess; die Einstellung des Systems bleibt unverändert.
  - Das Startskript setzt `$ErrorActionPreference = 'Stop'` und die Ausgabe auf UTF-8.
  - Eine nicht abgefangene Ausnahme beendet das Skript mit Exit-Code 1. Ihre Meldung wird zum Fehler der Action.
- **Edition:**
  - `auto` nimmt PowerShell 7 (`pwsh`), wenn vorhanden, sonst Windows PowerShell 5.1 (`powershell.exe`).
  - `pwsh` und `windows` erzwingen eine Edition.
  - Unter Linux gibt es nur `pwsh`.
- **Ergebnis:** wie `shell.run`. Mit `json: true` (seit R5b `format: json`) wird der zuletzt ausgegebene JSON-Wert gelesen, typisch aus
  `ConvertTo-Json`. Ein Text, der kein JSON ist, ist dann ein Fehler.

### 4. Gemeinsame Ausführung

- **Prozessbaum:** Bei Timeout oder Abbruch des Laufs endet der ganze Prozessbaum.
  - Unter Windows über ein Job Object (`golang.org/x/sys/windows`, schon Abhängigkeit), unter Linux über eine
    Prozessgruppe.
  - Ohne das liefen z. B. von `robocopy` oder einem Skript gestartete Kindprozesse weiter.
- **Arbeitsordner und Pfade:** relativ zum Projektordner (ADR 0020), UNC-Pfade wie lokale.
- **Kodierung:** `auto` liest UTF-8. Ist der Text kein gültiges UTF-8, nimmt Runnel unter Windows die OEM-Codepage der
  Konsole (z. B. 850), wie sie ältere Konsolenprogramme ausgeben. `shell.powershell` erzwingt UTF-8.
- **Grenzen:**
  - stdout und stderr werden je bis 1 MB gehalten (`maxOutput`). Was darüber hinausgeht, wird verworfen, mit
    `truncated: true` im Output und einer Warnung im Log.
  - Beide Ströme werden laufend gelesen, damit volle Puffer das Programm nicht blockieren.
- **Output der Action:** `exitCode`, `stdout`, `stderr`, `truncated`, `duration` und bei `json: true` zusätzlich `json`
  (seit R5b `durationMs` und `value`, siehe Nachtrag).
- **Erfolg:** Der Exit-Code steht in `okCodes`. Sonst schlägt die Action fehl. Die Meldung nennt Exit-Code und die
  letzten Zeilen von stderr; Retry und `when: failure` greifen wie bei jeder Action.
- **Log:**
  - Start (Programm, Zahl der Argumente) und Ende (Exit-Code, Dauer) stehen im Log, nicht aber stdout.
  - Argumente stehen nur im Debug-Log, maskiert (`vars.Mask`).
  - Secrets aus `env` und alle Werte der Maske werden auch in stdout und stderr ersetzt, bevor sie in Output oder Log
    gelangen.

### 5. Sicherheit

- **Keine Shell:** Werte aus dem Lauf erreichen Programme nur als einzelne Argumente, als `params` oder als
  Umgebungsvariablen. Sie werden nie von `cmd.exe`, `sh` oder PowerShell als Befehl gedeutet. Die Ausnahme sind
  Batch-Dateien, und dort sind gefährliche Zeichen verboten (Abschnitt 2).
- **Feste Programme und Skripte:**
  - `program` kennt keine Workflow-Parameter und keine Ausdrücke.
  - `script` und `file` kennen gar keine Variablen.
  - Ein HTTP-Trigger kann so nicht bestimmen, *was* ausgeführt wird, nur *womit*.
- **Secrets:**
  - Secrets sind nur in `env` erlaubt, nicht in `args`, weil Argumente in der Prozessliste für andere Benutzer
    sichtbar sind.
  - Sie werden in Log und Output maskiert und nicht an Prozesse anderer Actions weitergegeben.
- **Rechte:**
  - Programme laufen mit dem Konto des Runnel-Prozesses, beim Dienst also mit dem Dienstkonto. Runnel erhöht keine
    Rechte.
  - Die Doku empfiehlt ein eigenes Dienstkonto mit den nötigen Rechten statt `LocalSystem`.
- **Grenzen:** Timeout, Größe der Ausgabe und geschlossenes stdin verhindern hängende oder überlaufende Läufe.
- **Kein Freigabe-Schalter:** wie oben begründet. Schutz bieten der Zugriff auf Projektordner und Server (ADR 0005)
  und die Regeln oben.
- **Tests für Ablehnungen:**
  - Timeout mit beendetem Kindprozess;
  - zu große Ausgabe;
  - gefährliche Zeichen bei `.bat`;
  - Variablen in `script`, in `program` (Parameter) und Secrets in `args`;
  - Exit-Code außerhalb von `okCodes`.

### 6. Umsetzung

| Schritt | Inhalt |
|---|---|
| S60 | Gemeinsame Ausführung (Prozessbaum, Grenzen, Kodierung, Maskierung) und `shell.run`, mit Prüfung bei `validate`; Testplan für Windows (robocopy, `.bat`, OEM-Ausgabe, Abbruch eines Prozessbaums) |
| S61 | `shell.powershell` mit Startskript, `params` als JSON, `json`, Edition; Testplan für PowerShell 5.1 und 7 |
| später | Transform `shell.powershell` (Datensätze als NDJSON), eigener Schritt |

Die Schritte kommen mit dem ersten Umsetzungs-PR in die Roadmap.

## Konsequenzen

- Vorhandene Programme und Skripte lassen sich ohne Umweg einbinden. Für viele Windows-Anwender ist das der Einstieg.
- Workflows werden dadurch von der Maschine abhängig (installierte Programme, PowerShell-Edition, Rechte des
  Dienstkontos). `validate` kann nur Form und Variablen prüfen, nicht ob das Programm vorhanden ist; das zeigt erst der
  Lauf.
- Die Tests brauchen echte Prozesse. Unter Linux in der CI über kleine Hilfsprogramme (`go run` eines Testprogramms);
  Windows-Besonderheiten zusätzlich im Testplan.
- Ohne Freigabe-Schalter bleibt die Verantwortung beim Betrieb: Wer Projektdateien schreiben darf, darf Programme
  starten. Das steht so in der Doku zum Betrieb und in SECURITY.md.

## Abstimmung 2026-11-23

Mit dem Maintainer bestätigt:
- Standardwerte `timeout: 10m` und `maxOutput` 1 MB je Strom.
- `program` mit Variablen des Projekts und der Environments, ohne Workflow-Parameter und Ausdrücke.
- Batch-Dateien mit gefährlichen Zeichen in Argumenten werden strikt abgelehnt.
- Kein Freigabe-Schalter in `project.yaml`.

## Nachtrag 2026-11-23 (S60): `shell.run`

- **Programm:** Ein Name ohne Ordner wird über `PATH` gesucht, ein Pfad relativ zum Projektordner aufgelöst; Fehler
  „not found in PATH“ bzw. „not found or not executable“. Unter Windows startet das Programm ohne eigenes Fenster.
- **Format `runnel-program`** kennzeichnet `program`: `validate` und der Start eines Laufs melden `{{ … }}` und
  Workflow-Parameter darin; die Engine löst nur `${…}` auf.
- **`env`** ist eine Abbildung mit dem Format `runnel-secret` für jeden Wert. Das Schema erlaubt dafür das Format an
  Abbildungen von Texten (Ergänzung zu ADR 0014). Die Werte von `env` werden in stdout und stderr durch `***` ersetzt;
  Werte unter drei Zeichen nicht, damit Ausgaben lesbar bleiben.
- **Prozessbaum:** Unter Windows landet das Programm direkt nach dem Start in einem Job Object; beendet wird der Job
  nur bei Timeout oder Abbruch. Prozesse, die ein Programm nach normalem Ende zurücklässt, laufen weiter (z. B. ein
  gestarteter Dienst). Unter Linux eine eigene Prozessgruppe.
- **Ende:** Nach dem Ende wartet Runnel höchstens 2 s auf noch offene Ausgaben von Kindprozessen.

## Nachtrag 2026-11-23 (S61): `shell.powershell`

- **Startskript:** in Runnel eingebettet (`runner.ps1`) und wie das Inline-Skript je Lauf in einen temporären Ordner
  geschrieben, UTF-8 mit BOM (sonst liest Windows PowerShell 5.1 im ANSI-Zeichensatz); der Ordner wird danach gelöscht.
  Aufruf: `-NoProfile -NonInteractive -ExecutionPolicy Bypass -File runner.ps1 <skript>`, die Parameter als JSON über
  stdin. Ein Skript aus `file` wird nicht kopiert, sondern direkt aufgerufen.
- **Exit-Code:** der des Skripts (`exit N`) bzw. des letzten Programms, das es gestartet hat (`$LASTEXITCODE`); eine
  nicht abgefangene Ausnahme gibt 1 mit ihrer Meldung auf stderr. Erfolg ist nur 0 (kein `okCodes` wie bei `shell.run`).
- **`${…}` im Skript:** PowerShell kennt selbst `${name}`. Die Prüfung meldet es trotzdem, mit dem Hinweis, `$Name`
  oder `$env:NAME` zu schreiben – so ist eindeutig, dass Runnel im Skript nichts ersetzt.
- **`json`:** der ganze stdout als JSON, sonst der letzte JSON-Wert ab einer Zeile, die mit `{` oder `[` beginnt (z. B.
  `ConvertTo-Json` nach anderen Ausgaben). Gilt seitdem auch für `shell.run`.
- **Tests:** Die Skriptläufe brauchen PowerShell und laufen in der CI (Linux mit `pwsh`, Windows mit beiden
  Editionen); fehlt PowerShell in der CI, schlagen sie fehl statt übersprungen zu werden.

## Nachtrag 2026-11-26 (R5b): `format` und `durationMs`

- `shell.run` und `shell.powershell` lesen stdout mit `format: text|json` (Standard `text`) statt `json: true`; mit
  `json` steht der Wert in `output.value` statt `output.json`. Das folgt ADR 0025 („Lesart einer Datei: `format`“) und
  `file.read` (`format: json` → `output.value`). Andere Werte von `format` sind ein Fehler.
- Die Laufzeit heißt `output.durationMs` und ist eine Zahl in Millisekunden statt des Texts `duration` (`"1.25s"`), wie
  `durationMs` der Läufe in der API und `results.<id>.durationMs`; mit Text ließ sich nicht rechnen.
- Der alte Name `json` ist ein unbekanntes Feld (bewusster Bruch vor 1.0, ADR 0029); Ausdrücke mit
  `output.json` oder `output.duration` liefern nichts mehr und müssen angepasst werden.
