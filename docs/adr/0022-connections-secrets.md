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
- **Eingebaut immer vorhanden** (ohne Datei): `local` (lokaler Store des Projekts), `shared` (lokaler Store, für alle
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
