# ADR 0007: Typsystem in Pipelines – schemalos mit optionalen Typen

- **Status:** Angenommen
- **Datum:** 2026-11-18
- **Bezug:** [Konzept, Kap. 2.3 – Pipeline](../KONZEPT.md#23-pipeline-datenfluss), [Kap. 6.2 – Pipeline-Engine](../KONZEPT.md#62-pipeline-engine)

## Kontext

Datensätze (Records) fließen in Pipelines durch viele Transforms. Zu entscheiden war, wie streng deren Struktur ist:

- **Strikt (wie Apache Hop):** Jeder Hop hat ein festes Schema (Felder, Typen, Reihenfolge). Fehler fallen früh auf,
  aber jede Pipeline muss vollständig typisiert sein, und flexible Daten (z. B. JSON aus APIs) sind umständlich.
- **Schemalos (wie n8n):** Datensätze sind beliebige JSON-Objekte. Einfach und flexibel, aber Tippfehler in
  Feldnamen und Typfehler zeigen sich erst zur Laufzeit.

## Entscheidung

Pipelines sind **schemalos mit optionalen Typen**. Ein Schema ist nie Pflicht; wo es angegeben oder ableitbar ist,
wird es genutzt – für Konvertierung, Validierung und Autovervollständigung im Editor.

### Record

- Ein Record ist eine **geordnete** Liste von Feldern (Name → Wert). Die Reihenfolge bleibt erhalten (wichtig für CSV/Excel-Ausgaben).
- Feldnamen mit Präfix `_` sind für Metadaten reserviert (`_error`, `_source`, `_rowNumber`).

### Werttypen

| Typ | Go-Repräsentation | Hinweis |
|---|---|---|
| `string` | `string` | |
| `int` | `int64` | |
| `decimal` | `shopspring/decimal` | exakt, für Geldbeträge |
| `float` | `float64` | |
| `bool` | `bool` | |
| `date` / `datetime` | `time.Time` | `date` ohne Uhrzeit |
| `binary` | `[]byte` | |
| `json` | `map[string]any` / `[]any` | verschachtelte Strukturen |
| `null` | `nil` | jedes Feld darf `null` sein |

### Woher Typen kommen

| Quelle | Verhalten |
|---|---|
| CSV, Fixed-Width | Ohne Felddefinition sind alle Werte `string`. Mit Felddefinition (`fields:` inkl. `type`, `format`) wird beim Lesen konvertiert |
| Excel | Zelltypen werden übernommen (Zahl, Datum, Text); optional per `fields:` überschreibbar |
| Datenbank-Eingabe | Typen werden automatisch aus den Spaltenmetadaten abgeleitet |
| JSON, REST | Typen aus den JSON-Werten; verschachtelte Objekte als `json` |
| Transforms | Jeder Transform meldet in `Init` sein Ausgabeschema, soweit bekannt (z. B. „Felder auswählen“ exakt, „JavaScript“ meist offen) |

**„Felder ermitteln“ im Editor** (wie „Get Fields“ in Hop): liest eine Stichprobe der Eingabe, schlägt Feldnamen und Typen
vor und schreibt sie als `fields:` in die YAML-Datei. Typen werden **zur Laufzeit nicht geraten** – das Verhalten
hängt nur von der Definition ab, nicht vom Inhalt der jeweiligen Datei.

### Schema an Links

Das Schema an einem Link ist entweder

- **geschlossen** – alle Felder bekannt (z. B. nach „Felder auswählen“ oder Datenbank-Eingabe), oder
- **offen** – bekannte Felder plus möglicherweise weitere (z. B. nach JSON-Eingabe oder JavaScript).

Die Engine leitet die Schemas beim Laden durch den Graphen ab (`Init(schemaIn) → schemaOut`).
Der Editor ergänzt sie um Felder aus der Datenvorschau bzw. aus Samples des letzten Laufs.

### Konvertierung und Fehler

- Ausgaben mit festem Ziel (Datenbanktabelle, Excel mit Formaten) konvertieren in die Zieltypen.
  Nicht konvertierbare Werte gehen auf den **Fehler-Link** (bzw. brechen ab, wenn keiner existiert).
- Transform „Typ konvertieren“ für explizite Umwandlung mit Format (z. B. `"1.234,56"` → `decimal`).
- In Ausdrücken gibt es **keine implizite Typumwandlung** zwischen Text und Zahl (`"5" > 3` ist ein Fehler);
  umgewandelt wird explizit mit `int()`, `decimal()`, `parseDate()` (siehe [ADR 0006](0006-ausdruckssprache.md)).

### Validierung

| Prüfung | Stufe |
|---|---|
| Feld wird verwendet, existiert im **geschlossenen** Schema des Links aber nicht | Fehler |
| Feld wird verwendet, ist im **offenen** Schema nicht bekannt | Warnung („Tippfehler?“, mit Vorschlag ähnlicher Feldnamen) |
| Bekannter Typ passt nicht zur Verwendung (z. B. `string` in Rechenoperation) | Warnung |

### Strikter Modus (optional)

Mit `settings.strict: true` an einer Pipeline müssen alle Links ein **geschlossenes** Schema haben; Warnungen zu
unbekannten Feldern werden zu Fehlern. Gedacht für produktive, stabile Pipelines.

### Nachtrag 2026-11-20 (S14, umgesetzt in `internal/record` und `internal/transform`)

- **`date` ist ein eigener Typ** (`record.Date`: Jahr, Monat, Tag ohne Uhrzeit und Zeitzone), `datetime` bleibt
  `time.Time`. Ein Datum bleibt beim Weiterreichen ein Datum und verschiebt sich nicht um Mitternacht.
- **`decimal`** mit `github.com/shopspring/decimal` (MIT, v1.6.0).
- **Schema-Ableitung:** Transforms mit `Init(ic) → Schema` melden ihr Ausgabeschema; die Engine ruft `Init` vor dem Lauf
  in der Reihenfolge der Links auf und mischt die Schemas mehrerer Eingänge (unterschiedliche Typen → unbekannt, ein
  offenes → offen). Transforms ohne `Init` liefern ein offenes Schema, über das nichts bekannt ist.
- **Verwendete Felder** meldet ein Transform in `Init` mit `ic.Use(name, pfad)`. Geschlossen + fehlt → Fehler, offen +
  unbekannt → Warnung, beide mit Vorschlag. **Ist im offenen Schema gar kein Feld bekannt, gibt es keine Warnung** –
  sonst wäre nach jedem Transform ohne Schema jedes verwendete Feld gemeldet, ohne Aussage.
- **Zahlen in der Konfiguration** (z. B. Konstanten) bleiben ganze Zahlen (`int64`) bzw. werden `float64`; sie werden nicht
  über `float64` gelesen.
- **Strikter Modus** (S17c): `settings.strict: true`. Hat ein Link ein offenes Schema, ist das ein Fehler – beim
  Transform, dessen Records es sind (z. B. `csv.input` ohne `fields`), bzw. beim Fehler-Link. Ein verwendetes Feld,
  das nicht bekannt ist, ist immer ein Fehler, auch hinter einem offenen Schema.

## Konsequenzen

**Positiv**
- Einfacher Einstieg: Pipelines funktionieren ohne jede Typangabe.
- Flexible Verarbeitung von JSON/API-Daten.
- Trotzdem frühe Fehlererkennung und gute Autovervollständigung, wo Schemas bekannt sind.
- Deterministisches Verhalten, da Typen nicht zur Laufzeit geraten werden.

**Negativ / zu beachten**
- Bei offenen Schemas fallen Tippfehler in Feldnamen ggf. erst zur Laufzeit auf (abgemildert durch Warnungen und Vorschau).
- Ohne Felddefinition sind CSV-Werte Text; wer rechnen will, muss Typen angeben oder konvertieren.
- Jeder Transform muss seine Schema-Ableitung in `Init` sorgfältig implementieren (Teil der Tests).

## Nachtrag 2026-11-26 (R5c)

- **Typen von Parametern und Outputs** (`parameters[].type`, `outputs[].type` in Workflows und Pipelines) nutzen
  dieselbe Liste wie `fields[].type`: `string` (Standard), `int`, `decimal`, `float`, `bool`, `date`, `datetime`,
  `json`; `binary` nicht, weil sich ein Wert aus Text ohne Kodierung nicht eindeutig lesen lässt. Die früheren Namen
  `integer`, `number`, `boolean` sind unbekannte Werte (Fehler wie bei jedem anderen ungültigen Typ, vor 1.0 ohne
  Abkündigung).
- **Lesen aus Text:** Werte von `--param`, aus `params` von Triggern und Aufrufen (`core.run-*`), aus `POST /runs`, aus
  dem Body eines HTTP-Triggers und aus `default` sind Text und werden wie ein Feld ohne `format` gelesen
  (`internal/format`): Zahlen als `1234.5` ohne Tausendertrennzeichen, `bool` als `true`/`false`, `1`/`0`, `yes`/`no`,
  `date` als `2026-11-20`, `datetime` nach ISO 8601 (ohne Zone in lokaler Zeit), `json` als beliebiger JSON-Wert.
  Leerzeichen am Rand zählen außer bei `string` nicht; ein leerer Text ist kein Wert eines anderen Typs. `enum` vergleicht
  wie Records (`1` = `1.0`).
- **In Ausdrücken** (`params.NAME`) haben die Werte die Darstellung der Records; ein `date` ist wie bei Feldern eine Zeit
  um Mitternacht in lokaler Zeit (für `today()`, `formatDate()`, `<`). Als Variable `${NAME}` bleibt der Wert Text.
- **Fehler-Workflow (ADR 0027):** Die `FAILED_…`-Parameter bleiben `string`, auch `FAILED_STARTED`/`FAILED_FINISHED`
  – sie sind leer, wenn der Lauf nicht starten konnte.
- **JSON Schema:** Ein MCP-Server (ADR 0012) bildet die Typen an seiner eigenen Grenze ab (`int` → `integer`,
  `decimal`/`float` → `number`, `bool` → `boolean`, `date`/`datetime` → `string` mit `format: date`/`date-time`, `json`
  ohne Einschränkung); die Dateiformate behalten das Vokabular dieser ADR.
