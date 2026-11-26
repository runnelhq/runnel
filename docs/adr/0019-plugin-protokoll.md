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

## Nachtrag 2026-11-24 (vor S49): Connections, Parallelität, Betriebssysteme, SDKs, Version

Abgestimmt mit dem Maintainer. Seit der ADR sind Connections und Secrets (ADR 0022) hinzugekommen; außerdem ist
festgelegt, wie Plugins gebaut, verteilt und versioniert werden.

### 1. Connections und Secrets

„Keine Dienste des Hosts“ bleibt: Ein Plugin fragt den Host nie nach Connections oder Secrets. Der Host löst sie
vorher auf, wie bei eingebauten Bausteinen.

- **Eingebaute Connection-Typen:** Ein Feld mit `format: runnel-connection` (und `x-runnel-connection` mit den
  erlaubten Typen) im Schema eines Plugin-Bausteins nimmt den Namen einer Connection des Projekts. In der `config`
  steht sie **aufgelöst**: `{"name": "erp", "type": "sqlserver", …Felder der Connection…}`, Secrets im Klartext.
- **Eigene Connection-Typen:** `describe` darf zusätzlich `connectionTypes` liefern, je Typ `type`, `label`,
  `description` und `schema` (wie bei eingebauten, ADR 0022). Der Name des Typs ist der Namensraum des Plugins oder
  beginnt mit `<namensraum>-` (z. B. `ldap` oder `ldap-ad`). Connection-Dateien mit diesem Typ prüft der Host gegen
  das Schema; Secrets nur in Feldern mit `format: runnel-secret`, sonst gelten dieselben Regeln wie für eingebaute
  Typen. Ohne Freigabe des Plugins ist der Typ unbekannt (Fehler mit Hinweis auf `runnel plugins trust`).
- **Testen einer Connection** (S39c1): optional mit der Methode `connection.test` (`type`, aufgelöste Connection →
  `{ok, message, version}`), angekündigt in `connectionTypes` mit `"test": true`; sonst bietet der Editor keinen Test an.
- **Maskierung:** Secrets, die der Host einem Plugin übergibt, maskiert er in allem, was vom Plugin zurückkommt: Zeilen
  auf stderr, Fehlermeldungen, `outputs`. So landen sie weder im Log noch in der Run-Historie.

### 2. Parallele Anfragen in einem Prozess

- Die Antwort auf `initialize` darf `"concurrency": n` nennen: so viele Anfragen (`action.execute`, `transform.init`,
  `transform.detectFields`, `connection.test`) bearbeitet das Plugin gleichzeitig. **Ohne Angabe gilt 1**, und der
  Host reiht weitere Anfragen ein. Ein einfaches Plugin muss also keine Parallelität beherrschen; das SDK meldet einen
  höheren Wert.
- Offene Ströme von Transforms zählen nicht mit: Eine Pipeline braucht alle ihre Ströme gleichzeitig, deren Menge
  regeln die Kredite.

### 3. Ausführbare Datei je Betriebssystem

`executable` ist ein Text (für alle Systeme) oder eine Zuordnung nach Betriebssystem:

```yaml
executable:
  windows: bin/iban-plugin.exe
  linux: bin/iban-plugin
```

Schlüssel sind die Namen von Go (`windows`, `linux`, `darwin`). Fehlt der Eintrag für das laufende System, wird das
Plugin mit Meldung nicht geladen. Der Hash der Freigabe umfasst weiter den ganzen Ordner, also alle Varianten.

### 4. SDKs in eigenen Repositorys

Das Go-SDK kommt nicht als `pkg/sdk` in dieses Repository, sondern in ein **eigenes Repository** (`runnelhq/sdk-go`,
Namen nach ADR 0013, Nachtrag 2026-11-24) mit eigenen Versionen; SDKs für Python, C#/.NET, Rust oder Node folgen nach demselben
Muster. Verbindlich ist das Protokoll, nicht ein SDK:

