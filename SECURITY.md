# Sicherheit

## Schwachstellen melden

Bitte Sicherheitslücken **nicht** als öffentliches Issue melden, sondern vertraulich über
[GitHub – Report a vulnerability](https://github.com/runnelhq/runnel/security/advisories/new) (Private Vulnerability
Reporting). Hilfreich sind: Runnel-Version (`runnel version`), Windows-Version, betroffene Funktion (z. B. HTTP-Trigger,
Editor-API, Dienst), Schritte zum Nachstellen und – falls bekannt – die Auswirkung.

Wir bestätigen den Eingang in der Regel innerhalb einer Woche und stimmen Behebung und Veröffentlichung mit dir ab.

## Unterstützte Versionen

Runnel ist vor Version 1.0. Behoben wird jeweils in der neuesten Version; ältere Vorabversionen erhalten keine
Korrekturen.

## Sicherheitsmodell in Kürze

- Der Server (Editor und API) lauscht nur auf `127.0.0.1`; Netzwerkbetrieb des Editors folgt mit S40b.
- HTTP-Trigger haben einen eigenen Listener: Standard lokal, im Netz nur mit TLS (außer ausdrücklich
  `--triggers-insecure`), je Trigger ein Token (`Authorization: Bearer`) oder ausdrücklich `auth: none`
  ([ADR 0018](docs/adr/0018-http-trigger.md)).
- Tokens stehen nicht in Projektdateien, sondern in Umgebungsvariablen (`${env:…}`); ein Secret-Store folgt mit S39.
- Regeln für Beiträge: [AGENTS.md, Abschnitt „Sicherheit“](AGENTS.md#sicherheit).
