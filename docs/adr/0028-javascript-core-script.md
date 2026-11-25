# ADR 0028: JavaScript mit `core.script`

- **Status:** Angenommen
- **Datum:** 2026-11-25
- **Bezug:** KONZEPT Kap. 7.1 (Skript-Actions/-Transforms), [ADR 0004 – Lizenz MIT](0004-lizenz-mit.md),
  [ADR 0006 – Ausdruckssprache](0006-ausdruckssprache.md), [ADR 0007 – Typsystem](0007-typsystem-pipelines.md),
  [ADR 0020 – Relative Pfade](0020-relative-pfade.md), [ADR 0022 – Connections und Secrets](0022-connections-secrets.md),
  [ADR 0023 – Programme und PowerShell](0023-programme-powershell.md); Roadmap S73a–S73c

## Kontext

Vieles lässt sich mit Ausdrücken (`{{ … }}`, ADR 0006) und den eingebauten Bausteinen lösen. Es bleiben Aufgaben, für
die es keinen eigenen Baustein gibt: verschachtelte JSON-Antworten umbauen, Prüfziffern berechnen, Logik mit Schleifen
und Zwischenwerten. In n8n deckt der „Code“-Knoten das ab und gehört zu den meistgenutzten. `shell.run` und
`shell.powershell` (ADR 0023) starten dafür einen eigenen Prozess. Je Datensatz einer Pipeline ist das zu langsam, und
es braucht installierte Programme.

Das Konzept (Kap. 7.1) sah JavaScript mit goja vor: als Action und als Transform, in einer Sandbox mit CPU-Limit und
`require` aus `scripts/`. Offen waren Werte und Typen, Ablauf je Datensatz, Grenzen und Sicherheit.

Betrachtete Varianten:

| Variante | Bewertung |
|---|---|
| **goja** (`github.com/dop251/goja`, MIT) | Reines Go, kein CGO, ES5.1 und große Teile von ES2015+; unterbrechbar (`Interrupt`), mehrere Laufzeiten im Prozess. Gewählt. |
| `otto` | Reines Go, aber nur ES5 und langsamer; kaum noch gepflegt. |
| V8 oder QuickJS über CGO | Schneller und vollständiger, aber CGO: schwieriger zu bauen und auszuliefern (Runnel ist eine einzelne exe ohne CGO). |
| Lua, Starlark | Einfach einzubetten, aber für Anwender aus Windows-Firmen weniger vertraut als JavaScript. |
| Nur `shell.*` und Plugins | Gibt es schon; je Datensatz zu langsam und mit Abhängigkeiten auf dem Rechner. |

## Entscheidung

### 1. Engine

- **goja** in einer fest gepinnten Version, Lizenz MIT (ADR 0004). Die exe wächst um einige MB.
- JavaScript gibt es nur in `core.script`. Ausdrücke `{{ … }}` bleiben bei `expr-lang` (ADR 0006).

### 2. Action `core.script` (Workflow)

```yaml
- id: total
  type: core.script
  with:
    params: { orders: "{{ results.load.output.rows }}", rate: 0.19 }
    script: |
      const sum = params.orders.reduce((s, o) => s + Number(o.amount), 0);
      console.log(`${params.orders.length} orders`);
      return { net: sum, gross: sum * (1 + params.rate) };
```

- **Code** aus `script` (Format `runnel-script` wie bei `shell.powershell`: `${…}` und `{{ … }}` werden darin
  **nicht** ersetzt) oder aus `file`, einer `.js`-Datei im Projekt (ADR 0020). Genau eines von beiden.
- **Werte** kommen nur über `params` hinein, mit aufgelösten Variablen und Ausdrücken. Im Skript stehen sie als `params`
  bereit.
- Der Code ist der Rumpf einer Funktion: **`return`** liefert `result.output` (Text, Zahl, Boolean, Liste, Objekt).
  Ohne `return` ist `output` leer.
- **`console.log`, `console.warn`, `console.error`** schreiben ins Log des Laufs (info, warn, error).
- Eine **Exception** lässt die Action fehlschlagen, mit ihrer Meldung (bei `Error` die `message`, mit Zeile im Skript).
  Für die Action gelten `retry`, `timeout` und `continueOnError` wie für jede andere.

### 3. Transform `core.script` (Pipeline)

```yaml
- id: split
  type: core.script
  with:
    fields:
      - { name: name, type: string }
      - { name: amount, type: decimal }
    script: |
      if (!record.name) return null;            // verwerfen
      return { name: record.name.trim(), amount: record.amount };
```

