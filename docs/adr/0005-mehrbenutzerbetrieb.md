# ADR 0005: Mehrbenutzerbetrieb – ein Server, mehrere Browser, keine Benutzerverwaltung

- **Status:** Angenommen
- **Datum:** 2026-11-18
- **Bezug:** [Konzept, Kap. 8 – Sicherheit](../KONZEPT.md#8-sicherheit), [Kap. 10 – API](../KONZEPT.md#10-api-server-modus)

## Kontext

Zu klären war, ob mehrere Personen gleichzeitig mit Runnel arbeiten können sollen und welche Infrastruktur
dafür nötig ist: Benutzerkonten und Rollen, Echtzeit-Kollaboration im Editor oder ein einfacheres Modell.

Apache Hop kommt ohne Benutzerverwaltung aus: Projekte sind Ordner, Zusammenarbeit läuft über Git,
der Schutz erfolgt über Betriebssystem, Netzwerk und Dateirechte.

## Entscheidung

1. **Ein Server, mehrere Browser.** Eine `runnel`-Instanz bedient beliebig viele gleichzeitig geöffnete Editoren.
2. **Keine Benutzerverwaltung** – keine Benutzerkonten, keine Rollen, kein Login pro Person (wie bei Apache Hop).
3. **Keine Echtzeit-Kollaboration** (kein gleichzeitiges Bearbeiten derselben Datei à la Google Docs).
   Zusammenarbeit über Projekte hinweg erfolgt über **Git**.

### Konfliktschutz beim Speichern

- Jede Datei wird mit **ETag** (Hash des Inhalts) ausgeliefert; Speichern erfolgt mit `If-Match`.
- Hat sich die Datei zwischenzeitlich geändert (anderer Browser, `git pull`, Editor außerhalb), antwortet der Server
  mit `412 Precondition Failed`. Der Editor bietet dann an: neu laden, eigene Version überschreiben oder als Kopie speichern.
- Über WebSocket (`file.changed`) zeigen alle offenen Editoren sofort an, wenn eine geöffnete Datei anderswo geändert wurde.
- **Hinweis „wird gerade bearbeitet“**: Öffnet ein Editor eine Datei mit ungespeicherten Änderungen, sehen andere
  Browser einen Hinweis (Rechnername/IP). Das ist eine Information, keine Sperre.

### Schutz ohne Benutzerverwaltung

| Modus | Schutz |
|---|---|
| Standard | Server bindet nur an `127.0.0.1` – nur lokal erreichbar, kein Schutz nötig |
| Netzwerk (`--listen 0.0.0.0:7300`) | **TLS** + ein **gemeinsamer Zugangsschlüssel** für den Server (in `runnel.yaml` bzw. als Secret, nicht pro Person). Der Browser fragt ihn einmal ab und merkt ihn sich als Cookie |
| Webhooks | Weiterhin eigenes Token bzw. HMAC-Signatur pro Webhook, unabhängig vom Zugangsschlüssel |
| Zusätzlich möglich | Windows-Firewall (nur bestimmte Hosts) oder Reverse-Proxy (IIS, Caddy) mit eigener Anmeldung davor |

Der Zugangsschlüssel ist **keine Benutzerverwaltung**, sondern das Äquivalent zu einem Passwort auf einer Netzwerkfreigabe.
Ohne ihn wäre ein Server im Netzwerk für jeden erreichbaren Rechner offen – inklusive Ausführung von PowerShell.

### Nachvollziehbarkeit

Ohne Benutzer wird statt „wer“ der **Client** protokolliert: IP-Adresse, Rechnername (Reverse-DNS) und User-Agent –
für manuelle Starts, Abbrüche und Dateiänderungen über den Editor. Für fachliche Historie dient Git.

## Konsequenzen

**Positiv**
- Deutlich weniger Komplexität: keine Benutzer-Tabelle, keine Rollenprüfung, kein Login-Dialog, keine Passwort-Resets.
- Konsistent mit Apache Hop und dem dateibasierten Projektmodell.
- Kein Risiko verlorener Änderungen dank ETag-Prüfung.

**Negativ / zu beachten**
- **Keine Rechtetrennung**: Wer Zugriff hat, kann alles (ansehen, ändern, starten, Secrets setzen).
  Gesetzte Secrets bleiben dennoch nicht im Klartext abrufbar.
- **Keine personenbezogene Nachvollziehbarkeit** im Tool selbst – nur Client-Informationen und Git-Historie.
- Werden später doch Rollen benötigt (z. B. „nur starten“ für Fachabteilungen), ist eine neue ADR nötig;
  der Weg wäre dann Windows-Authentifizierung oder ein vorgeschalteter Reverse-Proxy, keine eigene Benutzerdatenbank.

## Änderungen am Konzept

- Kap. 8: Benutzer/Rollen ersetzt durch Zugangsschlüssel im Netzmodus; Audit über Client-Informationen.
- Kap. 9.2: Tabelle `users` entfällt, `runs` erhält `client_info`.
- Kap. 15: „Auth/Rollen“ in Phase 0.4 wird zu „Zugangsschlüssel + TLS“; „Windows-Auth“ entfällt aus „später“.
