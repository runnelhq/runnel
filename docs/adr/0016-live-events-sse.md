# ADR 0016: Live-Events per Server-Sent Events statt WebSocket

- **Status:** Angenommen
- **Datum:** 2026-11-19
- **Bezug:** [Konzept, Kap. 10 – API](../KONZEPT.md#10-api-server-modus), [docs/dev/API.md](../dev/API.md), Roadmap-Schritt E3

## Kontext

Der Editor muss Läufe live verfolgen: Start und Ende, Status je Knoten, Logzeilen, später Metriken je Transform und
Dateiänderungen. Das Konzept sah dafür einen WebSocket unter `/ws` vor (Bibliothek `coder/websocket`) und zusätzlich
einen SSE-Strom für Logs.

Alle Befehle des Editors (Lauf starten, abbrechen, Datei speichern) sind gewöhnliche HTTP-Anfragen. Live-Daten fließen
nur vom Server zum Browser.

## Entscheidung

- Live-Ereignisse werden als **Server-Sent Events** (SSE) unter `GET /api/v1/events` geliefert – ein Strom für alle
  Ereignisse, optional gefiltert (`?run=<id>`). Ein eigener Log-Strom entfällt; Logzeilen sind Ereignisse (`log`).
- Jedes Ereignis trägt eine fortlaufende Nummer als SSE-`id`. Verbindet sich ein Client neu, sendet der Browser sie als
  `Last-Event-ID` und erhält die verpassten Ereignisse. Sind sie nicht mehr vorrätig (die letzten 2.000 werden
  gehalten), folgt das Ereignis `reset`: Der Client lädt den Zustand neu über `GET /api/v1/runs`.
- Ereignisarten: `run.started`, `run.finished`, `action.started`, `action.finished`, `action.skipped`, `log`;
  spätere Schritte ergänzen weitere (z. B. Metriken, `file.changed`) im selben Strom.
- Umsetzung mit der Go-Standardbibliothek; im Browser genügt `EventSource`.

## Begründung

| | SSE | WebSocket |
|---|---|---|
| Abhängigkeit | keine | `coder/websocket` o. ä. |
| Neuverbinden, verpasste Ereignisse | im Browser eingebaut (`EventSource`, `Last-Event-ID`) | selbst zu bauen |
| Richtung | Server → Browser | beide Richtungen |
| Proxys, Firewalls, Zugangsschlüssel (S39) | normales HTTP, gleicher Header wie die übrige API | Upgrade-Handshake, Schlüssel im Handshake |
| Testen, Debuggen | `curl -N` | eigenes Werkzeug |

Der einzige Vorteil eines WebSockets – Nachrichten vom Browser über dieselbe Verbindung – wird nicht gebraucht.

## Konsequenzen

- Konzept (Kap. 3, 4.2, 6.2, 10, 14) und Roadmap sind angepasst; `coder/websocket` entfällt aus der Bibliotheksliste.
- Browser begrenzen gleichzeitige HTTP/1.1-Verbindungen je Server (meist 6). Der Editor hält deshalb **eine**
  Ereignisverbindung je Tab offen und filtert selbst.
- Sollte später eine Funktion echte Zwei-Wege-Kommunikation brauchen (z. B. interaktives Debugging), wird das in einer
  neuen ADR entschieden; der SSE-Strom bleibt davon unberührt.