- Die **Beschreibung des Protokolls** liegt in diesem Repository (`docs/dev/PLUGINS.md`, mit S49) und gilt für alle SDKs.
- Die Tests und das Beispiel-Plugin in diesem Repository nutzen **kein SDK**, sondern sprechen das Protokoll direkt
  (ein kleines Go-Programm unter `testdata`). Damit prüft Runnel das Protokoll und nicht ein SDK.
- Das Repository des SDK legt der Maintainer an; S51 setzt es dort um.

### 5. Version und Kompatibilität

- Das Protokoll startet direkt mit **`protocolVersion: 1`**, ohne Vorschauphase.
- Innerhalb von Version 1 sind Änderungen nur **additiv**: neue optionale Felder, neue optionale Methoden, die ein
  Plugin in `initialize` oder `describe` ankündigt (wie `init`, `detectFields`, `test`, `concurrency`). Beide Seiten
  **ignorieren unbekannte Felder**. Ein Plugin, das eine Methode nicht kennt, antwortet mit dem JSON-RPC-Fehler
  `-32601`; der Host behandelt das wie „nicht angeboten“.
- Eine inkompatible Änderung erhöht `protocolVersion`. Der Host kann dann mehrere Versionen anbieten
  (`initialize` mit `protocolVersions: [1, 2]`); ein Plugin, dessen Version er nicht kennt, wird mit Meldung nicht
  geladen.
- Eine **Mindestversion von Runnel** in `plugin.yaml` gibt es vorerst nicht (Backlog). Bis dahin erkennt ein Plugin
  die Version des Hosts an `runnel.version` in `initialize`.

### Sicherheit

- **Secrets** gehen nur aufgelöst in die `config` von Bausteinen, die die Connection nennen. Der Host maskiert sie in
  allem, was vom Plugin zurückkommt (Punkt 1).
- **Eigene Connection-Typen** gibt es nur von freigegebenen Plugins. Ihr Schema prüft der Host, und die Namen sind auf
  den Namensraum des Plugins beschränkt.
- **Parallelität:** Ohne Angabe schickt der Host höchstens eine Anfrage gleichzeitig, damit ein einfaches Plugin nicht
  überlastet wird (Punkt 2).
- **Tests mit S49:**
  - Maskierung eines Secrets in stderr und Fehlern,
  - `executable` ohne Eintrag für das System,
  - Einreihen ohne `concurrency`,
  - unbekannte Methode (`-32601`).

### Folgen für die Roadmap

- **S49:**
  - `executable` je System, `concurrency`, Maskierung, Connections eingebauter Typen in der `config`.
  - Die Beschreibung des Protokolls `docs/dev/PLUGINS.md`.
- **S49c (neu, zuerst S49b genannt):** Connection-Typen aus Plugins mit `connection.test`.
- **S51:** Go-SDK im eigenen Repository.
- **Backlog:**
  - Mindestversion von Runnel in `plugin.yaml`,
  - SDKs für Python, C#/.NET, Rust und Node in eigenen Repositorys.

## Nachtrag 2026-11-24 (S49a): Datei und Freigabe

S49 ist geteilt: S49a (Datei `plugin.yaml` und Freigabe), S49b (Protokoll und Actions), S49c (Connection-Typen; im
Nachtrag davor „S49b“ genannt). Präzisierungen bei der Umsetzung von S49a:

- **`plugin.yaml`:** `name` wie der Ordner, `version` nach SemVer, `namespace` und `name` aus Kleinbuchstaben, Ziffern
  und `-`; ein Namensraum, den zwei Plugins nennen, ist bei beiden ein Fehler, keines wird geladen. Ein Fehler in einer
  Plugin-Datei ist ein Fehler des Projekts, wie bei Connections.
