# ADR 0022: Connections und Secrets

- **Status:** Angenommen
- **Datum:** 2026-11-22
- **Bezug:** KONZEPT Kap. 2.5, 2.6, 4.2, 8, 12; [ADR 0004 – MIT-Lizenz](0004-lizenz-mit.md),
  [ADR 0005 – Mehrbenutzerbetrieb](0005-mehrbenutzerbetrieb.md), [ADR 0007 – Typsystem](0007-typsystem-pipelines.md),
  [ADR 0012 – KI-Fähigkeit](0012-ki-faehigkeit.md), [ADR 0013 – Typnamen](0013-typnamen-namensraeume.md),
  [ADR 0014 – Descriptor](0014-action-descriptor-schema.md), [ADR 0019 – Plugins](0019-plugin-protokoll.md),
  [ADR 0020 – Relative Pfade](0020-relative-pfade.md), Roadmap S20, S21, S23, S39

## Kontext

Viele geplante Bausteine sprechen mit Systemen, die Adresse und Zugangsdaten brauchen:

- Datenbanken: `db.*`, S20, S21, S23
- Dateitransfer: FTP/SFTP
- Mail: SMTP, IMAP
- HTTP-APIs: `http.request` mit Basis-URL und Anmeldung
- Modell-Anbieter und MCP-Server: ADR 0012

Das Konzept nennt dafür **Connections** als Metadaten-Objekt unter `connections/` und **Secrets** im Secret-Store
(`${secret:…}`), beschreibt aber weder Format noch Regeln. Die Beispiele (`connection: smtp-office365`,
`connection: crm-api`) setzen sie schon voraus. Der Secret-Store (S39) steht im Backlog. S20 plant als Übergang
Passwörter in Umgebungsvariablen; beim Windows-Dienst müssten sie am Dienstkonto gesetzt werden.

Weitere offene Punkte:

- Der MySQL-Treiber `go-sql-driver/mysql` steht in KONZEPT 4.2, ist aber MPL-2.0. Das passt nicht zu ADR 0004.
- `ftp` und `sftp` sind zwei reservierte Namensräume für dasselbe Thema.
- Ausdrücke im SQL-Text würden SQL-Injection ermöglichen, z. B. mit Parametern eines HTTP-Triggers.

## Entscheidung

### 1. Connection-Dateien

Eine Connection ist eine Datei **`connections/<name>.yaml`** im Projekt, versioniert mit Git wie Workflows.

```yaml
# connections/erp-sql.yaml
kind: Connection
apiVersion: runnel/v1
name: erp-sql                     # muss dem Dateinamen entsprechen (wie bei Environments)
type: sqlserver
description: ERP-Datenbank, nur lesend
with:
  host: ${ERP_HOST}
  database: ERP
  user: runnel_read
  password: ${secret:erp-sql-password}
  encrypt: true
```

- **`type`** wählt die Art der Verbindung. Die Felder unter `with` beschreibt ein Descriptor mit JSON-Schema, wie bei
  Actions und Transforms (ADR 0014). Daraus entstehen die Prüfung bei `validate` und das Formular im Editor. Unbekannte
  Felder sind ein Fehler.
- **Eingebaute Typen** (je mit dem Schritt, der sie braucht):

  | Typ | Für |
  |---|---|
  | `sqlserver`, `postgres`, `oracle`, `sqlite`, `odbc` | `db.*` |
  | `sftp`, `ftps`, `ftp` | `transfer.*` |
  | `smtp`, `imap` | `mail.*` |
  | `http` | `http.request` |
  | `anthropic`, `openai`, `ollama` | `ai.*` |
  | `mcp` | `mcp.*` |

- **Plugins** bringen vorerst keine eigenen Connection-Typen mit; sie bekommen Werte über ihre `config`. Eigene Typen aus
  Plugins wären eine Erweiterung von ADR 0019.

### 2. Werte je Environment über Variablen

Unterschiede zwischen Umgebungen stehen **nicht** in der Connection-Datei, sondern in Variablen des Projekts bzw. des
Environments: `host: ${ERP_HOST}` mit `ERP_HOST` in `environments/prod.yaml`. Es gibt keine Abschnitte je Environment in
der Datei. So gibt es genau eine Stelle für Unterschiede, und `validate` prüft Variablen schon je Environment
(KONZEPT 2.5).

### 3. Verwendung in Bausteinen

- Bausteine nennen die Connection mit **`connection: <name>`**. Das Feld hat im Schema das Format
  `runnel-connection` und die erlaubten Typen, z. B. `x-runnel-connection: [sqlserver, postgres, oracle, sqlite, odbc]`.
  Der Editor bietet dann eine Auswahl an.
- `validate` prüft feste Namen: Die Connection existiert, ihr Typ passt, ihre Felder sind gültig, ihre Variablen sind
  definiert.
- Namen mit `${…}` oder `{{ … }}` werden beim Lauf geprüft und bekommen bei `validate` eine Warnung, wie dynamische Ziele
  (ADR 0008, Nachtrag S25b).
- Der Baustein erhält die aufgelöste Connection: Variablen und Secrets sind ersetzt. Die Werte erscheinen nie im Log
  oder im Ergebnis.

### 4. Secrets und Secret-Quellen

**Schreibweise.** Workflows und Connections schreiben `${secret:NAME}` oder, mit ausdrücklicher Quelle,
`${secret:QUELLE/NAME}`. Woher der Wert kommt, entscheidet die Konfiguration, nicht der Workflow.

**Secret-Quellen** sind Metadaten-Dateien im Projekt, wie Connections:

```yaml
# secret-stores/company-vault.yaml
kind: SecretStore
apiVersion: runnel/v1
name: company-vault               # muss dem Dateinamen entsprechen
type: azure-keyvault
with:
  url: https://contoso-runnel.vault.azure.net
  auth: managed-identity
  cache: 5m
```

```yaml
# environments/prod.yaml
kind: Environment
apiVersion: runnel/v1
name: prod
secretStore: company-vault        # Standardquelle für ${secret:NAME} in diesem Environment
variables: { … }
```

- **Standardquelle:** `secretStore` im Environment, sonst in `project.yaml`, sonst `local`. So nutzt z. B. dev den lokalen
  Store und prod einen Tresor, ohne dass sich Workflows oder Connections ändern.
- **Eingebaut immer vorhanden** (ohne Datei; seit R5a nur `env` und `inline` setzbar, seit S39d wieder `local`, siehe Nachträge 2026-11-26 und 2026-11-27): `local` (lokaler Store des Projekts), `shared` (lokaler Store, für alle
  Projekte des Kontos gemeinsam) und `env` (Umgebungsvariablen; `${env:NAME}` bleibt als Schreibweise für nicht
  geheime Werte). Diese Namen sind für eigene Quellen reserviert.
- **Typen:**

  | Typ | Was | Wann |
  |---|---|---|
  | `local` | eigener Store (unten) | S39a |
  | `env` | Umgebungsvariablen des Prozesses | S39a |
  | `windows-credentials` | Windows-Anmeldeinformationsverwaltung | bei Bedarf |
  | `azure-keyvault` | Azure Key Vault über die REST-API, ohne SDK; Anmeldung per Managed Identity, Client-Secret oder Zertifikat | bei Bedarf |
  | `vault` | HashiCorp Vault / OpenBao über die HTTP-API, ohne Client-Bibliothek (MPL); Token oder AppRole | bei Bedarf |
  | `aws-secrets`, `gcp-secrets` | Cloud-Tresore | später |
  | Plugin | ein Plugin bietet eine Quelle an (neue Methode `secrets.get`, Nachtrag zu ADR 0019) | später |

- **Kein `exec`:** Eine Quelle, die ein Programm startet (z. B. `op read`), gibt es nicht. Sie würde ein fremdes Projekt
  Code ausführen lassen; Fälle wie 1Password oder KeePass laufen über ein freigegebenes Plugin (ADR 0019, Nachtrag).
- **Anmeldung am Tresor:** über die Identität des Rechners (Managed Identity, gMSA/Kerberos) oder ein Secret aus einer
  anderen Quelle, z. B. `clientSecret: ${secret:local/vault-client}`. Ketten enden immer bei `local`, `env` oder der
  Identität des Rechners; Zyklen sind ein Fehler.
