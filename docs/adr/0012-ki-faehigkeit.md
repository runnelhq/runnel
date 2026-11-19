# ADR 0012: KI-Fähigkeit – LLMs, Agenten und MCP

- **Status:** Angenommen
- **Datum:** 2026-11-19
- **Bezug:** [Konzept, Kap. 2.9 – KI-Funktionen](../KONZEPT.md#29-ki-funktionen), [Roadmap](../ROADMAP.md)

## Kontext

Workflow-Werkzeuge wie n8n bieten inzwischen umfangreiche KI-Funktionen: Aufrufe von Sprachmodellen (LLMs),
Agenten, die selbstständig Werkzeuge aufrufen, Gesprächsgedächtnis, Vektor-Datenbanken für RAG, Chat-Trigger sowie
MCP (Model Context Protocol) – als Client, der fremde MCP-Server nutzt, und als Server, der eigene Workflows als
Werkzeuge für KI-Assistenten bereitstellt.

Zu prüfen war, ob eine der bisherigen Entscheidungen solche Funktionen verhindert, und was jetzt festgelegt werden
muss, damit sie später ohne Umbau möglich sind.

### Prüfung der bisherigen Entscheidungen

| Entscheidung | Ergebnis |
|---|---|
| Go ([ADR 0002](0002-go-backend.md)) | Kein Hindernis: offizielle Go-SDKs für gängige LLM-Anbieter und MCP verfügbar; Python-Bibliotheken über Plugins (Kap. 7.3) |
| Plugin-Protokoll JSON-RPC über stdio | Günstig: MCP basiert ebenfalls auf JSON-RPC 2.0 über stdio |
| Ausdrücke `expr-lang`, JavaScript in Skripten ([ADR 0006](0006-ausdruckssprache.md)) | Kein Hindernis: LLM-Antworten als JSON über `fromJSON()` weiterverarbeitbar |
| Pipelines schemalos mit optionalen Typen ([ADR 0007](0007-typsystem-pipelines.md)) | Günstig: LLM-Ausgaben sind oft lose strukturiertes JSON |
| Ausführung pro Datensatz ([ADR 0008](0008-ausfuehrungsvarianten.md)) | Günstig: Batch-Verarbeitung mit LLMs, RAG-Aufbereitung als Pipeline |
| Workflow-Engine mit Links nach Apache-Hop-Vorbild | Kein Hindernis: Agenten-Schleifen liegen innerhalb einer Action (wie bei n8n), nicht im Graphen |
| Keine Benutzerverwaltung ([ADR 0005](0005-mehrbenutzerbetrieb.md)) | Kein Hindernis, aber Leitplanken für Agenten nötig (siehe unten); MCP über HTTP braucht ein Token |
| YAML-Dateien, Git, `runnel validate` | Günstig: LLMs können Workflows erzeugen und anhand der präzisen Meldungen selbst korrigieren |
| MIT-Lizenz ([ADR 0004](0004-lizenz-mit.md)) | Kein Hindernis |

**Ergebnis:** Keine Entscheidung verhindert KI-Funktionen. Es fehlen aber Bausteine im Datenmodell, die mit wachsendem
Code teurer nachzurüsten wären: typisierte Parameter, Rückgabewerte von Workflows, Ausgabedaten von Actions und
Leitplanken für Agenten.

## Entscheidung

### 1. KI-Funktionen sind Actions und Transforms

- Aufrufe von Sprachmodellen (`ai.chat`), Agenten (`ai.agent`) und MCP-Werkzeugen (`mcp.call-tool`) sind normale
  Actions; Embeddings, Text-Zerlegung und Vektor-Ausgaben sind Pipeline-Transforms.
- Die **Agenten-Schleife** (Modell entscheidet → Werkzeug aufrufen → Ergebnis zurück an das Modell) läuft **innerhalb**
  der Action `ai.agent`. Die Semantik von Workflows und Links ändert sich dafür nicht.
- Modell-Anbieter werden als **Connection** hinterlegt (z. B. Anthropic, OpenAI-kompatible APIs, Ollama lokal);
  Schlüssel nur über `${secret:…}`. Kein Anbieter ist fest eingebaut.

### 2. Workflows sind Werkzeuge – mit typisierten Parametern und Rückgabewerten

Derselbe Vertrag gilt für Sub-Workflows, Agenten-Werkzeuge, MCP-Tools und Webhooks:

```yaml
parameters:
  - name: CUSTOMER_ID
    type: integer            # string (Standard) | integer | number | boolean | json
    description: Kundennummer im ERP
    required: true
  - name: MODE
    enum: [full, delta]      # erlaubte Werte
    default: delta
outputs:
  - name: STATUS
    type: string
    description: Ergebnis des Abgleichs
    value: "${SYNC_STATUS}"  # ab S08 auch Ausdrücke {{ … }}
```

- `type`, `enum` und `description` ergeben das **Eingabe-Schema** (JSON Schema) eines Werkzeugs.
- `outputs` werden ausgewertet, wenn der Workflow erfolgreich endet, und bilden sein **Ergebnis**.
- Parameter ohne `type` bleiben Text – bestehende Dateien bleiben gültig.

### 3. Actions liefern Ausgabedaten

Das Ergebnis einer Action enthält neben Erfolg/Fehler strukturierte **Ausgabedaten** (z. B. Antwort eines Modells,
Statuscode, Dateiliste), die nachfolgende Actions über `result` und `results["id"]` lesen (Kap. 6.1, ADR 0006).
Das ist Bestandteil von Roadmap-Schritt S08.

### 4. MCP in drei Rollen

| Rolle | Umsetzung |
|---|---|
| **Server** | `runnel mcp` stellt die Workflows eines Projekts als MCP-Tools bereit – zuerst über stdio (z. B. für Claude Desktop, Claude Code), später über HTTP mit Token; OAuth bei Bedarf |
| **Client** | Action `mcp.call-tool`; MCP-Server werden als Connection hinterlegt (stdio-Prozess oder HTTP) |
| **Plugin-Quelle** | Die Werkzeuge eines eingebundenen MCP-Servers erscheinen als Actions in der Palette |

### 5. Werkzeuge eines Agenten

- Zunächst in der Konfiguration: `with.tools` verweist auf Workflows, Actions oder MCP-Tools.
- Später im Editor sichtbar über einen zusätzlichen Link-Typ `tool` – eine Erweiterung des Formats `runnel/v1`, kein Bruch.

### 6. Leitplanken für Agenten

Da es keine Benutzerverwaltung gibt (ADR 0005), schützen technische Regeln:

- **Positivliste:** Ein Agent darf nur die Werkzeuge aufrufen, die in seiner Konfiguration stehen – kein impliziter
  Zugriff auf Dateien, Shell oder andere Workflows.
- **Freigabe:** Action `core.approval` pausiert einen Lauf, bis jemand mit Zugang zum Server zustimmt oder ablehnt
  (mit Zeitlimit). Pausen über Stunden setzen voraus, dass Läufe einen Neustart überstehen (Resume, S29).
- **Nachvollziehbarkeit:** Jeder Modell- und Werkzeugaufruf erscheint in den Lauf-Ereignissen (Modell, Dauer, Token);
  Secrets werden in Prompts, Antworten und Logs maskiert.
- **Begrenzung:** Höchstzahl an Agenten-Schritten je Aufruf, Rate-Limit je Connection, Token- und Kosten-Erfassung je Lauf.

## Konsequenzen

**Positiv**
- KI-Funktionen lassen sich schrittweise ergänzen, ohne Engine oder Dateiformat umzubauen.
- Ein einheitlicher Werkzeug-Vertrag (Parameter + Outputs) dient Sub-Workflows, Agenten, MCP und Webhooks.
- `runnel mcp` macht vorhandene Workflows mit wenig Aufwand für KI-Assistenten nutzbar.

**Negativ / zu beachten**
- Das Workflow-Format wird um `type`, `enum` und `outputs` erweitert (Roadmap-Schritt S05a) – bevor weitere Schritte darauf aufbauen.
- Ausgabedaten von Actions sind fester Bestandteil von S08; Resume (S29) ist Voraussetzung für Freigaben.
- MCP über HTTP mit OAuth und Agenten-Freigaben im Editor erfordern später eigene Schritte.
- LLM-Ergebnisse sind nicht deterministisch: Tests von KI-Actions nutzen Testdoubles der Anbieter-APIs.