- **`executable`:** ein Pfad im Ordner des Plugins (ohne `..`), ein absoluter Pfad oder ein Befehl des Rechners (z. B.
  `python`); Befehle und absolute Pfade außerhalb des Ordners zählen nicht zum Hash.
- **Hash:** SHA-256 über alle Dateien des Ordners in fester Reihenfolge, je Datei Pfad (mit `/`), Länge und Inhalt;
  Verknüpfungen und andere besondere Dateien lehnt Runnel ab. Ohne Cache: Ab S49b wird er beim Laden des Projekts
  zwischengespeichert, solange sich Größe und Änderungszeit der Dateien nicht ändern.
- **Datei des Dienstes:** `%ProgramData%\runnel\trust\plugins.json`. Ändern dürfen sie nur Administratoren und SYSTEM;
  lesen dürfen sie alle angemeldeten Benutzer und das Dienstkonto (die Freigaben sind kein Geheimnis, Schutz gilt dem
  Schreiben). Unter Linux gibt es nur die Datei des Benutzers (`$XDG_CONFIG_HOME/runnel/trusted-plugins.json`).
- **Rückfrage:** `runnel plugins trust` zeigt Name, Version, Namensraum, Ordner, Programm und Hash und fragt je Plugin;
  nur `y`/`yes` gibt frei. Ohne Eingabe (z. B. in Skripten) bleibt das Plugin gesperrt; dort gilt `--yes`.

## Nachtrag 2026-11-24 (S49b1): Host

S49b ist geteilt: S49b1 (Host als Bibliothek, `runnel plugins` zeigt die Typen), S49b2 (Actions in Workflows, Prüfung,
Editor, Connections). Präzisierungen:

- **`action.execute`** antwortet mit `{"output": …}` (Text, Zahl, Liste, Objekt; im Workflow `result.output`). Ein Fehler
  der Action ist eine JSON-RPC-Fehlerantwort; ihre `message` ist die Meldung der Action.
- **Abbruch:** Endet der Kontext einer Anfrage (Abbruch, Timeout), schickt der Host `$/cancelRequest` und wartet nicht
  auf die Antwort. Das Plugin kann sie mit `-32800` beantworten; der Host verwirft sie.
- **Ende:** `shutdown`, dann schließt der Host stdin; nach 5 s beendet er den Prozessbaum. Endet ein Plugin während
  einer Anfrage, scheitert sie mit „the plugin ended unexpectedly; last lines: …“ (die letzten fünf Zeilen von stderr,
  maskiert).
- **stderr:** je Zeile ein Eintrag im Log mit `<plugin>: ` davor; `DEBUG`, `INFO`, `WARN` oder `ERROR` am Anfang (mit
  Leerzeichen oder `:` danach) setzen die Stufe. Zeilen über 64 KiB werden abgeschnitten (`…`).
- **Anfragen des Plugins** an den Host beantwortet er mit `-32601`; Notifications ohne bekannte Methode verwirft er.
- **Prüfung von `describe`:** gültige Typnamen im Namensraum des Plugins, jeder Typ einmal, `kind` (fehlend oder
  `action`), ein `schema`; sonst wird das Plugin nicht genutzt. `initialize` muss den Namen aus `plugin.yaml` nennen.
- **`runnel plugins`** startet freigegebene Plugins, um ihre Typen zu zeigen; nicht freigegebene nie.
- **Tests:** Das Test-Binary ist selbst das Plugin (`internal/plugin/plugintest`, gestartet mit `RUNNEL_TEST_PLUGIN`);
  so prüfen die Tests das Protokoll ohne SDK und ohne `go build` auf Linux und Windows.

## Nachtrag 2026-11-24 (S49b2): Actions in Workflows

