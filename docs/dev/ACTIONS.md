# Eine neue Action entwickeln

Anleitung für eingebaute Actions (Bausteine in Workflows). Für Transforms (Pipelines) folgt eine eigene Anleitung mit
S12/S13; für Plugins mit S49–S51.

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
(Beispiel: `core.success` und `core.abort` in `end.go`).

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
	Overwrite bool   `json:"overwrite" doc:"Replace existing files" schema:"default=false"`
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

  Optionen werden mit Komma getrennt (`schema:"required,example=30s"`); Werte dürfen daher kein Komma enthalten.
  Backslashes in Tags verdoppeln (`example=C:\\in`), sonst ist das ganze Tag ungültig.
- **Typen:** `string`, `bool`, Ganz- und Kommazahlen, Slices, `map[string]T`, verschachtelte Structs, `any`. Felder,
  die `${…}` enthalten dürfen, als `string` anlegen – auch Zahlen und Zeitangaben.
- **`DecodeConfig`** verwenden – es lehnt unbekannte Felder ab (Tippfehler im Workflow werden gemeldet; Vertragstest
  prüft das) und prüft Pflichtfelder.
- **`New` prüft nur die Struktur:** Pflichtfelder, Typen, unbekannte Felder. `New` wird zweimal aufgerufen: vor dem Lauf
  mit dem Text aus der Datei (Werte können noch `${…}` enthalten) und vor jeder Ausführung mit aufgelösten Variablen.
  **Werte** wie Dauern, Zahlen in Textform, leere Texte oder Pfade werden daher erst in `Execute` geprüft.
- **Zeitangaben** als `string` übernehmen und in `Execute` mit `model.Duration(s).Value()` umwandeln.
- **Actions ohne Konfiguration** lassen `Config` weg und lehnen jedes Feld ab (`len(with) > 0` → Fehler); Vorlage: `core/dummy.go`.
- **Kategorie** (`Category`): eine der Konstanten `action.Category…` (Gruppe in der Palette).
- **Bezeichnung** (`Label`): kurzer englischer Anzeigename mit Großbuchstaben am Anfang, z. B. `Copy Files`.
- **Beschreibung** (`Description`): ein kurzer englischer Satz ohne Punkt; erscheint in der Palette des Editors.
- Kommentar über der Definition: was die Action tut, Besonderheiten.

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
| `Result.End` | nur für Actions, die den Workflow beenden (`core.success`, `core.abort`); sonst leeres `Result` |
| Ausgabedaten | *kommt mit S08:* strukturierte Ergebnisse (z. B. Dateiliste, Statuscode) für nachfolgende Actions über `result.output` |
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
  `New` angenommen, Abbruch mit dieser Konfiguration. Pflichtfelder brauchen dafür ein realistisches `example`.
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
- [ ] Konfiguration über `DecodeConfig`; `New` prüft nur Struktur, Werte werden in `Execute` geprüft
- [ ] `ctx` wird beachtet (Abbruch, Timeout); keine Panics; keine Secrets im Log
- [ ] Verständliche englische Fehlermeldungen
- [ ] Tests: Konfiguration, Erfolg, Fehler, Abbruch; Golden-Dateien von `runnel catalog` aktualisiert und geprüft
- [ ] `make lint` und `make test` grün
- [ ] Doku und ggf. Beispiel aktualisiert