- **Lesen und Schreiben:** Runnel liest aus allen Quellen, schreibt aber nur in `local` (später ggf.
  `windows-credentials`). Tresore pflegt man mit deren Werkzeugen.
- **Verfügbarkeit:** Ist eine Quelle nicht erreichbar oder fehlt ein Secret, startet ein Lauf nicht; die Meldung nennt
  Quelle und Namen, nie einen Wert. Werte werden je Quelle im Speicher zwischengespeichert (`cache`, Standard 5 min),
  nie auf der Platte. `validate` prüft die Konfiguration der Quellen, ruft aber keine Werte ab.

**Der lokale Store (`local`, Schritt S39a vor S20):**

- **Ablage außerhalb des Projekts:** für Benutzer `%LOCALAPPDATA%\runnel\secrets.db` bzw.
  `<Konfigurationsordner>/runnel/secrets.db`, für den Dienst `%ProgramData%\runnel\secrets\` (geschützt wie der Ordner
  des Zertifikats). Verschlüsselt mit AES-256-GCM; der Schlüssel ist unter Windows per **DPAPI** an Benutzer bzw.
  Dienstkonto gebunden, sonst kommt er aus `RUNNEL_MASTER_KEY` oder einem Master-Passwort beim Start.
- **Je Projekt:** Werte gehören zum Projekt (über seinen Ordner); zwei Projekte mit `erp-password` teilen sich den Wert
  nicht. Gemeinsame Werte nur ausdrücklich über die Quelle `shared` (`${secret:shared/NAME}`).
- **Inhalt:** Text, mehrzeiliger Text (z. B. private SSH-Schlüssel, PEM) und Dateien (z. B. PFX, als Base64) über
  `--from-file`. Je Secret Änderungszeit und optional ein **Ablaufdatum** (`--expires`), vor dem `runnel secret list`
  und der Editor warnen.
- **Bedienung:**
  - `runnel secret set <name>` liest den Wert verdeckt von der Konsole, von stdin oder mit `--from-file`, nie als Argument.
  - `runnel secret list` zeigt Namen, Änderungszeit und Ablauf, keine Werte.
  - `runnel secret delete <name>`.
  - **`runnel secret missing`** zeigt, welche Secrets das Projekt braucht (aus Connections und Feldern mit
    `secret`-Format) und welche in der jeweiligen Quelle fehlen; bei Tresoren nur, wenn das Recht zum Auflisten besteht,
    sonst „nicht prüfbar“.
  - `runnel secret export` / `import`: verschlüsselte Datei mit eigenem Passwort, für Umzug und Sicherung.
  - Alles mit `--service` für den Store des Dienstes (Administratorrechte).
- **Editor:** Secrets setzen und löschen, **nie anzeigen**; Hinweis auf fehlende und bald ablaufende Secrets (Mockup).
  Im Netzbetrieb (S40b) ist das Setzen im Editor standardmäßig aus und nur per Schalter am Server erlaubt.

**Wo Secrets hinfließen dürfen:**

- `${secret:…}` ist **nur in Connection- und SecretStore-Dateien und in Feldern mit `secret`-Format** erlaubt, nicht
  in beliebigen Texten wie `core.log`. So landet ein Secret nicht versehentlich in Log, Datei oder Ergebnis.
- **Plugins** erhalten ein Secret nur über ein Feld mit `secret`-Format in ihrer Konfiguration; ihr Log wird ebenso
  maskiert.
- **Maskierung:** Alle aufgelösten Secret-Werte ersetzt Runnel in Log, Fehlermeldungen, Vorschau, Ereignissen und
  Ergebnissen durch `***`, ebenso Werte aus `${env:…}`, die in Connections stehen. Werte unter 4 Zeichen werden nicht
  maskiert, damit nicht jedes einzelne Zeichen ersetzt wird. Die Maskierung erkennt nur den exakten Wert; kodiert
  (Base64, URL-kodiert) oder in Teilen bleibt er erkennbar. Die Doku sagt das deutlich.

### 5. Datenbanken: Werte nur als Parameter

- **Abfragen** in `db.table-input`, `db.execute` und `db.lookup` bekommen Werte **nur als Parameter**:

  ```yaml
  sql: SELECT * FROM orders WHERE region = @region AND day >= @from
  params:
    region: "{{ params.REGION }}"
    from: ${IMPORT_DATE}
  ```

  `${…}` und `{{ … }}` im SQL-Text selbst sind ein Fehler bei `validate`. Bezeichner (Tabellen- und Spaltennamen) aus
  Variablen gibt es nur über eigene Felder wie `table:`, die gegen ein Muster für Bezeichner geprüft und vom Treiber
  quotiert werden. Platzhalter schreibt man einheitlich `@name`; Runnel übersetzt sie in die Syntax des Treibers.
- **Typen:** Die Zuordnung zu ADR 0007 legt der Schritt S20 je Treiber in einer Tabelle fest. `decimal` bleibt exakt
  (nie `float64`), `date` und `datetime` werden getrennt, `NULL` wird `null`.
- **Verbindungen:**
  - Ein Pool je Connection und Environment im Server, mit Obergrenze offener Verbindungen (Standard 4).
  - Timeout je Abfrage (Standard 5 min); ein Abbruch des Laufs bricht die Abfrage ab.
  - Transaktionen je Baustein mit `commitSize`; „Unique Connection“ (eine Transaktion je Pipeline) folgt mit S21.
- **Treiber:**

  | Treiber | Lizenz |
  |---|---|
  | `microsoft/go-mssqldb` (mit Windows-Authentifizierung) | BSD-3 |
  | `jackc/pgx/v5` | MIT |
  | `sijms/go-ora` | MIT |
  | `modernc.org/sqlite` | BSD-3 |
  | `alexbrainman/odbc` | BSD-3 |

  Die Lizenzen wurden bisher nicht geprüft. Jeder Treiber kommt mit seinem Schritt, die Lizenz wird im PR geprüft und in
  den `THIRD_PARTY_NOTICES` geführt.
- **MySQL/MariaDB entfällt vorerst.** `go-sql-driver/mysql` ist MPL-2.0 und passt nicht zu ADR 0004. Bis eine Lösung
  mit passender Lizenz gefunden ist, geht MySQL über `odbc` oder ein Plugin. Der Typ `mysql` bleibt reserviert.
- **Sicherheit der Verbindung:**
  - TLS ist an, wo der Treiber es kann. `encrypt: true` ist Standard bei SQL Server.
  - Zertifikate werden geprüft. `trustServerCertificate: true` gibt es nur als ausdrückliche Option, und `validate`
    warnt dann.

### 6. Dateitransfer: ein Namensraum `transfer`

Statt zweier Namensräume `ftp` und `sftp` gibt es **einen Namensraum `transfer`**. Das Protokoll steht in der Connection
(`type: sftp | ftps | ftp`), wie bei `archive` das Format.

- Actions: `transfer.upload`, `transfer.download`, `transfer.list`, `transfer.delete`, `transfer.move`,
  `transfer.mkdir`, `transfer.exists`, `transfer.wait`.
- Die Optionen und Ergebnisse entsprechen den gleichnamigen `file.*`-Actions: Platzhalter, Konfliktprüfung vor dem
  ersten Schreiben, Ergebnis als Pfadliste, Schreiben über eine temporäre Datei mit anschließendem Umbenennen.
- **SFTP:** Der Fingerprint des Hostschlüssels steht in der Connection (`hostKey: SHA256:…`). Ohne ihn baut Runnel keine
  Verbindung auf und nennt in der Meldung den Fingerprint des Servers zum Prüfen. „Beim ersten Mal vertrauen“ gibt es
  nicht.
- **FTP ohne TLS** nur mit `insecure: true` in der Connection; `validate` warnt dann (wie `--triggers-insecure`).
- Grenzen: Timeout je Verbindung und je Übertragung; eine Größengrenze je Datei ist optional.

### 7. Mail und HTTP-APIs

- **`smtp`:** Host, Port, TLS (`starttls` oder `tls`), Anmeldung `password` oder **`oauth2`** (Client Credentials:
  Tenant, Client-ID, Secret). Office 365 schränkt die Anmeldung mit Passwort für SMTP ein; deshalb ist OAuth2 von
  Anfang an vorgesehen.
- **`imap`:** wie SMTP, dazu Grenzen für Anzahl und Größe der Nachrichten je Abruf.
- **`http`:** `baseUrl`, Standard-Header und Anmeldung: `bearer`, `basic`, `apiKey` (Header oder Query) oder `oauth2`
  (Client Credentials, Token im Speicher bis kurz vor Ablauf). Dazu Proxy und Zertifikatsprüfung (nur ausdrücklich
  abschaltbar).
  - Mit `connection:` gilt `url:` in `http.request` relativ zur Basis-URL; ein absoluter `url:` zu einem anderen Host
    ist ein Fehler, damit Zugangsdaten nicht an fremde Hosts gehen.

### 8. Verbindung testen

Der Editor bietet „Test connection“ über `POST /api/v1/connections/{name}/test`:

- **Lokal:** nur über den lokalen Server, mit dem Schutz von ADR 0005 (Origin, DNS-Rebinding).
- **Inhalt:** nur mit der gespeicherten Connection (aufgelöst für das gewählte Environment), keine frei übergebenen
  Hosts.
- **Grenzen:** Zeitgrenze 15 s, höchstens ein Test je Connection gleichzeitig.
- **Ergebnis:** Erfolg oder Meldung ohne Werte von Secrets. Bei SFTP mit unbekanntem Hostschlüssel enthält die Meldung
  den Fingerprint.

### 9. Einheitliche Regeln für Metadaten-Dateien

Für `Project`, `Environment`, `Trigger`, `Connection`, `SecretStore`, `Plugin` und später `RunConfig` gilt:

- `kind` und `apiVersion` sind Pflicht.
- Wo es einen Ordner je Art gibt, gilt **Name gleich Dateiname**.
- Unbekannte Felder sind ein Fehler.
- `validate` prüft die Datei.
- Der Explorer zeigt die Datei mit eigenem Symbol.
- Verweise auf eine Datei (Connection, Workflow, Pipeline) passt das Umbenennen im Editor an (wie E12).

Abweichungen bestehender Dateiarten gleicht der Schritt an, der sie zuerst betrifft.

### Sicherheit

- **Secrets** liegen nie im Projekt und nie im Klartext auf der Platte; der Schlüssel ist an Benutzer bzw. Dienstkonto
  gebunden (DPAPI). Werte erscheinen weder in Logs, Fehlermeldungen, Vorschau, Ereignissen noch Ergebnissen
  (Maskierung).
- **`${secret:…}`** ist nur in Connections, Secret-Quellen und Feldern mit `secret`-Format erlaubt.
- **Secret-Quellen** im Projekt enthalten nur Adresse und Art der Anmeldung, keine Werte; eine Quelle, die Programme
  startet, gibt es nicht. Tresore werden nur gelesen; Werte liegen nur im Speicher und nur für die Dauer von `cache`.
- **Export** des lokalen Stores nur verschlüsselt mit eigenem Passwort; der Editor zeigt Werte nie an.
- **SQL:** Werte nur als Parameter, Bezeichner nur über geprüfte Felder. Das schützt vor SQL-Injection auch bei Werten
  aus HTTP-Triggern.
- **Transport:**
  - TLS und Zertifikatsprüfung sind Standard; Abschalten nur ausdrücklich und mit Warnung.
  - SFTP nur mit bekanntem Hostschlüssel.
  - Zugangsdaten einer HTTP-Connection gehen nur an deren Host.
- **„Test connection“** nur für gespeicherte Connections, mit Zeitgrenze, ohne Secret-Werte in der Antwort.
- **Grenzen:** Timeouts je Abfrage bzw. Übertragung und Obergrenzen für offene Verbindungen.
- **Tests für Ablehnungen** gehören zu den jeweiligen Schritten:
  - `${secret:…}` an verbotener Stelle
  - fehlendes Secret und nicht erreichbare Quelle (Lauf startet nicht, Meldung ohne Wert)
  - Zyklus in der Anmeldung von Quellen
  - Ausdruck im SQL-Text
  - fremder Host bei HTTP-Connection
  - unbekannter Hostschlüssel
  - FTP ohne `insecure`
  - Zeitüberschreitung des Tests

## Betrachtete Alternativen

- **Abschnitte je Environment in der Connection-Datei:** verworfen (vom Maintainer entschieden). Unterschiede stünden
  dann an zwei Stellen, Variablen leisten dasselbe.
- **`file.*` mit `connection:` statt eigenem Namensraum:** verworfen (vom Maintainer entschieden). Die lokalen Actions
  bekämen Optionen, die nur für entfernte Server gelten; Fehlerbilder und Grenzen unterscheiden sich.
- **MySQL mit `go-sql-driver/mysql`:** vorerst verworfen wegen MPL-2.0 (vom Maintainer entschieden).
- **Passwörter bis S39 nur über Umgebungsvariablen:** verworfen. Für den Windows-Dienst ist das umständlich und
  fehleranfällig; ein kleiner Kern des Secret-Stores ist überschaubar.
- **Ein fest eingebauter Speicher ohne Quellen:** verworfen. Teams mit Tresor müssten Werte doppelt pflegen; mit
  Quellen bleibt `${secret:NAME}` gleich und nur die Konfiguration je Environment ändert sich.
- **Quelle `exec`** (ein Programm liefert den Wert): verworfen (vom Maintainer entschieden). Sie würde Code aus dem
  Projekt ausführen; dafür gibt es Plugins mit Freigabe.
- **Secret-Quellen nur in der Rechner-Konfiguration:** verworfen (vom Maintainer entschieden). Im Projekt sieht jeder,
  woher die Secrets kommen, und `runnel secret missing` kann gezielt helfen; Werte und Zugangsdaten liegen dort nicht.
- **Secrets in einer Datei im Projekt (verschlüsselt):** verworfen. Ein Schlüssel müsste mitgegeben werden, und Git
  würde alte Stände behalten.

## Abstimmung

Mit dem Maintainer am 2026-11-22 abgestimmt: Werte je Environment über Variablen, ein Namensraum `transfer`, MySQL
vorerst nicht; Ort der Secrets, `${secret:…}` nur an bestimmten Stellen, Platzhalter `@name`, Standardwerte wie
vorgeschlagen; Secret-Quellen im Projekt ohne `exec`, lokaler Store je Projekt mit Export, Ablaufdatum und Editor nur
zum Setzen.

## Konsequenzen

- **Positiv:**
  - Zugangsdaten stehen an einer Stelle je Verbindung, sind getrennt vom Projekt und gegen Klartext und Log geschützt.
  - Der Editor erzeugt Formulare und Auswahl aus den Schemas.
  - Ein Muster für alle Arten von Verbindungen.
- **Negativ:**
  - Mehr Arbeit vor S20: Secret-Quellen mit lokalem Store, Connection-Format und Prüfung.
  - MySQL fehlt vorerst.
  - Die strengen Regeln (nur Parameter im SQL, Hostschlüssel Pflicht) verlangen mehr Angaben.
- **Folgen für Dokumente:**
  - ADR 0013 erhält einen Nachtrag: `transfer` statt `ftp`/`sftp`.
  - KONZEPT 2.6, 4.2 (Treiber) und 8 werden angepasst.
  - Die Roadmap bekommt S39a (Secret-Quellen mit `local` und `env`, lokaler Store) vor S20, weitere Quellen als
    eigene Schritte bei Bedarf, ein Mockup für Connections und Secrets im Editor und die
    `transfer`-Actions als eigenen Schritt.

## Nachtrag 2026-11-22 (S39a): Reihenfolge der Quellen, Format der Felder

Abgestimmt mit dem Maintainer vor der Umsetzung.

- **Reihenfolge:** S39a bringt nur die Quelle `env` und den Rahmen: Schreibweise `${secret:[QUELLE/]NAME}`,
  Standardquelle über `secretStore:` (Environment, sonst `project.yaml`), die Regeln, wo Secrets stehen dürfen, und die
  Maskierung. Der lokale Store (`local`) folgt mit S39d, `shared`, Export/Import, Ablaufdatum und Secret-Quellen als
  Datei mit S39e. Workflows und Connections ändern sich dadurch später nicht.
- **Standardquelle bleibt `local`:** Bis S39d meldet `${secret:NAME}` ohne `secretStore:` „the local secret store is not
  available yet; set `secretStore: env` …“. Ein Standard `env`, der später auf `local` wechselt, würde bestehende Projekte
  ihre Werte an anderer Stelle suchen lassen.
- **Felder für Secrets:** Das Format heißt `runnel-secret` (Tag `schema:"format=runnel-secret"`, wie die übrigen
  Formate von Runnel). Solche Felder werden nur mit `${…}` aufgelöst, ohne Ausdrücke `{{ … }}`; so wird ein Wert nie Teil
  eines Ausdrucks oder seiner Fehlermeldung. Workflows lesen sie vor dem Start, ein fehlendes Secret verhindert den Lauf.
- **Keine Secrets über Variablen:** `${secret:…}` muss direkt im Feld stehen. Eine Variable, deren Wert ein Secret
  enthält, ist ein Fehler (in `project.yaml` und Environments schon beim Laden), sonst könnte das Secret über `${NAME}` in
  beliebige Texte gelangen.
- **Token der HTTP-Trigger:** `auth: { token: ${secret:[QUELLE/]NAME} }` ist neben `${env:NAME}` erlaubt; gelesen wird
  über die Standardquelle des Environments des Triggers (sonst des Standard-Environments).
- **Maskierung:** Jeder Lauf hat eine eigene Maske mit den Werten, die er gelesen hat. Die Engines ersetzen sie in
  Ereignissen (Log, Fehler, Ausgaben), im Ergebnis eines Workflows und in der Datenvorschau. Die Datensätze einer Pipeline
  bleiben unverändert: Sie sind Daten für den nächsten Schritt.

## Nachtrag 2026-11-22 (S39a2): Quelle `inline` für Klartext

Abgestimmt mit dem Maintainer. Abweichung von „Secrets liegen nie im Projekt“, bewusst und sichtbar:

- **Zweck:** Entwicklungssysteme und einfache APIs mit geringer Auswirkung, für die ein Store auf jedem Rechner zu viel
  Aufwand wäre.
- **Form:** Eingebaute Quelle `inline`. Die Werte stehen unter `secrets:` im Environment bzw. in `project.yaml`
  (Environment vor Projekt), Namen wie bei `${secret:NAME}`. Workflows schreiben weiter `${secret:NAME}`; für die
  Produktion stellt man nur `secretStore:` des Environments um, z. B. auf `local`.
- **Behandlung wie jedes Secret:** nur in Secret-Feldern und als Token, Maskierung im Lauf. Die Werte gehen nicht über die
  JSON-API des Projekts hinaus (die Datei selbst liest der Editor wie jede Datei des Projekts).
- **Warnung statt Verbot:** `runnel validate` und der Editor warnen bei jedem `secrets:` mit Werten („secrets in plain
  text …“). Ein Schalter, der `inline` im Betrieb verbietet, ist vorerst nicht vorgesehen (vom Maintainer entschieden).
- **Portabler Betrieb** bleibt davon getrennt: Dort liegt der lokale Store neben der exe, mit Schlüsseldatei statt DPAPI
  (mit S39d bzw. dem portablen Modus).

## Nachtrag 2026-11-22 (S20a): Connections zuerst mit SQLite

Abgestimmt mit dem Maintainer vor der Umsetzung.

- **Reihenfolge:** S20 wird geteilt. S20a bringt den Rahmen (Dateien, Registry der Typen, Prüfung, Auflösung) mit dem
  Typ `sqlite` und der Action `db.execute`; S20b den Transform `db.table-input` und das Anpassen der Verweise beim
  Umbenennen; S20c SQL Server und PostgreSQL. Der lokale Store (S39d) folgt danach; bis dahin kommen Secrets aus `env`
  oder `inline`.
- **SQLite zuerst:** `modernc.org/sqlite` ist reines Go (kein CGO), BSD-3, und lässt sich ohne Datenbank-Server in der CI
  testen. Die Binärdatei wird um etwa 5 MB größer; derselbe Treiber dient später der Run-Historie (S26).
- **Formate:** `connection:` hat das Format `runnel-connection`, die erlaubten Typen stehen unter
  `x-runnel-connection` (Tag `schema:"connection=sqlite|…"`). SQL-Felder haben das Format `runnel-sql`: Die Engine
  reicht sie unverändert weiter; `${…}` oder `{{ … }}` darin sind ein Fehler bei `validate` und vor dem Lauf.
- **Auflösung:** Connections mit festem Namen werden vor dem Start aufgelöst. Eine fehlende Connection, Variable oder ein
  fehlendes Secret verhindert den Lauf; Secrets und die Werte von `${env:…}` in einer Connection kommen in die Maske des
  Laufs. Namen mit `${…}` oder `{{ … }}` werden beim Ausführen aufgelöst (Warnung bei `validate`).
- **Pool:** je Datenbank (Treiber und Datenquelle) für die Lebensdauer des Prozesses, höchstens 4 offene Verbindungen.
- **Pfade:** `path` einer SQLite-Connection ist relativ zum Projektordner (ADR 0020); der Ordner wird bei Bedarf angelegt.
- **Editor:** Dateien unter `connections/` sind wie Trigger im YAML-Editor bearbeitbar und werden geprüft (Reference aus
  `/schemas/connection`); ein eigenes Symbol und Formular folgen mit dem Mockup S39b.
- **S20b, `db.table-input`:** Die Typen der Felder folgen dem deklarierten Spaltentyp (`DECIMAL`/`NUMERIC` exakt als
  `decimal`, `DATE` als `date`, `DATETIME`/`TIMESTAMP` als `datetime`, `BOOLEAN` als `bool`, `NULL` als leer); sonst der
  Typ, den der Treiber liefert. Das Schema am Ausgang ist offen: Die Spalten stehen erst beim Lauf fest, und das Prüfen
  einer Pipeline öffnet keine Datenbank. Beim Umbenennen einer Connection passt der Editor `connection:` in Workflows und
  Pipelines und `name:` in der Datei an (wie E12).
- **S20c, SQL Server und PostgreSQL:** Treiber `microsoft/go-mssqldb` (BSD-3, mit `golang-sql/civil`, Apache 2.0) und
  `jackc/pgx/v5` (MIT), beide reines Go; die Binärdatei wird um etwa 5 MB größer. SQL Server ohne `user` meldet sich mit
  dem Windows-Konto an, unter dem Runnel läuft (nur unter Windows); `encrypt` ist standardmäßig an. PostgreSQL prüft
  das Zertifikat standardmäßig (`sslMode: verify-full`); pgx kennt keine benannten Parameter, daher übersetzt Runnel
  `@name` in `$1, $2 …` (gleicher Name, gleiche Nummer). `validate` warnt bei abgeschalteter Prüfung oder
  Verschlüsselung (`trustServerCertificate`, `encrypt: false`, `sslMode: require|disable`). Felder wie `port` sind Text
  wie bei Actions, damit sowohl `port: 1433` als auch `port: ${DB_PORT}` gehen.
- **S21, `db.table-output` und `db.upsert`:** Tabellen- und Spaltennamen werden nie als Parameter übergeben, daher nur
  `tabelle` oder `schema.tabelle` aus Buchstaben, Ziffern und `_`; Runnel setzt sie in Anführungszeichen (`"x"`, bei
  SQL Server `[x]`). Ohne `fields` schreibt der Transform alle Felder des Records außer denen mit `_` am Anfang
  (z. B. `_error`). Je `commitSize` Records eine Transaktion (Standard 1000); `timeout` gilt je
  Transaktion. Scheitert eine Transaktion, wird sie zurückgerollt; ohne Fehler-Link hält die Pipeline an, frühere
  Transaktionen bleiben. Mit Fehler-Link wiederholt der Transform die Records einzeln, jeder in eigener Transaktion, und
  gibt nur die fehlerhaften an den Link. Records gehen erst nach dem Commit weiter. Upsert: SQLite und PostgreSQL mit
  `INSERT … ON CONFLICT (keys)`, das einen eindeutigen Index auf den Schlüsseln braucht (sonst Fehler der Datenbank);
  SQL Server kennt das nicht, dort `UPDATE … WITH (UPDLOCK, SERIALIZABLE)` und, wenn keine Zeile betroffen war,
  `INSERT` – `MERGE` ist bei parallelen Läufen fehleranfällig. Ohne `update` werden vorhandene Zeilen nicht geändert.
  Datumswerte gehen als Zeitpunkt, Listen und Objekte als JSON-Text an den Treiber.
- **S59, `db.query`:** Liest die Zeilen einer Abfrage als Output der Action: `rows` (je Zeile ein Objekt), `count`,
  `first` und `value` (erste Spalte der ersten Zeile). Alles liegt im Speicher und im Ergebnis des Laufs, daher
  `maxRows` (Standard 1000); mehr Zeilen sind ein Fehler statt still abgeschnitten. Größere Mengen gehören in eine
  Pipeline mit `db.table-input`.
- **S23a, `db.lookup`:** Eine vorbereitete Abfrage `SELECT … FROM tabelle WHERE spalte = @p …` je Schlüssel, mit
  denselben Regeln für Namen wie S21. Die Ergebnisse (auch „keine Zeile“) hält ein LRU-Cache je Kopie des Transforms
  (`cache`, Standard 1000 Schlüssel, `0` = ohne); Änderungen an der Tabelle während des Laufs sieht der Transform daher
  erst, wenn der Schlüssel aus dem Cache fällt. Ein leerer Schlüssel passt wie in SQL zu keiner Zeile und fragt nicht.
  Bei mehreren Treffern nimmt `multiple: first` die erste Zeile, die die Datenbank liefert (ohne feste Reihenfolge).
  Datenbankfehler halten die Pipeline an; nur „keine Zeile“ und „mehrere Zeilen“ gehen an den Fehler-Link.

## Nachtrag 2026-11-23 (S62a): Mail senden mit `smtp`

- **Teilung:** S62a bringt den Typ `smtp` mit Anmeldung per Passwort oder ohne Anmeldung und die Action `mail.send`;
  S62b ergänzt `auth: oauth2` für Office 365.
- **Keine neue Abhängigkeit:** Statt `wneessen/go-mail` (KONZEPT 4.2) genügt die Go-Standardbibliothek: `net/smtp` für
  den Dialog, `mime`, `mime/multipart` und `mime/quotedprintable` für die Nachricht. Die Anmeldung `LOGIN` (Exchange,
  Office 365 bieten oft kein `PLAIN`) ist in Runnel umgesetzt.
- **Felder der Connection:** `host`, `port` (leer: 587 bei `starttls`, 465 bei `tls`, 25 bei `none`), `tls`
  (`starttls` Standard, `tls` von Anfang an, `none`), `auth` (`password`, `none`; leer: `password`, wenn `user`
  gesetzt ist; seit R5b `basic` statt `password`, Nachtrag), `user`, `password` (Secret), `from` (Absender, wenn die Action keinen nennt), `timeout` (Standard 1m für
  Verbinden und Senden einer Mail).
- **Sicherheit:** Das Zertifikat des Servers wird immer geprüft (Zertifikate des Systems); einen Schalter zum Abschalten
  gibt es nicht. Bietet der Server bei `starttls` kein `STARTTLS`, bricht Runnel ab, statt unverschlüsselt zu senden.
  Ein Passwort geht nie unverschlüsselt über das Netz: `tls: none` mit Anmeldung ist nur zum eigenen Rechner erlaubt.
  `validate` warnt bei `tls: none` zu einem anderen Rechner (Relay im vertrauenswürdigen Netz).
- **Nachricht:** Betreff, Text und HTML in UTF-8; Zeilenumbrüche im Betreff werden zu Leerzeichen, damit ein Ausdruck
  (z. B. eine Fehlermeldung) keine Kopfzeilen einschleusen kann. Adressen werden mit `net/mail` gelesen und
  geschrieben. `Bcc` steht nur im Umschlag, nicht in der Mail. Jede Mail hat `Auto-Submitted: auto-generated`
  (RFC 3834), damit Abwesenheitsnotizen nicht antworten.
- **Grenzen:** Anhänge zusammen höchstens 25 MB (die Nachricht entsteht im Speicher); nennt der Server mit `SIZE` eine
  kleinere Grenze, bricht Runnel vor dem Senden mit einer Meldung ab.
- **Output:** `messageId`, `recipients` (Anzahl) und `size` (Bytes). Das Log nennt nur die Anzahl der Empfänger, die
  Adressen erst auf Stufe Debug.

## Nachtrag 2026-11-23 (S62b): OAuth2 für `smtp`

- **Felder:** `auth: oauth2` mit `user` (das Postfach, das sendet), `tenant` (Microsoft Entra, ID oder Domain),
  `clientId`, `clientSecret` (Secret) und `scope` (Standard `https://outlook.office365.com/.default`). `tokenUrl`
  ersetzt die aus `tenant` gebildete Adresse für andere Anbieter; sie muss `https://` sein (`http://` nur zum eigenen
  Rechner, für Tests). Im Schema gehören die Felder mit `only=auth:oauth2` zu dieser Anmeldung.
