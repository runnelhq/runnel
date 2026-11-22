# ADR 0019: Plugin-Protokoll – eigene Prozesse, JSON-RPC über stdin/stdout

- **Status:** Angenommen
- **Datum:** 2026-11-21
- **Bezug:** [ADR 0002 – Go-Backend](0002-go-backend.md), [ADR 0004 – MIT-Lizenz](0004-lizenz-mit.md),
  [ADR 0007 – Typsystem in Pipelines](0007-typsystem-pipelines.md), [ADR 0012 – KI-Fähigkeit](0012-ki-faehigkeit.md),
  [ADR 0013 – Typnamen und Namensräume](0013-typnamen-namensraeume.md),
  [ADR 0014 – Descriptor](0014-action-descriptor-schema.md), KONZEPT Kap. 7.3 und 8, Roadmap S49–S53

## Kontext

Phase 0.7 soll Runnel um eigene Actions und Transforms erweitern, ohne Runnel selbst zu ändern (z. B. SAP RFC, eine
Firmen-API, ein Treiber, den es nur für .NET gibt). Das Konzept (Kap. 7.3) sieht dafür ein prozessbasiertes Protokoll
vor; Go-Plugins (`plugin`-Paket) funktionieren unter Windows nicht (ADR 0002).

Anforderungen:

- **Beliebige Sprache:** Go als Referenz, dazu Python, C#/.NET und Node mit dünnen SDKs.
- **Große Datenmengen:** Plugin-Transforms verarbeiten Records in Batches als Strom, mit Backpressure wie die
  eingebauten Transforms (gepufferte Kanäle).
- **Windows-first:** keine offenen Ports, Prozesse sauber beenden (auch Kindprozesse), Pfade mit Leerzeichen und UNC.
- **Robust:** Ein abstürzendes Plugin lässt den Lauf scheitern, nicht den Server.
- **Ein Descriptor für alles:** Plugins beschreiben ihre Bausteine wie die eingebauten (ADR 0014); der Editor braucht
  keinen eigenen Code je Plugin.

Erwogen wurden:

| Variante | Bewertung |
|---|---|
| **`hashicorp/go-plugin`** (gRPC) | Bewährt (Terraform, Vault), binär und schnell, Prozessverwaltung inklusive. Aber: Lizenz MPL-2.0 (schwaches Copyleft, nicht MIT-artig, ADR 0004); unter Windows ein TCP-Port auf `127.0.0.1`, den andere lokale Prozesse erreichen (nur mit AutoMTLS geschützt); Plugins in anderen Sprachen brauchen gRPC, Protobuf, den Handshake und den Health-Service – das ist kein dünnes SDK mehr; Records nach ADR 0007 bräuchten ohnehin einen eigenen Protobuf-Werttyp; gRPC und Protobuf vergrößern `runnel.exe` deutlich |
| **WebAssembly** (z. B. wazero) | Sandbox und ein Format für alle Sprachen. Aber: Bibliotheken mit nativem Code oder eigener Laufzeit (SAP NW RFC SDK, .NET-Treiber, ODBC) laufen darin nicht – gerade die sind der Grund für Plugins |
| **HTTP-Server je Plugin** | Einfach zu testen, aber wieder ein Port je Plugin und eigene Absicherung |
| **JSON-RPC 2.0 über stdin/stdout** | Keine Abhängigkeit, kein Port, in jeder Sprache mit Bordmitteln; dasselbe Grundgerüst wie MCP (S53, ADR 0012). Nachteil: JSON ist langsamer als ein Binärformat |

Zur Größenordnung (gemessen beim Entwurf, ein Kern, `encoding/json`, Batch mit 500 Records zu je 5 Feldern, einmal
Host → Plugin → Host): Records als JSON-Objekte schaffen etwa 200.000 Records/s, als Arrays mit einmaligem Feldkopf
etwa 465.000 Records/s; die eingebaute Engine reicht eine Kette mit etwa 7,7 Mio. Records/s weiter. Ein Plugin-Transform
ist damit eine Größenordnung langsamer als ein eingebauter – für typische Mengen (einige Millionen Zeilen) sind das
Sekunden. Das Format der Records entscheidet über den Durchsatz mehr als die Bibliothek.

