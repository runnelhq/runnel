# Server-API (`runnel serve`)

Stand: E8b. Die API ist die einzige Schnittstelle des Editors zum Server und ebenso für Skripte nutzbar
([Konzept, Kap. 10](../KONZEPT.md#10-api-server-modus)).

```bash
runnel serve examples/hello            # Projekt im Ordner oder darüber, Port 7300
runnel serve --port 0 .                 # freier Port; die Ausgabe nennt die Adresse
```

Außerhalb von `/api/v1` liefert der Server den eingebetteten Editor aus ([EDITOR.md](EDITOR.md)).

## Grundsätze

| Thema | Regel |
|---|---|
| Adresse | nur `127.0.0.1` (Netzwerkbetrieb mit Zugangsschlüssel und TLS folgt mit S39) |
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
| GET | `/projects/current/tree` | Dateibaum: `{name, path, type: "dir"\|"file", kind, size, children}`; Ordner vor Dateien, alphabetisch; `kind`: `workflow`, `pipeline`, `project`, `environment`, `other`; versteckte Einträge und symbolische Links fehlen |
| GET | `/files/{path}` | Inhalt der Datei, Header `ETag` |
| PUT | `/files/{path}` | Datei speichern (Body = Inhalt, max. 10 MiB); Antwort `{path, etag}` und Header `ETag` |
| DELETE | `/files/{path}` | Datei löschen, `204` |
| GET | `/catalog` | `{"actions": [Descriptor …], "transforms": [Descriptor …]}` wie `runnel catalog --json` ([ADR 0014](../adr/0014-action-descriptor-schema.md)); `kind` ist `action` bzw. `transform` |
| GET | `/expressions` | Referenz für den Ausdruckseditor (S35): `{"roots": [Doc …], "functions": [Doc …], "variables": [{name, value, source} …]}`; `Doc` = `{name, signature?, type, description, example?, group?, fields?}` aus `internal/expr/docs.go`; `variables` aus `project.yaml` und dem Standard-Environment (Wert wie in der Datei, `source` z. B. `environments/dev.yaml`). Ist das Projekt fehlerhaft, fehlen nur die Variablen |
| POST | `/validate` | Workflow oder Pipeline prüfen, siehe unten |
| POST | `/workflows/edit` | Änderung vom Canvas auf den Text anwenden (nicht speichern), siehe unten |
| POST | `/pipelines/edit` | dasselbe für Pipelines (S25a), siehe unten |
| POST | `/runs` | Lauf starten, siehe unten |
| GET | `/runs` | `{"runs": [Run …]}`, neueste zuerst |
| GET | `/runs/{id}` | Lauf mit `actions` (Status je Knoten) und `log` |
| POST | `/runs/{id}/cancel` | Lauf abbrechen: `202`; bereits beendet `409` |
| GET | `/events` | Live-Ereignisse (Server-Sent Events), siehe unten |
| GET | `/triggers` | Trigger des Projekts (S37): `{"triggers": [{name, file, type, target, schedule, timezone, enabled, next, last} …], "problems": [Problem …]}`. `next` ist der nächste Zeitpunkt (fehlt bei deaktivierten Triggern), `last` das Ergebnis des letzten fälligen Zeitpunkts seit dem Start des Servers (`{time, runId}`, `{time, skipped: true}` bei `overlap: skip` oder `{time, error}`); `problems` sind Fehler in Trigger-Dateien (Format wie `/validate`), solche Trigger sind nicht aktiv |

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
Konfiguration und verwendete Felder der Transforms (ADR 0007); die Pfade lauten `$.transforms[i]…`.

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
  "environment": "dev",
  "status": "running",
  "started": "2026-11-19T14:20:50.898Z",
  "finished": "2026-11-19T14:20:52.899Z",
  "durationMs": 2001,
  "error": "…",
  "outputs": { "MESSAGE": "Hello" }
}
```

`status` eines Laufs: `running`, `succeeded`, `failed`, `cancelled`. `GET /runs/{id}` ergänzt `actions` – je Action
`id`, `type`, `status` (`pending`, `waiting`, `running`, `succeeded`, `failed`, `skipped`, `cancelled`), Zeiten, `durationMs`,
`error` und `runs` (Anzahl Ausführungen bei Schleifen) – und `log` (`time`, `level`, `actionId`, `message`; höchstens
5.000 Zeilen, ältere entfallen mit `logTruncated: true`).

Bei einem Pipeline-Lauf ist `kind` `pipeline` und `workflow` der Name der Pipeline. Seine Transforms erscheinen als
`actions` (Ereignisse `action.started`, `action.finished`), ein beendeter Transform zusätzlich mit `counts`
(`read`, `written`, `errors`). `linkRows` zählt die Datensätze je Link (Index in `links`); ein Link ist gezählt, sobald
der Transform davor beendet ist. Das Ereignis `action.finished` eines Transforms trägt `counts` und `linkRows` seiner
ausgehenden Links (`{ "1": 2, "2": 2 }`).

Läufe liegen **nur im Speicher**: die letzten 100, laufende werden nie verworfen. Beim Beenden des Servers (Strg+C)
werden laufende Läufe abgebrochen. Die dauerhafte Historie folgt mit S26 (zurückgestellt, siehe Backlog der Roadmap), ohne dass sich diese API ändert.

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
| `set` | `copies`, `distribution` und `with.<name>` eines Transforms |
| `setPipeline` | `name`, `description`, `settings.batchSize`, `settings.bufferSize`, `settings.strict` |
| `entries` | nur `parameters` (Pipelines haben keine Outputs) |