- Das Skript läuft **je Datensatz** mit `record` als Objekt. Felder des Datensatzes sind seine Eigenschaften.
  `params` steht wie bei der Action bereit und wird einmal je Lauf aufgelöst.
- **Rückgabe:**
  - ein Objekt: der neue Datensatz (Felder in der Reihenfolge des Objekts);
  - `null` oder `undefined`: der Datensatz wird verworfen;
  - ein Array von Objekten: mehrere Datensätze.
- Eine **Exception** schickt den Datensatz unverändert auf den Fehler-Link (`_error` mit der Meldung). Ohne Fehler-Link
  endet die Pipeline mit dem Fehler.
- **`fields`** (optional) deklariert die Felder des Ausgangs mit Typ. Das Schema ist dann geschlossen, und der Editor
  bietet die Felder in den folgenden Schritten an. Ohne `fields` ist das Schema offen.
- **Kopien:** `copies` ist erlaubt (`parallel`), je Kopie eine eigene JS-Laufzeit. Globale Variablen des Skripts gelten
  je Kopie, nicht über Kopien hinweg.
- Optional `init` und `end`: Code, der einmal vor dem ersten und nach dem letzten Datensatz läuft, z. B. für Zähler.
  `end` darf mit `return` weitere Datensätze liefern (z. B. eine Summe).

### 4. Typen (ADR 0007)

| Runnel | Im Skript | Zurück ohne `fields` |
|---|---|---|
| `string` | Text | Text → `string` |
| `int` | Zahl; über ±2^53 als `BigInt` | ganze Zahl → `int`, `BigInt` → `int` |
| `float` | Zahl | andere Zahl → `float` |
| `decimal` | **Text** (exakt, z. B. `"12.50"`) | Text bleibt `string`; mit `fields` → `decimal` |
| `bool` | Boolean | Boolean → `bool` |
| `date` | Text `"YYYY-MM-DD"` | Text bleibt `string`; mit `fields` → `date` |
| `datetime` | `Date` | `Date` → `datetime` |
| `binary` | `Uint8Array` | `Uint8Array`/`ArrayBuffer` → `binary` |
| `json` | Objekt oder Array | Objekt oder Array → `json` |
| leer | `null` | `null`/`undefined` → leer |

- Mit `fields` wird jeder Wert in den deklarierten Typ umgewandelt (wie `core.convert`). Ein Wert, der nicht passt, ist
  ein Fehler des Datensatzes (Fehler-Link).
- `decimal` als Text verhindert stille Rundungsfehler. Wer rechnen will, wandelt selbst (`Number(record.amount)`) und
  entscheidet damit bewusst über die Genauigkeit.

### 5. Sandbox und Grenzen

- **Kein Zugriff** auf Dateisystem, Netzwerk, Prozesse, Umgebungsvariablen oder die Uhr des Rechners über eigene APIs
  hinaus (`Date` ist verfügbar). Für Dateien, HTTP, Mail und Datenbanken gibt es die Actions und Transforms. APIs wie
  `http` oder `fs` im Skript kommen in dieser Fassung nicht.
- **`require('./scripts/utils.js')`** lädt nur `.js`-Dateien aus dem Ordner `scripts/` des Projekts (relativ zum
  Projekt, kein `..`, keine npm-Module). Jedes Modul wird je Laufzeit einmal geladen.
- **Zeit:**
  - Action: `timeout` der Action (Standard wie bei anderen Actions); danach bricht `Interrupt` das Skript ab.
  - Transform: **`maxRecordTime`** je Datensatz, Standard **1 s**, einstellbar (z. B. `5s`). Eine Überschreitung ist
    ein Fehler des Datensatzes.
  - Ein Abbruch des Laufs bricht laufende Skripte sofort ab.
- **Ausgabe:** `console.*` höchstens 1.000 Zeilen je Ausführung der Action bzw. je Lauf des Transforms, eine Zeile
  höchstens 8 KB, danach ein Hinweis. Ein Rückgabewert der Action höchstens 16 MiB (als JSON).
- **Speicher** lässt sich in goja nicht hart begrenzen (siehe Sicherheit).

### 6. Secrets

- `${secret:…}` ist nur in `params` erlaubt (Feld mit `format: runnel-secret` nicht nötig: `params` ist ein Mapping
  wie bei `shell.powershell`). Werte von Secrets werden in Log, Fehlern und `output` maskiert (ADR 0022).
- Im Code selbst (`script`, `file`) sind `${…}` und `{{ … }}` nur Text. Ein Secret steht also nie im Code.

