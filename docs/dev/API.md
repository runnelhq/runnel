# Server-API (`runnel serve`)

Stand: S38c. Die API ist die einzige Schnittstelle des Editors zum Server und ebenso für Skripte nutzbar
([Konzept, Kap. 10](../KONZEPT.md#10-api-server-modus)); zugesagt sind für Skripte aber nur die
[öffentlichen Endpunkte](#endpunkte) (ADR 0029).

```bash
runnel serve examples/hello            # Projekt im Ordner oder darüber, Port 7300
runnel serve --port 0 .                 # freier Port; die Ausgabe nennt die Adresse
```

**Ohne Projekt (seit R10):** Findet `runnel serve` im Ordner und darüber keine `project.yaml`, startet der Server
trotzdem für den angegebenen Ordner (ohne Argument das aktuelle Verzeichnis; er muss existieren). Bis ein Projekt
angelegt ist, antworten nur `/version`, `/health`, `/project` und der Editor (Startseite); alle übrigen Endpunkte unter
`/api/v1` antworten `409` mit `{"error": "…", "code": "no_project"}`. Nach `POST /project` – oder sobald eine
`project.yaml` auf anderem Weg entsteht, z. B. mit `runnel init` im Terminal – bedient der Server das Projekt wie nach
einem Start, mit Run-Historie und Triggern, ohne Neustart. Der Windows-Dienst braucht weiterhin ein Projekt.

Außerhalb von `/api/v1` liefert der Server den eingebetteten Editor aus ([EDITOR.md](EDITOR.md)).

## Grundsätze

| Thema | Regel |
|---|---|
| Adresse | nur `127.0.0.1` (Netzwerkbetrieb des Editors mit Zugangsschlüssel und TLS folgt mit S40b; HTTP-Trigger haben seit S38a/S40a einen eigenen Listener, siehe unten) |
| Präfix | alle Endpunkte unter `/api/v1` |
| Format | JSON (UTF-8, eingerückt); Dateiinhalte als Rohdaten |
| Fehler | Statuscode und `{"error": "…"}` mit englischer, verständlicher Meldung; manche Fehler tragen zusätzlich einen festen `code` (seit R10: `no_project`, `project_exists`, `not_empty`, `invalid_name`) |
| Schutz | Anfragen müssen an `localhost`, `127.0.0.1` oder `[::1]` gerichtet sein (`Host`-Header, Schutz vor DNS-Rebinding); ein `Origin`-Header muss zum Server passen (Schutz vor Anfragen fremder Webseiten). Sonst `403` |
| Pfade | relativ zum Projektordner, Trenner `/`; versteckte Einträge (`.git`, `.env` …), `..`, `\`, `:` und unter Windows unzulässige oder reservierte Namen (`CON`, `NUL`, `COM1` …) werden mit `400` abgelehnt. Symbolische Links aus dem Projekt heraus sind nicht erreichbar |

## Endpunkte

**Öffentlich und intern** ([ADR 0029](../adr/0029-stabilitaet-versionen.md), Abschnitt 5): Die Spalte „API“ nennt für
jeden Endpunkt, ob er zugesagt ist. **Öffentliche** Endpunkte (`version`, `health`, `catalog`, `expressions` und die
Läufe unter `/runs`) ändern sich ab `v1.0.0` nur additiv (neue Schlüssel, neue Filter); Leser müssen unbekannte
Schlüssel ignorieren. **Interne** Endpunkte dienen dem eingebetteten Editor, der immer zur Version des Servers passt,
und dürfen sich in jeder Version ändern; Skripte sollten sie nicht nutzen. Ein interner Endpunkt wird öffentlich nur
durch einen Nachtrag zu ADR 0029. Die Prüfungen von Host und Origin gelten für beide gleich: „öffentlich“ heißt
zugesagt, nicht im Netz erreichbar.

| Methode | Pfad | API | Ergebnis |
|---|---|---|---|
| GET | `/version` | **öffentlich** | `{"version", "commit", "date", "goVersion", "os", "arch"}` wie `runnel version --json` |
| GET | `/health` | **öffentlich** | Zustand für Monitoring (S43a): `200` bei `ok` und `degraded`, `503` bei `fail`, siehe [Zustand](#zustand-health) |
| GET | `/project` | intern | Ob der Ordner des Servers ein Projekt hat (R10): `{"exists": true, "dir": "C:\\…\\hello", "name": "hello", "empty": false}`; ohne Projekt `{"exists": false, "dir", "defaultName": "my-flows", "empty": true}` – `defaultName` ist der Name, den `runnel init` vergäbe, `empty`, ob der Ordner außer `.git` nichts enthält |
| POST | `/project` | intern | Projekt im Ordner des Servers anlegen wie `runnel init` (R10): Body `{"name": "flows", "force": false}`, beides optional (ohne `name` der Vorschlag aus dem Ordner; `force` ergänzt einen nicht leeren Ordner, vorhandene Dateien bleiben). Einen Pfad nimmt der Endpunkt nicht an. `201` + `{"name", "dir", "created": ["project.yaml", …, "pipelines/"], "skipped": […]}`; danach bedient der Server das Projekt. `400` ungültiger Name (`invalid_name`) oder Body, `409` Projekt schon da (`project_exists`) oder Ordner nicht leer ohne `force` (`not_empty`), `413` Body über 4 KiB, `503` beim Beenden |
| GET | `/projects/current` | intern | `{"name": "hello", "dir": "C:\\…\\hello", "environments": ["dev", "prod"], "defaultEnvironment": "dev", "history": {"keepDays": 30, "maxRuns": 10000}}`; `history` ist die Aufbewahrung der Run-Historie mit den Standardwerten (seit S26e1); `onError` der Fehler-Workflow aus `settings.onError` (fehlt, wenn keiner gesetzt ist; seit S72b) |
| GET | `/projects/current/tree` | intern | Dateibaum: `{name, path, type: "dir"\|"file", kind, size, children}`; Ordner vor Dateien, alphabetisch; `kind`: `workflow`, `pipeline`, `project`, `environment`, `trigger` (seit E10), `connection`, `plugin` (`plugins/<name>/plugin.yaml`, seit S52b1), `binary` (Programme, Bibliotheken, Archive, Datenbanken, Office-Dateien, z. B. `.exe`, `.dll`, `.zip`, `.db`, `.xlsx`, `.pdf`; öffnet der Editor nicht), `document` (`*.md`), `image` (`*.png`, `*.jpg`, `*.jpeg`, `*.gif`, `*.webp`; beide seit E17; `*.svg` seit R8), `other`; versteckte Einträge und symbolische Links fehlen |
| GET | `/files/{path}` | intern | Inhalt der Datei, Header `ETag`; `415` für Binärdateien: `kind: binary` oder – außer bei Bildern – binärer Inhalt in den ersten 8 KiB (seit R8): Nullbytes oder Bytes, die weder gültiges UTF-8 noch Text in Windows-1252 sind (Steuerzeichen außer Tab, Zeilenumbruch und Seitenvorschub, in Windows-1252 nicht belegte Bytes); ein an der Grenze von 8 KiB abgeschnittenes UTF-8-Zeichen zählt nicht, UTF-16 mit BOM und leere Dateien sind Text. Am Status `415` erkennt der Editor Binärdateien und zeigt den Hinweis statt des Texts, `422` für Dateien über 10 MiB (sie werden nicht gelesen); Bilder (siehe `kind: image`) mit ihrem Typ (`image/png` …, `image/svg+xml`), damit Dokumente und die Bildansicht sie zeigen können. Dateien des Projekts sind Daten, keine Seiten des Editors: `X-Content-Type-Options: nosniff` und `Content-Security-Policy: default-src 'none'; …; sandbox` (seit E17), direkt im Browser geöffnet führt etwa eine SVG- oder HTML-Datei keine Skripte aus; der Editor zeigt SVG nur als `<img>` (R8), das ebenfalls keine Skripte ausführt |
| PUT | `/files/{path}` | intern | Datei speichern (Body = Inhalt, max. 10 MiB); Antwort `{path, etag}` und Header `ETag` |
| DELETE | `/files/{path}` | intern | Datei löschen, `204`; `project.yaml` nicht (`409`: ohne sie findet `runnel serve` das Projekt nicht mehr) |
| POST | `/folders` | intern | Ordner anlegen (E11): `{"path": "data/archive"}` → `201`; fehlende Elternordner entstehen mit, `409`, wenn es ihn schon gibt |
| DELETE | `/folders/{path}` | intern | leeren Ordner löschen (E11): `204`; `409`, wenn er Dateien enthält (auch versteckte), `400` für eine Datei |
| POST | `/rename` | intern | Datei oder Ordner umbenennen oder verschieben (E11, E12): `{"from": "pipelines/count.pipeline.yaml", "to": "shared/count.pipeline.yaml", "references": [{"file": "workflows/main.workflow.yaml", "line": 6}]}` → `{path, updated, notUpdated}`; `409`, wenn der neue Name vergeben ist (nur die Schreibweise zu ändern ist erlaubt), `400` für einen Ordner in sich selbst, `409` für `project.yaml`. `references` (aus `/references`, Datei und Zeile) werden auf den neuen Pfad geändert – nur der Pfad in dieser Zeile, die Datei wird gespeichert; `updated` nennt die geänderten Dateien (am neuen Ort, falls sie mit verschoben wurden), `notUpdated` Verweise mit Grund (z. B. Datei inzwischen geändert, keine UTF-8-Datei). Ohne `references` bleiben Verweise unverändert |
| GET | `/references/{path}` | intern | gespeicherte Stellen, die auf eine Datei oder auf Dateien in einem Ordner verweisen (E11 vor dem Löschen, E12 vor dem Umbenennen und Verschieben): `{"references": [{"file": "triggers/hourly.yaml", "what": "target", "field": "target", "target": "workflows/main.workflow.yaml", "value": "workflows/main.workflow.yaml", "line": 5}, {"file": "workflows/main.workflow.yaml", "what": "action call (core.run-workflow)", "field": "workflow", …}]}` – Trigger-Ziele und Aufrufe mit festem Pfad (`value` wie geschrieben, `target` aufgelöst); Pfade mit `${…}` oder `{{ … }}` werden nicht erkannt |
| GET | `/variables` | intern | Variablen des Projekts für die Liste „Variables“ (E18a): `{"environments": [...], "defaultEnvironment": "dev", "variables": [{"name", "project": {"value","file","line"} \| null, "environments": {"<env>": {"value","file","line"}}, "uses": [{"file","line","column","parameter"}]}], "truncated"}`. Gesucht wird in allen YAML-Dateien des Projekts (ohne versteckte Ordner, höchstens 5000 Dateien zu je 1 MiB); `${env:…}` und Secrets zählen nicht. `parameter` heißt: Die Datei deklariert einen gleichnamigen Parameter |
| GET | `/schemas/{kind}` | intern | JSON Schema einer Dateiart (`project`, `environment`, `trigger`, `connection`; S58) wie `runnel schema <kind>`: Felder mit Typ, Beschreibung, erlaubten Werten und Standardwerten, Namen wie in den YAML-Dateien; unbekannte Art: `404`. Bei `connection` zusätzlich die Connection-Typen als `enum` von `type` und unter `with` je Typ das Schema seiner Felder (`x-runnel-variants`: `{"<typ>": Schema}`) für Vervollständigung und Reference; bei `trigger` ebenso unter `with` die Felder je Trigger-Typ mit ihren Pflichtfeldern und Beschreibungen nur für diesen Typ (seit E20a) |
| POST | `/connections/{name}/test` | intern | Connection testen (S39c1, ADR 0022 Abschnitt 8): Body `{"environment": "prod"}` (leer: Standard-Environment); Antwort `{"ok", "message", "environment", "durationMs"}`, auch bei Fehlschlag `200` mit `ok: false`; Meldungen ohne Secrets. `404` unbekannte oder fehlerhafte Connection, `400` unbekanntes Environment, `409` Test läuft schon; höchstens 15 s |
| POST | `/forms/edit` | intern | Connection-, Trigger- oder Environment-Datei bzw. `project.yaml` für das Formular lesen und ein Feld setzen (S39c2a; Trigger seit E20a, Environments und `project.yaml` seit E20b; vorher `/connections/edit`): `{"path", "content", "set": {"field", "value", "map"}}`. Felder: Connections `description`, `type`, `with.<name>`; Trigger zusätzlich `target`, `environment`, `enabled`, `params.<NAME>`; Environments `description`, `variables.<NAME>`, `secretStore`, `secrets.<NAME>` (der Name ist der Dateiname und bleibt); `project.yaml` zusätzlich `name`, `defaultEnvironment`, `settings.<name>` (seit R5b auch `settings.defaultJoin`, vorher `workflow.defaultJoin`). `value: null` entfernt das Feld (nicht bei `type` und `target`), `map` setzt eine Zuordnung (z. B. `with.headers`, `with.auth`). Antwort `{"content", "connection": {"name", "type", "description", "with"}}` bzw. `{"content", "trigger": {"name", "type", "description", "target", "environment", "enabled", "params", "with", "schedule": {"text", "next": [RFC 3339 …]}}, "removed": ["schedule", …]}` bzw. `{"content", "environment"|"project": {"name", "description", "defaultEnvironment", "variables", "secretStore", "secrets": ["name", …], "settings"}}` – `secrets` nur mit Namen, nie mit Werten; `schedule` bei `cron` mit gültigem Zeitplan (in Worten, leer wenn nicht eindeutig sagbar, und die nächsten drei Zeitpunkte in der Zeitzone des Triggers), `removed` die Felder unter `with:`, die ein neuer Typ entfernt hat; ohne `connection`/`trigger`, wenn der Text nicht lesbar ist. Nur die betroffenen Zeilen ändern sich; gespeichert wird nicht. `400` bei anderen Dateien oder Feldern |
| GET | `/secrets` | intern | Secrets des Projekts für die Anzeige an Secret-Feldern (S39c1): `{"environments": [...], "secrets": [{"name", "store", "uses": [{"file","line","column"}], "environments": {"<env>": {"store", "state"}}}], "truncated"}`; `state` ist `set`, `missing` oder `unavailable` (Quelle nicht lesbar, z. B. unbekannt oder beschädigt); `local` ist seit S39d1 der Store des Benutzers, unter dem der Server läuft, beim Windows-Dienst seit S39d2 der Store des Dienstes (`runnel secret set --service`); ohne Environments unter `""`. Nie Werte |
| GET | `/connections` | intern | Connections des Projekts für die Auswahl von `connection:` im Panel: `{"connections": [{"name", "type", "file"}]}`, nach Namen sortiert; fehlerhafte Dateien fehlen |
| GET | `/catalog` | **öffentlich** | `{"actions": [Descriptor …], "transforms": [Descriptor …]}` ([ADR 0014](../adr/0014-action-descriptor-schema.md)); `kind` ist `action` bzw. `transform`; Typen von Plugins mit `plugin` (Name) und ohne passende Kategorie mit leerer `category`; seit E15b mit `outputs`, `records`, `errors` und `limits`, wo beschrieben; abgekündigte Felder im Schema mit `deprecated: true` und `x-runnel-replacement` (seit R4, ADR 0029). seit E15g mit `connectionTypes` (Connection-Typen mit `type`, `label`, `description`, `schema`, `example`, `errors`, `test`, auch die der freigegebenen Plugins mit `plugin` und ohne `example` und `errors`; für die Hilfe des Editors). `runnel catalog --json` gibt dieselben Schlüssel für die eingebauten Typen aus, dazu `version` (E15a; `connectionTypes` seit E15e mit `example`, `errors` und `test`) |
| GET | `/expressions` | **öffentlich** | Referenz für den Ausdruckseditor (S35): `{"roots": [Doc …], "functions": [Doc …], "variables": [{name, value, source} …]}`; `Doc` = `{name, signature?, type, description, example?, group?, fields?}` aus `internal/expr/docs.go`; `variables` aus `project.yaml` und dem Standard-Environment (Wert wie in der Datei, `source` z. B. `environments/dev.yaml`). Ist das Projekt fehlerhaft, fehlen nur die Variablen. `runnel catalog --expressions --json` gibt `roots` und `functions` ohne Server aus, mit `version` statt `variables` (E15a) |
| POST | `/validate` | intern | Workflow oder Pipeline prüfen, siehe unten |
| POST | `/workflows/edit` | intern | Änderung vom Canvas auf den Text anwenden (nicht speichern), siehe unten |
| POST | `/pipelines/edit` | intern | dasselbe für Pipelines (S25a), siehe unten |
| POST | `/copy` | intern | Actions oder Transforms als YAML-Text für die Zwischenablage kopieren (E16), siehe unten |
| POST | `/pipelines/fields` | intern | Felder eines Eingabe-Transforms ermitteln (S46), siehe unten |
| POST | `/preview` | intern | Datenvorschau einer Pipeline bis zu einem Transform (S44), siehe unten |
| POST | `/runs` | **öffentlich** | Lauf starten, siehe unten |
| GET | `/runs` | **öffentlich** | `{"runs": [Run …]}`, neueste zuerst: laufende und kürzlich beendete aus dem Speicher, dazu die aus der Run-Historie (seit S26a). Filter (S26b): `status`, `path` (Datei im Projekt), `trigger` (z. B. `cron:nightly`, `cli` für `runnel run`, `error` für Fehler-Workflows, `editor` für Läufe ohne Trigger – aus dem Editor oder mit `POST /runs`, seit S26e1), `parent` (Lauf-ID: die Läufe, die zu diesem gehören, z. B. der Fehler-Lauf eines fehlgeschlagenen Laufs; seit S72b), `before` (Lauf-ID: ältere Läufe, nächste Seite), `limit` (Standard 200, höchstens 1.000; sonst `400`) |
| GET | `/runs/{id}` | **öffentlich** | Lauf mit `actions` (Status je Knoten) und `log`, auch aus der Run-Historie; `500`, wenn sie nicht lesbar ist (Details im Server-Log) |
| POST | `/runs/{id}/cancel` | **öffentlich** | Lauf abbrechen: `202`; bereits beendet `409` |
| GET | `/events` | intern | Live-Ereignisse (Server-Sent Events), siehe unten |
| GET | `/plugins` | intern | Plugins des Projekts (S52a), siehe [Plugins](#plugins) |
| POST | `/plugins/{name}/trust` | intern | Plugin für das Konto des Servers freigeben, siehe unten |
| POST | `/plugins/{name}/untrust` | intern | Freigabe zurücknehmen, siehe unten |
| POST | `/plugins/{name}/reload` | intern | Plugin erneut prüfen und starten (`describe`), siehe unten |
| GET | `/triggers` | intern | Trigger des Projekts (S37, S38): `{"triggers": [{name, file, type, target, schedule, timezone, enabled, next, last} …], "problems": [Problem …]}`. `schedule` ist der Cron-Ausdruck, `every 15m` (`interval`), `new *.csv in inbox` (`file`) oder `POST /hooks/import` (`http`, seit S38a); `http` ist die Adresse des Listeners der HTTP-Trigger, sobald er offen ist; `timezone` nur bei `cron`. `next` ist der nächste Zeitpunkt, bei `file` die nächste Abfrage des Ordners (fehlt bei deaktivierten Triggern), `last` das Ergebnis des letzten fälligen Zeitpunkts seit dem Start des Servers (`{time, runId}`, `{time, skipped: true}` bei `overlap: skip` oder `{time, error}`, bei `file` zusätzlich `files` mit den absoluten Pfaden; ein nicht lesbarer Ordner steht als `error`); `problems` sind Fehler in Trigger-Dateien (Format wie `/validate`), solche Trigger sind nicht aktiv |

## Zustand (`/health`)

Für Monitoring-Werkzeuge auf demselben Rechner (PRTG, Zabbix, Checkmk) und `runnel health` (S43a). Es gelten dieselben
Regeln wie für die übrige API: nur `127.0.0.1`, `Host` muss lokal sein, ein `Origin` muss passen. Ein Token gibt es
nicht – die API hat keins –, deshalb genügt ein einfaches `curl -s http://127.0.0.1:7300/api/v1/health` (curl sendet
`Host: 127.0.0.1:7300` und kein `Origin`). Über den Listener der HTTP-Trigger ist der Endpunkt nicht erreichbar.

Die Antwort entsteht aus dem Speicher und wenigen Aufrufen des Dateisystems (Größe der Historie, freier Platz), ohne
Abfragen an die Datenbank; `Cache-Control: no-store`. Statuscode `200` bei `ok` und `degraded`, `503` bei `fail`:

```json
{
  "status": "degraded",
  "version": "v0.2.0",
  "uptimeSeconds": 7384,
  "startedAt": "2026-11-26T08:00:00+01:00",
  "project": {"name": "hello", "dir": "D:\\flows\\hello"},
  "checks": [
    {"name": "scheduler", "status": "ok", "message": "running, 4 of 5 triggers active"},
    {"name": "triggers", "status": "degraded", "message": "1 trigger file with errors, not loaded: triggers/bad.yaml"},
    {"name": "runs", "status": "ok", "message": "0 running"},
    {"name": "history", "status": "ok", "message": "writable, 3 MB"},
    {"name": "disk", "status": "ok", "message": "51200 MB free of 102400 MB in C:\\ProgramData\\runnel\\history"},
    {"name": "listener", "status": "ok", "message": "http triggers listening on 127.0.0.1:7301"},
    {"name": "certificate", "status": "ok", "message": "valid until 2028-11-20"}
  ],
  "scheduler": {"running": true, "triggers": 5, "active": 4, "problems": [Problem …]},
  "runs": {"running": 0, "onErrorQueue": 0,
           "lastFailed": {"id": "01JD…", "time": "…", "path": "workflows/import.workflow.yaml", "trigger": "cron:nightly", "error": "…"}},
  "history": {"enabled": true, "path": "C:\\ProgramData\\runnel\\history\\hello-1a2b3c4d\\runs.db", "writable": true, "sizeBytes": 3145728},
  "disk": {"path": "C:\\ProgramData\\runnel\\history", "freeBytes": 53687091200, "totalBytes": 107374182400},
  "listener": {"configured": "127.0.0.1:7301", "running": true, "address": "127.0.0.1:7301", "tls": true, "notAfter": "2028-11-20T10:00:00Z"}
}
```

`status` ist der schlechteste Stand der `checks`; jede Prüfung nennt in `message` den Grund (englisch, für die Anzeige
im Monitoring). Die Prüfungen:

| Prüfung | `ok` | `degraded` | `fail` |
|---|---|---|---|
| `scheduler` | Scheduler der Trigger läuft | – | läuft nicht (z. B. beim Beenden) |
| `triggers` | alle Trigger-Dateien geladen | eine Trigger-Datei hat Fehler und ist nicht aktiv (Datei und Meldung in `scheduler.problems`, Format wie `/validate`) | – |
| `runs` | – | der letzte beendete Lauf eines aktiven Triggers ist fehlgeschlagen, oder sein letzter Start war nicht möglich | – |
| `history` | Run-Historie schreibbar, oder ausgeschaltet (Läufe nur im Speicher) | – | sie ließ sich nicht öffnen, oder der letzte Schreibvorgang schlug fehl |
| `disk` | genug Platz, oder unbekannt (`disk.error`) | im Datenordner ist weniger frei als 1 GiB bzw. 5 % des Laufwerks – der kleinere Wert gilt (kleine Laufwerke) | – |
| `listener` (nur mit `--triggers`) | offen, oder kein HTTP-Trigger aktiv | ein HTTP-Trigger ist aktiv, der Listener aber nicht offen (Port belegt, siehe Server-Log) | – |
| `certificate` (nur mit HTTPS) | gültig noch mindestens 14 Tage | läuft in weniger als 14 Tagen ab oder ist abgelaufen | – |

Weitere Angaben: `uptimeSeconds` seit `startedAt` (bis R5c `uptime`); `scheduler.triggers` geladene, `active` davon
eingeschaltete Trigger; `runs.running` laufende Läufe, `lastFailed` der letzte fehlgeschlagene Lauf seit dem Start
mit der Datei in `path` (bis R5c `file`; auch von Hand gestartete; sie ändern den Status nicht), `onErrorQueue` fehlgeschlagene Läufe, die auf ihren Fehler-Workflow warten
(ADR 0027); `history.sizeBytes` mit dem Write-Ahead-Log; `disk.path` der Datenordner (Ordner der Run-Historien, ohne
Historie der Projektordner); `listener.notAfter` das Ende des Zertifikats, bei eigenen Zertifikaten das zuletzt
geladene. Der Stand der Läufe gilt seit dem Start des Servers (aus dem Speicher, nicht aus der Historie); „schreibbar“
heißt: geöffnet, und der letzte Schreibvorgang gelang – ein Fehler zeigt sich also beim nächsten Lauf. Secrets und
Stacktraces enthält die Antwort nicht; Pfade schon, wie die übrige API.

**Ohne Projekt** (seit R10, `runnel serve` in einem Ordner ohne `project.yaml`) ist der Status `degraded` (`200`): Die
einzige Prüfung neben `disk` heißt `project` („no project in …; create one in the editor or with runnel init“),
`project.name` ist leer, `project.dir` der Ordner des Servers, `scheduler.running` ist `false`. Sobald das Projekt
angelegt ist, gelten die Prüfungen oben; `startedAt` bleibt der Start des Servers.

## Konfliktschutz (ETag)

Jede Datei hat ein `ETag` (Hash des Inhalts). Gespeichert wird nur, wenn sich die Datei seit dem Laden nicht geändert
hat – egal ob durch einen anderen Browser, einen Texteditor oder `git pull` ([ADR 0005](../adr/0005-mehrbenutzerbetrieb.md)).

| Anfrage | Datei existiert | Datei fehlt |
|---|---|---|
| `PUT` ohne `If-Match` | `428` – ETag erforderlich | `201` – angelegt (fehlende Ordner werden erzeugt) |
| `PUT` mit passendem `If-Match` | `200` – ersetzt | – |
| `PUT` mit anderem `If-Match` | `412` – „changed in the meantime“ | `412` – „deleted in the meantime“ |
| `DELETE` ohne `If-Match` | `428` | `428` |
| `DELETE` mit passendem / anderem `If-Match` | `204` / `412` | `412` |

`If-Match: *` überschreibt bewusst ohne Prüfung. Gespeichert wird atomar (temporäre Datei, dann Umbenennen).

## Prüfen (`/validate`)

```json
POST /api/v1/validate
{ "path": "workflows/import.workflow.yaml", "content": "kind: Workflow\n…" }
```

`content` ist optional; ohne ihn wird die gespeicherte Datei geprüft, mit ihm der ungespeicherte Stand des Editors.
Geprüft wird genau wie bei `runnel validate`: Felder, Links, Zyklen, Erreichbarkeit, Variablen gegen alle
Environments, Projektdateien und – wie vor einem Lauf – unbekannte Action-Typen und Konfiguration `with:`. Für
Pipeline-Dateien (`*.pipeline.yaml`, seit S25a1a): Felder, Links, Zyklen, `layout` und – wie vor einem Lauf – Typen,
Konfiguration und verwendete Felder der Transforms (ADR 0007); die Pfade lauten `$.transforms[i]…`. Seit E10 auch
`project.yaml` (Felder, Variablennamen, `defaultEnvironment` gegen die Dateien unter `environments/`),
Environments (`environments/*.yaml`, Name gleich Dateiname) und Trigger (`triggers/**/*.yaml`: Felder und Zeitplan, gegen
das Projekt Ziel, Environment und Parameter, Name gegen die anderen gespeicherten Trigger-Dateien); die Antwort hat
dann nur `problems`, `errors` und `warnings`. Andere Dateien: `400`.

```json
{
  "errors": 1,
  "warnings": 0,
  "problems": [
    {
      "file": "workflows/import.workflow.yaml",
      "line": 7, "column": 42,
      "path": "$.actions[2].with",
      "severity": "error",
      "message": "action \"greet\" (core.log): unknown field \"msg\""
    }
  ]
}
```

`path` (YAML-Pfad, sofern bekannt) ordnet die Meldung einem Knoten zu: `$.actions[2]…` ist die dritte Action,
`$.links[0]…` der erste Link. Auch Meldungen zu Variablen tragen den Pfad des Werts, z. B. `$.actions[3].with.message`.

Lässt sich die Datei als Workflow lesen (keine Syntaxfehler, falschen Typen oder unbekannten Felder), enthält die
Antwort zusätzlich `workflow` – seit E7a auch bei ungültigen Feldwerten wie `timeout: 5 mins`, damit der Editor die
Meldung am Feld zeigen kann: das
geparste Modell mit `name`, `actions` (`id`, `type`, `join`, `with` …), `links` (`from`, `to`, `when`, `loop`,
`disabled`) und `layout` (Koordinaten je Action-ID in Pixeln, linke obere Ecke). Der Canvas des Editors baut darauf
auf; der Editor braucht so keinen eigenen YAML-Parser. Für Pipelines heißt das Feld `pipeline`: `name`, `transforms`
(`id`, `type`, `with`, `copies`, `distribution`), `links` (`from`, `to`, `type`), `settings` und `layout` (je Transform-ID).

Seit S25a2 enthält die Antwort für Pipelines außerdem `schemas`: die abgeleiteten Felder (ADR 0007) je Transform-ID –
`input` (Felder der eingehenden Datensätze, zusammengeführt; fehlt bei Transforms ohne eingehende Links), `output`
(Datensätze auf den Datenlinks) und `errors` (Datensätze auf den Fehlerlinks, mit `_error`, `_errorField`,
`_errorTransform`); seit S23b1 bei Transforms mit Info-Link `info` (Datensätze des Info-Links, nicht in `input`
enthalten). In `counts` eines Laufs zählt `info` die Datensätze über den Info-Link. Ein Schema ist `{ "fields": [{ "name": "kdnr", "type": "int" }], "open": true }`; ohne `type` ist der
Typ unbekannt, `open` heißt, dass weitere Felder erst beim Lauf bekannt sind. Ein Transform, der wegen eines Fehlers
nicht angelegt werden kann, gilt als unbekannt (offenes Schema). `schemas` fehlt, solange Felder oder Graph der
Pipeline Fehler haben (z. B. ein Link auf einen unbekannten Transform).

```json
"schemas": {
  "read":  { "output": { "fields": [], "open": true }, "errors": { "fields": [], "open": true } },
  "types": { "input": { "fields": [], "open": true }, "output": { "fields": [], "open": true },
             "errors": { "fields": [{ "name": "_error", "type": "string" }, …], "open": true } }
}
```

## Läufe

```json
POST /api/v1/runs
{ "path": "workflows/hello.workflow.yaml", "environment": "prod", "params": { "GREETING": "Moin" } }
```

`environment` und `params` sind optional (ohne `environment` gilt das Standard-Environment des Projekts). Statt
`path` startet `{"trigger": "triggers/nightly.yaml"}` das Ziel einer **gespeicherten** Trigger-Datei mit ihrem
Environment und ihren `params` („Run target now“, E20a; `trigger` des Laufs wie beim Trigger selbst, z. B.
`cron:nightly`); `path`, `environment` und `params` sind dann nicht erlaubt (`400`), ein Trigger mit Fehlern gibt `422`
mit den Meldungen. Gestartet wird
die **gespeicherte** Datei; vorher wird sie wie bei `runnel run` geprüft. Seit S25a3 lassen sich auch Pipelines
(`*.pipeline.yaml`) starten.

| Antwort | Bedeutung |
|---|---|
| `201` + `Run`, Header `Location` | Lauf gestartet; er läuft im Hintergrund weiter |
| `422` + `{error, errors, warnings, problems}` | Fehler in Workflow oder Projektdateien (Format wie `/validate`) |
| `400` | unbekanntes Environment, Parameter fehlen oder passen nicht zum Typ |
| `429` | Limit gleichzeitiger Läufe erreicht (`settings.maxConcurrentRuns` des Workflows oder in `project.yaml`, seit S37); später erneut versuchen |
| `404` | Datei nicht gefunden |
| `503` | Server wird beendet |

```json
{
  "id": "01M7T2X0AHHW9G9ZAV04RRCXJC",
  "path": "workflows/parallel.workflow.yaml",
  "kind": "workflow",
  "name": "parallel",
  "trigger": "cron:nightly",
  "client": "127.0.0.1",
  "environment": "dev",
  "status": "running",
  "startedAt": "2026-11-19T14:20:50.898Z",
  "finishedAt": "2026-11-19T14:20:52.899Z",
  "durationMs": 2001,
  "error": "…",
  "outputs": { "MESSAGE": "Hello" }
}
```

`trigger` nennt, was den Lauf gestartet hat (`cron:<name>`, `interval:<name>`, `file:<name>`, `http:<name>`, `cli` für
`runnel run`, `error` für einen Fehler-Workflow; fehlt bei Starts von Hand), `client` bei `http` die Adresse des
Aufrufers. `parent` (seit S72a) nennt beim Fehler-Workflow ([ADR 0027](../adr/0027-fehler-workflow.md)) den
fehlgeschlagenen Lauf, den er meldet; leer, wenn dieser vor dem Start scheiterte. Läufe mit `POST /runs` (auch mit
`trigger`) starten keinen Fehler-Workflow.

`status` eines Laufs: `running`, `succeeded`, `failed`, `cancelled`. `GET /runs/{id}` ergänzt `actions` – je Action
`id`, `type`, `status` (`pending`, `waiting`, `running`, `succeeded`, `failed`, `skipped`, `cancelled`), `startedAt`, `finishedAt`, `durationMs`,
`error` und `runs` (Anzahl Ausführungen bei Schleifen) – und `log` (`time`, `level`, `actionId`, `message`; höchstens
5.000 Zeilen, ältere entfallen mit `logTruncated: true`).

`name` ist der Name des Workflows, bei einem Pipeline-Lauf (`kind` `pipeline`) der Name der Pipeline. Bis R5c hießen
die Felder `workflow`, `started` und `finished` (auch je Action). Bei einem Pipeline-Lauf erscheinen die Transforms als
`actions` (Ereignisse `action.started`, `action.finished`), ein beendeter Transform zusätzlich mit `counts`
(`read`, `written`, `errors`). `linkRows` zählt die Datensätze je Link (Index in `links`); ein Link ist gezählt, sobald
der Transform davor beendet ist. Das Ereignis `action.finished` eines Transforms trägt `counts` und `linkRows` seiner
ausgehenden Links (`{ "1": 2, "2": 2 }`).

Läufe liegen **nur im Speicher**: die letzten 100, dazu die letzten 100 von HTTP-Triggern (seit S38b2; Läufe mit
`history: errors` nur, wenn sie gescheitert sind, mit `history: none` gar nicht), laufende werden nie verworfen. Beim Beenden des Servers (Strg+C)
werden laufende Läufe abgebrochen. Die dauerhafte Historie folgt mit S26 (zurückgestellt, siehe Backlog der Roadmap), ohne dass sich diese API ändert.

## HTTP-Trigger (eigener Listener)

HTTP-Trigger (seit S38a) antworten **nicht** unter `/api/v1`, sondern auf ihrem eigenen Listener (`--triggers`, Standard
`127.0.0.1:7301`; im Netz per HTTPS, seit S40a) unter den Pfaden ihrer Trigger-Dateien; Format und Regeln in KONZEPT 5.3.

| Antwort | Bedeutung |
|---|---|
| `202` + `{"runId"}`, Header `Location` | `response: accepted`: Lauf gestartet; `Location` ist der Pfad des Laufs in dieser API (bis R5a stand er auch in `status`) |
| Status, Header und Body von `http.response` | `response: run` (seit S38b1): die erste Antwort des Laufs |
| `200` + `outputs` / `500` + `{"error", "runId"}` | `response: run` ohne `http.response`: Lauf erfolgreich bzw. gescheitert |
| `504` + `{"error", "runId"}` | `response: run`: keine Antwort innerhalb von `timeout`; der Lauf läuft weiter |
| `400` | Pflichtparameter fehlt, Wert passt nicht zum Typ, Body ist kein JSON-Objekt (bei Pipelines auch: keine Liste von Objekten) |
| `401` | Token fehlt oder falsch (`Authorization: Bearer <token>`) |
| `403` | Anfrage aus einer Webseite (`Origin`) oder – bei einem lokalen Listener – an einen fremden Host-Namen |
| `404` / `405` | kein Trigger für den Pfad bzw. deaktiviert / andere Methode (Header `Allow`) |
| `413` / `415` | Body über 1 MiB / weder JSON noch Formular |
| `429` | `maxConcurrentRuns` des Triggers (Standard 4, seit R5a; bis R5b `maxConcurrent`) oder `settings.maxConcurrentRuns` des Projekts erreicht |
| `500` / `503` | Ziel kann nicht gestartet werden (Meldung im Server-Log) / Server wird beendet |

Bei `response: run` trägt jede Antwort den Header `Runnel-Run-Id`.

## Live-Ereignisse (`/events`)

Server-Sent Events ([ADR 0016](../adr/0016-live-events-sse.md)); im Browser mit `EventSource`, auf der Konsole mit
`curl -N http://127.0.0.1:7300/api/v1/events`.

```
id: 7
event: log
data: {"seq":7,"kind":"log","time":"…","runId":"01M7…","actionId":"fetch-a","actionType":"core.wait","level":"debug","message":"waiting 2s"}
```

| Ereignis | Zusätzliche Felder |
|---|---|
| `run.started` | `message` (Workflow-Name) |
| `action.started` | `actionId`, `actionType` |
| `log` | `actionId`, `level` (`debug`, `info`, `warn`, `error`), `message` |
| `action.finished` | `status`, `durationMs`, `error` |
| `action.skipped` | `status: skipped` |
| `action.waiting` | `status: waiting`, `message` (z. B. „waiting for 1 more incoming link (join: all)“) – eine Action mit `join: all` wartet nach einem eingehenden Link auf weitere (seit E8b) |
| `run.finished` | `status`, `durationMs`, `error`, `outputs` |
| `reset` | – verpasste Ereignisse sind nicht mehr vorrätig; Zustand über `GET /runs` neu laden |

- Ohne Angabe liefert der Strom nur **neue** Ereignisse. Mit `Last-Event-ID` (setzt `EventSource` beim Neuverbinden
  selbst) oder `?since=<seq>` folgen zuerst die verpassten; vorrätig sind die letzten 2.000.
- `?run=<id>` liefert nur die Ereignisse eines Laufs.
- Alle 15 s kommt ein Kommentar (`: ping`), damit Verbindungen offen bleiben. Beim Beenden des Servers endet der Strom.

## Bearbeiten (`/workflows/edit`)

Der Editor schickt den angezeigten Text und eine Änderung; der Server gibt den neuen Text zurück. Gespeichert wird erst
mit `PUT /files/{path}` (ETag, siehe oben).

```json
POST /api/v1/workflows/edit
{ "path": "workflows/parallel.workflow.yaml", "content": "kind: Workflow\n…",
  "move": { "fetch-b": { "x": 360, "y": 320 } }, "delete": ["combine"] }

{ "path": "…", "content": "…",
  "add": [{ "id": "log-1", "type": "core.log" }], "move": { "log-1": { "x": 760, "y": 400 } },
  "addLinks": [{ "from": "report-error", "to": "log-1", "when": "failure" }] }

→ 200 { "content": "kind: Workflow\n…" }
```

| Feld | Wirkung |
|---|---|
| `move` | setzt die Position (Pixel, linke obere Ecke) im `layout`-Block; fehlende Einträge werden angehängt, ein fehlender Block angelegt |
| `delete` | entfernt die Actions, alle Links von und zu ihnen und ihre `layout`-Einträge |
| `add` | hängt Actions (`id`, `type`, ohne `with:`) an die Liste `actions` an; die Position kommt über `move` |
| `rename` | ändert IDs (`{"alt": "neu"}`) in der Action, in `from`/`to` aller Links und im `layout`; nur allein, nicht zusammen mit anderen Änderungen |
| `addLinks` | hängt Links an (`from`, `to`, optional `when`: `success`, `failure`, `always`, `loop`, `disabled`), im Flow-Stil `- { from: a, to: b }` |
| `deleteLinks` | entfernt Links über ihre Nummer in der Liste (ab 0, wie `$.links[i]`) |
| `updateLinks` | ändert Links über ihre Nummer: `when` (`success` entfernt `when:`; auch ein Ausdruck `{{ … }}` in einer Zeile, er wird in Anführungszeichen geschrieben), `loop`, `disabled` – nur die angegebenen Felder, z. B. `[{"index": 2, "when": "failure"}]` oder `[{"index": 0, "disabled": true}]` |
| `setWorkflow` | ändert ein Feld des Workflows selbst (`[{"field": "description", "value": "…"}]`): `name`, `description`, `settings.timeout`, `settings.maxConcurrentRuns`; `null` entfernt es. Eine fehlende Description kommt hinter `name:`, fehlende Settings vor `actions:` |
| `set` | ändert ein Feld einer Action (`[{"id": "combine", "field": "with.message", "value": "Done"}]`); erlaubt sind `description`, `timeout`, `continueOnError`, `join`, `joinTimeout`, `retry.attempts`, `retry.backoff`, `retry.strategy`, `forEach`, `parallel`, `continueOnEntryError` (seit E7c als `stopOnError`, seit R5a mit umgekehrter Bedeutung) und `with.<name>`. Werte: Text, Zahl oder `true`/`false`; `null` entfernt das Feld (mit dem letzten Eintrag auch `with:` bzw. `retry:`). Mehrzeilige Texte werden als `\|`-Block geschrieben, Texte, die YAML anders lesen würde (`true`, `1`, `a: b`), in Anführungszeichen; ein Kommentar am Zeilenende bleibt erhalten. Höchstens ein Feld je Action und Anfrage. Statt `value` setzt `map` ein Feld der Konfiguration auf eine Zuordnung von Texten (`{"id": "call", "field": "with.params", "value": null, "map": [{"key": "FILE", "value": "{{ row }}"}]}`); Schlüssel, die schon in der Datei stehen, behalten ihre Reihenfolge |
| `entries` | ändert einen Parameter oder Output (E7c), höchstens einen je Anfrage, adressiert über den Index: `{"list": "parameters", "op": "set", "index": 0, "field": "default", "value": "5s"}`. `op`: `add` (mit `name`, am Ende; ein Output bekommt `value: ""`), `set`, `delete` (der letzte Eintrag entfernt die ganze Liste), `move` (mit `by`: −1 oder 1). Felder: `name`, `description`, `type`, `default`, `required` (`true`/`false`), `enum` (Liste von Texten) bei Parametern, `name`, `description`, `type`, `value` bei Outputs; `null` oder ein leerer Text entfernen das Feld. Ein Eintrag im Block-Stil wird zeilengenau geändert, ein einzeiliger (`- { name: … }`) neu geschrieben, mit seinem Kommentar. Fehlende Listen entstehen an ihrer Stelle im Format (vor `outputs`, `settings`, `actions` …) |

Fehlende Listen (`actions:`, `links:`, `layout:`) werden angelegt. Listen im Flow-Stil (`actions: [ … ]`) mit Einträgen
lassen sich nicht ergänzen; die Fehlermeldung sagt, was im Text zu ändern ist. Geänderte Links schreibt der
Server neu (einzeilige Links bleiben einzeilig, mit ihrem Kommentar).

Geändert werden **nur die betroffenen Zeilen** (Positionen aus dem Syntaxbaum); Kommentare, Reihenfolge, Anführungszeichen,
Flow- oder Block-Stil und Zeilenenden (LF/CRLF) aller anderen Zeilen bleiben erhalten – ein gespeicherter Stand ergibt
einen kleinen Git-Diff. Antworten: `400` für unbekannte oder ungültige Action-IDs, unbekannte Links und nicht
unterstützte Bedingungen, `422` (Format wie `/validate`), wenn die Datei nicht als Workflow lesbar ist. Ungültige
Feldwerte verhindern das Bearbeiten nicht – sie lassen sich so im Panel korrigieren.

### Pipelines (`/pipelines/edit`)

Wie `/workflows/edit`, für `*.pipeline.yaml` (S25a1a); dieselbe Zeilen-genaue Bearbeitung.

```json
POST /api/v1/pipelines/edit
{ "path": "pipelines/customers.pipeline.yaml", "content": "kind: Pipeline\n…",
  "add": [{ "id": "rejected", "type": "csv.output" }], "move": { "rejected": { "x": 256, "y": 380 } },
  "addLinks": [{ "from": "types", "to": "rejected", "type": "error" }] }
```

| Feld | Unterschied zu `/workflows/edit` |
|---|---|
| `move`, `delete`, `add`, `rename`, `deleteLinks` | wie dort, für Transforms (`transforms:`); `rename` ändert zusätzlich die Felder anderer Transforms, die den Transform nennen (Format `runnel-target` im Schema des Typs, z. B. `to` und `default` von `core.switch`, `else` von `core.filter`, auch bei Plugins) |
| `addLinks` | `from`, `to`, optional `type: error` (`data` ist der Standard und wird nicht geschrieben) |
| `updateLinks` | ändert den Typ: `[{"index": 1, "type": "error"}]`; `data` entfernt `type:` |
| `set` | `copies`, `distribution` und `with.<name>` eines Transforms; zusätzlich setzt `list` ein Feld der Konfiguration auf eine Liste von Zuordnungen, je eine Zeile im Flow-Stil (S46): `{"id": "read", "field": "with.fields", "value": null, "list": [[{"key": "name", "value": "kdnr"}, {"key": "type", "value": "int"}]]}` → `- { name: kdnr, type: int }`; Texte mit `,` `[` `]` `{` `}` in Anführungszeichen; Werte in `list` dürfen auch Zahlen und `true`/`false` sein (E14a, z. B. `- { name: total, descending: true }`). `texts` setzt eine Liste von Texten, geschrieben als `[a, b]` (E14a, z. B. `groupBy`). Eine vorhandene Liste wird ersetzt, ohne `list`, `texts` und `value` entfernt |
| `setPipeline` | `name`, `description`, `settings.batchSize`, `settings.bufferSize`, `settings.strict` |
| `entries` | nur `parameters` (Pipelines haben keine Outputs) |


### Kopieren und Einfügen (`/copy`, `paste`)

Kopieren und Einfügen auf dem Canvas (E16, Mockup `design/mockups/e16-copy-paste.html`). `/copy` gibt die gewählten
Actions bzw. Transforms aus dem angezeigten Text zurück – so, wie sie in der Datei stehen, mit den Links zwischen
ihnen und ihren Positionen. Der Editor legt den Text in die Zwischenablage; er funktioniert damit über Dateien, Tabs
und Fenster hinweg und lässt sich auch in die YAML-Ansicht oder eine Mail einfügen.

```json
POST /api/v1/copy
{ "path": "pipelines/customers.pipeline.yaml", "content": "kind: Pipeline\n…", "ids": ["types", "active"] }

→ 200 { "text": "# Runnel: 2 transforms and 1 link from pipelines/customers.pipeline.yaml\ntransforms:\n  - id: types\n…",
        "steps": 2, "links": 1, "omitted": 2 }
```

`omitted` zählt die Links zu Schritten, die nicht kopiert wurden; sie fehlen im Text. Kommentare vor einem Eintrag und
Leerzeilen danach gehören nicht dazu. Eingefügt wird mit `paste` an `/workflows/edit` bzw. `/pipelines/edit`, allein
(nicht zusammen mit anderen Änderungen):

```json
POST /api/v1/pipelines/edit
{ "path": "pipelines/orders.pipeline.yaml", "content": "…",
  "paste": { "text": "# Runnel: 2 transforms …\ntransforms:\n…", "at": { "x": 256, "y": 328 } } }

→ 200 { "content": "…", "pasted": { "ids": ["types-2", "active"], "links": 1,
        "renamed": { "types": "types-2" }, "references": 0 } }
```

- Die Schritte werden **wie geschrieben** angehängt (Kommentare, Stil, mehrzeilige Texte bleiben), die Links im
  Flow-Stil. Links zu Schritten, die nicht im Text stehen, entfallen.
- **IDs:** Freie IDs bleiben; belegte bekommen `-2`, `-3` … (`types-2` wird zu `types-3`). Verweise unter den
  eingefügten Schritten folgen: in Workflows `results.<id>` und `results["<id>"]` in Ausdrücken `{{ … }}` (auch in
  `when` der Links; eine ID mit `-` wird mit Klammern geschrieben, in den Anführungszeichen, die den YAML-Wert der Zeile
  nicht beenden), in Pipelines die Ziel-Felder der Typen (Format `runnel-target`: `to` und `else` von `core.switch`,
  `else` von `core.filter`, Ziele von Plugins). `references` zählt die Änderungen.
- **Position:** `at` ist die linke obere Ecke der eingefügten Schritte (der Editor schickt die Mausposition); ohne
  `at` bleiben die Positionen, bei umbenannten IDs um 40 px versetzt.
- **Ablehnungen (`400`):** Actions in einer Pipeline bzw. Transforms in einem Workflow („Actions can't be pasted into a
  pipeline“), Text ohne Schritte („the clipboard holds no actions or transforms“), ein Eintrag ohne `id`, ein nicht
  unterstützter Link, mehr als 1 MiB Text. `/copy` lehnt unbekannte IDs, eine leere Auswahl und andere Dateien ab.

## Felder ermitteln (`/pipelines/fields`)

„Detect fields“ im Editor (S46, [ADR 0007](../adr/0007-typsystem-pipelines.md)): Ein Eingabe-Transform liest die ersten
Zeilen seiner Eingabe (höchstens 1.000) und schlägt Felder mit Typ und Format vor. Gelesen wird mit der Konfiguration
aus `content` (dem Text im Editor, gespeichert oder nicht); `${…}` werden mit den Standardwerten der Parameter, den
Variablen des Projekts und des Standard-Environments aufgelöst (wie ein Lauf ohne Werte). Welche Typen das können,
meldet `/catalog` mit `"detectFields": true` (zuerst `csv.input`). Der Server schreibt nichts; geschrieben wird mit
`/pipelines/edit` (`set` mit `list`).

```json
POST /api/v1/pipelines/fields
{ "path": "pipelines/customers.pipeline.yaml", "content": "kind: Pipeline\n…", "transform": "read" }

→ 200 { "source": [{ "name": "File", "value": "data/customers.csv" },
                   { "name": "Delimiter", "value": ";" }, { "name": "Header", "value": "first row" },
                   { "name": "Encoding", "value": "UTF-8" }],
        "rows": 4, "complete": true, "environment": "dev", "variables": false,
        "fields": [
          { "name": "kdnr", "type": "int", "values": ["4711", "4712", "4713", "4714"], "selected": true },
          { "name": "umsatz", "type": "string", "values": ["12.500,00", "0,00", "980,50", "k. A."], "selected": true,
            "warning": true, "note": "3 of 4 values are decimal (#.##0,00); “k. A.” in line 5 is not. Kept as text." } ] }
```

| Feld | Bedeutung |
|---|---|
| `source` | was gelesen wurde (Datei, Trennzeichen, Kopfzeile, Kodierung), zur Anzeige in dieser Reihenfolge |
| `rows`, `complete` | gelesene Zeilen; `complete`, wenn das alle Zeilen der Eingabe sind |
| `environment`, `variables` | Environment, dessen Variablen galten; `variables`, wenn die Konfiguration `${…}` enthält |
| `fields[]` | `name`, `type`, `format` (falls nötig), `values` (die ersten 5 Werte als Text, `""` für leer), `note` und `warning` (Begründung, z. B. warum eine Spalte Text bleibt) |
| `fields[].state` | nur wenn die Konfiguration schon `fields:` hat: `listed` (Typ und Format aus der Datei), `new` (nur in der Eingabe), `missing` (nur in der Datei); die Reihenfolge ist die der aufgeführten Felder, neue folgen |
| `fields[].selected` | Vorschlag zum Schreiben: ohne `fields:` alle, sonst die aufgeführten (die Liste wählt bei `csv.input` auch die gelesenen Spalten) |

Ein Typ wird nur vorgeschlagen, wenn **jeder** gelesene Wert passt (`int`, `decimal` auch mit `#.##0,00` oder
`#,##0.00`, `date` und `datetime` ISO oder `dd.MM.yyyy`, `bool`); sonst `string` mit Begründung. Zahlen mit führender
Null bleiben Text. Antworten: `404` für einen unbekannten Transform, `400` für Typen ohne Erkennung, `422`, wenn die
Pipeline oder das Projekt nicht lesbar ist, oder – mit `{"error", "baseDir"}` – wenn die Eingabe nicht lesbar ist
(relative Pfade beziehen sich auf `baseDir`, den Projektordner, ADR 0020).

## Plugins

Für die Plugin-Verwaltung im Editor (S52a, Mockup [`s52-plugins.html`](../../design/mockups/s52-plugins.html)).
Dieselben Freigaben wie `runnel plugins trust` ([PLUGINS.md](PLUGINS.md#freigabe)).

```json
GET /api/v1/plugins
→ 200 { "plugins": [ {
          "name": "iban", "version": "1.2.0", "namespace": "iban", "description": "Checks IBAN and BIC",
          "folder": "plugins/iban", "program": "bin/iban.exe", "executable": { "windows": "bin/iban.exe", "linux": "bin/iban" },
          "files": 4, "bytes": 6502400, "hash": "8b0d5e…41d2",
          "state": "trusted",
          "user": { "version": "1.2.0", "hash": "8b0d5e…41d2", "trusted": "2026-11-20T09:12:00Z", "current": true },
          "types": { "actions": [ { "type": "iban.check", "label": "Check IBAN", "description": "…" } ],
                     "transforms": [ … ], "connectionTypes": [ … ] },
          "uses": [ { "file": "workflows/sync.workflow.yaml", "kind": "action", "id": "verify", "type": "iban.check" },
                    { "file": "connections/bank.yaml", "kind": "connection", "id": "bank", "type": "iban-registry" } ] } ],
        "serviceApprovals": true, "canTrust": true }
```

| Feld | Bedeutung |
|---|---|
| `state` | `trusted`, `not-trusted`, `changed` (seit der Freigabe geändert) oder `error` (nicht prüfbar oder startet nicht; Grund in `message`) |
| `hash` | SHA-256 des Plugin-Ordners jetzt – das, was eine Freigabe bestätigt |
| `user`, `service` | gespeicherte Freigabe des Kontos bzw. des Windows-Dienstes; `current: false`, wenn sich das Plugin seitdem geändert hat; fehlt ohne Freigabe |
| `types` | Typen aus `describe`, nur bei `trusted`. Runnel startet dafür freigegebene Plugins, deren Typen noch nicht zwischengespeichert sind; andere startet es nie |
| `uses` | Schritte und Connections mit Typen des Plugins, aus den Dateien des Projekts gelesen, ohne das Plugin zu starten |
| `serviceApprovals` | es gibt Freigaben für den Windows-Dienst (Windows) |
| `canTrust` | der Editor darf freigeben; `false`, wenn Runnel als Windows-Dienst läuft |

`POST /plugins/{name}/trust` mit `{"hash": "<hash aus der Liste>"}` gibt das Plugin für das Konto frei, unter dem
`runnel serve` läuft (nie für den Dienst), und antwortet mit dem Plugin wie in der Liste. Der Hash muss dem Stand jetzt
entsprechen: So gilt die Freigabe genau dem, was im Editor zu sehen war. Antworten: `400` ohne `hash`, `409`, wenn
sich das Plugin seitdem geändert hat, `404` unbekanntes oder fehlerhaftes Plugin, `403`, wenn Runnel als Windows-Dienst
läuft. `…/untrust` nimmt die Freigabe des Kontos zurück, `…/reload` verwirft den Zwischenspeicher und prüft und
startet das Plugin erneut (z. B. nach einem Fehler beim Start). Freigeben und Zurücknehmen stehen im Server-Log.

## Datenvorschau (`/preview`)

„Preview data“ im Editor (S44): Die Pipeline läuft bis zu einem Transform – nur er und die Transforms davor –, jede
Eingabe liest höchstens `limit` Datensätze (Standard 100, höchstens 1.000). Ausgaben (Kategorie `output`, z. B.
`csv.output`) laufen nicht, **es wird nichts geschrieben**; die Transforms nach dem gewählten nehmen nur die
Datensätze seiner Links auf. Gelesen wird `content` (der Text im Editor, gespeichert oder nicht); Parameter und
Environment wie bei `POST /runs`. Die Vorschau ist kein Lauf: Sie erscheint nicht unter `/runs` und nicht in
`/events`.

```json
POST /api/v1/preview
{ "path": "pipelines/customers.pipeline.yaml", "content": "kind: Pipeline\n…", "transform": "types",
  "limit": 100, "environment": "dev", "params": { "BATCH": "42" } }

→ 200 { "status": "succeeded", "durationMs": 6, "limit": 100,
        "counts": { "read": { "read": 0, "written": 4 }, "types": { "read": 4, "written": 3, "errors": 1 } },
        "links": { "0": { "rows": 4, "fields": [ … ], "records": [ … ] },
                   "1": { "rows": 1, "fields": [ { "name": "kdnr", "type": "string" }, …, { "name": "_error", "type": "string" } ],
                          "records": [ [ "4714", "Klein KG", "k. A.", "Essen", "", "field \"umsatz\": …", "umsatz", "types" ] ] } },
        "output": { "rows": 3, "fields": [ { "name": "kdnr", "type": "int" }, { "name": "umsatz", "type": "decimal" }, … ],
                    "records": [ [ 4711, 12500, … ] ] } }
```

| Feld | Bedeutung |
|---|---|
| `status`, `error`, `failedAt` | `succeeded`, `failed` (mit Meldung und dem Transform, an dem die Vorschau stehen blieb) oder `cancelled` (nach einer Minute abgebrochen) |
| `counts` | gelesene, weitergegebene und an Fehler-Links gegebene Datensätze des gewählten Transforms und der Transforms davor |
| `links` | je Link (Index in `links:`), der gelaufen ist: `rows` (Datensätze über den Link) und die ersten `limit` Datensätze als `fields` (Name, Typ des ersten Werts, der nicht null ist) und `records` (Werte in der Reihenfolge der Felder; Zahlen als JSON-Zahlen, Dezimalzahlen exakt, Datum ISO 8601, `null` für fehlende Werte) |
| `output` | dasselbe für die Ausgabe des gewählten Transforms (alle Datensätze, die er weitergibt) |

Antworten: `422` mit `problems` (Format wie `/validate`), wenn die Pipeline Fehler hat, `422` für einen unbekannten
Transform, `429`, wenn schon zwei Vorschauen laufen, `400` für andere Dateien.
