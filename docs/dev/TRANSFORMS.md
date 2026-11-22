# Einen neuen Transform entwickeln

Transforms sind die Bausteine von Pipelines: Sie lesen Datensätze (Records), verändern, filtern oder schreiben sie.
Diese Anleitung ergänzt [ACTIONS.md](ACTIONS.md) – Namensräume, Konfiguration (`with:`), Schema-Tags, Texte, Windows-
Besonderheiten und Dokumentation gelten genauso. Hier steht nur, was bei Transforms anders ist.

Beispiele: [`internal/transforms/core/`](../../internal/transforms/core/) – `generate.go` (Eingabe ohne eingehende Links),
`dummy.go` (reicht weiter), `log.go` (Konfiguration, Log); [`internal/transforms/csv/`](../../internal/transforms/csv/) –
Datei lesen und schreiben (Positionen in Fehlern, temporäre Datei, Kodierungen).

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
- **Für den Editor (E14):** Namen vorhandener Felder der eingehenden Datensätze mit `schema:"format=runnel-field"` (auch an
  Listen von Texten wie `groupBy`), Ziele von Daten-Links mit `format=runnel-target`, Eigenschaften, die nur für einige
  Werte einer anderen gelten, mit `only=<feld>:<a>|<b>`. Namen neuer Felder bleiben ohne Format. Ein Transform mit einer
  Liste in der Konfiguration bekommt ein **`Example`** (die `with:`-Werte in YAML) für die Hilfe im Panel; der
  Vertragstest prüft, dass `New` es annimmt.

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
| Ausgabe | `out.Emit(r)` je Record; die Engine bündelt zu Batches und gibt jeden Record an alle ausgehenden Links (ab dem zweiten als Kopie). Einen Record nach `Emit` nicht mehr ändern. `out.EmitTo(id, r)` gibt einen Record nur an den Daten-Link zu einem bestimmten Transform (Beispiel `switch.go`); die möglichen Ziele stehen in `Init` in `ic.Targets`. Test: `transformtest.RunRouted` |
| Backpressure | `Emit` blockiert, solange die folgenden Transforms beschäftigt sind – nichts selbst puffern, sonst steigt der Speicher |
| Abbruch | `Emit` gibt bei Abbruch einen Fehler zurück: sofort zurückgeben. Bei langen Schritten ohne `Emit` (z. B. Datei öffnen) `ctx` weitergeben |
| Fehler | Ein Fehler, den `Process` zurückgibt, hält die ganze Pipeline an. Kann ein **einzelner Record** nicht verarbeitet werden (Wert passt nicht), `out.Fail(r, feld, err)` aufrufen: mit Fehler-Link geht der Record dorthin (mit `_error`, `_errorField`, `_errorTransform`), sonst gibt `Fail` den Fehler zurück – ihn zurückgeben. Den eingehenden Record unverändert übergeben, dazu `RecordErrors: true` in der Definition. Test: `transformtest.RunWithErrorLink`. Meldungen wie bei Actions |
| Frühes Ende | Ein Transform darf aufhören, bevor `in` leer ist; die Engine verwirft den Rest |
| Records | geordnete Felder (`internal/record`): `Get`, `Set`, `Delete`, `Clone`; Namen mit `_` sind für Metadaten reserviert ([ADR 0007](../adr/0007-typsystem-pipelines.md)). Werte in der Go-Darstellung ihres Typs: `string`, `int64`, `decimal.Decimal`, `float64`, `bool`, `record.Date`, `time.Time`, `[]byte`, `map[string]any`/`[]any`; fremde Werte mit `record.Normalize` umwandeln |
| Log | `out.Logf`: `Info` für Ergebnisse, `Debug` für Details; bei großen Datenmengen nie je Record auf `Info` |
| Zustand | Ein Transform-Objekt läuft in genau einer Goroutine; Zustand im Objekt ist erlaubt, globaler nicht |
| HTTP-Aufrufer | `out.HasCaller()` meldet, ob ein Aufrufer eines HTTP-Triggers wartet; `out.Respond(resp)` antwortet ihm (erste Antwort gilt, `resp.Fallback` nur ohne andere Antwort). Beispiel `internal/transforms/http/response.go` (S38c) |
| Aufrufer | `out.Rows()` liefert die Datensätze, die ein Workflow mitgegeben hat (`rows` an `core.run-pipeline`); `out.AddResult(r)` gibt einen Datensatz an ihn zurück (`output.rows`). Beispiele `fromresult.go`, `toresult.go` |
| Leistung | Was je Record läuft, zählt bei Millionen Records. Neue Records mit bekannter Feldzahl mit `record.Make(n)` anlegen; stehen die Namen fest und sind eindeutig (z. B. Spalten einer Datei), die Felder sammeln und mit `record.FromFields` übernehmen – `Set` sucht bei jedem Feld nach dem Namen. Haben die Records meist dieselben Felder in derselben Reihenfolge, liest `r.GetAt(i, name)` zuerst an Position `i` (Beispiel `csv/output.go`). Nachschlagen (Codecs, Indizes) einmal vor der Schleife, nicht je Record |
| Kopien | Mit `Parallel: true` in der Definition darf der Transform mit `copies: n` laufen: die Engine erzeugt n Objekte (je `New` und `Init`), die sich die Eingabe teilen. Nur setzen, wenn jeder Record für sich verarbeitet wird und nichts doppelt gelesen oder geschrieben wird (nicht bei Ein- und Ausgaben) |
| Felder ermitteln | Ein Eingabe-Transform, dessen Felder aus der Eingabe kommen, setzt `DetectFields` in der Definition (S46): Die Funktion bekommt die Konfiguration mit aufgelösten Variablen und ein Limit an Zeilen und gibt eine `transform.Detection` zurück – je Feld Name, Typvorschlag über `format.Propose` (nur wenn jeder Wert passt), die ersten Werte und eine Begründung. Vorhandene `fields:` gleicht `transform.MergeFields` ab. Der Editor zeigt dann „Detect fields…“. Tests mit Beispieldateien und ein `Fuzz…`-Ziel wie bei `csv.input` (`detect.go`) |

