# ADR 0024: Excel lesen mit eigenem Reader, schreiben mit `excelize`

- **Status:** Angenommen
- **Datum:** 2026-11-24
- **Bezug:** KONZEPT Kap. 4.2 (Bibliotheken) und 12.2 (Transforms); [ADR 0004 – MIT-Lizenz](0004-lizenz-mit.md),
  [ADR 0007 – Typsystem in Pipelines](0007-typsystem-pipelines.md), [ADR 0020 – Relative Pfade](0020-relative-pfade.md);
  Roadmap S67a (`excel.input`), S67b (`excel.output`)

## Kontext

Excel ist für Fachabteilungen unter Windows das häufigste Austauschformat. Die Transforms dafür sind `excel.input`
(S67a, umgesetzt) und `excel.output` (S67b). ADR 0007 legt fest, dass Excel-Zellen ihren Typ behalten (Zahl, Datum,
Text). Das Konzept nannte `xuri/excelize/v2` als Bibliothek für beides.

Bei der Umsetzung von S67a hat sich gezeigt, dass `excelize` beim **gestreamten Lesen** (`Rows().Columns()`) nur Text
je Zelle liefert, ohne Zelltyp und ohne Zahlenformat. Ob eine Zahl ein Datum ist, steht nur im Zellformat. Zelltypen
gibt es bei `excelize` nur, wenn das ganze Blatt im Speicher liegt. Für das **Schreiben** hat `excelize` die Stärken,
die ein eigener Writer erst nachbauen müsste:
- gestreamtes Schreiben,
- Formatvorlagen (fett, Zahlen- und Datumsformate, fixierte Zeilen, Spaltenbreiten),
- das Ersetzen eines einzelnen Blatts in einer vorhandenen Mappe, ohne Diagramme, Pivot-Tabellen oder Makros der
  anderen Blätter zu verlieren.

Eine neue Laufzeitabhängigkeit braucht nach AGENTS.md eine ADR.

Betrachtete Varianten:

| Variante | Bewertung |
|---|---|
| **Eigener Reader, `excelize` zum Schreiben** | Lesen gestreamt mit Zelltypen und Datumsformaten (S67a, `internal/xlsx`). Schreiben mit einer verbreiteten Bibliothek, inklusive „nur ein Blatt ersetzen“. Gewählt. |
| `excelize` für beides | Lesen nur als Text (Datumswerte als Seriennummer, keine sichere Typerkennung) oder mit dem ganzen Blatt im Speicher. Widerspricht ADR 0007 bzw. den Grenzen für Eingaben. |
| Eigener Reader und eigener Writer | Keine Abhängigkeit, aber das Ersetzen eines Blatts in einer vorhandenen Mappe (Formatvorlagen zusammenführen, `calcChain.xml`, Relationships, Content-Types) ist fehleranfällig. Fehler fallen erst in echtem Excel auf („Wir haben ein Problem mit Inhalten gefunden“). Ohne diese Funktion etwa 700 Zeilen, mit ihr über 1.000. |
| Excel per COM fernsteuern | Braucht ein installiertes Excel, ist langsam, nicht für Dienste geeignet. Abgelehnt. |

## Entscheidung

### 1. Lesen: eigener Reader `internal/xlsx`

- Liest `.xlsx` und `.xlsm` mit `archive/zip` und `encoding/xml` aus der Standardbibliothek.
- Die Zeilen eines Blatts kommen als Strom. Jede Zelle hat ihren Typ: Text, Zahl (exakt als `decimal`), `true`/`false`,
  Datum oder Datum mit Zeit nach dem Zellformat, Fehlerwert wie `#N/A`.
- Jeder Teil der Mappe hat eine Grenze für seine entpackte Größe (Schutz vor ZIP-Bomben).
- `.xls`, `.xlsb` und Mappen mit Passwort werden mit Hinweis abgelehnt.

### 2. Schreiben: `excelize` v2.11.1

