# ADR 0027: Fehler-Workflow

- **Status:** Angenommen
- **Datum:** 2026-11-25
- **Bezug:** [ADR 0003 – Join-Semantik](0003-join-semantik.md), [ADR 0020 – Relative Pfade](0020-relative-pfade.md), [ADR 0022 – Connections und Secrets](0022-connections-secrets.md)
  (Maskierung), [ADR 0026 – Run-Historie](0026-run-historie.md); KONZEPT Kap. 6 (Betrieb); Roadmap S72a, S72b

## Kontext

Runnel läuft im Betrieb unbeaufsichtigt: Trigger starten Workflows und Pipelines im Server oder im Windows-Dienst,
geplante Aufgaben rufen `runnel run` auf. Schlägt ein Lauf fehl, steht das in der Historie und im Server-Log. Es merkt
aber niemand, solange keiner nachsieht.

Innerhalb eines Workflows lassen sich Fehler schon behandeln, mit Fehler-Links (`when: failure`), `retry` und
`continueOnError`. Das muss aber jeder Workflow selbst tun, und Pipelines haben keine Links für einen fehlgeschlagenen
Lauf. n8n löst das mit einem „Error Workflow“, der bei jedem Fehler startet, Apache Hop mit Fehler-Hops und eigenen
Benachrichtigungs-Actions. Gesucht ist **eine Stelle je Projekt**, an der „Benachrichtige mich, wenn etwas schiefgeht“
einmal eingerichtet wird.

Betrachtete Varianten:

| Variante | Bewertung |
|---|---|
| **Fehler-Workflow** als Einstellung: ein normaler Workflow des Projekts, den Runnel nach einem fehlgeschlagenen Lauf mit dessen Daten startet | Nutzt alles Vorhandene (Mail, HTTP, Teams-Webhook, Datei, Datenbank); einmal je Projekt, je Datei abschaltbar. Gewählt. |
| Eingebaute Benachrichtigung (Mail-Einstellungen im Projekt) | Schnell eingerichtet, aber auf einen Kanal festgelegt; Teams, Ticketsystem oder Protokolltabelle bräuchten je eigene Einstellungen. |
| Trigger-Typ `error` (wie `cron`, `files`) | Passt in das Trigger-Modell, aber der Bezug „welche Läufe“ wäre eine zweite Konfiguration neben den Dateien; Abschalten je Datei umständlich. |
| Nur Fehler-Links in jedem Workflow | Gibt es schon; wird vergessen und deckt Pipelines und Fehler vor dem Start (Prüfung, Secrets) nicht ab. |

## Entscheidung

### 1. Einstellung `settings.onError`

- **`project.yaml`:** `settings.onError: workflows/fehler-melden.workflow.yaml` gilt für alle Workflows und Pipelines
  des Projekts.
- **Workflow oder Pipeline:** `settings.onError` überschreibt die Einstellung des Projekts mit einer anderen Datei;
  `none` schaltet sie für diese Datei ab.
- Der Wert ist ein Pfad relativ zum Projektordner ([ADR 0020](0020-relative-pfade.md)) und muss ein **Workflow** des Projekts sein, keine Pipeline.

### 2. Wann der Fehler-Workflow startet

- Wenn ein Lauf mit Status **`failed`** endet. Das gilt auch, wenn er schon vor dem Start scheitert (z. B. ein Secret
  fehlt), sobald Runnel weiß, welche Datei laufen sollte. Bei `cancelled` (Abbruch, Beenden des Dienstes) startet er
  nicht.
- **Für Läufe aus Triggern**, aus dem Windows-Dienst und aus **`runnel run`**. Im CLI läuft er danach synchron im selben
  Prozess. Der Exit-Code bleibt der des fehlgeschlagenen Laufs (1); ein Fehler des Fehler-Workflows ändert ihn nicht.
  `--no-on-error` schaltet ihn für diesen Aufruf ab.
- **Nicht für Läufe aus dem Editor** (`trigger: editor`): Wer im Editor ausführt, sieht den Fehler.
- **Nur für den obersten Lauf.** Ein Unterlauf (`core.run-workflow`, `core.run-pipeline`) meldet seinen Fehler an den
  Aufrufer; scheitert dadurch der Aufrufer, startet der Fehler-Workflow einmal für ihn.
- **Nie für den Fehler-Workflow selbst:** Scheitert er, steht das in der Historie und im Server-Log, ohne einen weiteren
  Fehler-Workflow. Das gilt auch, wenn derselbe Workflow anderswo als normaler Lauf gestartet wird und dort scheitert.
  Dann löst er den Fehler-Workflow aus wie jeder andere Lauf, nur nicht als Fehler-Lauf.

### 3. Was der Fehler-Workflow bekommt

