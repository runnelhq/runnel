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
| `ai`, `archive` (vormals `zip`, siehe Nachtrag), `core`, `csv`, `db`, `excel`, `file`, `ftp`, `http`, `json`, `mail`, `mcp`, `sftp`, `shell`, `xml` | in Runnel eingebaut (reserviert, auch wenn noch nicht umgesetzt) |
| `mcp-<connection>` | Werkzeuge eines MCP-Servers, der als Connection eingebunden ist (ADR 0012) |
| jeder andere | genau ein Plugin, das ihn in `plugin.yaml` erklärt |

- Ein Plugin darf nur Typen im eigenen Namensraum anbieten.
- Ein reservierter Namensraum, ein von zwei Plugins beanspruchter Namensraum oder ein Typ außerhalb des eigenen
  Namensraums ist beim Laden der Plugins ein Fehler. Plugins können eingebaute Bausteine also weder überschreiben
  noch mit anderen Plugins kollidieren.
- Die Liste steht in `internal/model/namespaces.go`; ein Vertragstest prüft, dass jede eingebaute Action in einem
  reservierten Namensraum liegt.

### 3. `provides:` nach Art getrennt

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
