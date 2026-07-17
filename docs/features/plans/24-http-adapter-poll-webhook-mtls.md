# Plan: #24 — HTTP Adapter (poll + webhook, mTLS)

## Ausgangslage

F-10. Anders als bei #11 (MQTT) existiert hier bereits die vollständige
Spec-/Architektur-Doku auf Beschreibungsebene, aber noch keine Zeile
Implementierung:

- `docs/req42/req42.adoc` F-10 (Beschreibung + AC) — deckungsgleich mit der
  Issue-Beschreibung
- `docs/arc42/arc42.adoc` §3.2 Technical Context: Zeile
  "Gateway → HTTP Device / HTTPS"
- `docs/arc42/arc42.adoc` §5.2 Building Block View: Zeile "HTTP Adapter /
  Go, net/http / Transport implementation for HTTP poll + webhook"
- `docs/arc42/arc42.adoc` §8.4 Configuration: `adapters.http.poll_interval:
  5s` im Beispiel
- `architecture.jsonc`: Komponente `gateway.adapters.http_adapter`
  (Titel/Beschreibung/Technologie), Relationship `router → http_adapter`
  ("route"), Aufnahme in die Views `context` und `gateway_internal`

Der Router verzweigt aktuell nicht nach `Transport=http` — analog zur
Ausgangslage, die #11 für MQTT beschrieben hatte, bevor der MQTT-Adapter
gebaut wurde.

## Doku-Status (per doc-check, vor Implementierungsbeginn)

**Bereits vollständig — keine Änderung nötig:** die oben genannten
Spec-/arc42-/architecture.jsonc-Stellen sind vollständig und stimmen mit den
Issue-AC überein.

**Offen — erst NACH der Implementierung nachziehen** (analog zum
MQTT-Ticket, dessen `architecture.jsonc`-Relationships/Runtime-View-Szenarien
und CHANGELOG-Eintrag erst im letzten Phasen-Commit e6b7dab/b509762 dazukamen,
nachdem der reale Ablauf feststand):

- `architecture.jsonc`: Relationship `http_adapter → iot_device` fehlt
  (`mqtt_adapter` hat die analoge Kante zu `mqtt_broker`, `can_adapter` zu
  `can_bus`)
- `architecture.jsonc`: kein Runtime-View-Szenario für den Poll-Pfad
  (ReadProperty) oder den Webhook-Push-Pfad — `mqtt_adapter` hat beide
  (Szenario-Indizes 5–7 bzw. 1–3 in den vorhandenen `views`)
- `docs/arc42/arc42.adoc` §6 Runtime View: Prosa-Pendant zu den beiden neuen
  Szenarien
- `docs/arc42/arc42.adoc` §8.4 Configuration: `adapters.http` hat aktuell
  nur `poll_interval`; kein Config-Key für den mTLS-Client-Cert
  ("gateway presents client cert to device when configured") — klären, ob
  das pro Device (Device-Registry-Feld) oder global unter `adapters.http`
  konfiguriert wird, dann dort ergänzen
- `CHANGELOG.md`: Eintrag unter `[Unreleased]` nach Abschluss

**Vorbestehender, nicht #24-spezifischer Gap** (nur zur Kenntnis, nicht Teil
dieses Tickets): `mqtt_adapter` hat eine `reads`-Relationship zu
`gateway.capability_registry` ("validate schema"), `http_adapter` und
`can_adapter` nicht. Falls Schema-Validierung transportunabhängig gelten
soll, gehört das in ein eigenes Ticket, nicht hierher.

## E2E-Testabdeckung

Neuer Adapter mit zwei Produzent/Konsument-Ketten, die über Unit-Tests
hinaus e2e abgedeckt werden sollten (Chained-Test, kein reines Mocking der
jeweils anderen Seite):

- **Poll-Pfad:** Adapter pollt einen echten Test-HTTP-Server, parsed die
  JSON-Antwort, `GetProperty` liefert den typisierten Wert zurück
- **Webhook-Pfad:** eingehender Webhook-Call → Router → an einen
  `Subscribe`-Stream ausgeliefert (Pendant zum MQTT-Telemetrie-Pfad, der in
  #11 e2e getestet wurde)
- **mTLS-Pfad:** Adapter präsentiert beim Poll ein Client-Zertifikat gegen
  einen Test-HTTPS-Server mit Client-Cert-Verifikation (nicht nur gemockt)
- **Fehlerpfad:** HTTP 4xx/5xx vom Device-Endpoint → erwarteter gRPC-Status
  auf `GetProperty`
