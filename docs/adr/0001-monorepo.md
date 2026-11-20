# ADR 0001: Monorepo als Repository-Struktur

- **Status:** Angenommen
- **Datum:** 2026-11-18
- **Bezug:** [Konzept, Kap. 14 – Code-Struktur](../KONZEPT.md#14-code-struktur-monorepo)

## Kontext

Runnel besteht aus mehreren Teilen: Runner/Engine, Server, Editor (Web-UI), eingebaute Actions/Transforms,
JSON-Schemas, Plugin-SDKs, Beispielprojekte und Installer. Zu entscheiden war, ob diese Teile in einem gemeinsamen
Repository (Monorepo) oder in mehreren Repositories entwickelt werden.

Rahmenbedingungen:

- Der Editor wird per `go:embed` in die `runnel.exe` eingebettet – Editor und Backend werden gemeinsam ausgeliefert.
- In den frühen Phasen (0.1–0.5) ändern sich Datenmodell, Dateiformat, Descriptor und REST-API häufig;
  solche Änderungen betreffen fast immer Backend **und** Editor gleichzeitig.
- Das Plugin-Protokoll ist noch nicht stabil.
- Das Team ist klein.

## Betrachtete Optionen

1. **Monorepo** – alles in `runnel`.
2. **Mehrere Repos** – z. B. `runnel-core`, `runnel-editor`, `runnel-sdk-*`, je Plugin ein Repo.

### Mehrere Repos – Vorteile

- Unabhängige Releases und Versionierung (z. B. SDKs auf PyPI/NuGet/npm).
- Erzwungene saubere Grenzen über das Plugin-Protokoll.
- Niedrigere Hürde für externe Beiträge, granulare Rechte, getrennte Lizenzen möglich.
- Kleinere, schnellere CI-Läufe pro Repo.

### Mehrere Repos – Nachteile

- Editor ↔ Runner sind eng gekoppelt (Embedding); Getrenntheit erfordert Artefakt-Austausch und Kompatibilitätsmatrix.
- Komponentenübergreifende Änderungen werden zu koordinierten PRs mit Versionssprüngen.
- Duplizierte CI-, Lint-, Release- und Dependency-Konfiguration.
- Ende-zu-Ende-Tests müssen Stände aus mehreren Repos zusammenführen.
- Höhere Einstiegshürde für neue Mitwirkende.

## Entscheidung

Wir entwickeln Runnel **vorerst als Monorepo** (Struktur gemäß Konzept, Kap. 14).

Das Go-Plugin-SDK (`pkg/sdk`) bleibt im Monorepo; bei Bedarf wird es als eigenes Go-Modul innerhalb des Repos geführt.

## Konsequenzen

**Positiv**
- Änderungen an Modell, API und Editor sind atomar in einem Commit/PR möglich.
- Eine CI-Pipeline, ein Release-Prozess, eine Version für exe und Editor.
- Ende-zu-Ende-Tests laufen ohne Abstimmung zwischen Repos.

**Negativ / zu beachten**
- CI muss pfadbasiert filtern (z. B. Frontend-Build nur bei Änderungen unter `web/`), damit Läufe schnell bleiben.
- Grenzen zwischen Paketen (`internal/` vs. `pkg/sdk`) müssen per Konvention und Review eingehalten werden.
- Plugin-Autoren außerhalb des Kernteams arbeiten zunächst ebenfalls im Monorepo oder in eigenen, unabhängigen Repos.

## Neubewertung

Die Entscheidung wird überprüft, sobald einer der folgenden Punkte eintritt:

- Das Plugin-Protokoll erreicht eine stabile Version (geplant Phase 0.7 / 1.0) → **SDKs für Python, C# und Node**
  sowie **externe Plugins** (z. B. SAP) werden dann voraussichtlich in eigene Repos ausgelagert.
- Externe Beitragende oder abweichende Lizenzmodelle erfordern getrennte Repos.
- CI-Laufzeiten bleiben trotz Pfadfilterung dauerhaft zu lang.
