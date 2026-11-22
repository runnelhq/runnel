# ADR 0014: Action-Descriptor und Schema aus den Konfigurations-Structs

- **Status:** Angenommen
- **Datum:** 2026-11-19
- **Bezug:** [Konzept, Kap. 7.4 – Registry & UI-Integration](../KONZEPT.md#74-registry--ui-integration),
  [ADR 0012 – KI-Fähigkeit](0012-ki-faehigkeit.md), [ADR 0013 – Typnamen](0013-typnamen-namensraeume.md),
  Roadmap-Schritt E1

## Kontext

Der Editor soll Palette und Eigenschaftsdialog **vollständig aus einer Beschreibung** der Bausteine erzeugen
(Konzept Kap. 7.4). Dieselbe Beschreibung braucht die API (`/catalog`), später der MCP-Server und KI-Agenten, die
Actions als Werkzeuge mit Eingabe-Schema nutzen (ADR 0012). Bisher hatte eine Action nur Typname, Beschreibung und
Konstruktor; ihre Felder waren nur im Code bekannt.

Offene Fragen:

1. Was gehört in die Beschreibung (Descriptor)?
2. Wie entsteht das JSON-Schema der `with:`-Felder – von Hand geschrieben oder aus dem Code erzeugt?
3. Falls erzeugt: mit einer Bibliothek oder einem eigenen Generator?

## Entscheidung

### 1. Descriptor

Jede Action liefert einen Descriptor:

```json
{
  "type": "core.wait",
  "kind": "action",
  "category": "general",
  "label": "Wait",
  "description": "Waits for a given time",
  "schema": {
    "$schema": "https://json-schema.org/draft/2020-12/schema",
    "type": "object",
    "properties": {
      "duration": {
        "type": "string",
        "description": "How long to wait, e.g. 30s, 5m or 1h30m",
        "format": "runnel-duration",
        "examples": ["30s"]
      }
    },
    "required": ["duration"],
    "additionalProperties": false
  }
}
```

| Feld | Bedeutung |
|---|---|
| `type` | Typname (ADR 0013) |
| `kind` | `action`; Transforms erhalten später `transform` |
| `category` | feste Kennung für die Gruppe in der Palette: `general`, `execution`, `files`, `network`, `database` (Konzept Kap. 12.1); weitere nur per Änderung dieser Liste |
| `label` | kurzer Anzeigename, englisch |
| `description` | ein englischer Satz ohne Punkt |
| `schema` | JSON Schema (Draft 2020-12) der `with:`-Konfiguration |

- **Sprache:** Kennungen, Bezeichnungen und Beschreibungen sind englisch, wie die gesamte Oberfläche
  ([ADR 0015](0015-sprache-oberflaeche.md)). Der Editor zeigt die Kategorie-Kennungen als englische Anzeigenamen.
- **Noch nicht enthalten:** `icon`, `ports` und `uiHints` aus dem Konzept. Sie kommen mit dem Schritt, der sie braucht
  (Icons mit E5/E6, Ports mit den Pipelines, Darstellungshinweise mit E7).
- **Variablen:** Das Schema beschreibt den Wert **nach** dem Auflösen von `${…}`. Werte mit `${…}` oder `{{ … }}`
  werden im Editor nicht gegen `enum`, `format` oder Zahlentypen geprüft; das tut die Action beim Ausführen.

### 2. Schema wird aus dem Konfigurations-Struct erzeugt

Die Struct, in die `action.DecodeConfig` die Konfiguration einliest, ist die **einzige Quelle**. Die Definition nennt
sie in `Config`; die Registry erzeugt daraus beim Registrieren das Schema. Beschreibungen und Regeln stehen als Tags
an den Feldern:

```go
type wait struct {
	Duration string `json:"duration" doc:"How long to wait, e.g. 30s, 5m or 1h30m" schema:"required,format=runnel-duration,example=30s"`
}
```

| Tag | Inhalt |
|---|---|
| `json` | Feldname wie im YAML |
| `doc` | Beschreibung für Editor und Werkzeuge (Pflicht, prüft der Vertragstest) |
| `schema` | `required`, `default=…`, `enum=a\|b\|c`, `example=…`, `format=…` |

Eigene Formate (`format`): `runnel-duration` (Zeitangabe wie `30s`, `5m`, `1h30m`) und – ergänzt mit E7a –
`runnel-text` (mehrzeiliger Text; der Editor zeigt ein wachsendes Textfeld, alle anderen Texte sind einzeilig).

- **Pflichtfelder** prüft `DecodeConfig` anhand von `schema:"required"`. Actions schreiben keine eigenen Prüfungen
  auf fehlende Felder mehr; Schema und Verhalten können so nicht voneinander abweichen.
- **Typen:** Text → `string`, Ganzzahl → `integer`, Kommazahl → `number`, `bool` → `boolean`, Slices → `array`,
  `map[string]T` → `object` mit `additionalProperties`, verschachtelte Structs → `object` mit eigenen Feldern,
  `any` → beliebiger Wert. Andere Typen (Kanäle, Funktionen, rekursive Typen) sind beim Registrieren ein Fehler.
- **Unbekannte Felder** sind verboten (`additionalProperties: false`), wie bisher in `DecodeConfig`.
- **Eigene Formate:** `runnel-duration` für Zeitangaben wie `30s` oder `1h30m`. JSON Schemas eigenes Format `duration`
  meint ISO 8601 (`PT30S`) und wird daher nicht verwendet.
- Die **Reihenfolge der Felder** im Schema entspricht der Struct; der Editor zeigt sie in dieser Reihenfolge.
- Regeln zwischen Feldern („entweder `source` oder `pattern`“) kann der Generator nicht ausdrücken. Braucht eine Action
  sie, wird der Generator um eine Möglichkeit zum Ergänzen des Schemas erweitert – erst dann (kein Vorgriff).

### 3. Eigener, kleiner Generator statt Bibliothek

Der Generator liegt in `internal/schema` (knapp 400 Zeilen inkl. Prüfung der Pflichtfelder) und kann genau das, was
Konfigurations-Structs brauchen.

| | Eigener Generator | `invopop/jsonschema` v0.14.0 |
|---|---|---|
| Abhängigkeiten | keine | `pb33f/ordered-map`, `bahlo/generic-list-go`, `buger/jsonparser`, `go.yaml.in/yaml/v4` (Release Candidate) u. a. |
| Umfang | nur benötigte Typen und Optionen | vollständig, inkl. `$ref`/`$defs`, Kommentare aus dem Quellcode |
| Pflichtfelder in `DecodeConfig` | gleiche Tag-Auswertung | eigene Auswertung nötig |
| Pflege | durch uns | durch die Bibliothek |

Unsere Konfigurationen sind flach und einfach; der volle Umfang der Bibliothek wird nicht gebraucht. Weniger
Abhängigkeiten bedeuten weniger Pflege bei Lizenzhinweisen, SBOM und `govulncheck`. Reicht der eigene Generator später
nicht mehr, lässt er sich ersetzen, ohne den Descriptor zu ändern.

### 4. Vertragstest und Ausgabe

Der Vertragstest (`internal/actions/actions_test.go`) verlangt für jede eingebaute Action: gültige Kategorie,
Bezeichnung mit Großbuchstaben am Anfang, Beschreibung ohne Punkt, Beschreibung (`doc`) für jedes Feld. Er baut aus dem
Schema eine Beispielkonfiguration (Beispiele, Standardwerte, erlaubte Werte) und prüft, dass `New` sie annimmt – so
fällt auf, wenn `Config` und `New` verschiedene Structs verwenden. Der Abbruch-Test nutzt dieselbe
Beispielkonfiguration; Pflichtfelder brauchen daher ein realistisches `example`.

`runnel catalog` zeigt die Actions nach Kategorien, `runnel catalog <typ>` ihre Felder, `--json` die Descriptors in
der Form, in der `/catalog` (E2) sie ausliefert.

## Konsequenzen

- Neue Actions erscheinen ohne Frontend-Code in Palette und Eigenschaftsdialog; eine Änderung am Struct ändert
  automatisch Schema, Editor und Werkzeugbeschreibung.
- Plugins (S49 ff.) liefern ihren Descriptor mit fertigem JSON Schema; Editor und API unterscheiden nicht, woher er
  stammt.
- Transforms (S12 ff.) verwenden denselben Generator und dieselben Tags.
- Golden-Dateien (`internal/cli/testdata/catalog*.golden*`) zeigen jede Änderung an Descriptors im PR-Diff.
- Die Tag-Syntax erlaubt keine Kommas in Werten von `default`, `enum` und `example`.

## Nachtrag 2026-11-21 (E14a)

- **Option `only=<feld>:<a>|<b>`:** Eine Eigenschaft gilt nur, wenn eine Geschwister-Eigenschaft einen der Werte hat
  (z. B. `separator` von `core.group-by` nur bei `function: concat`). Im Schema steht sie als
  `"x-runnel-only": {"function": ["concat"]}`; der Editor blendet die Eigenschaft sonst aus und schreibt sie nicht.
  Ein Name, der keine Geschwister-Eigenschaft ist, ist ein Fehler beim Erzeugen des Schemas.
- **`runnel-text`** kennzeichnet auch Bedingungen in Listen (`when` von `core.switch`): lange Texte, die der Editor als
  Karte statt als Tabellenzelle zeigt (Mockup E14).

## Nachtrag 2026-11-22 (E14b)

- **Formate für den Editor:** `runnel-field` kennzeichnet den Namen eines Felds der eingehenden Datensätze (der Editor
  bietet die bekannten Felder an), `runnel-target` die Id eines Transforms, zu dem ein Daten-Link führt (`to` und
  `default` von `core.switch`). An einer Liste von Texten (`[]string`, z. B. `groupBy`) gilt das Format für jeden Text
  (`items.format`).
- **`example` im Descriptor der Transforms:** die `with:`-Werte eines kleinen Beispiels in YAML (`Definition.Example`),
  angezeigt im Abschnitt „Help“ des Panels. Pflicht für Transforms mit einer Liste in der Konfiguration; der Vertragstest
  liest es wie eine Pipeline-Datei und prüft, dass `New` es annimmt.
