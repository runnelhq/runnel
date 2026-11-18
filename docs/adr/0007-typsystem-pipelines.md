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

### Schema an Hops

Das Schema an einem Hop ist entweder

- **geschlossen** – alle Felder bekannt (z. B. nach „Felder auswählen“ oder Datenbank-Eingabe), oder
- **offen** – bekannte Felder plus möglicherweise weitere (z. B. nach JSON-Eingabe oder JavaScript).

Die Engine leitet die Schemas beim Laden durch den Graphen ab (`Init(schemaIn) → schemaOut`).
Der Editor ergänzt sie um Felder aus der Datenvorschau bzw. aus Samples des letzten Laufs.

### Konvertierung und Fehler

- Ausgaben mit festem Ziel (Datenbanktabelle, Excel mit Formaten) konvertieren in die Zieltypen.
  Nicht konvertierbare Werte gehen auf den **Error-Hop** (bzw. brechen ab, wenn keiner existiert).
- Transform „Typ konvertieren“ für explizite Umwandlung mit Format (z. B. `"1.234,56"` → `decimal`).
- In Ausdrücken gibt es **keine implizite Typumwandlung** zwischen Text und Zahl (`"5" > 3` ist ein Fehler);
  umgewandelt wird explizit mit `int()`, `decimal()`, `parseDate()` (siehe [ADR 0006](0006-ausdruckssprache.md)).

### Validierung

| Prüfung | Stufe |
|---|---|
| Feld wird verwendet, existiert im **geschlossenen** Schema des Hops aber nicht | Fehler |
| Feld wird verwendet, ist im **offenen** Schema nicht bekannt | Warnung („Tippfehler?“, mit Vorschlag ähnlicher Feldnamen) |
| Bekannter Typ passt nicht zur Verwendung (z. B. `string` in Rechenoperation) | Warnung |

### Strikter Modus (optional)

Mit `settings.strict: true` an einer Pipeline müssen alle Hops ein **geschlossenes** Schema haben; Warnungen zu
unbekannten Feldern werden zu Fehlern. Gedacht für produktive, stabile Pipelines.

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