- **Registry je Projekt:** Vor jeder Prüfung und jedem Lauf ergänzt Runnel die eingebauten Actions um die der
  freigegebenen Plugins des Projekts (`describe`). Das Ergebnis je Plugin wird zwischengespeichert, solange sich Namen,
  Größen und Änderungszeiten seiner Dateien und die Dateien der Freigaben nicht ändern; erst dann bildet Runnel den Hash
  neu und startet `describe` erneut. Der Katalog des Editors (`/catalog`) enthält die Actions der Plugins, die Palette
  zeigt sie damit ohne eigenes Mockup in ihrer Kategorie (unbekannte Kategorie: `general`).
- **Nicht nutzbare Plugins:** Ist ein Plugin nicht freigegeben, seit der Freigabe geändert oder startet es nicht, melden
  `validate`, Editor und Lauf seine Typen an der Action mit dem Grund, z. B.
  `action "say": plugin "demo" is not trusted on this computer; check it and run "runnel plugins trust"`; ein Lauf
  startet nicht.
- **Prüfung der `config`:** vor dem Lauf gegen das Schema aus `describe` – Pflichtfelder und, bei
  `additionalProperties: false`, unbekannte Felder. Typen prüft das Plugin, weil Werte noch Variablen enthalten können.
- **Prozess je Lauf:** Der erste Aufruf eines Plugins in einem Lauf prüft die Freigabe erneut und startet es; am Ende
  des Laufs endet es (`shutdown`). Aufgerufene Workflows (`core.run-workflow`) haben eigene Prozesse. Die Zeilen von
  stderr erscheinen im Log der Action, die das Plugin zuletzt aufgerufen hat.
- **Connections:** Ein Feld mit `format: runnel-connection` erhält die aufgelöste Connection
  (`{"name", "type", …Felder}`); ihre Secrets und alle anderen Secrets des Laufs maskiert der Host in Log, Fehlern
  und `output`.
- **Umgebung:** Variablen, die Tokens der HTTP-Trigger des Projekts enthalten (`auth: { token: ${env:NAME} }`), fehlen
  in der Umgebung der Plugins.
- **Pipelines** können noch keine Plugins nutzen (Transforms folgen mit S50).

## Nachtrag 2026-11-24 (S49c): Connection-Typen aus Plugins

- **`describe`** liefert neben `actions` optional `connectionTypes`: je Typ `type`, `label`, `description`, `schema` und
  `test`. Der Host prüft: Name gleich dem Namensraum oder `<namensraum>-…` (Kleinbuchstaben, Ziffern, `-`), jeder
  einmal, mit `schema`; sonst wird das Plugin nicht genutzt (wie bei Actions außerhalb des Namensraums).
- **Registry je Projekt:** Wie bei Actions ergänzt Runnel die eingebauten Connection-Typen um die der freigegebenen
  Plugins (derselbe Zwischenspeicher je Plugin). Eingebaute Typen gehen vor; ein Plugin kann keinen überschreiben.
- **Prüfung der Dateien:** gegen das Schema aus `describe` – Pflichtfelder und, bei `additionalProperties: false`,
  unbekannte Felder; Secrets nur in Feldern mit `format: runnel-secret`. Typen der Werte prüft das Plugin, weil sie
  Variablen enthalten können. Ist das Plugin nicht freigegeben oder geändert, steht der Grund an `type`, z. B.
  `plugin "demo" is not trusted on this computer; check it and run "runnel plugins trust"`.
- **Editor:** Das Schema der Connection-Dateien (`/schemas/connection`) enthält die Typen der Plugins als Auswahl und
  ihre Felder als Variante; das Formular zeigt sie damit ohne eigenes Mockup.
- **`connection.test`:** Params `{"type", "connection"}` mit der aufgelösten Connection (`{"name", "type", …Felder}`),
  Antwort `{"ok", "message", "version"}`. `POST /connections/{name}/test` startet dafür das Plugin (Freigabe erneut
  geprüft), ruft die Methode auf und beendet es; Zeitgrenze wie bei eingebauten Typen (15 s). Secrets der Connection
  sind in der Meldung maskiert. Ohne `"test": true` antwortet der Server mit `ok: false` und dem Hinweis, dass der Typ
  nicht getestet werden kann; der Knopf im Formular bleibt (wie bei eingebauten Typen ohne Test).