## Entscheidung

Runnel bekommt ein **eigenes Protokoll: JSON-RPC 2.0 über stdin/stdout** des Plugin-Prozesses. `hashicorp/go-plugin`
wird nicht verwendet; einige Ideen daraus (Handshake, Protokollversion, Log über stderr, Prozessverwaltung) werden
übernommen.

### Plugin und Ablage

- Ein Plugin ist ein Ordner **`plugins/<name>/`** im Projekt mit **`plugin.yaml`** und der ausführbaren Datei.
  Plugins gibt es **nur im Projekt**: Ein Projektordner enthält alles, was er zum Laufen braucht (Git, Deployment);
  einen Ordner für alle Projekte oder Plugin-Pfade in `project.yaml` gibt es nicht.

  ```yaml
  kind: Plugin
  apiVersion: runnel/v1
  name: sap-rfc
  version: 1.0.0
  namespace: sap                    # eigener Namensraum (ADR 0013); eingebaute sind reserviert
  executable: sap-rfc-plugin.exe    # relativ zum Ordner des Plugins
  args: []                          # optional, z. B. für Skripte: executable: python.exe, args: [main.py]
  ```

- Welche Typen ein Plugin anbietet, sagt es selbst mit `describe`; `plugin.yaml` nennt sie nicht noch einmal (eine
  zweite Liste würde nur auseinanderlaufen). Typen außerhalb des eigenen Namensraums sind ein Fehler beim Laden.
- `kind: Plugin` ist ein neuer Dateityp; `runnel validate` prüft ihn, der Editor zeigt ihn wie andere YAML-Dateien.

### Transport

- **Eine Nachricht je Zeile** (NDJSON): ein JSON-Objekt, UTF-8, abgeschlossen mit `\n` (ein `\r` davor wird
  akzeptiert). Höchstens **16 MiB je Zeile**; größere Nachrichten beenden das Plugin mit Fehler.
- **stdin:** Host → Plugin. **stdout:** Plugin → Host, ausschließlich Protokoll. **stderr:** Log des Plugins, je Zeile
  ein Eintrag im Log des Laufs (Präfix `<plugin>: `; Stufe per Präfix `DEBUG`/`INFO`/`WARN`/`ERROR`, sonst `INFO`;
  höchstens 64 KiB je Zeile).
- Der Host liest stdout **ununterbrochen** in einer eigenen Goroutine; so blockiert das Plugin beim Schreiben nie
  länger, als die Flusssteuerung (unten) erlaubt.

### Handshake

- Der Host startet das Plugin mit der Umgebungsvariablen `RUNNEL_PLUGIN=1` (wie das „Magic Cookie“ von go-plugin);
  ohne sie gibt das Plugin einen kurzen Hinweis aus und endet – direkt gestartet, wartet es nicht stumm auf stdin.
- Erste Nachricht ist **`initialize`**:
  `{"protocolVersion": 1, "encodings": ["json"], "runnel": {"version": "0.7.0"}}` →
  `{"protocolVersion": 1, "encoding": "json", "name": "sap-rfc", "version": "1.0.0"}`.
  Passt die Version nicht, wird das Plugin mit Meldung nicht geladen. Neue Methoden und Felder sind
  abwärtskompatibel; eine inkompatible Änderung erhöht `protocolVersion`.
- `encodings` hält die Tür für ein Binärformat (MessagePack oder CBOR) offen; vorerst gibt es nur `json`.

### Methoden

