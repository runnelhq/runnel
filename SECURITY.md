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
- `shell.run` und `shell.powershell` starten Programme mit dem Konto des Runnel-Prozesses, ohne Shell und
  ohne Freigabe-Schalter: Wer Projektdateien schreiben darf, darf Programme starten. Für den Dienst ein eigenes Konto
  mit den nötigen Rechten statt `LocalSystem` verwenden ([ADR 0023](docs/adr/0023-programme-powershell.md)).
- `mail.send` prüft das Zertifikat des Mail-Servers immer und sendet ein Passwort nie unverschlüsselt (außer an den
  eigenen Rechner); `tls: none` ist nur für ein Relay im vertrauenswürdigen Netz gedacht und wird von `validate`
  gemeldet ([ADR 0022, Nachtrag S62a](docs/adr/0022-connections-secrets.md)). Mit `auth: oauth2` gilt dasselbe für das
  Token; es bleibt nur im Speicher des Prozesses (Nachtrag S62b).
- `http.request` mit einer `http`-Connection sendet deren Anmeldung nur an den Host von `baseUrl` und nie
  unverschlüsselt an einen anderen Rechner (ADR 0022, Nachtrag S63a). Mit `paginate` werden Folgeseiten nur auf
  Schema und Host der ersten Anfrage gelesen (Nachtrag S63b, S63c).
- „Test connection“ im Editor (`POST /connections/{name}/test`) testet nur gespeicherte Connections des Projekts mit
  15 s Zeitgrenze; `GET /secrets` nennt nur Namen und Stand, nie Werte (ADR 0022, Nachtrag S39c1).
- Regeln für Beiträge: [AGENTS.md, Abschnitt „Sicherheit“](AGENTS.md#sicherheit).
