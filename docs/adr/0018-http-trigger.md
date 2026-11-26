# ADR 0018: HTTP-Trigger – Läufe per HTTP starten und aus dem Lauf antworten

- **Status:** Angenommen
- **Datum:** 2026-11-20
- **Bezug:** [ADR 0005 – Mehrbenutzerbetrieb](0005-mehrbenutzerbetrieb.md), [ADR 0006 – Ausdruckssprache](0006-ausdruckssprache.md),
  [ADR 0012 – KI-Fähigkeit](0012-ki-faehigkeit.md) (Werkzeug-Vertrag), [ADR 0017 – Sub-Workflows](0017-sub-workflows.md),
  KONZEPT Kap. 2.7, 5.3, 8 und 10, Roadmap S38a–S38c, S40a

## Kontext

Das Konzept sah einen Trigger-Typ `webhook` vor: ein Endpunkt `/hooks/{name}`, Body und Query als Parameter, „synchrone
oder asynchrone Antwort“, abgesichert per Token oder HMAC. Beim Planen der Phase 0.5 zeigte sich, dass mehr gebraucht
wird als ein Webhook:

- **Verarbeitungen anstoßen:** Kleine Systeme im Netz melden etwas (z. B. „neue Datei da“), Runnel arbeitet im
  Hintergrund.
- **REST-artige Dienste:** Ein Aufruf wie `GET /api/customers/4711` liest über eine Pipeline (später aus einer
  Datenbank) und antwortet mit den Daten – oder mit `404`, wenn nichts gefunden wurde. Die Antwort samt Statuscode
  entsteht erst im Workflow bzw. in der Pipeline.
- **Lokale Skripte** stoßen Workflows an und warten ggf. auf das Ergebnis.

Erwogen wurde ein eigener Begriff „Endpoint“ neben den Triggern. Dagegen sprach: Format (`name`, `type`, `target`,
`environment`, `with`), Verwaltung (Ordner, API, aktivieren/deaktivieren), Run-Queue und Historie sind dieselben wie bei
den übrigen Triggern; alle Unterschiede (Methode und Pfad, Absicherung, wartender Aufrufer) sind Eigenschaften eines
Typs, wie `schedule` bei Cron.

## Entscheidung

### Trigger-Typ `http`

Der Typ `webhook` wird zum Typ **`http`** erweitert. Ein Webhook ist der Fall `response: accepted`, ein Dienst der Fall
`response: run`.

```yaml
kind: Trigger
apiVersion: runnel/v1
name: customer
type: http
target: pipelines/customer.pipeline.yaml   # Workflow oder Pipeline
environment: prod
with:
  method: GET                              # GET, POST, PUT, PATCH, DELETE
  path: /api/customers/{id}                # Platzhalter {name} werden zu Parametern
  auth: { token: ${env:CUSTOMER_API_TOKEN} }   # oder: auth: none
  response: run                            # accepted (Standard) | run
  timeout: 30s                             # nur bei run: ohne Antwort bis dahin 504
  maxConcurrentRuns: 10                    # gleichzeitige Läufe dieses Triggers, darüber 429 (Standard 4 seit R5a; Name seit R5b)
  history: errors                          # all | errors | none (Standard: all bei accepted, errors bei run)
```

- **Routing:** Methode und Pfad sind je Projekt eindeutig; Konflikte (auch über Platzhalter, z. B. `/a/{x}` und `/a/b`)
  meldet `runnel validate` beim Laden aller Trigger. Ein deaktivierter Trigger antwortet `404`.
- **Eingaben:** Pfad-Platzhalter, Query und die Felder eines JSON-Objekts im Body werden zu den typisierten `parameters`
  des Ziels (Werkzeug-Vertrag, ADR 0012); bei gleichem Namen gilt Pfad vor Query vor Body. Fehlt ein Pflichtparameter
  oder passt ein Typ nicht, antwortet Runnel **`400`** mit der Meldung, ohne einen Lauf zu starten. Unbekannte Felder
  werden ignoriert. Zusätzlich steht im Ausdruckskontext **`request`** (`method`, `path`, `headers`, `query`, `body`)
  – bei `body` der ganze JSON-Wert bzw. Text. Bei Pipelines kann ein JSON-Array im Body zu Datensätzen werden (wie
  `rows` bei `core.run-pipeline`, gelesen mit `core.from-result`).