| Methode | Richtung | Zweck |
|---|---|---|
| `initialize` | Host → Plugin | Handshake (oben) |
| `describe` | Host → Plugin | Descriptoren der Actions und Transforms (ADR 0014: `type`, `kind`, `category`, `label`, `description`, `schema`, später `icon`, `ports`, `uiHints`) |
| `action.execute` | Host → Plugin | eine Action ausführen: `type`, `config` (aufgelöste `with:`-Werte), `context` (Lauf-ID, Action-ID, Projektordner, Environment-Name) → `outputs` |
| `transform.open` / `.batch` / `.end` | Host → Plugin | Strom eines Transforms (unten) |
| `transform.emit` / `.closed` | Plugin → Host | Records auf einen Ausgang; Ende des Stroms |
| `$/ack` | beide | Flusssteuerung (unten) |
| `$/cancelRequest` | Host → Plugin | Abbruch (wie bei LSP); das Plugin antwortet mit dem Fehlercode `-32800` |
| `shutdown` | Host → Plugin | Ende; danach schließt der Host stdin |

Der Host prüft `config` vor dem Aufruf gegen das `schema` des Descriptors – Plugins bekommen nur gültige
Konfigurationen, Fehler erscheinen wie bei eingebauten Bausteinen mit Position in der Datei.

### Transforms als Strom

```text
→ transform.open    {"stream": 1, "type": "sap.table-input", "config": {…}, "batchSize": 500,
                     "inputs": [{"port": "main", "fields": [{"name": "kdnr", "type": "int"}, …]}]}
← (Antwort)         {"window": 4}
→ transform.batch   {"stream": 1, "port": "main", "rows": [[4711, "Müller GmbH", "1234.56"], …]}
← transform.emit    {"stream": 1, "port": "main", "fields": [ … nur beim ersten Mal oder bei Änderung … ], "rows": [[…], …]}
← transform.emit    {"stream": 1, "port": "error", "rows": [[…, "invalid date", "datum", "sap.table-input"]]}
→ transform.end     {"stream": 1}
← transform.closed  {"stream": 1}            // oder {"stream": 1, "error": {"message": "…"}}
```

- **Batches** und Notifications statt Anfrage/Antwort je Batch: Ein Transform kann filtern, zusammenfassen und auf
  mehrere Ausgänge schreiben; Eingabe-Transforms (ohne `inputs`) schreiben nur `emit`.
- **Feldkopf einmal, Records als Arrays:** `fields` (Name und Typ nach ADR 0007) stehen in `open` bzw. beim ersten
  `emit` eines Ausgangs und nur erneut, wenn sie sich ändern; `rows` folgen dieser Reihenfolge.
- **Werte:** `null`; `bool`; `int` als JSON-Zahl im Bereich von int64; `float` als Zahl; `decimal` als String
  (`"1234.56"`, exakt); `date` als `"2026-11-21"`; `timestamp` als RFC 3339 mit Offset; `bytes` als Base64; Listen und
  Objekte als JSON. Ohne Typ (`any`) gilt der JSON-Wert. Ein Wert, der nicht zum Typ passt, ist ein Fehler des Plugins.
- **Fehler je Record** gehen wie bei eingebauten Transforms auf den Ausgang `error` mit `_error`, `_errorField`,
  `_errorTransform` (KONZEPT Kap. 6). Ein Fehler, der den ganzen Strom betrifft, steht in `transform.closed`.
- **Flusssteuerung mit Krediten:** Die Antwort auf `open` nennt `window` – so viele Batches darf der Host senden,
  bevor das Plugin mit `$/ack {"stream": 1, "batches": n}` neue freigibt. Umgekehrt gibt der Host Ausgaben frei
  (Anfangsfenster in `open`: `"emitWindow": 4`). So füllt keine Seite die Pipe der anderen, und die Backpressure der
  Engine reicht bis ins Plugin.
- Ein Plugin-Prozess bedient **mehrere Ströme** gleichzeitig (mehrere Transforms desselben Plugins in einer Pipeline);
  `stream` ordnet die Nachrichten zu.

### Prozesse

- **Ein Prozess je Plugin und Lauf**, gestartet beim ersten Baustein des Plugins; Actions und Transforms des Plugins
  in diesem Lauf teilen ihn, mit dem Lauf endet er. Läufe sind so voneinander getrennt (kein Zustand aus einem früheren
  Lauf, ein hängendes Plugin trifft nur seinen Lauf). Ein Pool über Läufe hinweg ist nicht vorgesehen; braucht es ihn
  (z. B. für viele kurze HTTP-Läufe), gibt es dafür eine neue ADR.