- **Ablauf:** Runnel holt das Token mit Client Credentials (RFC 6749, 4.4) und meldet sich mit SASL `XOAUTH2` an. Das
  Token bleibt im Speicher des Prozesses, bis es in weniger als 5 Minuten abläuft, und wird nach einer Ablehnung durch
  den Mail-Server verworfen (z. B. nach Entzug der Berechtigung). Schlüssel des Zwischenspeichers ist ein Hash aus
  Adresse, Client-ID, Secret und Scope; ein geändertes Secret holt also ein neues Token. Tokens stehen nie in Datei, Log
  oder Ergebnis. `timeout` der Connection gilt für Token-Abruf und Versand zusammen.
- **Sicherheit:** Das Token ist ein Zugangsdatum wie ein Passwort: `tls: none` ist mit `oauth2` nur zum eigenen Rechner
  erlaubt. Fehlermeldungen des Token-Servers enthalten Code und erste Zeile der Beschreibung, ohne Trace- und
  Correlation-IDs; das Secret erscheint nie darin.
- **Paket:** Abruf und Zwischenspeicher liegen in `internal/oauth`, damit die spätere `http`-Connection (Abschnitt 7) sie
  wiederverwenden kann. Keine neue Abhängigkeit (`net/http`).

## Nachtrag 2026-11-23 (S63a): Connection `http`

