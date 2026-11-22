# Architekturentscheidungen (ADRs)

| Nr. | Titel | Status |
|---|---|---|
| [0001](0001-monorepo.md) | Monorepo als Repository-Struktur | Angenommen |
| [0002](0002-go-backend.md) | Go als Sprache für Backend und Runner | Angenommen |
| [0003](0003-join-semantik.md) | Join-Semantik in Workflows – konfigurierbar, Standard `all` | Angenommen |
| [0004](0004-lizenz-mit.md) | MIT-Lizenz | Angenommen |
| [0005](0005-mehrbenutzerbetrieb.md) | Mehrbenutzerbetrieb – ein Server, mehrere Browser, keine Benutzerverwaltung | Angenommen |
| [0006](0006-ausdruckssprache.md) | Ausdruckssprache – `expr-lang` für Ausdrücke, JavaScript für Skripte | Angenommen |
| [0007](0007-typsystem-pipelines.md) | Typsystem in Pipelines – schemalos mit optionalen Typen | Angenommen |
| [0008](0008-ausfuehrungsvarianten.md) | Ausführungsvarianten – Run-Konfigurationen, dynamische Ziele, Ausführung pro Datensatz | Angenommen |
| [0009](0009-begriff-link.md) | „Link“ statt „Hop“ für Verbindungen zwischen Knoten | Angenommen |
| [0010](0010-projektname-runnel.md) | Projektname „Runnel“ | Angenommen |
| [0011](0011-corporate-design.md) | Corporate Design | Angenommen |
| [0012](0012-ki-faehigkeit.md) | KI-Fähigkeit – LLMs, Agenten und MCP | Angenommen |
| [0013](0013-typnamen-namensraeume.md) | Typnamen, Namensräume und Aufbau der Bausteine | Angenommen |
| [0014](0014-action-descriptor-schema.md) | Action-Descriptor und Schema aus den Konfigurations-Structs | Angenommen |
| [0015](0015-sprache-oberflaeche.md) | Sprache der Oberfläche – Englisch | Angenommen |
| [0016](0016-live-events-sse.md) | Live-Events per Server-Sent Events statt WebSocket | Angenommen |
| [0017](0017-sub-workflows.md) | Sub-Workflows – Aufruf, Parameter, Pfade und Log | Angenommen |
| [0018](0018-http-trigger.md) | HTTP-Trigger – Läufe per HTTP starten und aus dem Lauf antworten | Angenommen |
| [0019](0019-plugin-protokoll.md) | Plugin-Protokoll – eigene Prozesse, JSON-RPC über stdin/stdout | Angenommen |
| [0020](0020-relative-pfade.md) | Relative Pfade beziehen sich auf den Projektordner | Angenommen |
| [0021](0021-unter-pipelines.md) | Unter-Pipelines in Pipelines – Mapping und Executor | Angenommen |
| [0022](0022-connections-secrets.md) | Connections und Secrets | Angenommen |

## Wann eine ADR?

Siehe [AGENTS.md, „Arbeitsweise“](../../AGENTS.md#arbeitsweise): neuer Dateityp oder `kind`, geänderte
Ausführungssemantik, neue Laufzeitabhängigkeit, neue Netzwerk- oder Sicherheitsgrenze, Namens- oder Sprachregeln, Abkehr
von einer bestehenden ADR. Nicht für neue Actions/Transforms nach bestehenden Regeln, neue Optionen oder Editor-Funktionen
nach freigegebenem Mockup.

Neue ADRs: nächste freie Nummer, Aufbau wie die bestehenden (Kontext, Entscheidung, Konsequenzen; bei neuer
Angriffsfläche ein Abschnitt „Sicherheit“), vorher mit dem Maintainer abstimmen, hier eintragen.

## Nachträge

- **Präzisierungen**, die die Entscheidung nicht ändern (z. B. Standardwerte, Grenzfälle, beim Umsetzen festgelegte
  Details), kommen als Abschnitt „Nachtrag JJJJ-MM-TT (Schritt)“ ans Ende der ADR – nur mit Entscheidungsinhalt.
  Schnittvermerke („S38b ist geteilt …“) und Messwerte gehören in die Roadmap bzw. das Konzept.
- **Widerspricht** etwas der Entscheidung oder regelt einen neuen Aspekt grundsätzlich (neues Format, neue Abhängigkeit,
  Sicherheitsmodell), gibt es eine **neue ADR**; die alte erhält den Status „Ergänzt durch ADR xxxx“ bzw. „Ersetzt durch
  ADR xxxx“.
- **Konsolidieren:** Mehr als drei Nachträge oder Nachträge länger als die Entscheidung selbst werden zu einem Nachtrag
  zusammengefasst, nach Themen gegliedert.
- Stellen im ursprünglichen Text, die ein Nachtrag überholt, bekommen einen kurzen Hinweis darauf.
