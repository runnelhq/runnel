# Runnel – Roadmap der Plugins und SDKs

Ideen und Plan für offizielle Plugins und SDKs. Sie liegen in eigenen Repositorys mit eigenem Release-Takt
(`runnelhq/plugin-<name>`, `runnelhq/sdk-<sprache>`; ADR 0013 und [ADR 0019](adr/0019-plugin-protokoll.md)).

**Abgrenzung:** Alles, was Runnel selbst ändert – Plugin-Protokoll, Schlüssel in `describe`, Freigabe und Verteilung
von Plugins, `secrets.get` –, steht als Schritt in der [Roadmap](ROADMAP.md). Diese Datei verweist darauf
(Abschnitt [Abhängigkeiten vom Kern](#abhängigkeiten-vom-kern)). Für die Arbeit in den Plugin-Repositorys gelten
[AGENTS.md](../AGENTS.md) und [docs/dev/PLUGINS.md](dev/PLUGINS.md) sinngemäß; jedes Repository hat eigenes README und
CHANGELOG.

**Zeitpunkt:** Mit 1.0 sagt Runnel das Plugin-Protokoll stabil zu ([ADR 0029](adr/0029-stabilitaet-versionen.md)).
Empfehlung: Neue Plugins beginnen nach 1.0; bis dahin werden nur bestehende Plugins und SDKs gepflegt.

## Kern oder Plugin

Entscheidung des Maintainers 2026-11-26:

- **In den Kern** gehört Allgemeines, das viele brauchen: `http` mit starker Anmeldung (OAuth mit Zertifikat bzw.
  signierter Client-Assertion), Dateitransfer, SMTP und später IMAP, verbreitete Datenbanken.
- **Als offizielles Plugin** kommt Herstellerspezifisches mit eigenem Release-Takt (z. B. Microsoft 365, Google, ELO,
  SAP, DATEV) und alles, was nicht in den Kern gehört (z. B. Testdaten erzeugen).

## Übersicht

| Repository | Art | Sprache | Stand | Inhalt |
|---|---|---|---|---|
| [`runnelhq/sdk-go`](https://github.com/runnelhq/sdk-go) | SDK | Go | veröffentlicht (S51) | Handshake, `describe` mit Schema aus Struct-Tags, Actions, Transforms, Connections, Test-Hilfen |
| [`runnelhq/sdk-rust`](https://github.com/runnelhq/sdk-rust) | SDK | Rust | v0.1.0 | wie das Go-SDK, idiomatisch in Rust |
| [`runnelhq/plugin-iban`](https://github.com/runnelhq/plugin-iban) | Plugin | Go | veröffentlicht (S51) | IBAN prüfen |
| [`runnelhq/plugin-ldap`](https://github.com/runnelhq/plugin-ldap) | Plugin | Go | veröffentlicht (S51) | Verzeichnisse abfragen (LDAP, Active Directory) |
| [`runnelhq/plugin-faker`](https://github.com/runnelhq/plugin-faker) | Plugin | Rust | v0.1.0 | Testdaten erzeugen und pseudonymisieren |

Die aktuellen Versionen stehen in den Releases der Repositorys.

## Offene Arbeiten an bestehenden Plugins und SDKs

| Nr. | Thema | Inhalt | Hängt ab von |
|---|---|---|---|
| P1 | Referenz in `describe` | `outputs`, `records`, `errors`, `limits` in beiden SDKs anbieten und in den offiziellen Plugins füllen, damit Editor und Website die volle Hilfe zeigen (Format: Nachtrag E15b zu ADR 0014) | E15b ✅ |
| P2 | IDs im Go-SDK | Prüfung der JSON-RPC-IDs wie im Rust-SDK (ganze Zahl im Bereich von int64 oder Text, sonst `-32600`; Nachtrag zu ADR 0019) | – |
| P3 | Ideen für `plugin-ldap` | `ldap.lookup` je Datensatz (Transform), Passwörter in Active Directory setzen | – |

## Ideen für neue Plugins (nach 1.0)

| Nr. | Plugin | Inhalt | Hängt ab von |
|---|---|---|---|
| P10 | `plugin-msgraph` | Microsoft 365 über Microsoft Graph: SharePoint, OneDrive (Upload großer Dateien in Stücken), Mail, Teams | `http` mit starker Anmeldung im Kern |
| P11 | `plugin-google` | Google Drive, Gmail | – |
| P12 | `plugin-elo` | ELO ECM/DMS | – |
| P13 | `plugin-pdf` | PDF lesen und erzeugen | – |
| P14 | SAP, DATEV | Anbindung je nach Bedarf | – |
| P15 | Tresore | z. B. Azure Key Vault als Secret-Quelle | `secrets.get` im Protokoll |

## Ideen für weitere Bausteine

Notizen zu Actions und Transforms, die als Plugin kommen könnten. Eine Zeile je Thema; Actions liefern einen Wert für
den Workflow, Transforms Datensätze für Pipelines (z. B. eine Zeile je Laufwerk oder Dienst).

### System (`plugin-system`)

Zustand des eigenen Rechners abfragen, zunächst **nur die aktuellen Werte**. Vergleiche mit Grenzwerten, Meldung beim
Wechsel des Zustands und Verlauf über die Zeit gehören nicht dazu; geprüft wird im Workflow mit `core.check`.

| Nr. | Baustein | Art | Inhalt |
|---|---|---|---|
| P30 | `system.disk` | Action, Transform | Laufwerke bzw. Ordner (auch UNC-Pfade): `free`, `total`, `percentFree`; ohne Angabe alle lokalen Laufwerke |
| P31 | `system.cpu` | Action | mittlere Auslastung über eine kurze Messdauer (`duration`, Standard 5 s, mit Obergrenze): `percent`, `cores` |
| P32 | `system.memory` | Action | Arbeitsspeicher: `used`, `total`, `percent` |
| P33 | `system.services` | Action, Transform | Windows-Dienste nach Namen oder Muster (`MSSQL*`): `name`, `displayName`, `state`, `startType` |

- **Plattformen:** Windows über die Windows-API (`golang.org/x/sys/windows`), ohne PowerShell; `disk`, `cpu` und
  `memory` auch unter Linux, `system.services` nur unter Windows mit klarer Meldung auf anderen Systemen.
- **Rechte:** Abfragen gehen ohne Administratorrechte, auch im Dienst als LocalService.
- **Nur der eigene Rechner;** andere Rechner (Remote-SCM, WMI) wären ein eigenes Thema.
- **Sprache:** Go mit `sdk-go`. Den freien Platz liest Runnel für `/health` schon selbst (`internal/server/diskfree_*.go`).
- **Später, nicht Teil des Plugins:** Dienste starten oder neu starten; Grenzwerte und Meldung beim Wechsel brauchen
  einen Zustand zwischen Läufen im Kern (wäre ein Schritt der [Roadmap](ROADMAP.md) mit ADR).

## Ideen für neue SDKs (nach 1.0)

| Nr. | SDK | Repository |
|---|---|---|
| P20 | Python | `runnelhq/sdk-python` |
| P21 | C#/.NET | `runnelhq/sdk-dotnet` |
| P22 | Node | `runnelhq/sdk-node` |

## Abhängigkeiten vom Kern

Diese Punkte ändern Runnel selbst und werden als Schritt in der [Roadmap](ROADMAP.md) geplant:

- **`secrets.get`:** Methode im Plugin-Protokoll für Secret-Quellen aus Plugins (ADR 0022, „später“; Backlog-Zeile
  „Secret-Quellen“ der Roadmap).
- **Mindestversion von Runnel in `plugin.yaml`:** z. B. `requires: ">=0.9"`, damit ein Plugin ältere Versionen klar
  ablehnt.
- **`example` und `errors` für Connection-Typen aus Plugins:** Eingebaute Typen haben sie seit E15e; `describe` nimmt
  sie bei Connection-Typen noch nicht an ([PLUGINS.md](dev/PLUGINS.md)).
