# Server-API (`runnel serve`)

Stand: E3. Die API ist die einzige Schnittstelle des Editors zum Server und ebenso für Skripte nutzbar
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
| GET | `/projects/current` | `{"name": "hello", "dir": "C:\\…\\hello"}` |
| GET | `/projects/current/tree` | Dateibaum: `{name, path, type: "dir"\|"file", kind, size, children}`; Ordner vor Dateien, alphabetisch; `kind`: `workflow`, `pipeline`, `project`, `environment`, `other`; versteckte Einträge und symbolische Links fehlen |
| GET | `/files/{path}` | Inhalt der Datei, Header `ETag` |
| PUT | `/files/{path}` | Datei speichern (Body = Inhalt, max. 10 MiB); Antwort `{path, etag}` und Header `ETag` |
| DELETE | `/files/{path}` | Datei löschen, `204` |
| GET | `/catalog` | `{"actions": [Descriptor …]}` wie `runnel catalog --json` ([ADR 0014](../adr/0014-action-descriptor-schema.md)) |
| POST | `/validate` | Workflow prüfen, siehe unten |
| POST | `/workflows/edit` | Änderung vom Canvas auf den Text anwenden (nicht speichern), siehe unten |
| POST | `/runs` | Lauf starten, siehe unten |
| GET | `/runs` | `{"runs": [Run …]}`, neueste zuerst |
| GET | `/runs/{id}` | Lauf mit `actions` (Status je Knoten) und `log` |
| POST | `/runs/{id}/cancel` | Lauf abbrechen: `202`; bereits beendet `409` |
| GET | `/events` | Live-Ereignisse (Server-Sent Events), siehe unten |

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
Environments, Projektdateien und – wie vor einem Lauf – unbekannte Action-Typen und Konfiguration `with:`. Derzeit nur
Workflow-Dateien.

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

Lässt sich die Datei als Workflow lesen (keine Syntax- oder Feldfehler), enthält die Antwort zusätzlich `workflow`: das
geparste Modell mit `name`, `actions` (`id`, `type`, `join`, `with` …), `links` (`from`, `to`, `when`, `loop`,
`disabled`) und `layout` (Koordinaten je Action-ID in Pixeln, linke obere Ecke). Der Canvas des Editors baut darauf
auf; der Editor braucht so keinen eigenen YAML-Parser.

## Läufe

```json
POST /api/v1/runs
{ "path": "workflows/hello.workflow.yaml", "environment": "prod", "params": { "GREETING": "Moin" } }
```

`environment` und `params` sind optional (ohne `environment` gilt das Standard-Environment des Projekts). Gestartet wird
die **gespeicherte** Datei; vorher wird sie wie bei `runnel run` geprüft.

| Antwort | Bedeutung |
|---|---|
| `201` + `Run`, Header `Location` | Lauf gestartet; er läuft im Hintergrund weiter |
| `422` + `{error, errors, warnings, problems}` | Fehler in Workflow oder Projektdateien (Format wie `/validate`) |
| `400` | unbekanntes Environment, Parameter fehlen oder passen nicht zum Typ |
| `404` | Datei nicht gefunden |
| `503` | Server wird beendet |

```json
{
  "id": "01M7T2X0AHHW9G9ZAV04RRCXJC",
  "path": "workflows/parallel.workflow.yaml",
  "workflow": "parallel",
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
`id`, `type`, `status` (`pending`, `running`, `succeeded`, `failed`, `skipped`, `cancelled`), Zeiten, `durationMs`,
`error` und `runs` (Anzahl Ausführungen bei Schleifen) – und `log` (`time`, `level`, `actionId`, `message`; höchstens
5.000 Zeilen, ältere entfallen mit `logTruncated: true`).

Läufe liegen **nur im Speicher**: die letzten 100, laufende werden nie verworfen. Beim Beenden des Servers (Strg+C)
werden laufende Läufe abgebrochen. Die dauerhafte Historie folgt mit S26, ohne dass sich diese API ändert.

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

→ 200 { "content": "kind: Workflow\n…" }
```

| Feld | Wirkung |
|---|---|
| `move` | setzt die Position (Pixel, linke obere Ecke) im `layout`-Block; fehlende Einträge werden angehängt, ein fehlender Block angelegt |
| `delete` | entfernt die Actions, alle Links von und zu ihnen und ihre `layout`-Einträge |

Geändert werden **nur die betroffenen Zeilen** (Positionen aus dem Syntaxbaum); Kommentare, Reihenfolge, Anführungszeichen,
Flow- oder Block-Stil und Zeilenenden (LF/CRLF) aller anderen Zeilen bleiben erhalten – ein gespeicherter Stand ergibt
einen kleinen Git-Diff. Antworten: `400` für unbekannte Action-IDs, `422` (Format wie `/validate`), wenn die Datei
nicht als Workflow lesbar ist.

