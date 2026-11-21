# ADR 0006: Ausdruckssprache – `expr-lang` für Ausdrücke, JavaScript für Skripte

- **Status:** Angenommen
- **Datum:** 2026-11-18
- **Bezug:** [Konzept, Kap. 2.5 – Variablen](../KONZEPT.md#25-environment--variablen), [Kap. 7.1 – Skript-Bausteine](../KONZEPT.md#71-stufe-1-skript-actions-transforms-javascript-eingebettet)

## Kontext

Ausdrücke kommen in Runnel an vielen Stellen vor: Link-Bedingungen, Filter, berechnete Felder, dynamische
Eigenschaften (Pfade, URLs, Mail-Betreff). In Pipelines werden sie **pro Datensatz** ausgewertet – bei Millionen Zeilen
ist die Ausführungsgeschwindigkeit entscheidend. Gleichzeitig gibt es Fälle, die echte Programmlogik brauchen.

Betrachtet wurden:

| Option | Geschwindigkeit | Sicherheit | Mächtigkeit |
|---|---|---|---|
| Nur JavaScript (goja) | mittel (Interpreter, Kontextaufbau pro Aufruf) | Sandbox nötig, Endlosschleifen möglich | hoch |
| Nur `expr-lang/expr` | hoch (einmal kompiliert, Bytecode-VM) | hoch (keine Schleifen, keine Seiteneffekte) | mittel |
| **Beides, nach Zweck getrennt** | hoch, wo es zählt | hoch, wo es zählt | hoch, wo nötig |

## Entscheidung

1. **`{{ … }}`-Ausdrücke werden mit [`expr-lang/expr`](https://github.com/expr-lang/expr) ausgewertet** – überall:
   Link-Bedingungen, Filter, Berechnen, dynamische Eigenschaften, Breakpoints.
2. **JavaScript (goja) nur in den Skript-Bausteinen** – Action `core.script` (Workflow) und Transform `core.script` (Pipeline), siehe ADR 0013.
3. **`${VAR}`** bleibt reine Textersetzung (kompatibel zu Apache Hop) und wird vor der Ausdrucksauswertung aufgelöst.

### Auswertungsregeln

- Besteht ein Wert **vollständig** aus einem Ausdruck (`"{{ record.umsatz * 1.19 }}"`), bleibt der Ergebnistyp erhalten
  (Zahl, Datum, Objekt, Liste).
- Ist der Ausdruck **Teil eines Textes** (`"Import {{ run.id }} fehlgeschlagen"`), wird das Ergebnis als Text eingefügt.
- Ausdrücke werden **einmal beim Laden kompiliert** und dann pro Datensatz bzw. Aufruf ausgeführt.
  Syntaxfehler fallen damit schon bei `runnel validate` bzw. im Editor auf, nicht erst im Lauf.

### Verfügbarer Kontext

| Name | Inhalt | Verfügbar in |
|---|---|---|
| `record` | Aktueller Datensatz | Pipeline |
| `vars` | Alle aufgelösten Variablen (`vars.INBOX`) | überall |
| `params` | Parameter des Workflows/der Pipeline | überall |
| `env` | Umgebungsvariablen des Systems | überall |
| `run` | Run-Infos: `id`, `startedAt`, `trigger`, `environment`, `lastError` | überall |
| `result` | Ergebnis der vorherigen Action (`success`, `rows`, `files`, `exitCode`, `output`) | Workflow |
| `results` | Ergebnisse aller bisherigen Actions nach ID (`results["load"].rows`) | Workflow |

**Secrets sind in Ausdrücken nicht zugreifbar** – nur über `${secret:name}` in Eigenschaftsfeldern, damit sie nicht
versehentlich in Logs, Feldwerten oder Vorschauen landen.

### Funktionsbibliothek

Zusätzlich zu den eingebauten Operatoren und Funktionen von `expr` (`upper`, `lower`, `trim`, `split`, `replace`,
`len`, `int`, `float`, `string`, `toJSON`, `fromJSON`, `??`, `in`, `matches`, `map`/`filter`/`any`/`all` für Listen …):

| Bereich | Funktionen |
|---|---|
| Datum/Zeit | `now()`, `today()`, `addDays(d, n)`, `addHours(d, n)`, `addMonths(d, n)`, `formatDate(d, "yyyy-MM-dd")`, `parseDate(s, "dd.MM.yyyy")`, `dayOfWeek(d)`, `diffDays(a, b)` |
| Text | `padLeft(s, n, "0")`, `padRight`, `substr(s, start, len)`, `left`, `right`, `normalize` (Umlaute/Leerzeichen) |
| Zahlen | `round(x, stellen)`, `decimal(s)`, `formatNumber(x, "#.##0,00")`, `parseNumber(s, "de-DE")` |
| Null/Leer | `coalesce(a, b, …)`, `isEmpty(x)` |
| Sonstiges | `uuid()`, `sha256(s)`, `md5(s)`, `basename(p)`, `dirname(p)`, `ext(p)` |

Datumsformate verwenden die bekannten Muster `yyyy`, `MM`, `dd`, `HH`, `mm`, `ss` (wie Hop/Java/.NET), nicht das
Go-Referenzlayout. Dezimalzahlen (`decimal`) rechnen exakt (`shopspring/decimal`) – die arithmetischen Operatoren
werden dafür per `expr.Operator` überladen, sodass `record.umsatz * 1.19` bei Geldbeträgen keine Rundungsfehler erzeugt.

Beispiele:

```yaml
default:   "{{ formatDate(addDays(today(), -1), 'yyyy-MM-dd') }}"
condition: "{{ record.umsatz > 0 && record.land in ['DE', 'AT', 'CH'] }}"
value:     "{{ coalesce(record.email, 'unbekannt@example.com') | lower() }}"
when:      "{{ result.rows > 0 }}"
```

### Grenzen und Sicherheit

- `expr` hat keine Schleifen (außer über Listenfunktionen), keine Variablenzuweisung und keinen Zugriff auf Dateien oder Netzwerk.
- Pro Auswertung gelten ein **Speicherbudget** und eine maximale Ausdrucksgröße; Überschreitung ist ein Fehler.

### Umsetzung (S08a)

- Paket `internal/expr`: `Compile` zerlegt einen Wert in Text und `{{ … }}`-Teile und übersetzt jeden Ausdruck einmal;
  `Eval` wertet aus. Ein Wert, der (bis auf Leerzeichen außen) aus genau einem Ausdruck besteht, behält den Ergebnistyp.
  Als Text eingefügt werden Datumswerte in ISO 8601 (`2026-11-19T14:30:00Z`), ganze Zahlen ohne Nachkommastellen,
  Listen und Objekte als JSON, `nil` als leerer Text. Ein Ausdruck endet am ersten `}}`.
- Erlaubte Namen sind die Kontextobjekte der Tabelle oben; ein Tippfehler (`reslt.rows`) ist ein Fehler beim Lesen der
  Datei, mit Datei, Zeile und Spalte. Felder darunter (`params.ORDRS`) werden erst im Lauf aufgelöst; ein unbekanntes
  Feld ergibt `nil`. Geprüft werden Link-Bedingungen, alle Texte unter `with:`, `outputs` und
  Standardwerte von Parametern.
- Grenzen: höchstens 4.096 Zeichen je Ausdruck und 2.000 Knoten im übersetzten Ausdruck; zur Laufzeit gilt das
  Speicherbudget von expr-lang.
- Funktionsbibliothek Teil 1: `today`, `addDays`, `addHours`, `addMonths`, `formatDate`, `parseDate`, `dayOfWeek`
  (1 = Montag … 7 = Sonntag), `diffDays(a, b)` (Kalendertage von `a` bis `b`), `padLeft`, `padRight`, `substr`
  (ab 0, in Zeichen), `left`, `right`, `normalize` (ä → ae … ß → ss, Leerraum zusammengefasst), `coalesce`
  (erster Wert, der nicht leer ist), `isEmpty` (`nil`, nur Leerzeichen, leere Liste/Map). `now()` ist die eingebaute
  Funktion von expr-lang. Datumsangaben dürfen auch Texte in ISO 8601 sein (`'2026-11-19'`).
- Datumsmuster: `yyyy`, `yy`, `MMMM`, `MMM`, `MM`, `M`, `dd`, `d`, `EEEE`, `EEE`, `HH`, `hh`, `h`, `mm`, `m`, `ss`, `s`,
  `SSS`, `a`, `XXX`, `Z`; Text in `'…'` wird unverändert übernommen (`''` für ein Hochkomma), Ziffern und andere
  Buchstaben außerhalb von Anführungszeichen sind ein Fehler. Ohne Zeitzone im Muster gilt die lokale Zeit.
- Zahlen (`round` mit Stellen, `decimal`, `formatNumber`, `parseNumber`) und Sonstiges (`uuid`, `sha256` …) folgen mit
  den Pipelines bzw. bei Bedarf.

### Umsetzung (S08b)

- **Reihenfolge:** In jedem Text werden zuerst `${…}`-Variablen ersetzt, dann `{{ … }}` ausgewertet. Übersetzte
  Ausdrücke werden je Lauf zwischengespeichert. Beim Prüfen einer Datei steht `${…}` für einen Wert, der als Zahl,
  Wahrheitswert oder – in Anführungszeichen – als Text passt.
- **Kontext:** `vars` (alle Variablen, aufgelöst), `params` (deklarierte Parameter mit ihrem Typ, z. B. `integer` als
  Zahl), `env`, `run` (`id`, `workflow`, `environment`, `startedAt`, `lastError` – die Meldung der zuletzt
  fehlgeschlagenen Action; `run.trigger` ist noch offen – Backlog), `results` (alle bisher beendeten Actions nach ID),
  `request` (Anfrage eines HTTP-Triggers, seit S38b1, [ADR 0018](0018-http-trigger.md)) und
  `result`. Ein Ergebnis hat `id`, `type`, `status` (`succeeded`, `failed`, `cancelled`, `skipped`), `success`,
  `output` (Ausgabedaten der Action), `error` und `durationMs`; `rows`, `files` und `exitCode` liefern die Actions, die
  sie haben, in `output`.
- **Wo `result` was ist:** In einer Link-Bedingung das Ergebnis der Action, von der der Link ausgeht. In `with:` das
  Ergebnis der Action, deren Link die Action zuletzt gestartet hat (bei `start` keins).
- **Link-Bedingungen** gelten unabhängig davon, ob die Action erfolgreich war; das Ergebnis muss `true` oder `false`
  sein. Lässt sich eine Bedingung nicht auswerten, wird der Link nicht genommen und der Lauf schlägt fehl.
- **`with:`:** Ausdrücke werden vor jedem Versuch ausgewertet (bei `retry` also neu). Ein ganzer Ausdruck behält seinen
  Typ; ist das Feld laut Schema ein Text, erhält die Action das Ergebnis als Text. Ein Fehler lässt die Action
  fehlschlagen (Fehler-Links greifen).
- **Standardwerte von Parametern** mit Ausdruck werden einmal beim Start ausgewertet, in der Reihenfolge der Deklaration,
  und nur, wenn der Parameter nicht übergeben wurde; Kontext ohne `result`/`results`. Ein Fehler beendet den Lauf.
- **`outputs`** werden nach einem erfolgreichen Lauf mit allen `results` ausgewertet und dann in den Typ des Outputs
  umgewandelt.

### JavaScript in Skript-Bausteinen

- Läuft in **goja** mit Sandbox (CPU-Zeitlimit, kein Datei-/Netzwerkzugriff außer über freigeschaltete APIs).
- Dieselbe Funktionsbibliothek (`formatDate`, `addDays` …) ist dort unter denselben Namen verfügbar.
- Pipeline-API: `record` lesen/ändern, `emit(record)`, `emitTo('link', record)`, `error(record, 'msg')`, `log.info(...)`.

### Editor

- Monaco mit eigener Sprachdefinition für `{{ … }}`: Syntaxhervorhebung, Autovervollständigung für Kontextobjekte,
  Feldnamen (aus [ADR 0007](0007-typsystem-pipelines.md)) und Bibliotheksfunktionen inkl. Signatur und Beschreibung.
- Fehler werden beim Tippen angezeigt (Kompilierung über `/api/v1/validate`).
- „Ausdruck testen“: Auswertung gegen einen Datensatz aus der Vorschau.
- **Nachtrag 2026-11-20 (S35):** Umgesetzt mit **CodeMirror 6** statt Monaco (Entscheidung zum Mockup S35, PR #43):
  für viele kleine Felder im Eigenschaften-Panel gebaut und ein Bruchteil der Größe. Funktionen mit Signatur und
  Beschreibung liefert der Server (`GET /api/v1/expressions`). Für Skript-Editoren (JavaScript, PowerShell) wird die
  Bibliothek mit dem jeweiligen Schritt erneut geprüft.

### Umsetzung (S16a): Ausdrücke je Datensatz

- **Kontext `record`** für Ausdrücke, die je Datensatz ausgewertet werden (zuerst die Bedingung von `core.filter`),
  zusammen mit `vars`, `params`, `env` und `run` (`id`, `pipeline`, `environment`, `startedAt`). Diese Werte stellt die
  Pipeline-Engine einmal je Lauf bereit (`transform.Output.Values`). Felder vom Typ `date` sind in Ausdrücken Zeitpunkte
  um Mitternacht (lokale Zeit), damit `today()`, `formatDate()` und Vergleiche funktionieren.
- **Exakte Dezimalzahlen:** Statt `expr.Operator`, das nur bei zur Übersetzungszeit bekannten Typen greift, werden
  Operatoren (`+ - * / %`, Vergleiche, `-x`) mit einem Patch des Syntaxbaums ersetzt, wenn ein Operand erst zur
  Laufzeit einen Typ hat (Felder, Parameter). Ist ein Operand `decimal`, wird exakt gerechnet bzw. verglichen; Ganz- und
  Gleitkommazahlen werden dazu umgewandelt, Text nicht (`decimal > string` ist ein Fehler). Sonst gilt das Verhalten
  von expr-lang. Operatoren mit bekannten Typen (`1 + 2`) bleiben unverändert.
- **Leere Werte (`null`)** wie in SQL: Rechnen mit `null` ergibt `null`, ein Größenvergleich (`< > <= >=`) mit `null` ist
  `false`. Ein Filter `record.betrag > 0` lässt Datensätze ohne Betrag also weg, statt die Pipeline abzubrechen.
  `== nil` und `!= nil` prüfen auf leere Werte. Das gilt auch in Workflows (vorher ein Fehler).
- **Prüfung:** Die Bedingung wird beim Laden übersetzt; die verwendeten Felder (`record.betrag`) prüft die Engine gegen
  das Schema (ADR 0007): fehlt ein Feld im geschlossenen Schema, ist das ein Fehler mit Vorschlag.
- `decimal()`, `round()`, `formatNumber()` und `parseNumber()` folgen mit `core.calculate` bzw. bei Bedarf.

## Konsequenzen

**Positiv**
- Hohe Geschwindigkeit in Pipelines; Ausdrücke sind sicher und frei von Seiteneffekten.
- Fehler in Ausdrücken werden vor der Ausführung erkannt.
- JavaScript steht weiterhin für komplexe Logik zur Verfügung.

**Negativ / zu beachten**
- Zwei Syntaxen: wer Skripte schreibt, muss die Unterschiede kennen (in der Doku gegenübergestellt).
- Die Funktionsbibliothek muss für `expr` und goja doppelt registriert und gemeinsam getestet werden.
- Ein späterer Import von Hop-Workflows muss Hop-Ausdrücke (z. B. aus „Formula“/JavaScript-Transforms) übersetzen
  oder in den Transform `core.script` übernehmen.
