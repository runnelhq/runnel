# Release vorbereiten

Ergänzt [README – Releases](../../README.md#releases) (Workflow, Tag, Archive) um die Schritte, die vor dem Tag im
Repository geschehen. Grundlage: [ADR 0029](../adr/0029-stabilitaet-versionen.md).

## 1. Changelog

In [CHANGELOG.md](../../CHANGELOG.md) wird „Unveröffentlicht“ zur neuen Version (`## v1.2.0 – 2027-03-01`), darüber
kommt ein leerer Abschnitt „Unveröffentlicht“. „Bruch“ gibt es nur in einer Major-Version.

## 2. Basis des Vertragstests

Die JSON-Ausgaben, die ADR 0029 zusagt, dürfen in 1.x nur wachsen. `TestContractGoldens` in
`internal/cli/contract_test.go` vergleicht dafür die aktuellen Golden-Dateien mit ihrem Stand beim letzten Release-Tag
in `internal/cli/testdata/contract/`:

| Basis | Ausgabe |
|---|---|
| `catalog.golden.json` | `runnel catalog --json` (wie `runnel-catalog.json` im Release und `GET /api/v1/catalog`) |
| `catalog-expressions.golden.json` | `runnel catalog --expressions --json` (wie `runnel-expressions.json`) |
| `schema-project.golden.json`, `schema-environment.golden.json`, `schema-trigger.golden.json`, `schema-connection.golden.json`, `schema-plugin.golden.json` | `runnel schema <art>` |

Der Test meldet mit JSON-Pfad, wenn gegenüber der Basis
- ein Schlüssel fehlt (z. B. ein Feld, eine Action, eine Funktion, ein Standardwert),
- ein Wert einen anderen JSON-Typ hat,
- ein `type` oder `default` einen anderen Wert hat,
- ein Wert aus `enum` oder `x-runnel-connection` fehlt,
- ein Feld neu in `required` steht.

Neue Schlüssel, Typen, Felder und Werte sind erlaubt, ebenso geänderte Texte. Frei sind auch `example`, `examples`,
`limits`, `x-runnel-only` und `x-runnel-group` (Hilfe und Anzeige im Editor). Listen von Objekten werden nach `name`
bzw. `type` verglichen, nicht nach ihrer Reihenfolge.

**Wann die Basis sich ändert:** nur beim Release, im Commit, der den Changelog abschließt, und erst nachdem
`make test` mit der alten Basis grün war:

```bash
cp internal/cli/testdata/catalog.golden.json internal/cli/testdata/catalog-expressions.golden.json \
   internal/cli/testdata/schema-*.golden.json internal/cli/testdata/contract/
```

`go test ./internal/cli -update` ändert die Basis nicht. Ein PR, der den Test rot macht, bricht eine Zusage: Das Feld
bleibt und wird abgekündigt ([ACTIONS.md](ACTIONS.md#felder-umbenennen-oder-abkündigen-deprecated-adr-0029)), oder der
Bruch wartet auf die nächste Major-Version.

**Vor `v1.0.0`** galt die Basis vom Stand der Einführung (R4); mit `v0.1.0-rc.1` wurde sie erneuert (Entscheidung
des Maintainers vom 2026-11-27), damit der Test ab dem Release Candidate gegen den Stand prüft, der unter Windows
getestet wird. Jeder weitere Release Candidate erneuert sie wie ein Release. Ein bewusster Bruch vor 1.0 (z. B. aus der Durchsicht der
Namen, R5) ändert die Basis im selben PR; der PR-Text nennt das unter „Golden-Dateien“ und „Stabilität (ADR 0029)“.