- **Felder:** `baseUrl`, `headers` (für jede Anfrage), `auth` mit `none` (Standard), `bearer` (`token`), `basic`
  (`user`, `password`), `apiKey` (`apiKey`, `apiKeyName` Standard `X-API-Key`, `apiKeyIn` `header` oder `query`) und
  `oauth2` (`tokenUrl`, `clientId`, `clientSecret`, `scope`; Client Credentials über `internal/oauth` wie bei `smtp`).
  Proxy kommt aus der Umgebung (`HTTPS_PROXY`); eine abschaltbare Zertifikatsprüfung folgt erst bei Bedarf.
- **Secrets in Zuordnungen:** `headers` hat das Format `runnel-secret`; ihre Werte dürfen `${secret:…}` enthalten und
  werden einzeln aufgelöst und maskiert. Allgemein gilt jetzt: Ein Secret-Feld einer Connection darf auch eine
  Zuordnung von Texten sein.
- **Adressen:** Mit `connection:` ist `url` in `http.request` relativ zu `baseUrl`, auch mit `/` am Anfang
  (`orders` und `/orders` ergeben bei `https://api.example.com/v1` beide `/v1/orders`); leer ist `baseUrl` selbst.
  Eine absolute Adresse ist nur mit Schema und Host der Connection erlaubt, sonst ein Fehler vor der Anfrage: Die
  Anmeldung geht nie an einen anderen Host.
