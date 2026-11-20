# Einen neuen Transform entwickeln

Transforms sind die Bausteine von Pipelines: Sie lesen Datensätze (Records), verändern, filtern oder schreiben sie.
Diese Anleitung ergänzt [ACTIONS.md](ACTIONS.md) – Namensräume, Konfiguration (`with:`), Schema-Tags, Texte, Windows-
Besonderheiten und Dokumentation gelten genauso. Hier steht nur, was bei Transforms anders ist.

Beispiele: [`internal/transforms/core/`](../../internal/transforms/core/) – `generate.go` (Eingabe ohne eingehende Links),
`dummy.go` (reicht weiter), `log.go` (Konfiguration, Log).

## 1. Aufbau

```
internal/transforms/<namensraum>/
  register.go          # eine Zeile je Transform
  <name>.go            # der Transform
  <name>_test.go       # seine Tests
```

- Eigene Registry, getrennt von den Actions ([ADR 0013](../adr/0013-typnamen-namensraeume.md)): derselbe Typname darf als
  Action und als Transform existieren (z. B. `core.log`). Ein neuer Namensraum wird in `internal/transforms/transforms.go`
  eingetragen.
- **Kategorie** (`transform.Category…`): `input`, `transform`, `flow`, `execution`, `combine`, `output`
  (KONZEPT Kap. 12.2).
- **Konfiguration** wie bei Actions mit `transform.DecodeConfig` und Tags (`doc`, `schema`). Felder, die `${…}` enthalten
  dürfen, als `string` anlegen – auch Zahlen (z. B. `rows`); die Engine schreibt eine Zahl in so einem Feld als Text.
  Der Wert wird in `Process` geprüft, nicht in `New`: `runnel validate` ruft `New` mit den Variablen, wie sie in der Datei
  stehen.

## 2. Verarbeiten (`Process`)

```go
func (t *upper) Process(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
	for b := range in {                 // bis alle eingehenden Links beendet sind
		for _, r := range b {
			v, _ := r.Get(t.Field)
			r.Set(t.Field, strings.ToUpper(fmt.Sprint(v)))
			if err := out.Emit(r); err != nil {
				return err              // Pipeline abgebrochen
			}
		}
	}
	return nil
}
```

| Thema | Regel |
|---|---|
| Eingabe | `in` liefert Batches, bis alle eingehenden Links beendet sind; bei mehreren Links gemischt. Ein Transform ohne eingehende Links (Eingabe) bekommt einen geschlossenen Kanal und erzeugt Records selbst |
| Ausgabe | `out.Emit(r)` je Record; die Engine bündelt zu Batches und gibt jeden Record an alle ausgehenden Links (ab dem zweiten als Kopie). Einen Record nach `Emit` nicht mehr ändern |
| Backpressure | `Emit` blockiert, solange die folgenden Transforms beschäftigt sind – nichts selbst puffern, sonst steigt der Speicher |
| Abbruch | `Emit` gibt bei Abbruch einen Fehler zurück: sofort zurückgeben. Bei langen Schritten ohne `Emit` (z. B. Datei öffnen) `ctx` weitergeben |
| Fehler | Ein Fehler hält die ganze Pipeline an (Fehler-Links für einzelne Records folgen mit S17). Meldungen wie bei Actions |
| Frühes Ende | Ein Transform darf aufhören, bevor `in` leer ist; die Engine verwirft den Rest |
| Records | geordnete Felder (`internal/record`): `Get`, `Set`, `Delete`, `Clone`; Namen mit `_` sind für Metadaten reserviert ([ADR 0007](../adr/0007-typsystem-pipelines.md)). Werttypen und Schema kommen mit S14 |
| Log | `out.Logf`: `Info` für Ergebnisse, `Debug` für Details; bei großen Datenmengen nie je Record auf `Info` |
| Zustand | Ein Transform-Objekt läuft in genau einer Goroutine; Zustand im Objekt ist erlaubt, globaler nicht |

## 3. Tests

```go
func TestUpper(t *testing.T) {
	tr := transformtest.New(t, upperDefinition, map[string]any{"field": "name"})
	out, logs, err := transformtest.Run(context.Background(), tr, record.New(record.Field{Name: "name", Value: "ada"}))
	…
}
```

- **`transformtest.Run`** gibt Records in einen Transform und liefert die ausgegebenen Records, Log und Fehler.
- **Vertragstest** (`internal/transforms/transforms_test.go`) läuft für jeden Transform: Kategorie, Texte, Beschreibungen
  aller Felder, unbekannte Felder werden abgelehnt, Beispielkonfiguration aus dem Schema wird angenommen, Abbruch.
- Zusammenspiel mehrerer Transforms über die Engine: Tests in `internal/engine/pipeline`, Beispiele unter `examples/`.
- Golden-Dateien des Katalogs mit `go test ./internal/cli -update` neu erzeugen und den Diff prüfen.

## 4. Checkliste für den PR

- [ ] Eine Datei je Transform, Eintrag in `register.go`, Kategorie, Label, Beschreibung, `doc` für jedes Feld
- [ ] Werte mit Variablen werden in `Process` geprüft; `Emit`-Fehler werden zurückgegeben
- [ ] Tests mit `transformtest`, Vertragstest grün, Katalog-Goldens geprüft
- [ ] KONZEPT Kap. 12.2 und ROADMAP aktualisiert, ggf. Beispiel unter `examples/`