- **Absicherung:** `auth: { token: … }` erwartet `Authorization: Bearer <token>` (falsch oder fehlend: `401`);
  `auth: none` ist ausdrücklich anzugeben und für das interne Netz gedacht. Das Token kommt bis zum Secret-Store (S39)
  aus einer Umgebungsvariablen (`${env:…}`). Eine HMAC-Signaturprüfung (für Anbieter mit eigenem Format) folgt später
  als weitere `auth`-Variante. Ein Body-Limit gilt immer (1 MiB, fest, `413`).

### Antwort

| `response` | Verhalten |
|---|---|
| `accepted` (Webhook) | Sofort **`202`** mit `{ "runId": … }` (bis R5a zusätzlich `status`, siehe Nachtrag 2026-11-26); der Lauf arbeitet im Hintergrund |
| `run` (Dienst) | Die Anfrage wartet, bis der Lauf antwortet, höchstens `timeout` (dann **`504`**) |

Bei `response: run` antwortet der Lauf auf zwei Wegen – **beide** sind vorgesehen:

- **Standard ohne eigenen Baustein:** Endet der Lauf erfolgreich, antwortet Runnel mit `200` und den `outputs` als
  JSON-Objekt (Pipelines: den Zählern wie bei `core.run-pipeline`). Scheitert er, mit `500` und einer kurzen Meldung
  ohne interne Details (Lauf-ID zum Nachsehen); die vollständige Meldung steht im Log.
- **Selbst bestimmt:**
  - **Workflow:** Action **`http.respond`** (heute `http.response`, siehe Nachtrag 2026-11-23) mit `status`, `headers` und `body` (jeweils mit Ausdrücken). Die erste
    ausgeführte `http.respond` antwortet; spätere schreiben eine Warnung ins Log. So können Zweige verschieden antworten
    (Erfolgszweig `200`, Fehlerzweig `422`), und der Workflow darf nach der Antwort weiterarbeiten. Ohne wartenden
    Aufrufer (Start aus Editor, CLI oder Cron) tut die Action nichts und schreibt eine Debug-Zeile – jeder Workflow
    bleibt auf jedem Weg startbar.
  - **Pipeline:** Ausgabe-Transform **`http.response`**: die Datensätze als JSON-Body (Liste, oder ein Objekt mit
    `single: true`), `status` und `headers` aus der Konfiguration. Unterschiedliche Status entstehen über mehrere
    `http.response` hinter `core.switch` (z. B. kein Datensatz → `404`; umgesetzt mit `whenEmpty` als Rückfallantwort,
    siehe Nachtrag); es antwortet der erste, der Datensätze erhält,
    oder – wenn keiner welche erhält – der mit `whenEmpty: true`.

### Listener

HTTP-Trigger laufen auf einem **eigenen Listener**, getrennt von Editor und API (`runnel serve --triggers <adresse>`,
Standard `127.0.0.1:7301`). So bleibt der Editor lokal, auch wenn Trigger aus dem Netz erreichbar sind. Für den
Zugriff aus dem Netz ist **S40a „Listener und TLS“** Voraussetzung: frei wählbare Adresse und TLS (eigenes Zertifikat
oder selbst erzeugt). Der übrige Netzwerkbetrieb des Editors (gemeinsamer Zugangsschlüssel, Client-Infos; S40b) ist
dafür nicht nötig.

### Historie und Last

- Läufe eines HTTP-Triggers tragen `trigger = http:<name>` und die Client-Adresse.
- `history` steuert, welche Läufe behalten werden; bis zur Lauf-Historie (S26) zählen HTTP-Läufe gegen ein eigenes
  Limit im Speicher, damit Läufe aus Editor und Cron nicht verdrängt werden.
- Vorbereitete Workflows und Pipelines werden zwischengespeichert (umgesetzt für alle Starts, siehe Nachtrag)
  und bei Änderung der Datei neu geprüft,
  statt bei jeder Anfrage.
