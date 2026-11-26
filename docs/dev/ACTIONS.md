# Eine neue Action entwickeln

Anleitung für eingebaute Actions (Bausteine in Workflows). Für Transforms (Pipelines) gilt ergänzend
[TRANSFORMS.md](TRANSFORMS.md); eine Anleitung für Plugins folgt mit S49–S51.

Grundlagen: [ADR 0013](../adr/0013-typnamen-namensraeume.md) (Typnamen, Namensräume, Code-Aufbau),
[ADR 0014](../adr/0014-action-descriptor-schema.md) (Descriptor, Schema aus dem Konfigurations-Struct),
[Konzept Kap. 6.1](../KONZEPT.md#61-workflow-engine) (Engine), [Kap. 12](../KONZEPT.md#12-mitgelieferte-bausteine-zielumfang)
(Bausteinliste). Ein Teil der Regeln wird automatisch geprüft – vom **Vertragstest** in
`internal/actions/actions_test.go`.

> Diese Anleitung wird mit jedem Schritt aktualisiert, der den Vertrag einer Action ändert (Roadmap-Regel).
> Abschnitte, die erst mit einem späteren Schritt gelten, sind markiert.

## 1. Action, Transform oder Plugin?

| Wenn … | dann |
|---|---|
| ein Schritt einmal pro Workflow-Lauf etwas tut (Datei kopieren, HTTP-Aufruf, Mail senden) | **Action** |
| Datensätze in einer Pipeline verarbeitet werden (lesen, umformen, schreiben) | **Transform** |
| der Baustein ein fremdes System anbindet, das nicht zum Kern gehört (SAP, Spezial-APIs), oder in einer anderen Sprache geschrieben ist | **Plugin** |

Derselbe Name darf als Action **und** als Transform existieren (z. B. `http.request`), siehe ADR 0013.

## 2. Namensraum und Typname

- Typnamen haben die Form `<namensraum>.<name>`: Kleinbuchstaben, Ziffern und `-`, z. B. `file.copy`, `http.request`.
- Der Name beschreibt die Tätigkeit (`copy`, `wait`, `send`), nicht die Technik.
- Eingebaute Actions liegen in einem **reservierten Namensraum** (`internal/model/namespaces.go`).
- **Neuer Namensraum?** Nur wenn kein bestehender passt:
  1. in `reservedNamespaces` eintragen (falls noch nicht vorhanden),
  2. Paket `internal/actions/<namensraum>/` mit `register.go` anlegen,
  3. dessen `Register` in `internal/actions/actions.go` (`namespaces`) eintragen.

## 3. Dateien

```
internal/actions/<namensraum>/
  register.go          # eine Zeile je Action ergänzen
  <name>.go            # die Action
  <name>_test.go       # ihre Tests
```

Kopiervorlage: [`internal/actions/core/wait.go`](../../internal/actions/core/wait.go) und `wait_test.go`.
Mehrere eng verwandte Actions dürfen sich eine Datei teilen, wenn sie dieselbe Implementierung nutzen
(Beispiel: `core.success` und `core.abort` in `end.go`). Hilfsfunktionen, die mehrere Actions eines Namensraums
gemeinsam nutzen, stehen in einer eigenen Datei des Pakets (Beispiel: `internal/actions/file/files.go` mit Platzhaltern,
Kopieren und Konfliktprüfung).

## 4. Definition und Konfiguration

```go
// file.copy copies files; it ends early when the run is cancelled.
var copyDefinition = action.Definition{
	Type:        "file.copy",
	Category:    action.CategoryFiles,
	Label:       "Copy Files",
	Description: "Copies files or folders",
	Config:      copyAction{},
	New:         newCopy,
}

type copyAction struct {
	Source    string `json:"source" doc:"File or folder to copy; wildcards allowed" schema:"required,example=C:\\in\\*.csv"`
	Target    string `json:"target" doc:"Target folder" schema:"required,example=D:\\archive"`
	IfExists  string `json:"ifExists" doc:"If a file exists in the target: replace it, skip it, or fail" schema:"enum=replace|skip|fail,default=fail"`
}

func newCopy(with map[string]any) (action.Action, error) {
	a := &copyAction{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err // unknown fields, missing required fields, wrong types
	}
	return a, nil
}
```

Regeln:

- **Felder** der Konfiguration (`with:`) als Struct mit `json`-Tags; Feldnamen in `camelCase`, wie im YAML.
- **`Config`** nennt dasselbe Struct, in das `New` einliest; daraus entsteht das JSON Schema für Editor, `/catalog` und
  KI-Werkzeuge (ADR 0014). Ansehen mit `runnel catalog <typ>` bzw. `--json`.
- **Tags je Feld:**

  | Tag | Inhalt |
  |---|---|
  | `doc:"…"` | Beschreibung des Feldes, ein englischer Satz – **Pflicht** (Vertragstest) |
  | `schema:"required"` | Pflichtfeld; `DecodeConfig` meldet `missing required field "…"` – keine eigene Prüfung schreiben |
  | `schema:"default=…"` | Wert, den die Action verwendet, wenn das Feld fehlt (nur Anzeige – die Action muss ihn selbst anwenden) |
  | `schema:"enum=a\|b\|c"` | erlaubte Werte (der Editor bietet eine Auswahl an) |
  | `schema:"example=…"` | Beispielwert; für Pflichtfelder empfohlen, der Vertragstest verwendet ihn |
  | `schema:"format=runnel-duration"` | Zeitangabe wie `30s`, `5m`, `1h30m` |
  | `schema:"format=runnel-text"` | mehrzeiliger Text (der Editor zeigt ein wachsendes Textfeld statt einer Zeile) |
  | `schema:"connection=sqlite"` | Name einer Connection (`connections/<name>.yaml`) der genannten Typen (`a\|b`); Format `runnel-connection`. Die Action holt sie mit `ec.Connection(ctx, name)` – Variablen und Secrets sind aufgelöst, Werte nie ausgeben ([ADR 0022](../adr/0022-connections-secrets.md)) |
  | `schema:"format=runnel-sql"` | SQL-Anweisung: wird unverändert übergeben, `${…}` und `{{ … }}` darin sind ein Fehler; Werte als `@name` mit einem Feld `params` |
  | `schema:"format=runnel-script"` | Skript oder Skriptdatei (`shell.powershell`, `core.script`; ADR 0023, 0028): wird unverändert übergeben, Werte nur über `params`. `${…}` und `{{ … }}` darin sind ein Fehler – außer im JavaScript von `core.script` (Action und Transform), wo sie Text sind (Felder mit Code: `model.CodeFields`, werden weder ersetzt noch geprüft) |
  | `schema:"format=runnel-params"` | Werte für ein Skript als Mapping beliebiger Werte (`core.script`, [ADR 0028](../adr/0028-javascript-core-script.md)): `${…}` und `{{ … }}` wie in anderen Feldern; ein Wert, der `${secret:…}` enthält, wird wie ein Feld für ein Secret aufgelöst (ohne Ausdrücke, maskiert). Bei Transforms nur `${…}`: `{{ … }}` bleibt Text |
  | `schema:"format=runnel-secret"` | Secret, z. B. Passwort oder Token ([ADR 0022](../adr/0022-connections-secrets.md)): nur hier ist `${secret:…}` erlaubt; aufgelöst ohne Ausdrücke, vor dem Lauf gelesen, der Wert wird in Log, Fehlern und Ergebnissen maskiert. Die Action darf ihn trotzdem nicht selbst ausgeben |

  Optionen werden mit Komma getrennt (`schema:"required,example=30s"`); Werte dürfen daher kein Komma enthalten.
  Backslashes in Tags verdoppeln (`example=C:\\in`), sonst ist das ganze Tag ungültig.
- **Typen:** `string`, `bool`, Ganz- und Kommazahlen, Slices, `map[string]T`, verschachtelte Structs, `any`. Felder,
  die `${…}` oder `{{ … }}` enthalten dürfen, als `string` anlegen – auch Zahlen und Zeitangaben. Ergibt ein Ausdruck
  für ein Textfeld eine Zahl oder ein Datum, erhält die Action den Wert als Text. Ebenso wird eine Zahl oder
  `true`/`false` in der Datei (`status: 404`) für ein Textfeld als Text übergeben – vor dem Lauf wie bei der
  Ausführung, wie in Pipelines.
- **Namen der Parameter** nach [ADR 0025](../adr/0025-einheitliche-parameter.md): `ifExists`, `ifMissing`, `format`,
  `as`, `from`, `maxRows`/`maxRecords`, Größen als Text (`10MB`), `timeout` als Dauer, Ausgabe `path`. Gibt es für eine
  Sache schon einen Namen, wird er übernommen; Abweichungen werden im PR begründet.
- **`DecodeConfig`** verwenden – es lehnt unbekannte Felder ab (Tippfehler im Workflow werden gemeldet; Vertragstest
  prüft das) und prüft Pflichtfelder.
- **Freie Werte** (`any`, `map[string]any`, `[]any`, z. B. ein Body oder Parameter) erhält die Action von
  `DecodeConfig` so, wie der Lauf sie liefert: exakte Dezimalzahlen bleiben `decimal.Decimal`, große Ganzzahlen
  `int64`, Datumswerte `record.Date`. Zum Schreiben als JSON `record.JSONValue` verwenden – sonst würde
  `encoding/json` Dezimalzahlen als Text in Anführungszeichen schreiben.
- **`New` prüft nur die Struktur:** Pflichtfelder, Typen, unbekannte Felder. `New` wird zweimal aufgerufen: vor dem Lauf
  mit dem Text aus der Datei (Werte können noch `${…}` und `{{ … }}` enthalten) und vor jeder Ausführung mit aufgelösten
  Variablen und ausgewerteten Ausdrücken.
  **Werte** wie Dauern, Zahlen in Textform, leere Texte oder Pfade werden daher erst in `Execute` geprüft.
- **Zeitangaben** als `string` übernehmen und in `Execute` mit `model.Duration(s).Value()` umwandeln.
- **Actions ohne Konfiguration** lassen `Config` weg und lehnen jedes Feld ab (`len(with) > 0` → Fehler); Vorlage: `core/dummy.go`.
- **Kategorie** (`Category`): eine der Konstanten `action.Category…` (Gruppe in der Palette).
- **Bezeichnung** (`Label`): kurzer englischer Anzeigename mit Großbuchstaben am Anfang, z. B. `Copy Files`.
- **Beschreibung** (`Description`): ein kurzer englischer Satz ohne Punkt; erscheint in der Palette des Editors.
- Kommentar über der Definition: was die Action tut, Besonderheiten.

### Ergebnis, Fehler und Grenzen (`Reference`, seit E15b)

Jede Action beschreibt im Feld `Reference` der Definition, was sie liefert, wann sie scheitert und welche Grenzen
gelten. `runnel catalog <typ>` zeigt es, `runnel catalog --json` (und `/api/v1/catalog`) enthält es als `outputs`,
`errors` und `limits`; daraus entstehen die Referenz auf der Website und die Hilfe im Editor (E15d).

```go
	Reference: action.Reference{
		Outputs: []action.OutputField{{Name: "output", Type: "list", Description: "Paths of the copied files in the target, without skipped ones"}},
		Errors:  "Fails when nothing matches the source, and with ifExists: fail (the default) when a file exists in the target, checked before anything is copied.",
	},
```

| Feld | Inhalt |
|---|---|
| `Outputs` | Teile von `result.output` mit ihrem Pfad als Name (`output` für den ganzen Wert, `output.status`, `output[].path` für die Einträge einer Liste), Typ und Beschreibung (eine Wendung ohne Punkt, wie `doc`). Typen: `string`, `integer`, `number`, `boolean`, `datetime`, `list`, `object`, `any`. **Pflicht** für jede Action mit `output`; Actions ohne stehen in `withoutOutput` im Vertragstest |
| `Errors` | Wann die Action scheitert (Fehler-Links, Retry), mit dem Verhalten von `ifExists`/`ifMissing`; „Never fails.“, wenn sie nie scheitert. **Pflicht** |
| `Limits` | Grenzen für Größe, Anzahl und Zeit mit ihren Standardwerten, z. B. `maxSize`, `timeout`; leer, wenn es keine gibt |

Texte englisch, als Sätze mit Punkt; Felder der Konfiguration mit ihrem Namen (`maxSize`), Zahlen mit Tausender-Komma
(`100,000`). Jede Angabe am Code prüfen – Standardwerte stehen in den Konstanten und `schema`-Tags, nicht im Gedächtnis.

## 5. Ausführen (`Execute`)

```go
func (a *copyAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	ec.Logf(slog.LevelDebug, "copying %s to %s", a.Source, a.Target)
	…
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	ec.Logf(slog.LevelInfo, "copied %d files", n)
	return action.Result{}, nil
}
```

| Thema | Regel |
|---|---|
| Abbruch und Timeout | `ctx` an alle blockierenden Aufrufe weitergeben (HTTP, DB, Prozesse) bzw. regelmäßig `ctx.Err()` prüfen; bei Abbruch `ctx.Err()` zurückgeben. Der Vertragstest prüft, dass eine Action bei abgebrochenem Lauf endet |
| Fehler | Ein zurückgegebener Fehler bedeutet „fehlgeschlagen“ → Fehler-Links, Retry. Meldungen für Anwender formulieren: was, wo, warum (`cannot copy "a.csv" to "D:\out": access denied`), englisch, ohne Stacktrace |
| Keine Panics | Fehler zurückgeben, nie `panic`; keine globalen Zustände ändern |
| Log | `ec.Log` / `ec.Logf`: `Info` für das Ergebnis (eine Zeile), `Debug` für Details (sichtbar mit `-v`), `Warn` für Auffälliges, das kein Fehler ist |
| Secrets | Passwörter, Tokens und `${secret:…}`-Werte nie loggen und nicht in Fehlermeldungen aufnehmen |
| Antwort an HTTP-Aufrufer | `ec.Respond` (nur `http.response`, seit S38b1): `nil` ohne wartenden Aufrufer – dann nichts tun; gibt `false` zurück, wenn schon geantwortet wurde |
| `Result.End` | nur für Actions, die den Workflow beenden (`core.success`, `core.abort`); sonst leeres `Result` |
| Ausgabedaten | `action.Result{Output: …}`: strukturierte Ergebnisse (z. B. Dateiliste, Statuscode) für nachfolgende Actions; Ausdrücke lesen sie als `result.output` bzw. `results["id"].output` (ADR 0006). Erlaubt sind Text, Zahlen, Wahrheitswerte, Listen und Maps davon; im Beschreibungstext der Action dokumentieren, was `output` enthält |
| Wiederholbarkeit | Eine Action kann wiederholt werden (`retry`, Schleifen) – sie sollte bei erneutem Aufruf sinnvoll funktionieren (z. B. Zieldatei überschreiben oder klar melden) |
| Nebenläufigkeit | Mehrere Instanzen können gleichzeitig laufen (parallele Zweige, `join: each`); keine gemeinsamen veränderlichen Variablen |

## 6. Windows-Besonderheiten

Runnel ist Windows-first; Actions, die mit Dateien, Prozessen oder Netzwerk arbeiten, beachten:

- **Pfade:** `path/filepath` statt `path`; Laufwerke (`D:\`), UNC-Pfade (`\\server\share\…`), Leerzeichen, lange Pfade (> 260 Zeichen).
- **Groß-/Kleinschreibung:** Dateinamen sind unter Windows nicht case-sensitiv.
- **Gesperrte Dateien:** Dateien, die ein anderes Programm geöffnet hat, verständlich melden.
- **Kodierungen:** Textdateien können UTF-8 (mit/ohne BOM), UTF-16 oder Windows-1252 sein.
- **Prozesse:** Argumente nicht über eine Shell zusammensetzen (keine Injection); Kindprozesse bei Abbruch beenden.

## 7. Tests

```go
func TestCopy(t *testing.T) {
	dir := t.TempDir()                       // nie echte Pfade
	…
	a := actiontest.New(t, copyDefinition, map[string]any{"source": src, "target": dst})
	res, logs, err := actiontest.Execute(context.Background(), a)
	…
}

func TestCopyConfig(t *testing.T) {
	actiontest.ConfigError(t, copyDefinition, nil, `missing required field "source"`)
}
```

Pflicht:

- **Konfigurationsfehler** (fehlende Pflichtfelder, falsche Werte) mit `actiontest.ConfigError`.
- **Erfolgsfall** und die wichtigsten **Fehlerfälle** von `Execute`, inklusive Meldungstext.
- **Abbruch** für Actions, die länger laufen können (`context.WithCancel` / `WithTimeout`).
- **Vertragstest** (`internal/actions/actions_test.go`) läuft automatisch für jede Action: Kategorie, Bezeichnung,
  Beschreibungen aller Felder, Beispielkonfiguration aus dem Schema (Beispiele, Standardwerte, erlaubte Werte) wird von
  `New` angenommen, Abbruch mit dieser Konfiguration. Pflichtfelder brauchen dafür ein realistisches `example`. Dazu
  `Reference` (seit E15b): `Errors` immer, `Outputs` außer für Actions in `withoutOutput`, bekannte Typen, Namen unter
  `output` (`actiontest.CheckReference`).
- **Golden-Dateien** `internal/cli/testdata/catalog*.golden*` mit `go test ./internal/cli -update` neu erzeugen und den
  Diff prüfen – er zeigt, wie die Action im Editor erscheint.
- Nur `t.TempDir()` und lokale Testserver (`httptest`), keine Abhängigkeit von Netz oder Rechnernamen.
- Tests dürfen nicht die **vollständige Liste** aller Action-Typen erwarten – sie wächst mit jeder neuen Action.
- Tests müssen unter Linux und Windows laufen (CI prüft beides).

## 8. Dokumentation und Beispiel im selben PR

- Bausteinliste im [Konzept, Kap. 12](../KONZEPT.md#12-mitgelieferte-bausteine-zielumfang) bzw. Roadmap-Schritt als erledigt markieren.
- Konfigurationsfelder über `doc`-Tags beschreiben (Grundlage für Eigenschaftsdialog und Werkzeugbeschreibung);
  Besonderheiten zusätzlich im Kommentar der Definition.
- Bei Bedarf ein Beispiel-Workflow unter `examples/`, der in `internal/cli/validate_test.go` mitgeprüft wird. Ein neues
  Beispiel ändert die Anzahl geprüfter Dateien: Erwartung in `TestValidateExamples` anpassen und
  `go test ./internal/cli -update` ausführen, danach den Diff der Golden-Datei prüfen.

## 9. Checkliste für den PR

- [ ] Typname im passenden, reservierten Namensraum; bei neuem Namensraum: `namespaces.go`, `register.go`, `actions.go`
- [ ] Eine Datei je Action, Tests daneben, Eintrag in `register.go`
- [ ] `Category`, `Label`, `Description`, `Config` gesetzt; jedes Feld mit `doc`, Pflichtfelder mit `required` und `example`
- [ ] Namen der Parameter nach ADR 0025 (gleiche Sache, gleicher Name)
- [ ] `Example` (die `with:`-Werte in YAML) für den Abschnitt „Help“ im Editor; der Vertragstest prüft es
- [ ] `Reference`: `Outputs` (Teile von `result.output`), `Errors`, `Limits` – am Code geprüft
- [ ] Konfiguration über `DecodeConfig`; `New` prüft nur Struktur, Werte werden in `Execute` geprüft
- [ ] `ctx` wird beachtet (Abbruch, Timeout); keine Panics; keine Secrets im Log
- [ ] Verständliche englische Fehlermeldungen
- [ ] Tests: Konfiguration, Erfolg, Fehler, Abbruch; Golden-Dateien von `runnel catalog` aktualisiert und geprüft
- [ ] `make lint` und `make test` grün
- [ ] Doku und ggf. Beispiel aktualisiert
