# ADR 0002: Go als Sprache für Backend und Runner

- **Status:** Angenommen
- **Datum:** 2026-11-18
- **Bezug:** [Konzept, Kap. 4 – Technologie-Stack](../KONZEPT.md#4-technologie-stack)

## Kontext

Runner, Engine und Server sollen als **eine ausführbare Datei ohne Laufzeitumgebung** unter Windows laufen,
als Windows-Dienst betreibbar sein und Pipelines mit vielen parallel laufenden Transforms effizient ausführen.
Der Editor (React/TypeScript) ist von dieser Entscheidung nicht betroffen.

## Betrachtete Optionen

| Kriterium | Go | .NET 9 (AOT) | Node/Electron | Rust |
|---|---|---|---|---|
| Single-Binary ohne Runtime | ✅ nativ | ✅ (AOT mit Einschränkungen bei Reflection) | ❌ | ✅ |
| Nebenläufigkeit für Pipelines | ✅ Goroutinen + Channels | ✅ Channels/Tasks | ⚠️ Single-Thread | ✅ (komplexer) |
| Windows-Dienst | ✅ `x/sys/windows/svc` | ✅ | ⚠️ | ✅ |
| DB-Treiber ohne CGO | ✅ MSSQL, Postgres, MySQL, Oracle, SQLite | ✅ | ✅ | ⚠️ |
| Entwicklungsgeschwindigkeit | ✅ hoch | ✅ hoch | ✅ hoch | ⚠️ mittel |
| Cross-Compile Linux/macOS | ✅ trivial | ✅ | ⚠️ | ✅ |

Node/Electron scheidet wegen Footprint und Runtime aus, Rust wegen Entwicklungsaufwand.
Ernsthaft abgewogen wurden **Go** und **.NET 9**.

## Entscheidung

Backend, Runner, Server und eingebaute Actions/Transforms werden in **Go** entwickelt
(unterstützte, stabile Go-Version, Build mit `CGO_ENABLED=0`).

Begründung:

- **Goroutinen und Channels bilden das Pipeline-Modell direkt ab**: Transform = Goroutine, Link = gepufferter Channel,
  Backpressure ergibt sich automatisch.
- **Kleine, statische Binary** ohne Laufzeitumgebung; Editor per `go:embed` eingebettet.
- **Reine Go-Treiber** für SQL Server, PostgreSQL, MySQL, Oracle und SQLite – kein CGO, einfacher Build.
- **Windows-Integration** (Dienst, EventLog, DPAPI) über `golang.org/x/sys/windows` vorhanden.
- **Cross-Compile** für Linux/macOS ohne Mehraufwand.

### Go-Version

- Es wird immer eine **vom Go-Projekt unterstützte Version** verwendet (unterstützt werden jeweils die zwei neuesten
  Hauptversionen); nur diese erhalten Sicherheitskorrekturen der Standardbibliothek.
- `go.mod` legt die Hauptversion (`go 1.26`) und den Mindest-Patchstand (`toolchain go1.26.11`) fest; die CI verwendet
  zusätzlich jeweils den neuesten Patch (`check-latest`). `govulncheck` meldet veraltete Stände automatisch.

## Konsequenzen

**Positiv**
- Einfacher Build und Auslieferung: ein Befehl, eine exe.
- Geringer Speicherbedarf und schneller Start.
- Großes Ökosystem für die benötigten Bibliotheken (siehe Konzept, Kap. 4.2).

**Negativ / zu beachten**
- **Go-Plugins (`plugin`-Paket) funktionieren unter Windows nicht** → externe Plugins laufen als eigene Prozesse
  über JSON-RPC (Konzept, Kap. 7.3). Dieses Design war ohnehin vorgesehen.
- Office-/COM-Integration ist in Go umständlicher als in .NET; wird bei Bedarf über PowerShell-Actions oder
  ein .NET-Plugin (prozessbasiert) abgedeckt.
- Windows-Authentifizierung (Kerberos/Negotiate) im Server erfordert zusätzliche Bibliotheken (geplant für eine spätere Phase).
- Teammitglieder ohne Go-Erfahrung brauchen Einarbeitung.