- **Vorrang:** Header der Connection, dann Header der Action, zuletzt die Anmeldung der Connection.
- **OAuth2:** Antwortet die API mit `401`, verwirft Runnel das Token und sendet die Anfrage einmal mit einem neuen.
- **Sicherheit:** Mit Anmeldung ist `http://` nur zum eigenen Rechner erlaubt; `validate` warnt bei einer `baseUrl`
  mit `http://` zu einem anderen Rechner. Meldungen nennen nur Schema, Host und Pfad, nie die Query (`apiKeyIn:
  query`) oder Header.

## Nachtrag 2026-11-23 (S63b, S63c): `Retry-After` und Paginierung

- **Folgeseiten auf demselben Host:** Eine Adresse für die nächste Seite aus der Antwort (`Link`-Header oder Body) wird
  nur angefragt, wenn Schema und Host der ersten Anfrage gleich sind; sonst bricht die Action ab. Das gilt auch ohne
  Connection und verhindert, dass eine manipulierte Antwort die Anmeldung an einen fremden Host lenkt. Eine Adresse oder
  ein Cursor, der schon einmal vorkam, ist ein Fehler statt einer Endlosschleife.
- **Grenzen:** `maxPages` (100) und `maxItems` (10000); mehr ist ein Fehler, denn alle Einträge liegen im Speicher und
  im Ergebnis des Laufs. `maxSize` und `timeout` gelten je Seite. Große Mengen gehören in den späteren Transform
  `http.input`, der dasselbe Paket `internal/pagination` nutzt.
