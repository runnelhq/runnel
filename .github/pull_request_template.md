<!--
Titel deutsch im Format „<Art oder Schritt>: <Inhalt>“, z. B. „S38c: …“, „Mockup E7c: …“, „Fix: …“, „Q-Doku: …“.
Regeln: AGENTS.md. Nicht zutreffende Abschnitte mit „Keine“ bzw. „Entfällt“ beantworten, nicht löschen.
-->

## Art
<!-- genau eine: Schritt (Nr.), Mockup, ADR, Roadmap/Backlog, Fix, Qualität -->

## Inhalt
<!-- Was ändert sich und warum; Entscheidungen fürs Review; Roadmap (Schritt ✅), Doku, CHANGELOG-Eintrag -->

## Golden-Dateien
<!-- Neue und geänderte Golden-Dateien mit Begründung (welche Datei, warum); sonst „Keine geändert“ -->

## Tests
<!-- Neue Tests, auch für Ablehnungen; Ergebnisse von make test, make lint (Linux und Windows), ggf. Playwright -->

## Leistung
<!-- Benchmark vorher/nachher bei Pfaden mit Last je Datensatz oder Anfrage; sonst „Entfällt“ -->

## Sicherheit
<!-- Neue Angriffsfläche, Grenzen, Geheimnisse; sonst „Keine neue Angriffsfläche“ -->

## Stabilität (ADR 0029)
<!--
Ändert dieser PR eine Zusage aus ADR 0029? Wenn ja: Abkündigung oder nächste Major-Version.
Zusagen: Dateiformate, Ausdrücke, Ausführung, Plugin-Protokoll, CLI (Befehle, Flags, Exit-Codes, --json),
HTTP-Trigger, öffentliche HTTP-API, Release-Dateien. Neue Felder, Werte, Schlüssel und Befehle sind erlaubt.
-->
- [ ] Nein, nur additiv (oder nicht zugesagt).
- [ ] Ja, als Abkündigung (Warnung mit Ersatz, Eintrag unter „Abgekündigt“ im CHANGELOG).
- [ ] Ja, als Bruch: Nachtrag oder neue ADR, wartet auf die nächste Major-Version.

## Prüfen
<!-- Wie man das Ergebnis selbst ausprobiert (Befehle, Beispiel unter examples/) -->

## Abhängigkeiten
<!-- Neue Abhängigkeiten mit Begründung, Lizenz (MIT-kompatibel) und fester Version; sonst „Keine neuen“ -->

## Umfang
<!-- Geänderte Zeilen Produktivcode (Go, TS/TSX), Richtwert ≤ ca. 800 -->