- `describe` wird beim Laden des Projekts aufgerufen und zwischengespeichert, solange sich `plugin.yaml` und die
  ausführbare Datei (Größe, Änderungszeit) nicht ändern; Palette, `runnel catalog` und die Prüfung nutzen den Cache.
- **Zeitgrenzen:** `initialize` und `describe` je 10 s; nach `shutdown` 5 s, dann wird der Prozess beendet.
- **Windows:** Der Prozess läuft in einem **Job Object** mit „kill on close“ – endet Runnel oder wird der Lauf
  abgebrochen, enden auch Kindprozesse des Plugins. Unter Linux eine eigene Prozessgruppe.
- **Absturz** (Prozess endet, ungültiges JSON, Zeile zu lang): Die laufenden Bausteine des Plugins scheitern mit einer
  Meldung samt den letzten Zeilen von stderr; der Lauf behandelt das wie jeden Fehler (Fehlerpfad, Retry), der Server
  läuft weiter.

### Nur Host → Plugin

Der Host ruft das Plugin; das Plugin ruft den Host nicht (außer `$/ack` und den Notifications seines Stroms). Es gibt
**keine Dienste des Hosts** für Plugins – keinen Zugriff auf Secret-Store, Verbindungen oder Ausdrücke. Alles, was ein
Baustein braucht, steht aufgelöst in seiner `config` (Variablen und Ausdrücke wertet der Host vorher aus, wie bei
eingebauten Bausteinen). Dienste des Hosts wären eine Erweiterung mit neuer ADR.

### Gemeinsamer Unterbau

Lesen und Schreiben von JSON-RPC über einen Datenstrom (Zeilen, IDs, Notifications, Abbruch) kommt in ein eigenes
Paket (`internal/jsonrpc`), das auch der MCP-Server (S53) nutzt.

## Sicherheit

- **Plugins sind Code mit den Rechten von Runnel** (beim Dienst: dessen Konto). Es gibt keine Sandbox. Vertrauen
  entsteht durch das Ablegen im Projektordner; wer dort schreiben darf, darf ohnehin Workflows ändern. Die Doku sagt
  das deutlich. Ergänzt durch die Freigabe je Projekt (Nachtrag vor S49).
- **Kein Port:** Die Kommunikation läuft nur über die Pipes des Kindprozesses; kein anderer Prozess kann sich
  dazwischenschalten.
- **Umgebung:** Das Plugin erhält die Umgebung von Runnel ohne Variablen, die Runnel selbst für Geheimnisse nutzt
  (z. B. Tokens der HTTP-Trigger); Geheimnisse bekommt es nur, wenn sie in seiner `config` stehen.
- **Grenzen:** 16 MiB je Nachricht, 64 KiB je Log-Zeile, Zeitgrenzen für Start und Ende, Kredite für die Datenmenge
  im Speicher. Ausgaben des Plugins (Typen, Ports, Namensraum) prüft der Host, statt ihnen zu vertrauen.
- **Fehlermeldungen** der API nennen das Plugin und die Meldung, aber keine Pfade oder Stacktraces des Hosts;
  vollständige Details stehen im Log.

## Konsequenzen

- **Positiv:** keine neue Abhängigkeit; Plugins in jeder Sprache mit wenig Code (das Go-SDK in S51 kapselt Handshake,
  Kredite und Typen); lesbar und von Hand testbar; derselbe Unterbau wie MCP.
- **Negativ:** JSON kostet Durchsatz – Plugin-Transforms sind etwa eine Größenordnung langsamer als eingebaute. Die
  Kredite machen SDKs etwas aufwendiger als reines Anfrage/Antwort.
