# ADR 0013: Typnamen, Namensräume und Aufbau der Bausteine

- **Status:** Angenommen
- **Datum:** 2026-11-19
- **Bezug:** [Konzept, Kap. 7 – Erweiterbarkeit](../KONZEPT.md#7-erweiterbarkeit-plugins), [Kap. 14 – Code-Struktur](../KONZEPT.md#14-code-struktur-monorepo)

## Kontext

Actions (Workflows) und Transforms (Pipelines) werden über Typnamen der Form `<namensraum>.<name>` angesprochen,
z. B. `file.copy`. Bei der Planung zeigten sich Konflikte:

| Typname | als Action | als Transform |
|---|---|---|
| `http.request` | HTTP-Aufruf im Workflow (S10) | HTTP-Aufruf je Datensatz (Konzept Kap. 5.2) |
| `core.log` | Log-Meldung (S05) | Testbaustein der Pipeline-Engine (S13) |
| `shell.powershell` | PowerShell-Action | PowerShell-Transform – im Konzept behelfsweise `shell.powershell-transform` |
| `core.script` | JavaScript-Action | JavaScript-Transform – im Konzept behelfsweise `core.script-transform` |

Außerdem war für Plugins offen, wie `provides:` Actions und Transforms unterscheidet und was passiert, wenn ein Plugin
einen Namensraum eines eingebauten Bausteins oder eines anderen Plugins verwendet.

Schließlich lagen alle Core-Actions in einer Datei; mit den geplanten Datei-, HTTP- und Datenbank-Actions würde der
Code unübersichtlich.

## Entscheidung

### 1. Getrennte Typ-Namensräume für Actions und Transforms

- Es gibt je eine Registry für Actions und für Transforms. Welche gilt, ergibt sich aus der Datei:
  `*.workflow.yaml` → Actions, `*.pipeline.yaml` → Transforms.
- Derselbe Typname darf daher als Action **und** als Transform existieren (z. B. `http.request`, `core.log`,
  `shell.powershell`, `core.script`). Behelfsnamen mit `-transform` entfallen.
- In Dokumentation und Editor wird die Art immer mitgenannt („Action `http.request`“, „Transform `http.request`“).

### 2. Ein Namensraum gehört genau einem Anbieter

| Namensräume | Anbieter |
|---|---|
| `ai`, `archive` (vormals `zip`, siehe Nachtrag), `core`, `csv`, `db`, `excel`, `file`, `http`, `json`, `mail`, `mcp`, `shell`, `transfer` (vormals `ftp`, `sftp`, siehe Nachtrag), `xml` | in Runnel eingebaut (reserviert, auch wenn noch nicht umgesetzt) |
| `mcp-<connection>` | Werkzeuge eines MCP-Servers, der als Connection eingebunden ist (ADR 0012) |
| jeder andere | genau ein Plugin, das ihn in `plugin.yaml` erklärt |

- Ein Plugin darf nur Typen im eigenen Namensraum anbieten.
- Ein reservierter Namensraum, ein von zwei Plugins beanspruchter Namensraum oder ein Typ außerhalb des eigenen
  Namensraums ist beim Laden der Plugins ein Fehler. Plugins können eingebaute Bausteine also weder überschreiben
  noch mit anderen Plugins kollidieren.
- Die Liste steht in `internal/model/namespaces.go`; ein Vertragstest prüft, dass jede eingebaute Action in einem
  reservierten Namensraum liegt.

### 3. `provides:` nach Art getrennt

> Überholt durch [ADR 0019](0019-plugin-protokoll.md): `plugin.yaml` nennt die Typen nicht mehr; das Plugin meldet sie
> mit `describe`, Typen außerhalb seines Namensraums sind ein Fehler beim Laden.

```yaml
# plugins/sap-rfc/plugin.yaml
name: sap-rfc
version: 1.0.0
namespace: sap
executable: sap-rfc-plugin.exe
provides:
  actions: [sap.rfc-call]
  transforms: [sap.table-input]
```

Der Descriptor eines Bausteins (Konzept Kap. 7.4) enthält weiterhin `kind: action|transform`; er muss zur Liste in
`provides:` passen.

### 4. Aufbau des Codes: ein Paket je Namensraum, eine Datei je Baustein

```
internal/actions/
  actions.go           # Builtin(): die einzige Liste aller eingebauten Namensräume
  actions_test.go      # Vertragstest für alle eingebauten Actions
  actiontest/          # Test-Hilfen für Actions
  core/                # ein Paket je Namensraum
    register.go        # Register(): alle Actions des Namensraums
    start.go           # eine Datei je Action: Definition, Konfiguration, Execute
    start_test.go      # Tests direkt daneben
    log.go
    log_test.go
    …
internal/transforms/   # gleicher Aufbau für Transforms (ab S12/S13)
```

**Zentrale und dezentrale Registrierung kombiniert:**

| Ebene | Aufgabe | Begründung |
|---|---|---|
| `register.go` je Namensraum | listet die Actions des Namensraums | Eine neue Action ändert nur ihr eigenes Paket; die Definitionen bleiben unexportiert |
| `internal/actions/actions.go` | listet die Namensräume, liefert `Builtin()` | Eine Stelle für CLI, Server und MCP; der Vertragstest erfasst automatisch alle Namensräume; die Engine kennt keine konkreten Actions |

Bewusst **nicht** gewählt:
- **Nur eine zentrale Datei**, die jede einzelne Action einträgt: Die Definitionen müssten exportiert werden, und jede
  neue Action änderte eine Datei außerhalb ihres Pakets (Merge-Konflikte bei parallelen PRs).
- **Selbstregistrierung über `init()`**: implizit, abhängig von Imports und deren Reihenfolge, schwer zu testen.
- **Ein Paket je Action**: zu kleinteilig; Pakete je Namensraum sind in Go üblich.

**Vertragstest:** Für jede eingebaute Action wird automatisch geprüft: gültiger Typname, reservierter Namensraum,
Beschreibung vorhanden, unbekannte Felder in `with:` werden abgelehnt, Ende bei abgebrochenem Lauf. Später kommen
JSON-Schema (Editor, Kap. 7.4) und Ausgabedaten (S08) hinzu.

## Konsequenzen

**Positiv**
- Kurze, sprechende Typnamen ohne `-transform`-Anhängsel; Parallelen zu Apache Hop (HTTP-Action und REST-Transform).
- Keine Kollisionen zwischen eingebauten Bausteinen, Plugins und MCP-Servern.
- Neue Actions sind kleine, abgeschlossene Änderungen (zwei Dateien plus eine Zeile in `register.go`); der
  Vertragstest schützt einheitliche Regeln.

**Negativ / zu beachten**
- Derselbe Name mit unterschiedlicher Bedeutung je Art – Dokumentation und Editor müssen die Art immer zeigen.
- Die Liste reservierter Namensräume muss bei neuen eingebauten Bereichen ergänzt werden (Vertragstest erinnert daran).

## Nachtrag 2026-11-22 (S54): Namensraum `archive` statt `zip`

Der reservierte Namensraum `zip` wird durch `archive` ersetzt. Die Actions `archive.create` und `archive.extract`
verarbeiten ZIP, TAR und TAR.GZ und wählen das Format über `format` (bzw. die Endung); weitere Formate kommen als
Wert von `format` hinzu, nicht als eigene Typnamen. `zip` ist nicht mehr reserviert, war aber nie umgesetzt.

## Nachtrag 2026-11-22 (ADR 0022): Namensraum `transfer` statt `ftp` und `sftp`

Die reservierten Namensräume `ftp` und `sftp` werden durch **`transfer`** ersetzt. Dateitransfer-Actions
(`transfer.upload`, `transfer.download`, `transfer.list` …) wählen das Protokoll über die Connection (`type: sftp |
ftps | ftp`), nicht über den Typnamen – wie `archive` das Format über `format` ([ADR 0022](0022-connections-secrets.md),
Abschnitt 6). `ftp` und `sftp` waren nie umgesetzt und sind nicht mehr reserviert.

## Nachtrag 2026-11-24: Namen von SDKs und Plugins (zu ADR 0019)

SDKs und offizielle Plugins liegen in eigenen Repositorys der Organisation `runnelhq` (ADR 0019, Nachtrag
2026-11-24). Abgestimmt mit dem Maintainer:

- **Repositorys in `runnelhq` ohne Präfix**, die Organisation nennt den Zusammenhang:

  | Art | Repository | Beispiel |
  |---|---|---|
  | SDK | `sdk-<sprache>` | `runnelhq/sdk-go`, `sdk-dotnet`, `sdk-python`, `sdk-rust`, `sdk-node` |
  | Offizielles Plugin | `plugin-<name>` | `runnelhq/plugin-iban` |
  | Vorlage für Plugins | `plugin-template-<sprache>` | `runnelhq/plugin-template-go` (GitHub-Template) |

- **Mit „runnel“ alles, was die Organisation verlässt:**
  - **Pakete der SDKs:**
    - Go: Modul `github.com/runnelhq/sdk-go`, Paket `runnel`; Major-Versionen mit `/v2`.
    - PyPI: `runnel-sdk`.
    - NuGet: `Runnel.Sdk`.
    - crates.io: `runnel-sdk`.
    - npm: `@runnelhq/sdk`.
  - **Release-Archive von Plugins:** `runnel-plugin-<name>_<version>_<os>_<arch>.zip`. Darin liegt der fertige Ordner
    `<name>/` mit `plugin.yaml`, ausführbaren Dateien, `LICENSE` und `THIRD_PARTY_NOTICES`, dazu `SHA256SUMS`.
  - **GitHub-Topic** `runnel-plugin`.
- **Name und Namensraum:** `<name>` des Repositorys ist gleich `name:` in `plugin.yaml` und gleich dem Ordner unter
  `plugins/`. Bei offiziellen Plugins ist er auch der Namensraum, kurz und ohne Präfix, z. B. `iban`, `ldap`, `pdf`.
- **Plugins von anderen:**
  - Empfohlen ist das Repository `runnel-plugin-<name>` in der eigenen Organisation.
  - Für den Namensraum empfiehlt sich ein Präfix der Firma oder des Autors, z. B. `contoso-sap`, damit es keine
    Zusammenstöße mit offiziellen Plugins gibt.
  - Durchgesetzt wird das nicht. Zwei Plugins mit demselben Namensraum in einem Projekt bleiben ein Fehler beim
    Laden (oben).
- **Versionen:**
  - Plugins und SDKs haben SemVer-Tags `v1.2.0`, unabhängig von der Version von Runnel.
  - Die Version in `plugin.yaml` muss zum Tag passen; das prüft die CI des Plugins.
  - Ein SDK nennt in seiner README die unterstützte Protokollversion.
