# Plugins entwickeln

Plugins erweitern Runnel um eigene Actions und Transforms, ohne Runnel selbst zu ändern. Ein Plugin ist ein eigenes
Programm in beliebiger Sprache, das Runnel als Kindprozess startet und über JSON-RPC auf stdin/stdout anspricht
([ADR 0019](../adr/0019-plugin-protokoll.md) mit Nachträgen; Namen nach [ADR 0013](../adr/0013-typnamen-namensraeume.md),
Nachtrag 2026-11-24).

Diese Datei ist die verbindliche Beschreibung für Plugins und SDKs. Stand: **S49c** – Ablage, `plugin.yaml`, Freigabe,
Protokoll, Actions aus Plugins in Workflows und eigene Connection-Typen. Transforms folgen mit S50.

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
| `describe` | `actions`: Descriptoren wie die eingebauten (`type`, `category`, `label`, `description`, `schema` als JSON Schema der Konfiguration, optional `example`); optional `connectionTypes` (siehe [Connection-Typen](#connection-typen)) | Zeitgrenze 10 s; Typen nur im eigenen Namensraum, jeder einmal, mit `schema` |
| `action.execute` | `{"output": …}` (Text, Zahl, Liste, Objekt; im Workflow `result.output`) | `config` ist geprüft und aufgelöst (Variablen, Ausdrücke); ein Fehler ist eine Fehlerantwort, ihre `message` die Meldung der Action |
| `connection.test` | `{"ok": true, "message": "…", "version": "…"}` | nur für Typen mit `"test": true`; `ok: false` mit `message` ist ein gescheiterter Test |
| `shutdown` | `null` | danach schließt der Host stdin; nach 5 s beendet er den Prozess mit allen Kindprozessen |

- **Parallelität:** Ohne `concurrency` schickt der Host eine Anfrage nach der anderen. Mit `concurrency: n` bis zu n
  gleichzeitig; Antworten werden über `id` zugeordnet und dürfen in beliebiger Reihenfolge kommen.
- **Abbruch:** `$/cancelRequest` (Notification, `{"id": n}`) bei Abbruch oder Timeout. Das Plugin soll die Arbeit
  beenden und kann mit dem Fehler `-32800` antworten; der Host wartet nicht darauf.
- **Unbekannte Methoden** beantwortet das Plugin mit `-32601`; der Host behandelt das als „nicht angeboten“. Unbekannte
  Felder ignorieren beide Seiten (Änderungen in Version 1 sind nur additiv).
- **Fehler:** Endet das Plugin während einer Anfrage oder schreibt es etwas anderes als JSON-RPC auf stdout, scheitert
  die Anfrage mit den letzten Zeilen von stderr; der Lauf behandelt das wie jeden Fehler.
- **Secrets**, die der Host in der `config` übergibt, maskiert er in stderr, Fehlermeldungen und `output` (mit S49b2).

Ströme für Transforms (`transform.open`, `.batch`, `.end`, Kredite) folgen mit S50 nach [ADR 0019](../adr/0019-plugin-protokoll.md).

## In Workflows

Die Actions eines freigegebenen Plugins stehen in Workflows des Projekts wie eingebaute bereit (auch in der Palette des
Editors):

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

## Testen

`runnel plugins <projekt>` startet freigegebene Plugins und zeigt ihre Typen; Fehler beim Start oder in `describe`
stehen dort. Von Hand lässt sich ein Plugin mit `RUNNEL_PLUGIN=1` starten und die Zeilen oben eintippen.
