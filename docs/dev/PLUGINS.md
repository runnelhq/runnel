# Plugins entwickeln

Plugins erweitern Runnel um eigene Actions und Transforms, ohne Runnel selbst zu ändern. Ein Plugin ist ein eigenes
Programm in beliebiger Sprache, das Runnel als Kindprozess startet und über JSON-RPC auf stdin/stdout anspricht
([ADR 0019](../adr/0019-plugin-protokoll.md) mit Nachträgen; Namen nach [ADR 0013](../adr/0013-typnamen-namensraeume.md),
Nachtrag 2026-11-24).

Diese Datei ist die verbindliche Beschreibung für Plugins und SDKs. Stand: **S50b** – Ablage, `plugin.yaml`, Freigabe,
Protokoll, Actions in Workflows, eigene Connection-Typen und Transforms in Pipelines mit Schema und „Felder ermitteln“.

In Go schreibt man Plugins am einfachsten mit dem [Go-SDK](https://github.com/runnelhq/sdk-go) (S51), in Rust mit dem
[Rust-SDK](https://github.com/runnelhq/sdk-rust) (Crate `runnel-plugin`, gleicher Umfang, dieselben Transkripte in
`testdata/protocol/`). Beispiele sind die offiziellen Plugins [`plugin-iban`](https://github.com/runnelhq/plugin-iban)
(Go: Actions und ein Transform mit Fehler-Link), [`plugin-ldap`](https://github.com/runnelhq/plugin-ldap) (Go:
Connection-Typ mit Test, Actions und eine Eingabe mit „Felder ermitteln“) und
[`plugin-faker`](https://github.com/runnelhq/plugin-faker) (Rust: Eingabe mit `transform.init`, Transform zum
Pseudonymisieren, Action). Die Tests von Runnel nutzen kein SDK, sondern sprechen das
Protokoll direkt (`internal/plugin/plugintest`).

## Ablage

Ein Plugin ist ein Ordner unter `plugins/` im Projekt:

```text
mein-projekt/
├─ project.yaml
├─ workflows/
└─ plugins/
   └─ iban/
      ├─ plugin.yaml
      └─ bin/
         ├─ iban.exe        # Windows
         └─ iban            # Linux
```

Plugins gibt es nur im Projekt, nicht für alle Projekte eines Rechners. So enthält ein Projektordner alles, was er zum
Laufen braucht.

## `plugin.yaml`

```yaml
kind: Plugin
apiVersion: runnel/v1
name: iban                  # wie der Ordner
version: 1.0.0              # SemVer
description: IBAN und BIC prüfen
namespace: iban             # Typen des Plugins: iban.validate, …
executable:                 # Text oder je Betriebssystem (windows, linux, darwin)
  windows: bin/iban.exe
  linux: bin/iban
args: []                    # z. B. [main.py] für ein Skript
```

| Feld | Regel |
|---|---|
| `name` | wie der Ordner; Kleinbuchstaben, Ziffern und `-`, beginnt mit einem Buchstaben |
| `version` | SemVer (`1.2.0`, `2.0.0-beta.1`) |
| `namespace` | wie `name` gebildet; nicht einer der eingebauten (`core`, `file`, `http`, …, siehe `runnel catalog`) und nicht `mcp-…`; zwei Plugins eines Projekts dürfen nicht denselben nennen |
| `executable` | Pfad im Ordner des Plugins (ohne `..`), absoluter Pfad oder ein Befehl des Rechners (z. B. `python` mit `args: [main.py]`); als Zuordnung je Betriebssystem. Fehlt der Eintrag für das laufende System, wird das Plugin nicht geladen |
| `args` | Argumente des Programms; Dateien im Ordner des Plugins sind relativ zu ihm |

`runnel validate` und das Laden des Projekts prüfen die Datei; `runnel schema plugin` gibt ihr JSON Schema aus.

## Freigabe

Ein Plugin ist Code mit den Rechten von Runnel (beim Dienst: dessen Konto). Damit ein geklontes oder fremdes Projekt
nicht schon beim Öffnen oder Prüfen Code ausführt, startet Runnel ein Plugin erst, wenn es **auf diesem Rechner für
dieses Projekt freigegeben** ist:

```bash
runnel plugins D:\flows
```
```bash
runnel plugins trust D:\flows
```
```bash
runnel plugins untrust D:\flows --plugin iban
```

- `runnel plugins` zeigt je Plugin Version, Namensraum, Programm und Stand: `trusted`, `trusted (service)`,
  `changed since trusted` oder `not trusted`.
- `runnel plugins trust` zeigt Name, Version, Namensraum, Ordner, Programm und den **Hash des Ordners** und fragt je
  Plugin nach. `--plugin <name>` wählt einzelne, `--yes` gibt ohne Rückfrage frei (für Skripte).
- **Hash:** SHA-256 über alle Dateien des Plugin-Ordners (Pfad, Länge, Inhalt). Jede Änderung, z. B. ein neues
  Programm nach einem Update, hebt die Freigabe auf; danach erneut `trust`. Verknüpfungen im Ordner sind nicht
  erlaubt; höchstens 1 GB und 10.000 Dateien.
- **Wo die Freigaben liegen** (nie im Projekt):
  - Benutzer: `%AppData%\runnel\trusted-plugins.json` (Windows) bzw. `$XDG_CONFIG_HOME/runnel/trusted-plugins.json`.
  - Windows-Dienst: `%ProgramData%\runnel\trust\plugins.json`, mit `--service` in einer Konsole „als Administrator“;
    nur Administratoren und SYSTEM dürfen sie ändern.

Im Editor (S52) zeigt der Explorer den Stand jedes Plugins; „Trust…“ zeigt Ordner, Programm und Hash und gibt das
Plugin für das Konto frei, unter dem `runnel serve` läuft (API: [`/plugins`](API.md#plugins)). Die Freigabe für den
Dienst bleibt `runnel plugins trust --service` vorbehalten.

## Namen und Veröffentlichung

- **Offizielle Plugins** liegen in `runnelhq/plugin-<name>`, SDKs in `runnelhq/sdk-<sprache>` (z. B. `sdk-go`).
- **Eigene Plugins:** Repository `runnel-plugin-<name>` in der eigenen Organisation, Namensraum mit Präfix der Firma
  (z. B. `contoso-sap`), damit es keine Zusammenstöße mit offiziellen Plugins gibt.
- **Release-Archiv:** `runnel-plugin-<name>_<version>_<os>_<arch>.zip` mit dem fertigen Ordner `<name>/` (`plugin.yaml`,
  Programme, `LICENSE`, `THIRD_PARTY_NOTICES`) und `SHA256SUMS`. Installieren: nach `plugins/` entpacken, dann
  `runnel plugins trust`.
- **Versionen:** SemVer-Tags, unabhängig von Runnel; die Version in `plugin.yaml` passt zum Tag.
- GitHub-Topic `runnel-plugin`.

## Protokoll

JSON-RPC 2.0, **eine Nachricht je Zeile** (UTF-8, `\n`, ein `\r` davor ist erlaubt), höchstens 16 MiB je Nachricht.

| Strom | Inhalt |
|---|---|
| stdin | Host → Plugin |
| stdout | Plugin → Host, **nur** Protokoll |
| stderr | Log des Plugins: je Zeile ein Eintrag (`DEBUG`, `INFO`, `WARN`, `ERROR` am Anfang setzen die Stufe), höchstens 64 KiB je Zeile |

Das Plugin läuft im **Projektordner** (Arbeitsverzeichnis) mit der Umgebung von Runnel und `RUNNEL_PLUGIN=1`. Ohne diese
Variable gestartet, gibt es einen kurzen Hinweis aus und endet.

### Ablauf

```text
→ {"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"encodings":["json"],"runnel":{"version":"0.9.0"}}}
← {"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"encoding":"json","name":"iban","version":"1.0.0","concurrency":4}}
→ {"jsonrpc":"2.0","id":2,"method":"describe","params":{}}
← {"jsonrpc":"2.0","id":2,"result":{"actions":[{"type":"iban.check","category":"general","label":"Check IBAN","description":"Checks an IBAN","schema":{"type":"object","properties":{"iban":{"type":"string"}},"required":["iban"]}}]}}
→ {"jsonrpc":"2.0","id":3,"method":"action.execute","params":{"type":"iban.check","config":{"iban":"DE02…"},"context":{"runId":"01M…","actionId":"check","projectDir":"D:\\flows","environment":"prod"}}}
← {"jsonrpc":"2.0","id":3,"result":{"output":{"valid":true,"country":"DE"}}}
→ {"jsonrpc":"2.0","id":4,"method":"shutdown","params":{}}
← {"jsonrpc":"2.0","id":4,"result":null}
```

| Methode | Antwort | Regeln |
|---|---|---|
| `initialize` | `protocolVersion` (1), `encoding` (`json`), `name` (wie in `plugin.yaml`), `version`, optional `concurrency` | Zeitgrenze 10 s; andere Version oder anderer Name: Plugin wird nicht genutzt |
| `describe` | `actions`: Descriptoren wie die eingebauten (`type`, optional `category`, `label`, `description`, `schema` als JSON Schema der Konfiguration, optional `example`); optional `transforms` (siehe [Transforms](#transforms)) und `connectionTypes` (siehe [Connection-Typen](#connection-typen)) | Zeitgrenze 10 s; Typen nur im eigenen Namensraum, jeder einmal, mit `schema` |
| `action.execute` | `{"output": …}` (Text, Zahl, Liste, Objekt; im Workflow `result.output`) | `config` ist geprüft und aufgelöst (Variablen, Ausdrücke); ein Fehler ist eine Fehlerantwort, ihre `message` die Meldung der Action |
| `connection.test` | `{"ok": true, "message": "…", "version": "…"}` | nur für Typen mit `"test": true`; `ok: false` mit `message` ist ein gescheiterter Test |
| `shutdown` | `null` | danach schließt der Host stdin; nach 5 s beendet er den Prozess mit allen Kindprozessen |

- **Parallelität:** Ohne `concurrency` schickt der Host eine Anfrage nach der anderen. Mit `concurrency: n` bis zu n
  gleichzeitig; Antworten werden über `id` zugeordnet und dürfen in beliebiger Reihenfolge kommen.
- **Abbruch:** `$/cancelRequest` (Notification, `{"id": n}`) bei Abbruch oder Timeout. Das Plugin soll die Arbeit
  beenden und kann mit dem Fehler `-32800` antworten; der Host wartet nicht darauf.
- **Unbekannte Methoden** beantwortet das Plugin mit `-32601`; der Host behandelt das als „nicht angeboten“. Unbekannte
  Felder ignorieren beide Seiten (Änderungen in Version 1 sind nur additiv). `null` anstelle einer Liste oder eines
  Objekts gilt als leer bzw. nicht angegeben; Runnel selbst schickt leere Listen als `[]`.
- **IDs:** Runnel nummeriert Anfragen mit ganzen Zahlen ab 1. Eine ID ist eine ganze Zahl im Bereich von int64 oder ein
  Text; eine andere (z. B. `1e999`, ein Objekt) beantwortet das Plugin mit `-32600` und `"id": null`, statt sie
  zurückzuschicken.
- **Fehler:** Endet das Plugin während einer Anfrage oder schreibt es etwas anderes als JSON-RPC auf stdout, scheitert
  die Anfrage mit den letzten Zeilen von stderr; der Lauf behandelt das wie jeden Fehler.
- **Secrets**, die der Host in der `config` übergibt, maskiert er in stderr, Fehlermeldungen und `output` (mit S49b2).

Transforms laufen als Strom mit eigenen Nachrichten, siehe [Transforms](#transforms).

## In Workflows

Die Actions eines freigegebenen Plugins stehen in Workflows des Projekts wie eingebaute bereit (auch in der Palette des
Editors). Mit einer Kategorie der eingebauten (`general`, `execution`, `files`, `network`, `database`) steht eine Action
in deren Gruppe der Palette; ohne `category` oder mit einer anderen in einer Gruppe des Plugins („Plugin: iban“):

```yaml
actions:
  - id: check
    type: iban.check
    with:
      iban: ${IBAN}
      api: erp            # Feld mit format: runnel-connection: das Plugin bekommt die aufgelöste Connection
```

- Ist das Plugin nicht freigegeben oder seit der Freigabe geändert, melden `runnel validate` und der Editor das an der
  Action; ein Lauf startet nicht.
- Runnel prüft die `config` vor dem Lauf gegen das Schema aus `describe` (Pflichtfelder, bei
  `additionalProperties: false` unbekannte Felder) und löst Variablen, Ausdrücke und Connections auf.
- Je Lauf ein Prozess je Plugin, gestartet beim ersten Aufruf, beendet am Ende des Laufs. Zeilen auf stderr stehen im
  Log der Action; Secrets des Laufs sind darin und in Fehlern und `output` maskiert.
- Die Umgebung enthält keine Variablen mit Tokens der HTTP-Trigger des Projekts.

## Transforms

Ein Transform eines Plugins bekommt die Records seines Eingangs als Strom und schreibt Records auf seine Ausgänge.
`describe` liefert ihn unter `transforms`, mit denselben Angaben wie eingebaute (`runnel catalog`):

```json
{"type": "iban.check-all", "category": "transform", "label": "Check IBANs", "description": "Checks the IBAN of every record",
 "recordErrors": true, "parallel": true,
 "schema": {"type": "object", "required": ["field"], "properties": {"field": {"type": "string"},
   "invalid": {"type": "string", "format": "runnel-target"}}}}
```

`recordErrors` erlaubt einen Fehler-Link, `parallel` erlaubt `copies`. `category` ist optional: Ohne eine Kategorie der
eingebauten (`input`, `transform`, `flow`, `execution`, `combine`, `output`) steht der Transform in der Palette in einer
Gruppe des Plugins und verhält sich wie einer der Kategorie `transform` (nur `input` hat keinen Eingang). `init` und `detectFields` kündigen die Methoden unter [Schema und Felder](#schema-und-felder) an.

### Ablauf

```text
→ transform.open   {"stream":1,"type":"iban.check-all","config":{"field":"iban"},"context":{"runId":"01M…","transformId":"check","projectDir":"D:\\flows"},"emitWindow":4}
← (Antwort)        {"window":4}
→ transform.batch  {"stream":1,"fields":[{"name":"id","type":"int"},{"name":"iban","type":"string"}],"rows":[[1,"DE02…"],[2,"XX"]]}
← transform.emit   {"stream":1,"to":"main","fields":[{"name":"id","type":"int"},{"name":"iban","type":"string"}],"rows":[[1,"DE02…"]]}
← transform.emit   {"stream":1,"to":"error","fields":[…,{"name":"_error","type":"string"},{"name":"_errorField","type":"string"}],"rows":[[2,"XX","invalid IBAN","iban"]]}
← $/ack            {"stream":1,"batches":1}
→ $/ack            {"stream":1,"batches":2}
→ transform.end    {"stream":1}
← transform.closed {"stream":1}
```

Nur `transform.open` ist eine Anfrage mit Antwort; alles andere sind Notifications (ohne `id`).

| Nachricht | Richtung | Inhalt |
|---|---|---|
| `transform.open` | Host → Plugin | `stream` (Nummer, eindeutig im Prozess), `type`, `config` (geprüft und aufgelöst), `context`, `emitWindow`; Antwort `window` (1–64) |
| `transform.batch` | Host → Plugin | `rows`; `fields` nur beim ersten Batch und wenn sich die Felder ändern |
| `transform.end` | Host → Plugin | keine weiteren Batches; ein Eingabe-Transform bekommt es gleich nach `open` |
| `transform.emit` | Plugin → Host | `to`, `rows`; `fields` beim ersten `emit` und wenn sie sich ändern |
| `transform.closed` | Plugin → Host | Ende des Stroms; mit `error: {"message"}` scheitert der Transform |
| `$/ack` | beide | `batches`: so viele Kredite gibt die Seite zurück |

- **Kredite:** Der Host schickt höchstens `window` Batches, bevor das Plugin mit `$/ack` neue freigibt; das Plugin
  schickt höchstens `emitWindow` `emit`, bevor der Host neue freigibt. Mehr `emit` beenden den Strom mit Fehler. So
  bleibt der Speicher auf beiden Seiten begrenzt, und ein langsamer Transform dahinter bremst das Plugin.
- **Ziele (`to`):** `main` (alle Daten-Links, die nicht zu einem Ziel der Konfiguration führen), `error` (Fehler-Link;
  Records mit `_error` und `_errorField`) oder die ID eines Transforms aus einem Feld mit `format: runnel-target`.
- **Werte** nach Typ: `string`, `int` (Zahl), `decimal` (Text, exakt), `float`, `bool`, `date` (`"2026-11-24"`),
  `datetime` (RFC 3339 mit Offset), `binary` (Base64), `json`; ohne Typ der JSON-Wert; `null` immer.
- **Mehrere Ströme:** Alle Transforms des Plugins in einer Pipeline (auch Kopien) teilen einen Prozess; `stream`
  ordnet die Nachrichten zu. Nachrichten verschiedener Ströme dürfen sich mischen.
- **Fehler:** Ein falscher Wert, ein unbekanntes Ziel oder ein Absturz beenden den Transform und die Pipeline mit
  einer Meldung; Zeilen von stderr stehen im Log des Transforms.

### Schema und Felder

Optional, je Transform in `describe` angekündigt:

| Methode | Anfrage | Antwort |
|---|---|---|
| `transform.init` (`"init": true`) | `type`, `config`, `input` (`{"fields":[{"name","type"}],"open":false}`), `targets` | `output` (Schema wie `input`, ohne: offen), `problems` (`[{"path":"field","message":"…","warning":false}]`) |
| `transform.detectFields` (`"detectFields": true`) | `type`, `config`, `limit` | `source` (`[{"name","value"}]`), `rows`, `complete`, `fields` (`name`, `type`, `format`, `values`, `note`, `warning`) |

- **`init`** prüft die Konfiguration und sagt, welche Felder herauskommen. Runnel ruft es beim Prüfen (`validate`,
  Editor) und vor dem Lauf, in einem kurz gestarteten Prozess, höchstens 10 s, und merkt sich die Antwort, solange
  sich Plugin und Anfrage nicht ändern. Felder mit Secrets kommen leer, Connections als Name, Variablen eventuell
  noch als `${…}`: `init` soll nur prüfen, nicht verbinden. `path` in `problems` ist der Pfad in `with` (z. B.
  `field` oder `fields[2].name`); der Editor zeigt die Meldung dort.
- **`detectFields`** liest die ersten Datensätze einer Eingabe für „Detect fields“ im Editor (höchstens `limit`,
  1 Minute). Connections kommen aufgelöst mit Secrets, wie im Lauf. Runnel vergleicht die Felder mit `fields:` der
  Konfiguration (vorhanden, neu, fehlt).

## Connection-Typen

Ein Plugin kann eigene Connection-Typen anbieten, z. B. für ein ERP oder ein DMS. `describe` liefert sie unter
`connectionTypes`:

```json
{"actions": […], "connectionTypes": [{
  "type": "iban", "label": "IBAN service", "description": "Account of the IBAN service", "test": true,
  "schema": {"type": "object", "required": ["url"], "additionalProperties": false, "properties": {
    "url": {"type": "string"},
    "apiKey": {"type": "string", "format": "runnel-secret"}}}}]}
```

- **Name:** der Namensraum des Plugins oder `<namensraum>-…` (z. B. `iban`, `iban-test`); eingebaute Typen kann ein
  Plugin nicht ersetzen.
- **Connection-Dateien** dieses Typs prüft Runnel gegen `schema` (Pflichtfelder, bei `additionalProperties: false`
  unbekannte Felder); `${secret:…}` nur in Feldern mit `format: runnel-secret`. Im Editor erscheinen Typ und Felder im
  Formular der Connection. Ist das Plugin nicht freigegeben, nennt die Prüfung den Grund an `type`.
- **In Actions:** Ein Feld mit `format: runnel-connection` und `x-runnel-connection: ["iban"]` nimmt eine Connection
  des Typs; sie kommt aufgelöst an (`{"name", "type", …Felder}`), Secrets im Klartext und in allem Zurückgegebenen
  maskiert.
- **Test:** Mit `"test": true` ruft „Test connection“ im Editor `connection.test` auf:

```text
→ {"jsonrpc":"2.0","id":3,"method":"connection.test","params":{"type":"iban","connection":{"name":"bank","type":"iban","url":"https://…","apiKey":"…"}}}
← {"jsonrpc":"2.0","id":3,"result":{"ok":true,"message":"signed in","version":"API 2.1"}}
```

  Runnel startet das Plugin dafür, wartet höchstens 15 s und beendet es danach. Nichts soll dabei geschrieben oder
  gesendet werden außer der Anmeldung.

## In Pipelines

```yaml
transforms:
  - id: check
    type: iban.check-all
    with:
      field: iban
      invalid: rejected   # Ziel: Records an diesen Transform
links:
  - { from: read, to: check }
  - { from: check, to: write }
  - { from: check, to: rejected }
  - { from: check, to: errors, type: error }
```

Wie bei Actions: nicht freigegebene Plugins meldet die Prüfung am Transform, die `config` wird gegen das Schema
geprüft, Connections kommen aufgelöst an. Ohne `transform.init` ist das Schema der Felder hinter einem
Plugin-Transform offen.

## Testen

`runnel plugins <projekt>` startet freigegebene Plugins und zeigt ihre Typen; Fehler beim Start oder in `describe`
stehen dort. Von Hand lässt sich ein Plugin mit `RUNNEL_PLUGIN=1` starten und die Zeilen oben eintippen.