- **`Retry-After`:** bei `429` und `503` mit dem Header; höchstens `retryAfter.maxRetries` Wiederholungen (3) und je
  höchstens `retryAfter.maxWait` (1 Minute). Ohne Header wird nicht wiederholt; dafür bleibt `retry:` der Action. Die
  Wiederholung gilt je Anfrage, bei Paginierung also je Seite.

## Nachtrag 2026-11-23 (S39c1): Test und Stand der Secrets

- **Test einer Connection** (Abschnitt 8): `POST /api/v1/connections/{name}/test` mit `{"environment": "…"}` (leer:
  Standard-Environment). Getestet wird nur die gespeicherte Datei, aufgelöst wie für einen Lauf. Höchstens 15 s und ein
  Test je Connection gleichzeitig (sonst `409`). Ein fehlgeschlagener Test ist eine Antwort (`ok: false`) mit Meldung,
  maskiert wie das Log eines Laufs.
- **Was getestet wird:** Datenbanken Ping und Version des Servers; SQLite legt dabei keine Datei an (eine fehlende Datei
  ist „wird beim ersten Lauf angelegt“). `http` sendet `GET` an `baseUrl` mit Headern und Anmeldung; `401`/`403` sind ein
  Fehlschlag, jede andere Antwort zeigt, dass die API erreichbar ist. `smtp` meldet sich an und beendet den Dialog ohne
  Mail. Geschrieben oder gesendet wird nichts.
- **Stand der Secrets:** `GET /api/v1/secrets` nennt jedes `${secret:…}` des Projekts mit Fundstellen und je Environment
  die Quelle und den Stand (`set`, `missing`, `unavailable` für Quellen, die es noch nicht gibt). Werte verlassen den
  Server nie; `env` meldet nur, ob die Variable gesetzt ist.

## Nachtrag 2026-11-24 (K1): Standardwerte für Variablen

- `${NAME:-text}` und `${env:NAME:-text}` setzen `text` ein, wenn die Variable nicht gesetzt oder leer ist (wie `:-` in
  der Shell). Der Standardwert ist wörtlicher Text ohne `}` und ohne weitere Verweise; die Prüfung meldet einen Verweis
  mit Standardwert nicht als fehlende Variable.
- `${secret:NAME:-text}` ist ein Fehler: Ein Standardwert für ein Secret stünde im Klartext in einer Datei des Projekts,
  was diese ADR gerade vermeidet. Ein fehlendes Secret bleibt ein Fehler beim Start des Laufs.

## Nachtrag 2026-11-26 (S57a): Dateitransfer mit `sftp`

- **Teilung:** S57a bringt den Typ `sftp` und `transfer.upload`, `transfer.download`, `transfer.list`; S57b die übrigen
  Actions (`delete`, `move`, `mkdir`, `exists`, `wait`), S57c die Typen `ftps` und `ftp` mit der Auswahl der
  FTP-Bibliothek.
- **Felder der Connection:** `host`, `port` (leer: 22), `hostKey`, `user`, `password` (Secret), `privateKey` (Secret,
  PEM-Text im Format OpenSSH, PKCS#1 oder PKCS#8), `passphrase` (Secret) und `timeout` (Standard 15 s für Verbinden und
  Anmelden). Mit Schlüssel und Passwort versucht Runnel zuerst den Schlüssel; das Passwort geht auch als
  `keyboard-interactive` (eine verdeckte Frage), weil manche Server es nur so annehmen.
- **`hostKey`:** ein oder mehrere SHA256-Fingerprints (durch Leerzeichen oder Kommas getrennt, mit oder ohne
  `SHA256:`), so wie `ssh-keygen -l` sie zeigt; MD5-Fingerprints sind ein Fehler. Das Feld ist im Schema nicht Pflicht,
  damit „Test connection“ mit einer neuen Connection den Fingerprint des Servers nennen kann; ohne `hostKey` verbindet
  sich aber kein Lauf, und `validate` warnt. Die Meldung nennt Fingerprint und Schlüsseltyp des Servers mit dem Hinweis,
  ihn mit dem Administrator des Servers zu prüfen; bei Abweichung zusätzlich, dass der Server ersetzt worden sein oder
  jemand dazwischen sitzen kann.
- **Eine Verbindung je Ausführung:** Jede Ausführung einer Action öffnet eine SSH-Verbindung und schließt sie am Ende.
  Einen Pool über Actions hinweg gibt es vorerst nicht; er lohnt erst bei vielen kleinen Actions hintereinander
  (S57b, `transfer.wait`).
- **Optionen wie `file.*` (ADR 0025):** `transfer.upload` und `transfer.download` haben `source`, `target` und
  `ifExists` wie `file.copy` (Standard `fail`, geprüft vor dem ersten Schreiben; Ordner rekursiv; mehrere Quellen oder
  `/` am Ende des Ziels gehen in den Zielordner), `transfer.list` hat `folder`, `pattern`, `recursive` und `include` wie
  `file.list`. Output wie dort: die Pfade der geschriebenen Dateien bzw. Einträge mit `path`, `name`, `size`,
  `modified`, `folder`.
- **Pfade auf dem Server** sind POSIX-Pfade mit `/`; relative beziehen sich auf den Ordner nach der Anmeldung.
  Platzhalter unterscheiden dort Groß- und Kleinschreibung (wie auf den meisten Servern; lokal unter Windows nicht).
  Lokale Pfade gelten relativ zum Projektordner (ADR 0020).
- **Temporäre Dateien:** Hochladen schreibt `.<name>.<zufall>.tmp` im Zielordner und benennt die Datei erst vollständig
  um, mit `posix-rename@openssh.com`, wo der Server es kann (ersetzt in einem Schritt), sonst durch Löschen der alten
  Datei und `rename`. Herunterladen schreibt lokal ebenso über eine temporäre Datei. Bricht die Verbindung ab, kann die
  temporäre Datei auf dem Server zurückbleiben (TESTPLAN K15); die Zieldatei ist nie halb geschrieben. Die Änderungszeit
  bleibt in beide Richtungen erhalten.
- **Grenzen:** `timeout` je Datei (bzw. für die ganze Liste und für das Prüfen der Ziele), Standard 10 Minuten; danach
  schließt Runnel die Verbindung, die Action schlägt fehl. Das `timeout` der Action begrenzt alles zusammen. `maxSize`
  beim Download ist optional (leer: ohne Grenze; seit R5b Standard 1 GB, Nachtrag) und wird vor und während der Übertragung geprüft. `transfer.list`
  liefert höchstens 10000 Einträge, mehr ist ein Fehler (die Liste liegt im Ergebnis des Laufs).
