# ADR 0015: Sprache der Oberfläche – Englisch

- **Status:** Angenommen
- **Datum:** 2026-11-19
- **Bezug:** [ADR 0011 – Corporate Design](0011-corporate-design.md), [ADR 0014 – Action-Descriptor](0014-action-descriptor-schema.md),
  [design/README.md, Kap. 7](../../design/README.md#7-sprache-und-ton)

## Kontext

CLI-Ausgaben, Fehlermeldungen und die Beschreibungen der Actions (ADR 0014) sind englisch. Das Handbuch zum Corporate
Design und das Konzept zeigten dagegen deutsche Texte für den Editor („Pipeline ausführen“, Palette „Allgemein“).
Vor dem ersten Mockup des Editors (E4) muss feststehen, in welcher Sprache die Oberfläche erscheint.

## Entscheidung

- **Alle Texte des Produkts sind englisch:** Editor, CLI, Fehlermeldungen, Descriptors, Installer, Tray und
  Windows-Dienst. Grund: ein breiterer Markt über den deutschsprachigen Raum hinaus.
- **Keine Übersetzungsschicht** zunächst. Wird später eine weitere Sprache gebraucht, folgt eine eigene ADR.
- **Kategorien** behalten ihre Kennungen (`general`, `files` …); der Editor zeigt sie als englische Anzeigenamen
  („General“, „Files“, „Network“, „Database“, „Execution“).
- **Ton** wie im Handbuch beschrieben, auf Englisch: Bedientexte als Imperativ ohne Anrede („Run pipeline“,
  „Save changes“), Anrede bei Bedarf „you“, Überschriften und Schaltflächen in Satzschreibweise („Run pipeline“, nicht
  „Run Pipeline“). Ausnahme: Anzeigenamen der Actions (`Label`) in Title Case wie bisher („Copy Files“).
- **Dokumentation** (Konzept, ADRs, Anleitungen, README) bleibt vorerst deutsch, wie in `AGENTS.md` festgelegt. Ob
  Anwenderdokumentation zusätzlich englisch erscheint, wird mit der Website bzw. vor 1.0 entschieden.

## Konsequenzen

- Mockups und Editor-Code verwenden englische Texte; Beispieldaten in Mockups dürfen deutsch sein (z. B. Dateinamen
  wie `kunden.csv`), um echte Einsatzfälle zu zeigen.
- Handbuch (Kap. 3.3 und 7), Konzept (Kap. 11) und ADR 0014 sind angepasst.
- Deutsche Anwender bedienen ein englisches Produkt mit deutscher Dokumentation – bis zur Entscheidung über englische
  Anwenderdokumentation bewusst in Kauf genommen.
