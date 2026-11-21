# Server-API (`runnel serve`)

Stand: S38c. Die API ist die einzige Schnittstelle des Editors zum Server und ebenso für Skripte nutzbar
([Konzept, Kap. 10](../KONZEPT.md#10-api-server-modus)).

```bash
runnel serve examples/hello            # Projekt im Ordner oder darüber, Port 7300
runnel serve --port 0 .                 # freier Port; die Ausgabe nennt die Adresse
```

Außerhalb von `/api/v1` liefert der Server den eingebetteten Editor aus ([EDITOR.md](EDITOR.md)).

## Grundsätze

| Thema | Regel |
|---|---|
| Adresse | nur `127.0.0.1` (Netzwerkbetrieb des Editors mit Zugangsschlüssel und TLS folgt mit S40b; HTTP-Trigger haben seit S38a/S40a einen eigenen Listener, siehe unten) |
| Präfix | alle Endpunkte unter `/api/v1` |
| Format | JSON (UTF-8, eingerückt); Dateiinhalte als Rohdaten |
| Fehler | Statuscode und `{"error": "…"}` mit englischer, verständlicher Meldung |
| Schutz | Anfragen müssen an `localhost`, `127.0.0.1` oder `[::1]` gerichtet sein (`Host`-Header, Schutz vor DNS-Rebinding); ein `Origin`-Header muss zum Server passen (Schutz vor Anfragen fremder Webseiten). Sonst `403` |
| Pfade | relativ zum Projektordner, Trenner `/`; versteckte Einträge (`.git`, `.env` …), `..`, `\`, `:` und unter Windows unzulässige oder reservierte Namen (`CON`, `NUL`, `COM1` …) werden mit `400` abgelehnt. Symbolische Links aus dem Projekt heraus sind nicht erreichbar |

## Endpunkte

| Methode | Pfad | Ergebnis |
|---|---|---|
| GET | `/version` | `{"version", "commit", "date", "goVersion", "os", "arch"}` wie `runnel version --json` |
| GET | `/projects/current` | `{"name": "hello", "dir": "C:\\…\\hello", "environments": ["dev", "prod"], "defaultEnvironment": "dev"}` |
| GET | `/projects/current/tree` | Dateibaum: `{name, path, type: "dir"\|"file", kind, size, children}`; Ordner vor Dateien, alphabetisch; `kind`: `workflow`, `pipeline`, `project`, `environment`, `trigger` (seit E10), `other`; versteckte Einträge und symbolische Links fehlen |
| GET | `/files/{path}` | Inhalt der Datei, Header `ETag` |
| PUT | `/files/{path}` | Datei speichern (Body = Inhalt, max. 10 MiB); Antwort `{path, etag}` und Header `ETag` |
| DELETE | `/files/{path}` | Datei löschen, `204`; `project.yaml` nicht (`409`: ohne sie findet `runnel serve` das Projekt nicht mehr) |
| POST | `/folders` | Ordner anlegen (E11): `{"path": "data/archive"}` → `201`; fehlende Elternordner entstehen mit, `409`, wenn es ihn schon gibt |
| DELETE | `/folders/{path}` | leeren Ordner löschen (E11): `204`; `409`, wenn er Dateien enthält (auch versteckte), `400` für eine Datei |
| POST | `/rename` | Datei oder Ordner umbenennen oder verschieben (E11, E12): `{"from": "pipelines/count.pipeline.yaml", "to": "shared/count.pipeline.yaml", "references": [{"file": "workflows/main.workflow.yaml", "line": 6}]}` → `{path, updated, notUpdated}`; `409`, wenn der neue Name vergeben ist (nur die Schreibweise zu ändern ist erlaubt), `400` für einen Ordner in sich selbst, `409` für `project.yaml`. `references` (aus `/references`, Datei und Zeile) werden auf den neuen Pfad geändert – nur der Pfad in dieser Zeile, die Datei wird gespeichert; `updated` nennt die geänderten Dateien (am neuen Ort, falls sie mit verschoben wurden), `notUpdated` Verweise mit Grund (z. B. Datei inzwischen geändert, keine UTF-8-Datei). Ohne `references` bleiben Verweise unverändert |
| GET | `/references/{path}` | gespeicherte Stellen, die auf eine Datei oder auf Dateien in einem Ordner verweisen (E11 vor dem Löschen, E12 vor dem Umbenennen und Verschieben): `{"references": [{"file": "triggers/hourly.yaml", "what": "target", "field": "target", "target": "workflows/main.workflow.yaml", "value": "workflows/main.workflow.yaml", "line": 5}, {"file": "workflows/main.workflow.yaml", "what": "action call (core.run-workflow)", "field": "workflow", …}]}` – Trigger-Ziele und Aufrufe mit festem Pfad (`value` wie geschrieben, `target` aufgelöst); Pfade mit `${…}` oder `{{ … }}` werden nicht erkannt |
| GET | `/catalog` | `{"actions": [Descriptor …], "transforms": [Descriptor …]}` wie `runnel catalog --json` ([ADR 0014](../adr/0014-action-descriptor-schema.md)); `kind` ist `action` bzw. `transform` |
| GET | `/expressions` | Referenz für den Ausdruckseditor (S35): `{"roots": [Doc …], "functions": [Doc …], "variables": [{name, value, source} …]}`; `Doc` = `{name, signature?, type, description, example?, group?, fields?}` aus `internal/expr/docs.go`; `variables` aus `project.yaml` und dem Standard-Environment (Wert wie in der Datei, `source` z. B. `environments/dev.yaml`). Ist das Projekt fehlerhaft, fehlen nur die Variablen |
| POST | `/validate` | Workflow oder Pipeline prüfen, siehe unten |
| POST | `/workflows/edit` | Änderung vom Canvas auf den Text anwenden (nicht speichern), siehe unten |
| POST | `/pipelines/edit` | dasselbe für Pipelines (S25a), siehe unten |
| POST | `/pipelines/fields` | Felder eines Eingabe-Transforms ermitteln (S46), siehe unten |
| POST | `/preview` | Datenvorschau einer Pipeline bis zu einem Transform (S44), siehe unten |
| POST | `/runs` | Lauf starten, siehe unten |
| GET | `/runs` | `{"runs": [Run …]}`, neueste zuerst |
| GET | `/runs/{id}` | Lauf mit `actions` (Status je Knoten) und `log` |
| POST | `/runs/{id}/cancel` | Lauf abbrechen: `202`; bereits beendet `409` |
| GET | `/events` | Live-Ereignisse (Server-Sent Events), siehe unten |
| GET | `/triggers` | Trigger des Projekts (S37, S38): `{"triggers": [{name, file, type, target, schedule, timezone, enabled, next, last} …], "problems": [Problem …]}`. `schedule` ist der Cron-Ausdruck, `every 15m` (`interval`), `new *.csv in inbox` (`file`) oder `POST /hooks/import` (`http`, seit S38a); `http` ist die Adresse des Listeners der HTTP-Trigger, sobald er offen ist; `timezone` nur bei `cron`. `next` ist der nächste Zeitpunkt, bei `file` die nächste Abfrage des Ordners (fehlt bei deaktivierten Triggern), `last` das Ergebnis des letzten fälligen Zeitpunkts seit dem Start des Servers (`{time, runId}`, `{time, skipped: true}` bei `overlap: skip` oder `{time, error}`, bei `file` zusätzlich `files` mit den absoluten Pfaden; ein nicht lesbarer Ordner steht als `error`); `problems` sind Fehler in Trigger-Dateien (Format wie `/validate`), solche Trigger sind nicht aktiv |

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
`_errorTransform`). Ein Schema ist `{ "fields": [{ "name": "kdnr", "type": "int" }], "open": true }`; ohne `type` ist der
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

`environment` und `params` sind optional (ohne `environment` gilt das Standard-Environment des Projekts). Gestartet wird
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
  "workflow": "parallel",
  "trigger": "cron:nightly",
  "client": "127.0.0.1",
  "environment": "dev",
  "status": "running",
  "started": "2026-11-19T14:20:50.898Z",
  "finished": "2026-11-19T14:20:52.899Z",
  "durationMs": 2001,
  "error": "…",
  "outputs": { "MESSAGE": "Hello" }
}
```

`trigger` nennt, was den Lauf gestartet hat (`cron:<name>`, `interval:<name>`, `file:<name>`, `http:<name>`; fehlt bei
Starts von Hand), `client` bei `http` die Adresse des Aufrufers.

`status` eines Laufs: `running`, `succeeded`, `failed`, `cancelled`. `GET /runs/{id}` ergänzt `actions` – je Action
`id`, `type`, `status` (`pending`, `waiting`, `running`, `succeeded`, `failed`, `skipped`, `cancelled`), Zeiten, `durationMs`,
`error` und `runs` (Anzahl Ausführungen bei Schleifen) – und `log` (`time`, `level`, `actionId`, `message`; höchstens
5.000 Zeilen, ältere entfallen mit `logTruncated: true`).

Bei einem Pipeline-Lauf ist `kind` `pipeline` und `workflow` der Name der Pipeline. Seine Transforms erscheinen als
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
| `202` + `{"runId", "status"}`, Header `Location` | `response: accepted`: Lauf gestartet; `status` ist der Pfad des Laufs in dieser API |
| Status, Header und Body von `http.respond` | `response: run` (seit S38b1): die erste Antwort des Laufs |
| `200` + `outputs` / `500` + `{"error", "runId"}` | `response: run` ohne `http.respond`: Lauf erfolgreich bzw. gescheitert |
| `504` + `{"error", "runId"}` | `response: run`: keine Antwort innerhalb von `timeout`; der Lauf läuft weiter |
| `400` | Pflichtparameter fehlt, Wert passt nicht zum Typ, Body ist kein JSON-Objekt (bei Pipelines auch: keine Liste von Objekten) |
| `401` | Token fehlt oder falsch (`Authorization: Bearer <token>`) |
| `403` | Anfrage aus einer Webseite (`Origin`) oder – bei einem lokalen Listener – an einen fremden Host-Namen |
| `404` / `405` | kein Trigger für den Pfad bzw. deaktiviert / andere Methode (Header `Allow`) |
| `413` / `415` | Body über 1 MiB / weder JSON noch Formular |
| `429` | `maxConcurrent` des Triggers oder `settings.maxConcurrentRuns` erreicht |
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
| `set` | ändert ein Feld einer Action (`[{"id": "combine", "field": "with.message", "value": "Done"}]`); erlaubt sind `description`, `timeout`, `continueOnError`, `join`, `joinTimeout`, `retry.attempts`, `retry.backoff`, `retry.strategy`, `forEach`, `parallel`, `stopOnError` (seit E7c) und `with.<name>`. Werte: Text, Zahl oder `true`/`false`; `null` entfernt das Feld (mit dem letzten Eintrag auch `with:` bzw. `retry:`). Mehrzeilige Texte werden als `\|`-Block geschrieben, Texte, die YAML anders lesen würde (`true`, `1`, `a: b`), in Anführungszeichen; ein Kommentar am Zeilenende bleibt erhalten. Höchstens ein Feld je Action und Anfrage. Statt `value` setzt `map` ein Feld der Konfiguration auf eine Zuordnung von Texten (`{"id": "call", "field": "with.params", "value": null, "map": [{"key": "FILE", "value": "{{ row }}"}]}`); Schlüssel, die schon in der Datei stehen, behalten ihre Reihenfolge |
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
| `move`, `delete`, `add`, `rename`, `deleteLinks` | wie dort, für Transforms (`transforms:`) |
| `addLinks` | `from`, `to`, optional `type: error` (`data` ist der Standard und wird nicht geschrieben) |
| `updateLinks` | ändert den Typ: `[{"index": 1, "type": "error"}]`; `data` entfernt `type:` |
| `set` | `copies`, `distribution` und `with.<name>` eines Transforms; zusätzlich setzt `list` ein Feld der Konfiguration auf eine Liste von Zuordnungen, je eine Zeile im Flow-Stil (S46): `{"id": "read", "field": "with.fields", "value": null, "list": [[{"key": "name", "value": "kdnr"}, {"key": "type", "value": "int"}]]}` → `- { name: kdnr, type: int }`; Texte mit `,` `[` `]` `{` `}` in Anführungszeichen. Eine vorhandene Liste wird ersetzt, ohne `list` und `value` entfernt |
| `setPipeline` | `name`, `description`, `settings.batchSize`, `settings.bufferSize`, `settings.strict` |
| `entries` | nur `parameters` (Pipelines haben keine Outputs) |


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

→ 200 { "source": [{ "name": "File", "value": "examples/pipelines/data/customers.csv" },
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
Pipeline oder das Projekt nicht lesbar ist, oder – mit `{"error", "workingDir"}` – wenn die Eingabe nicht lesbar ist
(relative Pfade beziehen sich auf `workingDir`, den Ordner, in dem `runnel` gestartet wurde).

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