- **Connections in der `config`:** Felder mit `format: runnel-connection` nehmen auch Connections der Typen von
  Plugins, wenn `x-runnel-connection` sie nennt; sie kommen aufgelöst und maskiert wie die eingebauten an.
- `runnel plugins` zeigt die Connection-Typen eines Plugins mit `connection` neben den Actions.

## Nachtrag 2026-11-24 (S50a): Transforms als Strom

S50 ist geteilt: S50a (Strom, dieser Nachtrag) und S50b (`transform.init`, `transform.detectFields`). Präzisierungen
bei der Umsetzung von S50a:

- **`describe`** liefert `transforms` mit Descriptoren wie die eingebauten (`type`, `category`, `label`,
  `description`, `schema`, optional `example`, `recordErrors`, `parallel`). Typen nur im eigenen Namensraum, jeder
  einmal, mit `schema`; unbekannte Kategorie wird `transform`. Ein Typ darf zugleich Action und Transform sein.
- **`transform.open`** (Anfrage): `stream`, `type`, `config` (geprüft und aufgelöst wie bei Actions, Connections als
  Objekt), `context` (`runId`, `transformId`, `projectDir`, `environment`) und `emitWindow`. Antwort `{"window": n}`
  (1 bis 64, ohne Angabe 1). `inputs` mit dem Schema des Eingangs entfällt bis S50b: Der Feldkopf kommt mit den Batches.
- **`transform.batch`** (Notification): `stream`, `rows` und `fields` (Name und Typ), wenn sich die Felder gegenüber
  dem letzten Batch geändert haben. Records mit anderen Feldern beginnen einen neuen Batch. Je Batch ein Kredit; der
  Plugin gibt sie mit `$/ack {"stream", "batches"}` zurück.
- **`transform.end`** (Notification) nach dem letzten Batch. Ein Eingabe-Transform (ohne eingehende Links) bekommt
  es sofort nach `open`; er schreibt, bis er fertig ist, und schließt dann.
- **`transform.emit`** (Notification vom Plugin): `stream`, `to`, `fields`, `rows`. Ohne `fields` gelten die des
  letzten `emit` (nicht des Ziels). `to` ist
  - `main` (oder leer): alle Daten-Links, die nicht zu einem Ziel der Konfiguration führen;
  - `error`: der Fehler-Link; die Records tragen `_error` und `_errorField`, `_errorTransform` setzt der Host. Ohne
    Fehler-Link scheitert der Transform mit der Meldung des ersten Records (wie eingebaute Transforms);
  - die ID eines Transforms aus einem Feld mit `format: runnel-target` der Konfiguration. Ein anderes Ziel ist ein
    Fehler des Plugins.
  Je `emit` ein Kredit des Hosts (`emitWindow`, 4); der Host gibt ihn mit `$/ack` zurück, sobald die Records bei den
  folgenden Transforms sind. Mehr `emit` als Kredite beenden den Strom mit Fehler.
- **`transform.closed`** (Notification vom Plugin): `stream`, optional `error.message`. Mit Fehler scheitert der
  Transform und mit ihm die Pipeline (Meldung maskiert).
- **Werte** nach ADR 0007 statt der Namen aus der ADR: `string`, `int`, `decimal` (Text), `float`, `bool`, `date`
  (`2026-11-24`), `datetime` (RFC 3339), `binary` (Base64), `json`; ohne Typ der JSON-Wert. Ein Wert, der nicht zum
  Typ passt, beendet den Strom mit Fehler (`row 3, field "amount": … is not a value of type decimal`).
- **Records sind Daten:** Der Host maskiert Log-Zeilen und Fehlermeldungen, nicht die Werte der Records (wie bei
  eingebauten Transforms).
