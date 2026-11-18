# Beispiele

| Ordner | Inhalt |
|---|---|
| [`hello/`](hello/) | Kleiner, gültiger Workflow |
| [`broken/`](broken/) | Absichtlich fehlerhafte Workflows, um die Meldungen von `runnel validate` zu zeigen |

```bash
runnel validate examples/hello
```

```bash
runnel validate examples/broken
```

Die Beispiele werden in den Tests geprüft (`internal/cli/validate_test.go`), damit sie aktuell bleiben.