- **Messbar:** S50 bekommt ein Benchmark mit einem Echo-Plugin; Ziel sind mindestens **300.000 Records/s** mit fünf
  Feldern. Wird es deutlich verfehlt, folgt ein Binärformat über `encodings` als eigener Schritt – nicht vorher.
- **Roadmap:** S49 umfasst Ablage, `kind: Plugin`, Handshake, Prozesse, `describe` und `action.execute`; S50 die Ströme
  mit Krediten; S51 das Go-SDK; S52 die Anzeige im Editor (Fehler beim Laden sichtbar).
- KONZEPT Kap. 2.1 und 7.3 sind angepasst (Ordner `plugins/`, Methodennamen, `kind: Plugin`, `provides` und Plugin-Pfade in `project.yaml` entfallen).

## Abstimmung

Mit dem Maintainer am 2026-11-21 abgestimmt: Plugins nur unter `plugins/` im Projekt, ein Prozess je Plugin und Lauf,
keine Dienste des Hosts für Plugins.

## Nachtrag 2026-11-22 (vor S49): Freigabe, Ausgänge, Schema, Fähigkeiten, Pfade

Präzisierungen vor der Umsetzung, abgestimmt mit dem Maintainer.

### 1. Freigabe je Projekt

`describe` startet ein Plugin schon beim Laden des Projekts. Ohne weitere Regel würde ein fremdes Projekt – geklont,
heruntergeladen, als Anhang erhalten – Code ausführen, sobald man es nur mit `runnel validate` prüft oder im Editor
öffnet. Plugins werden deshalb erst gestartet, wenn sie **auf diesem Rechner für dieses Projekt freigegeben** sind.

- **Was freigegeben wird:** je Plugin der absolute Projektordner (unter Windows ohne Unterscheidung der Groß- und
  Kleinschreibung), der Name des Plugins und ein **SHA-256 über den Inhalt des Plugin-Ordners** (`plugin.yaml`, die
  ausführbare Datei, DLLs, Skripte; Dateinamen und Inhalte, sortiert). Ändert sich eine Datei, ist die Freigabe
  ungültig und muss erneuert werden. Liegt die ausführbare Datei außerhalb des Ordners (z. B. `executable: python.exe`
  mit `args: [main.py]`), zählt sie nicht mit; die Skripte im Ordner schon. Grenzen für das Hashen: 1 GB und 10.000
  Dateien je Plugin, darüber wird das Plugin nicht geladen.
- **Wo die Freigaben liegen:** nie im Projekt (ein fremdes Projekt könnte sie sonst mitbringen).
  - Für Benutzer in `<Konfigurationsordner>/runnel/trusted-plugins.json` (`os.UserConfigDir`, unter Windows
    `%AppData%`).
  - Für den Windows-Dienst in `%ProgramData%\runnel\trust\plugins.json`, geschützt wie der Ordner des Zertifikats:
    nur Administratoren, SYSTEM und das Dienstkonto.
  - Runnel liest beide Dateien; die des Dienstes schreiben nur Administratoren.
- **Freigeben:** `runnel plugins trust [projektordner]` zeigt je Plugin Name, Version, Namensraum, ausführbare Datei
  und Hash und fragt nach. `--yes` gilt für Skripte. `--service` schreibt in die Datei des Dienstes und braucht
  Administratorrechte. `runnel plugins untrust` nimmt die Freigabe zurück; `runnel plugins` zeigt den Stand.
  - Der Editor zeigt nicht freigegebene Plugins als Hinweis mit „Trust“ (S52, nach Mockup). Wer den Editor erreicht,
    darf ohnehin Workflows ändern; die Abwehr von fremden Webseiten und DNS-Rebinding gilt auch hier.
- **Ohne Freigabe:**
  - Das Plugin wird nicht gestartet, auch nicht für `describe`.
  - `runnel validate` und der Editor melden Bausteine seines Namensraums als Fehler mit Hinweis, z. B.
    `plugin "sap-rfc" is not trusted on this computer; check it and run "runnel plugins trust"`.
  - Ein Lauf, der sie braucht, startet nicht (Exit-Code 2).
  - Andere Workflows des Projekts laufen normal.