## 3. Schema (`Init`)

```go
// Init meldet das Schema der ausgegebenen Records und die gelesenen Felder (ADR 0007).
func (t *upper) Init(ic *transform.InitContext) (record.Schema, error) {
	ic.Use(t.Field, "field")            // Pfad innerhalb von with: – für Datei, Zeile, Spalte
	return ic.In.With(t.Field, record.String), nil
}
```

- `ic.In` ist das Schema der eingehenden Records (bei mehreren Links gemischt; leer und geschlossen ohne Eingang).
- Jedes Feld, das der Transform liest, mit `ic.Use` melden: fehlt es im geschlossenen Schema, ist es ein Fehler, im
  offenen eine Warnung – jeweils mit „did you mean …?“. Ist im offenen Schema nichts bekannt, wird nichts gemeldet.
- Das Ergebnis beschreibt die Ausgabe: **geschlossen**, wenn alle Felder bekannt sind (z. B. Eingaben mit Felddefinition,
  `core.generate-rows`), **offen**, wenn weitere vorkommen können. Transforms, die nur weiterreichen, geben `ic.In` zurück.
- Ohne `Init` gilt die Ausgabe als offen und unbekannt – für neue Transforms nur, wenn sich wirklich nichts ableiten lässt.
- Ausdrücke je Datensatz (z. B. eine Bedingung) mit `transform.CompileExpression` in `New` übersetzen, die Felder aus
  `expr.Fields()` mit `ic.Use` melden und in `Process` mit `expr.Bool(out, r)` auswerten (Beispiel `filter.go`); die
  Werte des Laufs (`params` …) liefert `out.Values()`. In Tests: `transformtest.RunWithValues`. Ein `Expression` hält
  seinen Kontext zwischen den Records und übergibt nur die Felder, die der Ausdruck nennt; es gehört deshalb genau einem
  Transform-Objekt (in `New` anlegen, nicht teilen).
- Konstante Werte aus der Konfiguration mit `record.Normalize` umwandeln (`transform.DecodeConfig` liefert Zahlen als
  `json.Number`).

## 4. Tests

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

## 5. Checkliste für den PR

- [ ] Eine Datei je Transform, Eintrag in `register.go`, Kategorie, Label, Beschreibung, `doc` für jedes Feld
- [ ] Feldnamen mit `format=runnel-field`, Link-Ziele mit `format=runnel-target`; bei Listen ein `Example`
- [ ] Werte mit Variablen werden in `Process` geprüft; `Emit`-Fehler werden zurückgegeben
- [ ] `Init` meldet das Ausgabeschema und alle gelesenen Felder (`ic.Use`), mit Tests
- [ ] Tests mit `transformtest`, Vertragstest grün, Katalog-Goldens geprüft
- [ ] KONZEPT Kap. 12.2 und ROADMAP aktualisiert, ggf. Beispiel unter `examples/`