### 7. Editor

- Das Feld `script` ist ein Code-Feld mit Hervorhebung für JavaScript (`@codemirror/lang-javascript`, MIT, neue
  Abhängigkeit des Editors) und Vervollständigung für `record`, `params`, `console` und die Felder des eingehenden
  Datensatzes. Es ist ein vorhandenes Feld (wie bei `shell.powershell`) mit besserer Darstellung, ohne eigenes Mockup.

### Sicherheit

- **Vertrauen:** Skripte sind Dateien des Projekts wie Workflows und Pipelines. Wer sie ändern kann, kann auch
  `shell.run` aufrufen. Die Sandbox schützt vor **Versehen** (Endlosschleife, falscher Pfad), nicht vor einem
  absichtlich bösartigen Projekt.
- **Keine neue Angriffsfläche von außen:** Skripte laufen nur in Läufen. Daten aus HTTP-Triggern erreichen sie nur als
  Werte (`params`, `record`), nie als Code. Es gibt kein `eval` von Eingaben durch Runnel.
- **Speicher:** Ein Skript kann viel Speicher belegen (große Arrays). Das begrenzen Zeitlimit und `Interrupt`
  indirekt. Ein harter Grenzwert ist mit goja nicht möglich. Das ist dokumentiert.
- **`require`** bleibt auf `scripts/` im Projekt beschränkt (Pfadprüfung wie bei Dateien, ADR 0020, ohne Symlinks
  aus dem Projekt heraus).
- Tests für Ablehnungen: Zeitlimit, `require` außerhalb von `scripts/`, zu viele Logzeilen, zu große Rückgabe.

## Konsequenzen

- Aufgaben ohne eigenen Baustein lassen sich ohne externe Programme lösen, auch je Datensatz.
- Neue Laufzeitabhängigkeit goja; Updates werden wie andere Abhängigkeiten geprüft.
- JavaScript ist die zweite Sprache neben den Ausdrücken. Die Doku muss klar sagen, wann was: Ausdrücke für Werte und
  Bedingungen, `core.script` für Logik über mehrere Schritte.
- Leistung des Transforms ist geringer als bei eingebauten Transforms. Ziel: über 100.000 Datensätze/s für einfache
  Skripte (Benchmark).

Umsetzung: **S73a** goja, Action `core.script`, Sandbox, `require`, Beispiel; **S73b** Transform `core.script` mit
Typen, `fields`, `init`/`end`, Fehler-Link, Benchmark; **S73c** Editor (Hervorhebung, Vervollständigung).

## Nachtrag 2026-11-25 (S73a): Action

- **Code ist Text:** In `script` von `core.script` werden `${…}` und `{{ … }}` weder ersetzt noch geprüft, auch nicht
  bei der Suche nach Variablen und beim Umbenennen (Felder mit Code: `model.CodeFields`). `file` behält die Prüfung
  von `shell.powershell`: Der Pfad enthält kein `${…}` und kein `{{ … }}`, damit Werte nicht wählen, welches Skript läuft.
- **Secrets in `params`:** `params` hat das Format `runnel-params`. Ein Wert, der `${secret:…}` enthält, wird wie ein
  Feld für ein Secret aufgelöst (nur `${…}`, keine Ausdrücke) und vor dem Lauf gelesen. Tiefer verschachtelte Werte
  dürfen kein Secret enthalten (Fehler beim Prüfen).
- **`require`:** Pfade gelten immer relativ zum Projekt, auch in Modulen (`require('./scripts/x.js')`); Module setzen
  `module.exports` oder `exports`. Skriptdatei und Modul höchstens 4 MiB.
- **`console`:** `log` und `info` (info), `warn`, `error`; Objekte und Listen als JSON. Eine Zeile ist ein Aufruf.
- **Meldungen:** `script, line 3: no orders` bzw. mit dem Pfad der Datei; bei anderen Fehlertypen als `Error` mit
  ihrem Namen (`TypeError: …`). Rekursion über 10.000 Aufrufe ist ein Fehler statt eines Absturzes.
- **Rückgabe:** ganze Zahlen (bis ±2^53, `BigInt` bis int64) als `int`, andere als `float`; `NaN` und `Infinity`
  werden leer (wie bei `JSON.stringify`); `Date` als Zeitpunkt; Methoden in Objekten entfallen, eine Funktion oder ein
  Symbol als Wert ist ein Fehler. `decimal` kommt mit seinen Nachkommastellen als Text (`"12.50"`).