### 2. Ausgänge wie bei `core.switch`

Pipeline-Links kennen keine Ports, nur Daten- und Fehler-Links; weitere Ausgänge laufen über Konfigurationsfelder mit
`format: runnel-target` (wie `to:` bei `core.switch`, ADR 0021). Für Plugins gilt dasselbe:

- `transform.emit` nennt statt `port` ein Ziel: `"to": "main"` (alle Daten-Links ohne eigenes Ziel), `"to": "error"`
  (Fehler-Link) oder die **ID eines Transforms**, die in der `config` des Plugins in einem Feld mit `runnel-target`
  steht. Die Engine prüft, dass dorthin ein Daten-Link führt; sonst ist es ein Fehler des Plugins.
- `transform.open` meldet nur einen Eingang (`inputs` mit einem Eintrag). Mehrere Eingänge (Info-Links, S23) folgen
  mit einer eigenen Erweiterung.

### 3. Schema und „Felder ermitteln“ (ADR 0007, S46)

Zwei **optionale** Methoden, die ein Plugin in `describe` je Transform ankündigt (`"init": true`,
`"detectFields": true`):

| Methode | Zweck |
|---|---|
| `transform.init` | `type`, `config`, Schema des Eingangs → Schema des Ausgangs (Felder mit Typ, `open`), Fehler der Konfiguration mit Feldnamen. Wird bei `validate`, im Editor und vor dem Lauf aufgerufen, Zeitgrenze 10 s. |
| `transform.detectFields` | `type`, `config`, `limit` → vorgeschlagene Felder wie bei `csv.input` (S46). |

Ohne `init` ist das Schema hinter dem Plugin offen, wie heute hinter `core.from-result`. Die Prüfung von Feldnamen
endet dann dort.

### 4. Weitere Angaben im Descriptor

`describe` liefert je Baustein zusätzlich, alles optional:

- `example`: YAML für die Hilfe im Panel (E14b); der Host prüft es gegen das Schema.
- `recordErrors`: Der Transform darf einen Fehler-Link haben.
- `parallel`: Der Transform erlaubt `copies`.
- `init` und `detectFields`, siehe oben.

`icon` folgt erst mit einem Format ohne HTML und Skript: nur SVG-Pfaddaten (`d`, höchstens 4 KB), gezeichnet im
Rahmen der eingebauten Icons. Bis dahin zeigt der Editor das allgemeine Symbol.

### 5. Relative Pfade (ADR 0020)

- Das **Arbeitsverzeichnis** des Plugin-Prozesses ist der Projektordner. Relative Pfade in der `config` gelten damit
  wie bei eingebauten Bausteinen; der Projektordner steht zusätzlich in `context.projectDir`.
- `executable` und Dateien in `args` sind relativ zum Ordner des Plugins (wie bisher).

### Sicherheit

- **Ohne Freigabe** führt Runnel keinen Code eines Plugins aus, auch nicht beim Prüfen oder Anzeigen.
- **Jede Änderung im Plugin-Ordner** macht die Freigabe ungültig.
- **Ort der Freigaben:** Sie liegen außerhalb des Projekts; die des Dienstes ist nur für Administratoren schreibbar.
- **Grenzen:** Hashen bis 1 GB und 10.000 Dateien je Plugin, `init` höchstens 10 s, Icons nur als Pfaddaten bis 4 KB.
- **Ziele von `emit`** prüft die Engine gegen die Links, statt ihnen zu vertrauen.
- **Tests** gehören zu S49:
  - nicht freigegeben,
  - Freigabe nach Änderung einer Datei ungültig,
  - Datei des Dienstes nicht schreibbar ohne Administratorrechte (manuell, Testplan),
  - `emit` auf ein fremdes Ziel.

### Folgen für die Roadmap

- S49 enthält die Freigabe mit `runnel plugins` (CLI).
- S50 enthält Ziele statt Ports und `transform.init`.
- S52 enthält den Hinweis mit „Trust“ im Editor (Mockup vorher).
