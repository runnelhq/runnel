# ADR 0017: Sub-Workflows – Aufruf, Parameter, Pfade und Log

- **Status:** Angenommen
- **Datum:** 2026-11-20
- **Bezug:** [ADR 0008 – Ausführungsvarianten](0008-ausfuehrungsvarianten.md), [ADR 0012 – KI-Fähigkeit](0012-ki-faehigkeit.md),
  Roadmap S11

## Kontext

Mit `core.run-workflow` ruft ein Workflow einen anderen auf (Roadmap S11). Offen waren: welche Variablen der aufgerufene
Workflow sieht, worauf sich relative Pfade beziehen, was vom Lauf des Kinds im Log des Aufrufers erscheint und wann
Aufrufe geprüft werden. Apache Hop gibt Variablen standardmäßig an Unter-Workflows weiter; der Werkzeug-Vertrag aus
ADR 0012 beschreibt einen Workflow dagegen über seine `parameters` und `outputs`.

## Entscheidung

- **Nur Parameter:** Der aufgerufene Workflow erhält ausschließlich die Werte aus `with.params` – wie ein
  Funktionsaufruf. Projekt- und Environment-Variablen gelten, weil er im selben Projekt und Environment läuft.
  Parameter, die er nicht deklariert, sind ein Fehler (beim Start von der Kommandozeile bleiben sie erlaubt).
  Werte sind Text wie auf der Kommandozeile; Zahlen, Daten und Listen aus Ausdrücken werden formatiert und vom Kind
  nach dem deklarierten Typ gelesen.
- **Ergebnis:** Die `outputs` des Kinds sind `result.output` der Action (Map, bei keinem Output leer), z. B.
  `{{ results.import.output.ROWS }}`. Scheitert das Kind, scheitert die Action mit dessen Fehler; ein Abbruch des
  Aufrufers bricht das Kind ab.
- **Pfade** sind relativ zum Projektordner (wie in ADR 0008 und in der Datei-API des Editors); ohne Projekt relativ zum
  Ordner der aufrufenden Datei.
- **Log:** Die Meldungen des Kinds erscheinen im Log der aufrufenden Action mit Präfix `<workflow>/<action>: ` und
  derselben Stufe; Start und Ende der Kind-Actions als Debug. Das Kind hat eine eigene Lauf-ID, erzeugt aber keine
  eigenen Ereignisse für Editor und Server (Lauf-Historie mit `parent_run_id` folgt mit S26).
- **Prüfung:** Aufrufe mit festem Pfad prüfen `runnel validate`, der Editor und der Start eines Laufs vorab: Datei
  vorhanden und lesbar, Parameter deklariert, Pflichtparameter gesetzt, feste Werte passend zum Typ, kein Selbstaufruf
  (auch über andere Workflows). Pfade und Werte mit `${…}` oder `{{ … }}` werden beim Ausführen der Action geprüft;
  auch dort wird ein Selbstaufruf erkannt und abgelehnt.

## Konsequenzen

- Abhängigkeiten zwischen Workflows sind in der Datei sichtbar; ein Kind lässt sich auch einzeln starten und testen.
- Wer Werte weitergeben will, muss sie als Parameter deklarieren – mehr Schreibarbeit als bei Hop, dafür keine
  versteckten Abhängigkeiten.
- Rekursion ist ausgeschlossen. Wird sie gebraucht (z. B. Ordnerbäume), folgt eine eigene Entscheidung.
- `runConfig:` und `forEachRow` aus ADR 0008 folgen mit Phase 0.4 (S24).

## Nachtrag 2026-11-20 (S18): Pipelines

`core.run-pipeline` folgt denselben Regeln (nur Parameter, Pfade, Log mit Präfix `<pipeline>/<transform>: `, Prüfung
fester Aufrufe vorab). Ergebnis sind statt `outputs` die Zähler der Transforms: `output.transforms.<id>.read`,
`.written`, `.errors` und die Summe `output.errors`. Ein Selbstaufruf ist nicht möglich, weil Pipelines nichts aufrufen.