Feste Parameter. Runnel übergibt nur die, die der Fehler-Workflow unter `parameters` deklariert. So lässt sich jeder
Workflow nutzen, der einen Teil davon braucht:

| Parameter | Inhalt |
|---|---|
| `FAILED_RUN` | ID des fehlgeschlagenen Laufs (ULID) |
| `FAILED_PATH` | Pfad der Datei relativ zum Projekt |
| `FAILED_NAME` | `name` des Workflows bzw. der Pipeline |
| `FAILED_ACTION` | ID der Action bzw. des Transforms, der fehlschlug; leer, wenn der Lauf vor dem ersten Schritt scheiterte |
| `FAILED_ERROR` | Fehlermeldung des Laufs, Secrets maskiert (ADR 0022) |
| `FAILED_TRIGGER` | Auslöser des Laufs (z. B. `cron:triggers/nightly.yaml`, `cli`) |
| `FAILED_STARTED`, `FAILED_FINISHED` | Start und Ende (RFC 3339) |

- Er läuft im **selben Environment** wie der fehlgeschlagene Lauf, mit dessen Variablen und Secrets.
- Weitere Pflichtparameter darf er nicht haben; `validate` meldet das (Abschnitt 5).

### 4. Schutz vor einer Flut von Fehler-Läufen

- Je Projekt und Prozess läuft **höchstens ein Fehler-Workflow gleichzeitig**. Weitere Fehler warten in einer
  Warteschlange (höchstens **20**) und starten der Reihe nach.
- Ist die Warteschlange voll, wird der Fehler verworfen, mit einer Warnung im Server-Log („on-error queue full: …“).
  Ein Trigger, der jede Minute scheitert, erzeugt so keine hundert gleichzeitigen Mails.
- Beim Beenden des Servers oder Dienstes darf der laufende Fehler-Workflow wie andere Läufe fertig werden
  (`--stop-timeout`); wartende werden verworfen und im Log genannt.
- `settings.maxConcurrentRuns` des Projekts zählt den Fehler-Workflow nicht mit, damit ein volles Projekt den Fehler
  noch melden kann.

### 5. Prüfung, Historie und Editor

- **`validate`** prüft `settings.onError` in `project.yaml`, Workflows und Pipelines mit Position:
  - Die Datei muss existieren und ein Workflow sein.
  - Sie darf keine Pflichtparameter außer den Parametern aus Abschnitt 3 haben.
  - Ein Parameter aus Abschnitt 3 mit einem anderen Typ als `string` ist ein Fehler.
  - Ein Fehler-Workflow, der sich selbst als `onError` nennt, bekommt eine Warnung; die Einstellung wird dort
    ignoriert.
- **Historie (ADR 0026):** Der Fehler-Lauf hat den Auslöser `error` und als Eltern-Lauf (`parent_id`) den
  fehlgeschlagenen Lauf. Die Liste der Läufe zeigt beide Richtungen: am Fehler-Lauf „for run …“, am fehlgeschlagenen
  „reported by …“.
- **Editor:** Feld „On error“ in den Einstellungen von Projekt, Workflow und Pipeline. Es ist eine Auswahl der Workflows
  des Projekts, dazu „Project default“ bzw. „None“. Das ist ein weiteres Feld in den bestehenden Formularen, ohne eigenes
  Mockup.

### Sicherheit

- Keine neue Angriffsfläche: Der Fehler-Workflow ist eine Datei des Projekts und läuft mit denselben Rechten wie die
  anderen Läufe.
- `FAILED_ERROR` ist maskiert wie Log und Historie. Secrets des fehlgeschlagenen Laufs, die nicht im Text stehen, gehen
  nicht über; der Fehler-Workflow liest eigene Secrets über `${secret:…}`.
- Parameter des fehlgeschlagenen Laufs (z. B. Body eines HTTP-Triggers) werden **nicht** weitergegeben, nur die festen
  Werte aus Abschnitt 3.
- Die Warteschlange begrenzt Speicher und Läufe (Abschnitt 4).

## Konsequenzen

- Ein Projekt richtet Benachrichtigungen einmal ein, über jeden Kanal, den Workflows können (`mail.send`,
  `http.request`, Datei, Datenbank, Plugins).
- Fehler vor dem Start werden gemeldet, sofern die Datei bekannt ist. Ein Trigger mit kaputter Trigger-Datei lässt sich
  nicht zuordnen und steht nur im Server-Log.
- `runnel run` dauert bei einem Fehler um die Laufzeit des Fehler-Workflows länger; `--no-on-error` verhindert das.
- Die Historie bekommt den Auslöser `error`; Filter und Liste im Editor zeigen ihn.

Umsetzung: **S72a** Modell, Prüfung, Ausführung in Server, Dienst und CLI, Historie; **S72b** Felder im Editor,
Verweise in der Liste der Läufe, Beispiel `examples/hello/workflows/on-error.workflow.yaml`.