- `excel.output` schreibt mit dem `StreamWriter` von `excelize` (Blattdaten ab 16 MB über temporäre Dateien).
- Die Version wird fest gepinnt. Mit `excelize` kommen `xuri/efp`, `xuri/nfp` (BSD-3), `richardlehane/mscfb`,
  `richardlehane/msoleps` (Apache 2.0, ohne NOTICE-Datei) und `tiendc/go-deepcopy` (MIT). Alle sind nach ADR 0004 mit MIT
  vereinbar und landen über `scripts/third-party-notices.sh` in `THIRD_PARTY_NOTICES.txt`.
- `golang.org/x/crypto` und `golang.org/x/net` (schon indirekt im Projekt) steigen auf neuere Versionen.
- Die `runnel.exe` wird etwa 3,6 MB größer (gemessen mit `excel.output` in S67b: 26,0 → 29,6 MB für Windows; ein
  erster Probe-Build mit nur wenigen Funktionen von `excelize` hatte 1,5 MB ergeben). Das Ziel des Konzepts (< 40 MB)
  bleibt eingehalten.
- `excelize` wird **nur zum Schreiben** verwendet. Das Lesen bleibt beim eigenen Reader. Tests lesen die geschriebenen
  Dateien mit dem eigenen Reader wieder ein, so prüfen sich beide gegenseitig.

### 3. Verhalten beim Schreiben

- Werte behalten ihren Typ.
- Text wird **nie** als Formel geschrieben: `=1+1` aus einem Datensatz bleibt Text. Das schützt vor
  Formel-Injection, wenn Werte von außen kommen.
- Datum mit Zeit wird als Wanduhrzeit ohne Zeitzone geschrieben. `excelize` rechnet intern in UTC, Runnel übergibt
  deshalb die Wanduhrzeit.
- `ifExists: replace` schreibt über eine temporäre Datei neben dem Ziel, die alte Datei bleibt bei einem Fehler
  unverändert.
- `ifExists: sheet` öffnet die vorhandene Mappe, ersetzt nur das Blatt und setzt „beim Öffnen neu berechnen“. Teile, die
  `excelize` nicht kennt, übernimmt es unverändert.
- Grenzen von Excel (1.048.576 Zeilen, 16.384 Spalten, 32.767 Zeichen je Zelle, kein Datum vor 1900) werden gemeldet.

## Konsequenzen

- Für das Lesen gibt es keine fremde Abhängigkeit und keine Einschränkung durch sie. Wartung und Fuzzing des Readers
  liegen bei uns.
- Für das Schreiben hängen wir an `excelize`. Neue Versionen prüfen wir wie andere Abhängigkeiten (govulncheck in der
  CI, Lizenzen, Größe).
- **Speicher beim Schreiben:**
  - Die fertige, gepackte Mappe entsteht am Ende im Speicher (`WriteTo`); die Blattdaten liegen vorher in temporären
    Dateien.
  - Bei `ifExists: sheet` liegt die vorhandene Mappe beim Öffnen im Speicher, mit Grenzen für ihre entpackte Größe.
  - Bei sehr großen Mappen (Millionen Zeilen) ist das spürbar. Dokumentiert im Konzept.
- Ein späterer Wechsel des Writers (z. B. eigener Writer, falls `excelize` nicht mehr gepflegt wird) betrifft nur
  `excel.output`.

## Sicherheit

- **Eingaben:** Excel-Dateien aus dem Projekt oder von außen (z. B. über `http.request` mit `saveTo`) sind nicht
  vertrauenswürdig.
  - Der eigene Reader begrenzt jeden entpackten Teil und hat Fuzz-Ziele (`make fuzz`).
  - `encoding/xml` löst keine eigenen Entities auf, Billion Laughs ist damit nicht möglich.
  - Beim Öffnen für `ifExists: sheet` setzt Runnel `UnzipSizeLimit` (1 GB) und `UnzipXMLSizeLimit` (256 MB) von
    `excelize`.
- **Ausgaben:** Text wird nie als Formel geschrieben (keine Formel-Injection, z. B. `=HYPERLINK(…)` aus einem
  HTTP-Trigger).
- **Abhängigkeit:** govulncheck prüft `excelize` und ihre Abhängigkeiten in jeder CI und wöchentlich.
