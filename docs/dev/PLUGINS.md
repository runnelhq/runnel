# Plugins entwickeln

Plugins erweitern Runnel um eigene Actions und Transforms, ohne Runnel selbst zu ändern. Ein Plugin ist ein eigenes
Programm in beliebiger Sprache, das Runnel als Kindprozess startet und über JSON-RPC auf stdin/stdout anspricht
([ADR 0019](../adr/0019-plugin-protokoll.md) mit Nachträgen; Namen nach [ADR 0013](../adr/0013-typnamen-namensraeume.md),
Nachtrag 2026-11-24).

Diese Datei ist die verbindliche Beschreibung für Plugins und SDKs. Stand: **S49a** – Ablage, `plugin.yaml` und
Freigabe. Das Protokoll (Handshake, `describe`, `action.execute`) folgt mit S49b, Transforms mit S50.

## Ablage

Ein Plugin ist ein Ordner unter `plugins/` im Projekt:

```text
mein-projekt/
├─ project.yaml
├─ workflows/
└─ plugins/
   └─ iban/
      ├─ plugin.yaml
      └─ bin/
         ├─ iban.exe        # Windows
         └─ iban            # Linux
```

Plugins gibt es nur im Projekt, nicht für alle Projekte eines Rechners. So enthält ein Projektordner alles, was er zum
Laufen braucht.

## `plugin.yaml`

```yaml
kind: Plugin
apiVersion: runnel/v1
name: iban                  # wie der Ordner
version: 1.0.0              # SemVer
description: IBAN und BIC prüfen
namespace: iban             # Typen des Plugins: iban.validate, …
executable:                 # Text oder je Betriebssystem (windows, linux, darwin)
  windows: bin/iban.exe
  linux: bin/iban
args: []                    # z. B. [main.py] für ein Skript
```

| Feld | Regel |
|---|---|
| `name` | wie der Ordner; Kleinbuchstaben, Ziffern und `-`, beginnt mit einem Buchstaben |
| `version` | SemVer (`1.2.0`, `2.0.0-beta.1`) |
| `namespace` | wie `name` gebildet; nicht einer der eingebauten (`core`, `file`, `http`, …, siehe `runnel catalog`) und nicht `mcp-…`; zwei Plugins eines Projekts dürfen nicht denselben nennen |
| `executable` | Pfad im Ordner des Plugins (ohne `..`), absoluter Pfad oder ein Befehl des Rechners (z. B. `python` mit `args: [main.py]`); als Zuordnung je Betriebssystem. Fehlt der Eintrag für das laufende System, wird das Plugin nicht geladen |
| `args` | Argumente des Programms; Dateien im Ordner des Plugins sind relativ zu ihm |

`runnel validate` und das Laden des Projekts prüfen die Datei; `runnel schema plugin` gibt ihr JSON Schema aus.

## Freigabe

Ein Plugin ist Code mit den Rechten von Runnel (beim Dienst: dessen Konto). Damit ein geklontes oder fremdes Projekt
nicht schon beim Öffnen oder Prüfen Code ausführt, startet Runnel ein Plugin erst, wenn es **auf diesem Rechner für
dieses Projekt freigegeben** ist:

```bash
runnel plugins D:\flows
```
```bash
runnel plugins trust D:\flows
```
```bash
runnel plugins untrust D:\flows --plugin iban
```

- `runnel plugins` zeigt je Plugin Version, Namensraum, Programm und Stand: `trusted`, `trusted (service)`,
  `changed since trusted` oder `not trusted`.
- `runnel plugins trust` zeigt Name, Version, Namensraum, Ordner, Programm und den **Hash des Ordners** und fragt je
  Plugin nach. `--plugin <name>` wählt einzelne, `--yes` gibt ohne Rückfrage frei (für Skripte).
- **Hash:** SHA-256 über alle Dateien des Plugin-Ordners (Pfad, Länge, Inhalt). Jede Änderung, z. B. ein neues
  Programm nach einem Update, hebt die Freigabe auf; danach erneut `trust`. Verknüpfungen im Ordner sind nicht
  erlaubt; höchstens 1 GB und 10.000 Dateien.
- **Wo die Freigaben liegen** (nie im Projekt):
  - Benutzer: `%AppData%\runnel\trusted-plugins.json` (Windows) bzw. `$XDG_CONFIG_HOME/runnel/trusted-plugins.json`.
  - Windows-Dienst: `%ProgramData%\runnel\trust\plugins.json`, mit `--service` in einer Konsole „als Administrator“;
    nur Administratoren und SYSTEM dürfen sie ändern.

## Namen und Veröffentlichung

- **Offizielle Plugins** liegen in `runnelhq/plugin-<name>`, SDKs in `runnelhq/sdk-<sprache>` (z. B. `sdk-go`).
- **Eigene Plugins:** Repository `runnel-plugin-<name>` in der eigenen Organisation, Namensraum mit Präfix der Firma
  (z. B. `contoso-sap`), damit es keine Zusammenstöße mit offiziellen Plugins gibt.
- **Release-Archiv:** `runnel-plugin-<name>_<version>_<os>_<arch>.zip` mit dem fertigen Ordner `<name>/` (`plugin.yaml`,
  Programme, `LICENSE`, `THIRD_PARTY_NOTICES`) und `SHA256SUMS`. Installieren: nach `plugins/` entpacken, dann
  `runnel plugins trust`.
- **Versionen:** SemVer-Tags, unabhängig von Runnel; die Version in `plugin.yaml` passt zum Tag.
- GitHub-Topic `runnel-plugin`.

## Protokoll

Folgt mit S49b: Transport (NDJSON über stdin/stdout, stderr als Log), Handshake mit `protocolVersion: 1` und
`concurrency`, `describe`, `action.execute`, Abbruch, Maskierung von Secrets. Bis dahin gilt die Beschreibung in
[ADR 0019](../adr/0019-plugin-protokoll.md).
