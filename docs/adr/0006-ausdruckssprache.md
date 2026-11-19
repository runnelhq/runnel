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

### JavaScript in Skript-Bausteinen

- Läuft in **goja** mit Sandbox (CPU-Zeitlimit, kein Datei-/Netzwerkzugriff außer über freigeschaltete APIs).
- Dieselbe Funktionsbibliothek (`formatDate`, `addDays` …) ist dort unter denselben Namen verfügbar.
- Pipeline-API: `record` lesen/ändern, `emit(record)`, `emitTo('link', record)`, `error(record, 'msg')`, `log.info(...)`.

### Editor

- Monaco mit eigener Sprachdefinition für `{{ … }}`: Syntaxhervorhebung, Autovervollständigung für Kontextobjekte,
  Feldnamen (aus [ADR 0007](0007-typsystem-pipelines.md)) und Bibliotheksfunktionen inkl. Signatur und Beschreibung.
- Fehler werden beim Tippen angezeigt (Kompilierung über `/api/v1/validate`).
- „Ausdruck testen“: Auswertung gegen einen Datensatz aus der Vorschau.

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