- Run-Queue: Priorität manuell > HTTP > zeitgesteuerte Trigger.

## Nachtrag 2026-11-21 (Umsetzung S38a–S38c)

Beim Umsetzen mit dem Maintainer abgestimmt bzw. festgelegt (zusammengefasst aus den Schritten S38a, S40a, S38b1, S38b2
und S38c):

**Eingaben und Routing**
- **Feste `params` gewinnen:** Werte, die der Trigger unter `params` festlegt, kann die Anfrage nicht ändern; ein
  Pfad-Platzhalter mit demselben Namen ist ein Fehler.
- **Body auch als Formular:** neben einem JSON-Objekt `application/x-www-form-urlencoded` (einfache Geräte, `curl -d`).
  Eine **JSON-Liste** im Body nur für Pipelines (Datensätze, Felder nach Namen sortiert wie `rows` bei
  `core.run-pipeline`); `request` steht in Pipelines (noch) nicht zur Verfügung.
- **Routing ohne Groß-/Kleinschreibung** in den festen Teilen des Pfads (wie unter Windows üblich); die Prüfung auf
  Überschneidungen folgt derselben Regel.

**Absicherung und Listener**
- **Token nur aus der Umgebung:** `token` muss genau `${env:NAME}` sein; ein Token in der Datei ist ein Fehler.
  `request.headers` enthält `Authorization` nicht.
- **Listener nur bei Bedarf:** Er öffnet sich erst, wenn ein HTTP-Trigger aktiv ist – Projekte ohne HTTP-Trigger
  belegen keinen Port.
- **Optionen am Server, nicht im Projekt:** `--triggers`, `--triggers-tls auto`, `--triggers-cert`/`--triggers-key`
  und `--triggers-insecure` bei `runnel serve` und `runnel service install` – Adresse und Zertifikat hängen am Rechner.