- **Prozess:** wie bei Actions einer je Plugin und Lauf; alle Transforms und Kopien (`copies`) des Plugins in einer
  Pipeline teilen ihn mit eigenen `stream`-Nummern. Endet der Prozess, scheitern seine Ströme mit den letzten Zeilen
  von stderr.
- **Abbruch:** Bricht der Lauf ab, sendet der Host nichts mehr und beendet den Prozess mit dem Lauf.
- **Leistung:** Benchmark `BenchmarkPluginTransform` (200.000 Records mit fünf Feldern durch das Test-Plugin und
  zurück, mit Start des Plugins) erreicht 310.000–330.000 Records/s auf vier Kernen, mit einem bewusst einfachen
  Test-Plugin ohne SDK. Das Ziel ist erreicht, ein Binärformat ist nicht nötig.

## Nachtrag 2026-11-24 (S50b): `transform.init` und `transform.detectFields`

- **Ankündigung** je Transform in `describe`: `"init": true`, `"detectFields": true`. Ohne `init` ist das Schema
  hinter dem Transform offen; ohne `detectFields` bietet der Editor „Detect fields“ nicht an.
- **`transform.init`** (Anfrage): `type`, `config`, `input` (Schema des Eingangs, `{"fields": [{"name", "type"}],
  "open"}`; ohne Eingang leer und geschlossen), `targets` (IDs der Transforms, zu denen Daten-Links führen). Antwort:
  `output` (Schema wie `input`; fehlt es, ist der Ausgang offen) und `problems` (`path` in `with`, `message`,
  `warning`). Fehler stehen mit Position im Feld der Datei, wie bei eingebauten Transforms; Felder des Schemas haben
  Namen, jeden einmal, und Typen nach ADR 0007.
  - **Ohne Secrets:** Felder mit `runnel-secret` kommen leer, Connections als Name – `init` prüft, verbindet aber
    nicht. Variablen sind beim Prüfen ohne Lauf noch nicht aufgelöst.
  - **Eigener Prozess:** Runnel startet das Plugin dafür kurz (Freigabe erneut geprüft) und beendet es, Zeitgrenze
    10 s. Die Antwort wird je Plugin (Dateien und Freigaben) und Anfrage zwischengespeichert (höchstens 1.024
    Einträge), damit Editor, `validate` und Lauf das Plugin nicht bei jeder Prüfung starten.
  - Ein Fehler beim Start oder in `init` ist ein Fehler der Prüfung am Transform; der Lauf startet nicht.
- **`transform.detectFields`** (Anfrage): `type`, `config` (Variablen mit den Vorgaben aufgelöst, **Connections
  aufgelöst** wie im Lauf, mit Secrets), `limit` (1.000). Antwort wie bei eingebauten Eingaben (S46): `source`
  (`[{"name", "value"}]`), `rows`, `complete`, `fields` (`name`, `type`, optional `format`, `values`, `note`,
  `warning`). Runnel prüft Namen und Typen und vergleicht mit `fields:` der Konfiguration. Zeitgrenze 1 Minute;
  Secrets der Connections sind in Fehlermeldungen maskiert.

## Nachtrag 2026-11-24 (S51): Go-SDK

Präzisierungen bei der Umsetzung in `runnelhq/sdk-go` (Actions und Connection-Typen wurden zuerst gebaut, die
Transforms nach S50):

- **API:** `runnel.New(name, version)`, `runnel.AddAction`, `runnel.AddTransform` und `runnel.AddConnectionType` mit
  einem Struct je Konfiguration (Go-Generics), `p.Main()`. Fehler in den Definitionen (Typ außerhalb des Namensraums,
  doppelt, unbekannte Kategorie, ungültige Tags) sind Programmierfehler (`panic` beim Start, also im ersten Test).