- **Sicherheit:** Namen vom Server, die lokal kein einfacher Dateiname sind (`..`, `/`, `\`, unter Windows Laufwerke
  und reservierte Namen wie `NUL`), sind beim Herunterladen ein Fehler; so kann ein Server nichts außerhalb des
  Zielordners schreiben. Verknüpfungen auf dem Server werden nicht verfolgt. Passwort, Schlüssel und Passphrase stehen
  in keiner Meldung.
- **Verbindung testen:** meldet sich an und liest den Ordner nach der Anmeldung („signed in to … as …; folder … has
  … entries“); geschrieben wird nichts.
- **Abhängigkeiten:** `github.com/pkg/sftp` v1.13.12 (BSD-2-Clause) mit `github.com/kr/fs` v0.1.0 (BSD-3-Clause) und
  `golang.org/x/crypto/ssh` (BSD-3-Clause, `x/crypto` war schon indirekt im Modul).

## Nachtrag 2026-11-26 (S57b): weitere Actions für `sftp`

- **Optionen wie `file.*` (ADR 0025):** `transfer.delete` (`path`, `recursive`, `ifMissing` mit Standard `skip`),
  `transfer.move` (`source`, `target`, `ifExists` mit Standard `fail`), `transfer.mkdir` (`path`), `transfer.exists`
  (`path`, `kind`; seit R5b `include`, Nachtrag) und `transfer.wait` (`path`, `interval`, `maxWait`, `stableFor`) haben Namen, Standardwerte, Output
  und Meldungen der gleichnamigen `file.*`-Actions; dazu `timeout`. Pfade auf dem Server wie in S57a.
- **`transfer.move` nur auf dem Server:** Quelle und Ziel liegen auf demselben Server; Runnel lässt den Server
  umbenennen (`posix-rename@openssh.com`, wo vorhanden, sonst bei `ifExists: replace` Löschen und `rename` wie in S57a)
  und kopiert nichts. Liegt das Ziel auf einem anderen Dateisystem des Servers, meldet der Server einen Fehler. Lokal ↔
  Server bleibt bei `transfer.upload` und `transfer.download`.
- **Verknüpfungen:** `transfer.delete` und `transfer.move` behandeln Verknüpfungen als Einträge für sich: gelöscht bzw.
  verschoben wird die Verknüpfung, nie ihr Ziel, auch nicht beim rekursiven Löschen. `transfer.exists` und
  `transfer.wait` zählen eine Verknüpfung als das, worauf sie zeigt (wie `file.exists`), eine kaputte als fehlend.
- **`transfer.wait` mit einer Verbindung:** Das Warten nutzt eine SSH-Verbindung für alle Blicke auf den Server (keine
  Anmeldung je `interval`). Bricht sie ab oder dauert ein Blick länger als `timeout` (Standard 1 Minute), schließt
  Runnel sie, meldet eine Warnung und verbindet sich beim nächsten Blick neu; ein Fehler beim Neuverbinden wird beim
  übernächsten Blick wiederholt, bis `maxWait`, das `timeout` der Action oder das Ende des Laufs erreicht ist. Ein
  abgelehnter Hostschlüssel oder eine abgelehnte Anmeldung beenden das Warten sofort, damit kein Konto durch
  Wiederholen gesperrt wird. Einen Pool über Actions hinweg gibt es weiterhin nicht.
- **Grenzen:** `timeout` gilt je Treffer (beim Löschen mit dem Inhalt des Ordners, beim Verschieben je Eintrag) bzw.
  für das Suchen der Treffer, Standard 10 Minuten. Mehr als 10000 Treffer eines Platzhalters sind bei `delete`,
  `move`, `exists` und `wait` ein Fehler (wie die Grenze von `transfer.list`).
- **Sicherheit:** Namen aus einer Ordnerliste des Servers, die `""`, `.`, `..` sind oder `/` enthalten, sind für alle
  `transfer.*`-Actions ein Fehler (sonst könnte etwa ein rekursives Löschen den übergeordneten Ordner treffen); beim
  Löschen und Verschieben zusätzlich Namen mit `\`, weil Windows-Server ihn als Trennzeichen lesen.

## Nachtrag 2026-11-26 (S57c): `ftps` und `ftp`

- **Bibliothek:** `github.com/jlaffaye/ftp` v0.2.4 (ISC, veröffentlicht am 22.08.2026; Releases 2022 und 2023,
  seit Juni 2026 wieder gepflegt mit v0.2.1 bis v0.2.4). Sie kann explizites (`AUTH TLS`) und implizites TLS, `EPSV` mit Rückfall auf `PASV` (die Adresse
  aus der Antwort auf `PASV` wird nicht verwendet, sondern die des Servers – Schutz vor Umleitung), `MLST`/`MLSD` mit
  Rückfall auf `LIST` (Formate von Unix- und Windows-Servern), `MFMT`, `PBSZ`/`PROT P` und lehnt Befehle mit
  Zeilenumbruch ab. Keine weiteren Abhängigkeiten. Für die Tests ein FTP-Server im Testprozess:
  `github.com/fclairamb/ftpserverlib` v0.32.7 (MIT, 01.11.2026; v0.33.x war jünger als zwei Wochen) mit
  `github.com/spf13/afero` v1.15.0 (Apache-2.0); beide nur in Tests, nicht in der exe und nicht in den
  Third-Party-Hinweisen.
- **Felder `ftps`:** `host`, `port` (leer: 21 bei `tls: explicit`, 990 bei `tls: implicit`), `tls` (Standard
  `explicit`), `serverName` (Name im Zertifikat, wenn `host` eine Adresse ist), `fingerprint`, `user`, `password`
  (Secret), `timeout` (Standard 15 s für Verbinden und Anmelden).
- **Zertifikat:** TLS ab 1.2. Ohne `fingerprint` muss das Zertifikat für `serverName` bzw. `host` gültig und vom
  System vertraut sein (unter Windows der Zertifikatsspeicher). Mit `fingerprint` – ein oder mehrere
  SHA256-Fingerprints des Zertifikats als 64 Hex-Ziffern, mit oder ohne Doppelpunkte, wie FileZilla Server oder
  `openssl x509 -fingerprint -sha256` sie zeigen – gilt nur ein Zertifikat mit diesem Fingerprint, auch ein
  selbst signiertes; Name und Ablauf werden dann nicht geprüft. Die Meldung nennt den Fingerprint des Servers zum
  Prüfen, wie bei `hostKey`. Übersprungen wird die Prüfung nie. Technisch prüft Runnel selbst in
  `VerifyConnection` (deshalb `InsecureSkipVerify` in der TLS-Konfiguration), damit die Meldung den Fingerprint
  nennen kann; Datenverbindungen werden genauso geprüft und nehmen die TLS-Sitzung der Steuerverbindung wieder auf.
  Server, die das Wiederaufnehmen mit Session-ID verlangen (TLS 1.2 ohne Tickets, z. B. vsftpd mit
  `require_ssl_reuse`), kann Go nicht bedienen (Testplan W69).
- **`ftp`:** dieselben Felder ohne TLS und mit `insecure`. Ohne `insecure: true` baut Runnel keine Verbindung auf
  („ftp sends the password and all data unencrypted …“); `validate` warnt in beiden Fällen (ohne `insecure`, dass
  kein Lauf sich verbindet; mit, dass Passwort und Daten unverschlüsselt sind).
- **Gleiche Bedeutung wie `sftp`:** Die Actions sprechen über eine kleine Schnittstelle (`remoteFS`) mit dem Server;
  SFTP und FTP sind je eine Datei. Temporäre Datei und Umbenennen mit `RNFR`/`RNTO`; weil Server sich beim
  vorhandenen Ziel unterschiedlich verhalten, löscht Runnel es vorher (wie der Rückfall in S57a). Grenzen,
  `timeout`, `maxSize`, `ifExists`, unsichere Namen und `transfer.wait` wie bei `sftp`: Ein Fehler beim Anmelden
  (Antwort 530/430) oder ein abgelehntes Zertifikat beendet das Warten sofort; eine abgebrochene Verbindung (auch
  Antwort 421) wird beim nächsten Blick neu aufgebaut. Ein `timeout` schließt die Steuerverbindung und alle
  Datenverbindungen.
- **Abweichungen durch FTP:** Nur passiver Modus. `MLST` für einzelne Einträge, wo vorhanden, sonst die Liste des
  übergeordneten Ordners. Die Einträge `.` und `..` einer Liste sind bei FTP üblich und werden übersprungen statt
  abgelehnt; andere unsichere Namen bleiben ein Fehler. Die Änderungszeit bleibt beim Hochladen nur mit `MFMT`
  erhalten, sonst wird sie still nicht gesetzt; mit `LIST` sind Zeiten oft nur auf die Minute oder den Tag genau.
  Verknüpfungen zeigt FTP meist nicht (der Server löst sie auf); wo `LIST` sie zeigt, ist ihr Ziel unbekannt –
  `transfer.download` lehnt sie ab, `delete` und `move` behandeln sie als Eintrag für sich (TESTPLAN K16).
- **Verbindung testen:** wie bei `sftp`, „signed in to … as …; folder … has … entries“; geschrieben wird nichts.
- **Größe:** S57c blieb nach Absprache mit dem Maintainer ein Schritt (Schnittstelle, beide Typen, Editor).

## Nachtrag 2026-11-26 (R5a): Nur Quellen, die es gibt

`secretStore` in `project.yaml` und Environments nimmt nur Quellen an, die heute funktionieren: `env` und `inline`.
`shared` (S39e, nicht für 1.0) ist kein Wert mehr; `${secret:shared/NAME}` meldet eine unbekannte Quelle. `local` bleibt
die Standardquelle ohne `secretStore:` (Nachtrag S39a), lässt sich aber erst mit S39d setzen: `secretStore: local`
meldet bis dahin „the local secret store is not available yet“. Das Schema nennt nur `env` und `inline` und keinen
Standardwert; S39d fügt `local` wieder hinzu (additiv). So sagt 1.0 keine Bedeutung eines nie gebauten Werts zu.

## Nachtrag 2026-11-26 (R5b): einheitliche Namen und Standards

- **`timeout` in `postgres` und `sqlserver`:** Die Zeit für den Verbindungsaufbau heißt `timeout` wie bei `sftp`,
  `ftps`, `ftp` und `smtp` (Standard bleibt 30 s); die Beschreibung sagt, dass er nur den Aufbau begrenzt (Abfragen haben
  ihr eigenes `timeout` in Action bzw. Transform). Der alte Name `connectTimeout` ist ein unbekanntes
  Feld.
- **`auth: basic` bei `smtp`:** Anmeldung mit `user` und `password` heißt wie bei der Connection `http` `basic`;
  `oauth2` und `none` stimmen schon überein. Leer bleibt `basic`, wenn `user` gesetzt ist. `auth: password` ist wie
  jeder andere unbekannte Wert ein ungültiger Wert (vorher als Passwort-Anmeldung behandelt).
- **`transfer.exists`:** `include: all|files|folders` (Standard `all`) wie `transfer.list` statt `kind: any|file|folder`.
- **`transfer.wait`:** `maxWait` hat den Standard 1 h (wie `file.wait`); `0` wartet bis zum `timeout` der Action oder
  zum Ende des Laufs.
- **`transfer.download`:** `maxSize` hat den Standard 1 GB wie `http.request` mit `saveTo`; eine Datei ohne Grenze gibt
  es nicht mehr, größere Werte (z. B. `20GB`) sind möglich.
- Alte Namen sind unbekannte Felder, alte Werte ungültige Werte, ohne Übergangszeit (bewusster Bruch vor 1.0, ADR 0029).

## Nachtrag 2026-11-27 (S39d): lokaler Store

S39d ist geteilt (Store und CLI vor DPAPI und Dienst): S39d1 setzt den Store mit `RUNNEL_MASTER_KEY` und
`runnel secret set/list/delete` um, S39d2 den Schlüssel per DPAPI, den Store des Windows-Dienstes (`--service`) und
`runnel secret missing`.

**Entscheidungen des Maintainers (2026-11-27):**

- Für 1.0 gibt es nur `runnel secret set`, `list`, `delete` und `missing`. Ablaufdatum (`--expires`),
  `export`/`import`, ein Master-Passwort beim Start und das Setzen von Secrets im Editor (vorher Mockup) stehen im
  Backlog („S39d (Teil)“).
- Der Store des Windows-Dienstes (S39d2) schützt seinen Schlüssel mit DPAPI **für den Rechner**
  (`CRYPTPROTECT_LOCAL_MACHINE`), nicht für ein Benutzerkonto: ihn schreiben Administratoren, lesen das Konto des
  Dienstes. Den Zugriff begrenzen die Rechte des Ordners (Administratoren und SYSTEM, Lesen für das Konto des Dienstes).
- Werte sind an den Projektordner gebunden: Wird das Projekt verschoben, müssen die Secrets neu gesetzt werden.
- `RUNNEL_MASTER_KEY` ist Base64 von genau 32 Byte; eine Passphrase gibt es nicht.

**Umsetzung in S39d1:**

- **Ablage:** eine JSON-Datei je Projekt im Ordner des Benutzers, nie im Projekt: `%LOCALAPPDATA%\runnel\secrets\`
  (nicht `%AppData%`, damit nichts mit dem Profil wandert; sonst `<Konfigurationsordner>/runnel/secrets/`, also
  `$XDG_CONFIG_HOME` bzw. `~/.config`). Der Dateiname ist der Name des Projektordners mit 16 Hex-Ziffern eines SHA-256
  über den absoluten Pfad (unter Windows klein geschrieben), wie bei der Run-Historie (ADR 0026); die Datei nennt den
  Pfad selbst, eine Datei unter fremdem Namen ist ein Fehler. Statt `secrets.db` (SQLite) genügt JSON: wenige
  Einträge, keine Abfragen.
- **Format (Version 1):** `version`, `project`, `keyId` und je Secret `nonce`, `value` (AES-256-GCM, Base64) und
  `updated`. Jeder Wert ist einzeln mit eigener Zufalls-Nonce verschlüsselt; die zusätzlichen Daten von GCM
  (`runnel secret v1`, Projekt, Name) binden ihn an Projekt und Namen, vertauschte oder kopierte Werte lassen sich
  nicht entschlüsseln. Namen und Zeiten stehen im Klartext: `list`, `delete` und der Zustand im Editor brauchen
  keinen Schlüssel. Wer die Datei schreiben darf, kann Einträge löschen oder einen alten Stand zurückspielen, aber
  keinen Wert lesen oder unterschieben. Geschrieben wird über eine temporäre Datei mit Umbenennen (Rechte `0600`,
  Ordner `0700`; unter Windows schützt das Benutzerprofil). Zwei gleichzeitige `secret set` für dasselbe Projekt
  können sich überschreiben (der letzte gewinnt); eine Sperre gibt es nicht. Ein Wert hat höchstens 1 MB, eine Datei
  wird bis 32 MB gelesen; der Leser der Datei hat ein Fuzz-Ziel (`FuzzDecodeFile`).
- **Schlüssel:** `RUNNEL_MASTER_KEY` ist der Schlüssel, auf jedem System: genau 32 Byte in Base64 (Standard- oder
  URL-Alphabet, mit oder ohne `=`), etwa aus `openssl rand -base64 32`. Andere Längen, ungültiges Base64 und ein
  Schlüssel aus einem einzigen wiederholten Byte sind Fehler; die Meldung nennt den Wert nie. Ein Passwort mit
  Ableitung (scrypt) gibt es nicht: ein zu schwaches Passwort ließe sich nicht erkennen, ein Zufallsschlüssel schon.
  Ohne die Variable meldet jedes Lesen oder Setzen eines Werts „set RUNNEL_MASTER_KEY to 32 random bytes in base64 …“;
  unter Windows tritt mit S39d2 ein mit DPAPI geschützter Zufallsschlüssel an diese Stelle, die Variable hat dann
  Vorrang (z. B. im Container oder für den portablen Betrieb). Aus dem Schlüssel werden mit HMAC-SHA256 der Schlüssel
  für AES und die `keyId` abgeleitet; mit einem anderen Schlüssel meldet Runnel „stored with another key“ statt eines
  Fehlers je Wert, und `set` mischt keine Schlüssel in einer Datei.
- **Bedienung:** `runnel secret set NAME [projektordner]` liest den Wert verdeckt von der Konsole (Windows; sonst ein
  Hinweis auf stdin oder `--from-file`), von stdin (ein Zeilenumbruch am Ende entfällt) oder mit `--from-file` (Inhalt
  unverändert, muss UTF-8 sein), nie als Argument; `--base64` speichert Binärdateien wie PFX als Base64. `list` zeigt
  Namen und Änderungszeit, `delete` meldet ein nicht gesetztes Secret als Fehler. `--json` gibt es vorerst nicht (wie
  `triggers` und `plugins`). `runnel serve`, `run` und `validate` lesen den Store des Benutzers.
- **Prüfung:** `validate` meldet für `local` wie für `env` keine fehlenden Werte (es ruft keine Werte ab). Ein Lauf,
  dem ein Secret fehlt, startet nicht: „secret "X" is not set in the local secret store of this project (set it with:
  runnel secret set X)“. `GET /api/v1/secrets` meldet für `local` jetzt `set` oder `missing`; `unavailable` bleibt für
  unbekannte oder nicht lesbare Quellen. `secretStore: local` ist in Schema und Editor wieder ein Wert (additiv zu R5a).
- **Keine neue Abhängigkeit:** `crypto/aes`, `crypto/cipher`, `crypto/hmac` der Standardbibliothek. Paket
  `internal/secretstore`.