- **Im Netz nur mit TLS**, außer ausdrücklich `--triggers-insecure` (Warnung bei jedem Start). Das selbst erzeugte
  Zertifikat liegt neben den Logs (`%ProgramData%\runnel\tls\` bzw. `%LOCALAPPDATA%\runnel\tls\`), nie im Projekt;
  ein eigenes wird nach dem Erneuern ohne Neustart übernommen.
- Anfragen aus Webseiten (`Origin`) werden immer mit `403` abgelehnt, bei einem lokalen Listener auch Anfragen an
  fremde Host-Namen (DNS-Rebinding).

**Antwort**
- **Sub-Workflows:** Der wartende Aufrufer und `request` werden an aufgerufene Workflows weitergereicht; die erste
  `http.respond` irgendwo im Lauf antwortet.
- **Nach dem Timeout** läuft der Lauf weiter; eine spätere Antwort wird mit Warnung ignoriert. Jede Antwort bei
  `response: run` trägt den Header `Runnel-Run-Id`.
- **`whenEmpty` ist immer Rückfallantwort:** Ein `http.response` mit `whenEmpty: true` antwortet – mit oder ohne
  Datensätze – erst am Ende des Laufs und nur, wenn kein anderer geantwortet hat. Damit genügen für „gefunden → `200`,
  sonst `404`“ zwei `http.response` hinter demselben Transform, ohne `core.switch`.
- **JSON der Datensätze:** Felder in ihrer Reihenfolge, Dezimalzahlen als Zahl, Datum als `yyyy-mm-dd`, Zeitpunkte nach
  RFC 3339; höchstens `maxRows` (Standard 10.000) Datensätze im Speicher.

**Historie und Last**
- **`history: none`** behält den Lauf nicht und veröffentlicht keine Ereignisse; `errors` behält nur gescheiterte. Die
  Standardantwort aus `outputs` funktioniert trotzdem.
- **Eigenes Limit:** die letzten 100 Läufe von HTTP-Triggern, zusätzlich zu den letzten 100 übrigen.
- **Zwischenspeicher für alle Starts**, nicht je Trigger: geprüfte Dateien nach dem Hash ihres Inhalts, das Projekt nach
  Größe und Zeit seiner Dateien, übersetzte Ausdrücke nach ihrem Text; nur fehlerfreie Ergebnisse. Aufrufe anderer
  Workflows werden bei jedem Start geprüft, da sie von anderen Dateien abhängen.

## Konsequenzen

- Ein Begriff „Trigger“ für alles, was Läufe automatisch startet: `cron`, `file`, `interval`, `http`. Der Typ `webhook`
  entfällt; KONZEPT und Roadmap werden angepasst.
- Mit `response: run` wird Runnel zu einem kleinen Anwendungsserver – Antwortzeiten, Timeouts und Last werden ein
  Thema (Zwischenspeicher, `maxConcurrent`, `history`). Für hohe Last ist Runnel nicht gedacht.
- Workflows und Pipelines bleiben ohne HTTP nutzbar: `http.respond` ist ohne Aufrufer wirkungslos, und die
  Standardantwort aus `outputs` braucht gar keinen eigenen Baustein.
- Die Bearbeitung von Trigger-Dateien im Editor ist ein eigener Schritt mit Mockup; bis dahin sind es YAML-Dateien.
- Umsetzung in Schritten (Roadmap Phase 0.5): **S38a** HTTP-Trigger asynchron (`accepted`, lokal), **S40a** Listener
  und TLS, **S38b** Antwort aus dem Workflow (`run`, `http.respond`, `request`), **S38c** Antwort aus der Pipeline
  (`http.response`, Datensätze aus dem Body). Voraussetzung für alle: die Trigger-Grundlage aus S37.

## Nachtrag 2026-11-23: `http.respond` heißt `http.response`

Abgestimmt mit dem Maintainer. Die Action heißt wie der Transform `http.response`: Beide beantworten den Aufrufer eines
HTTP-Triggers, und ADR 0013 erlaubt denselben Namen für Action und Transform (wie `core.log`, `http.request`). Einen
Alias für `http.respond` gibt es nicht, da noch kein Release erschienen ist; Workflows mit `http.respond` meldet
`validate` als unbekannten Typ.

## Nachtrag 2026-11-26 (R5a): Grenze und Antwort `202`

- **`maxConcurrent` hat den Standard 4:** Ohne Angabe nimmt ein HTTP-Trigger höchstens 4 Läufe gleichzeitig an, die
  nächste Anfrage bekommt `429` (AGENTS.md „Sicherheit“: Grenze für gleichzeitige Läufe). `maxConcurrent: 0` gilt nur
  ausdrücklich gesetzt als „keine eigene Grenze“; dann greifen nur `settings.maxConcurrentRuns` von Projekt und
  Workflow. Im Modell ist das Feld ein Zeiger, damit `0` und „nicht gesetzt“ unterscheidbar sind.
  `settings.maxConcurrentRuns` behält den Standard `0` (keine Grenze): Es gilt auch für Läufe aus Editor, Zeitplan und
  Ordner-Überwachung, und die Anfragen von außen begrenzt schon der Trigger.
- **Antwort `202`** bei `response: accepted`: `{"runId": "…"}`. Das Feld `status` mit dem Pfad
  `/api/v1/runs/<id>` entfällt: `status` ist sonst überall ein Zustand, und der Pfad zeigt auf die API des Servers auf
  `127.0.0.1`, die ein Aufrufer im Netz nicht erreicht. Der Header `Location` bleibt wie bisher. Bewusster Bruch vor 1.0
  (ADR 0029).

## Nachtrag 2026-11-26 (R5b): `maxConcurrentRuns`

- Die Grenze der gleichzeitigen Läufe eines HTTP-Triggers heißt `with.maxConcurrentRuns` wie `settings.maxConcurrentRuns`
  in `project.yaml` und im Workflow: dieselbe Art Grenze mit derselben Antwort `429`. Standard (4) und Bedeutung von `0`
  bleiben wie im Nachtrag R5a.
- `maxConcurrent` ist ein Fehler mit Position und dem neuen Namen, ohne Übergangszeit (bewusster Bruch vor 1.0,
  ADR 0029). Die `429`-Meldung nennt `maxConcurrentRuns`.