- **Schema aus Struct-Tags** wie bei eingebauten Bausteinen (`json`, `doc`, `schema`, ADR 0014), damit Plugins und Kern
  dieselben Schemas liefern. Zusätzlich: `connection=<typ>` an einem Struct-Feld ergibt im Schema einen Text mit
  `format: runnel-connection`; das Feld bekommt die aufgelöste Connection. `schema:"-"` liest ein Feld, ohne es ins
  Schema zu nehmen (`runnel.ConnectionInfo` mit `name` und `type`).
- **Werte:** Texte in Feldern vom Typ Zahl oder `bool` wandelt das SDK um, weil aufgelöste Variablen als Text
  ankommen; ein Wert, der nicht passt, scheitert mit `field "x": expected …` ohne Go-Typnamen. Für `transform.init`
  bleiben solche Felder leer (Variablen sind dort eventuell noch `${…}`), Connections kommen dort als Name.
- **`concurrency`** ist im SDK 4, wenn das Plugin nichts anderes setzt; das SDK hält die Grenze selbst ein. Ströme
  zählen nicht mit.
- **Transforms:** `Stream.Next` liefert Batches, `Stream.Emit` schreibt an `main`, `error` oder ein Ziel. Das SDK liest
  eingehende Nachrichten in einer eigenen Goroutine und nimmt höchstens `window` (4) Batches an, die `Next` noch nicht
  geliefert hat; `$/ack` für einen Batch schickt es, sobald `Next` ihn liefert. `Emit` wartet auf Kredite des Hosts.
  `fields` gehen mit jedem `emit`: Runnel liest bis S50b ein `emit` ohne `fields` als eines ohne Felder, obwohl das
  Protokoll das Weglassen erlaubt.
- **Fehler:** Ein `error` der Action ist `-32000` mit seiner Meldung, ein `panic` ebenso (das Plugin läuft weiter, der
  Stack steht im Log); bei Transforms beendet beides den Strom mit `transform.closed` und Meldung. Abgebrochene
  Anfragen `-32800`, Anfragen vor `initialize` `-32002`, unbekannte Methoden `-32601`. Ein Fehler in `connection.test`
  ist ein gescheiterter Test (`ok: false`), keine Fehlerantwort.
- **Tests:** `runneltest` spricht das Protokoll im Testprozess (auch `RunTransform` mit Krediten wie der Host);
  `runneltest.NewProject` baut das Plugin in ein temporäres Projekt, gibt es frei (Freigaben im temporären Ordner) und
  startet `runnel`, wenn `RUNNEL_BIN` gesetzt ist. Die CI der Repositorys des SDK und der Plugins baut dafür `runnel`
  aus `main`.
- **Leistung:** Beispiel-Transform mit Runnel (500.000 Records mit fünf Feldern, ein Feld dazu, vier Kerne): etwa
  360.000 Records/s; `make bench` im SDK misst den Strom ohne Runnel (≈ 300.000 Records/s mit Test-Client).
- **Erste Plugins:** `runnelhq/plugin-iban` und `runnelhq/plugin-ldap` (Version 0.1.0) mit Release-Archiven nach
  `docs/dev/PLUGINS.md` und `THIRD_PARTY_NOTICES`.

## Nachtrag 2026-11-25 (S52a): Freigeben im Editor

Nach dem freigegebenen Mockup S52 kann der Editor Plugins freigeben (`POST /api/v1/plugins/{name}/trust`).

- **Gleiche Freigabe wie die CLI:** für das Konto, unter dem `runnel serve` läuft, in dessen Datei der Freigaben. Die
  Freigabe für den Windows-Dienst schreibt der Editor nie; läuft Runnel selbst als Dienst, lehnt der Server Freigeben
  und Zurücknehmen ab (`403`) und nennt den Befehl für Administratoren.
