# ADR 0004: MIT-Lizenz

- **Status:** Angenommen
- **Datum:** 2026-11-18
- **Bezug:** [Konzept, Kap. 16 – Entscheidungen](../KONZEPT.md#16-entscheidungen)

## Kontext

Für easy-flow war eine Lizenz festzulegen. Betrachtet wurden:

| Lizenz | Charakter | Beispiel |
|---|---|---|
| **MIT** | Permissiv, sehr kurz, kaum Pflichten außer Lizenz-/Copyright-Hinweis | React, React Flow, pgx |
| Apache 2.0 | Permissiv, ausdrückliche Patentlizenz, NOTICE-Pflichten | Apache Hop |
| GPL/AGPL | Copyleft, abgeleitete Werke müssen offengelegt werden | – |
| Fair-Code (z. B. Sustainable Use License) | Quelltext offen, kommerzielle Nutzung eingeschränkt | n8n |

## Entscheidung

easy-flow wird unter der **MIT-Lizenz** veröffentlicht. Der Lizenztext liegt in [`LICENSE`](../../LICENSE).

Die Lizenz gilt für das gesamte Monorepo (Runner, Server, Editor, SDKs, Beispiele, Doku) – siehe [ADR 0001](0001-monorepo.md).

## Begründung

- **Maximale Verbreitung und Einsatzfreiheit** – auch in Unternehmen, intern wie kommerziell, ohne Rechtsprüfung von Sonderklauseln.
- **Einfach verständlich** – kurzer Text, im Unternehmensumfeld bekannt und meist vorab freigegeben.
- **Plugin-freundlich** – Plugins und Konnektoren dürfen unter beliebiger Lizenz (auch proprietär) entwickelt werden;
  durch das prozessbasierte Plugin-Protokoll entsteht ohnehin keine Bindung an die Core-Lizenz.
- **Kompatibel mit den geplanten Abhängigkeiten** (überwiegend MIT, BSD-3-Clause, Apache 2.0).

## Konsequenzen

**Positiv**
- Keine Einstiegshürde für Nutzer und Beitragende.
- Keine Lizenzkonflikte bei der Wahl von Bibliotheken (Konzept, Kap. 4.2).

**Negativ / zu beachten**
- **Keine ausdrückliche Patentlizenz** (anders als Apache 2.0).
- **Kein Schutz vor kommerzieller Weiterverwendung** – Dritte dürfen easy-flow z. B. als eigenes Produkt oder
  gehostetes Angebot vertreiben. Ein späterer Wechsel zu einer restriktiveren Lizenz wirkt nur für neue Versionen.
- **Third-Party-Hinweise**: Lizenztexte aller eingebetteten Abhängigkeiten (Go-Module und Frontend-Pakete) müssen
  mit der exe ausgeliefert werden. Umsetzung: `THIRD_PARTY_NOTICES` wird im Build automatisch erzeugt
  (z. B. `go-licenses` für Go, `license-checker` für npm) und in Installer/ZIP aufgenommen; zusätzlich in der UI unter „Über“ abrufbar.
- **Abhängigkeitsprüfung in CI**: Neue Abhängigkeiten mit Copyleft-Lizenz (GPL/AGPL/LGPL statisch gelinkt) werden
  automatisch gemeldet und nur nach bewusster Entscheidung zugelassen.
- **Beiträge**: Externe Beiträge werden unter derselben Lizenz angenommen (Inbound = Outbound); ein CLA ist nicht vorgesehen,
  optional Developer Certificate of Origin (DCO, `Signed-off-by`).