- **Nur, was angezeigt wurde:** Die Anfrage nennt den Hash, den der Editor gezeigt hat; hat sich der Ordner seitdem
  geändert, lehnt der Server ab (`409`). So kann keine Datei zwischen Anzeigen und Freigeben ausgetauscht werden.
- **Nichts startet vor der Freigabe:** Die Liste liest Stand, Programm und Verwendung aus den Dateien; nur freigegebene
  Plugins werden für ihre Typen gestartet.

### Sicherheit

- Die Endpunkte gelten nur für Anfragen an `localhost`/`127.0.0.1` ohne fremden `Origin` (wie alle des Servers): Eine
  Webseite kann kein Plugin freigeben.
- Wer den Editor erreicht, darf ohnehin Workflows ändern und starten; Freigeben erweitert das nicht über das Konto des
  Servers hinaus.
- Freigeben und Zurücknehmen stehen mit Name, Version und Hash im Server-Log.

## Nachtrag 2026-11-25 (Fix): Kategorie optional

Befund aus dem Test mit `plugin-iban`: Bausteine mit unbekannter oder fehlender Kategorie standen unter „General“
(Actions) bzw. „Transform“ (Transforms) zwischen den eingebauten, ihre Herkunft war nicht zu erkennen.

- **`category` ist optional.** Eine Kategorie der eingebauten ordnet den Baustein wie bisher ein. Ohne sie oder mit
  einer unbekannten ist sie im Katalog leer, und der Editor zeigt den Baustein in einer Gruppe des Plugins
  („Plugin: \<name\>“, nach den Gruppen der Kategorien, Farbe der Kachel wie `general`). Die Gruppe trägt den Namen des
  Plugins, nicht den Namensraum, wie Explorer, Freigabe und Meldungen.
- **Katalog:** Descriptoren von Plugin-Typen tragen `plugin` (Name des Plugins), auch mit Kategorie.
- **Verhalten:** Ein Transform ohne Kategorie verhält sich wie einer der Kategorie `transform`.
- **SDK:** `runnelhq/sdk-go` behandelt eine unbekannte Kategorie bisher als Programmierfehler; eine leere Kategorie
  sollte es zulassen (Änderung im SDK).

## Nachtrag 2026-11-26 (Rust-SDK): leere Listen und IDs

Befunde aus `runnelhq/sdk-rust` (v0.1.0) und `runnelhq/plugin-faker` (v0.1.0, Rust):

- **Leere Listen:** Runnel schickte in `transform.init` für einen Transform ohne eingehende bzw. ausgehende Links
  `"input": {"fields": null}` und `"targets": null` statt `[]`; das Rust-SDK lehnte die Anfrage ab. Seit dem Fix
  schickt Runnel leere Listen als `[]`. Für alle Nachrichten gilt zusätzlich: `null` anstelle einer Liste oder eines
  Objekts bedeutet leer bzw. nicht angegeben; Plugins und SDKs lesen es so (auch wegen älterer Versionen von Runnel).
  Das Go-SDK liest `null` ohnehin als leere Liste, das Rust-SDK ab v0.1.0.
- **IDs:** Runnel nummeriert Anfragen mit ganzen Zahlen ab 1. Plugins dürfen sich darauf verlassen, dass eine ID eine
  ganze Zahl im Bereich von int64 oder ein Text ist. Eine andere ID (z. B. `1e999`, ein Objekt) beantworten sie mit
  `-32600` und `"id": null`, statt sie zurückzuschicken; strenge JSON-Parser könnten die Antwort sonst nicht lesen.
  Befund aus dem Zufallstest des Rust-SDK; das Go-SDK folgt in seinem Repository.
- **Rust-SDK:** Schemas aus Rust-Structs mit `#[derive(Config)]`, dieselben wie im Go-SDK; Laufzeit ohne async
  (Threads und Channels). Durchsatz eines Stroms mit fünf Feldern (vier Kerne): etwa 450.000 Records/s mit dem
  Test-Client, etwa 380.000 mit Runnel.
