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

**Nachgezogen nach Implementierung** (alle Punkte unten sind jetzt
geschlossen — analog zum MQTT-Ticket, dessen entsprechende Doku-Updates
ebenfalls erst im letzten Phasen-Commit dazukamen):

- `architecture.jsonc`: Relationships `http_adapter → iot_device` (Poll) und
  `iot_device → http_adapter` (Webhook-Push) ergänzt; HTTP hat anders als
  MQTT/CAN keinen Broker/Bus dazwischen, der Adapter spricht das Gerät
  direkt an
- `architecture.jsonc`: zwei neue Runtime-Szenarien `http-property-read` und
  `http-webhook-push` ergänzt (Pendant zu `property-read`/
  `telemetry-publish`)
- `docs/arc42/arc42.adoc` §6.4/§6.5: Prosa-Pendant zu den beiden neuen
  Szenarien
- `docs/arc42/arc42.adoc` §8.4 und `docs/req42/req42.adoc` F-10: Config-Keys
  `adapters.http.webhook_port` und `adapters.http.mtls.{cert,key}` ergänzt
  (mTLS ist ein einzelnes gateway-weites Client-Zertifikat, nicht pro
  Device — siehe Design-Entscheidungen unten) sowie F-10s neue "Endpoint
  Convention"-Tabelle (Pendant zu F-09s Topic-Convention-Tabelle)
- `CHANGELOG.md`: Eintrag unter `[Unreleased]` ergänzt

**Vorbestehender, nicht #24-spezifischer Gap** (nur zur Kenntnis, nicht Teil
dieses Tickets, unverändert gelassen): `mqtt_adapter` hat eine
`reads`-Relationship zu `gateway.capability_registry` ("validate schema"),
`http_adapter` und `can_adapter` nicht. Falls Schema-Validierung
transportunabhängig gelten soll, gehört das in ein eigenes Ticket.

## Design-Entscheidungen

- **Device-Konfiguration über `Device.Labels`**: Es gibt kein eigenes
  Proto-Feld für eine Geräte-URL (anders als MQTT, das keine braucht — die
  Topic-Konvention leitet sich allein aus der Device-ID ab). `http.endpoint`
  (Pflicht) und `http.poll_interval` (optional, überschreibt den
  Gateway-weiten Default) sind neue, dokumentierte Label-Keys
  (`httpadapter.LabelEndpoint`/`LabelPollInterval`).
- **Bulk-Snapshot-Endpoint (`GET {endpoint}/properties`) für den
  Poll-Loop**, statt einzelner Pfade zu erraten: Der Adapter kennt a priori
  nicht, welche Properties ein Gerät hat (anders als MQTT, das per
  `props/#`-Wildcard alles bekommt, was das Gerät publiziert). Der Poll-Loop
  cached den zuletzt gesehenen kodierten Wert pro Pfad und feuert `onUpdate`
  nur bei tatsächlicher Änderung — kein Duplicate-Fan-out bei jedem Tick.
- **mTLS ist ein einzelnes, gateway-weites Client-Zertifikat**
  (`adapters.http.mtls.cert`/`.key`), nicht pro Device: Die AC spricht von
  "the gateway" (singular), nicht von einem Zertifikat pro Geräteklasse —
  passt auch zum bestehenden Muster, dass die Gateway-eigene Server-TLS
  ebenfalls ein einzelnes Zertifikat ist.
- **`SetProperty` auf `transport=http`-Geräten gibt `UNIMPLEMENTED` zurück**,
  fällt NICHT auf den In-Memory-`PropertyStore` zurück: Issue #24s AC nennt
  keine WriteProperty (anders als #11 für MQTT). Da `GetProperty` für
  http-Geräte aber immer den Adapter live pollt, sobald einer konfiguriert
  ist, wäre ein stiller `PropertyStore`-Write von jedem folgenden Read
  unsichtbar — das ist ein während der Implementierung gefundener und
  bewusst vermiedener Footgun, kein AC-Erweiterung.
- **Webhook-Empfänger läuft ohne eigenes TLS** (`adapters.http.webhook_port`,
  Klartext-HTTP): Keine AC verlangt eine Absicherung der Push-Richtung
  (Geräte → Gateway), nur der Poll-Richtung (Gateway → Geräte, mTLS). Für
  produktive Deployments hinter einem eigenen TLS-Terminierungspunkt
  vermutlich ausreichend, aber ein bewusst enger Scope — falls Geräte
  direkt exponiert werden müssen, ist das ein Folgeticket.
- **Kein Circuit Breaker** (anders als #11/MQTT): Es gibt keine persistente
  Verbindung, die geschützt werden müsste — jeder Request trägt sein eigenes
  Timeout, ein einzelner erreichbarer/nicht erreichbarer Request hat keine
  Nebenwirkung auf andere Geräte.

## E2E-Testabdeckung

Neuer Adapter mit zwei Produzent/Konsument-Ketten, die über Unit-Tests
hinaus e2e abgedeckt werden sollten (Chained-Test, kein reines Mocking der
jeweils anderen Seite) — alle vier Punkte sind abgedeckt:

- **Poll-Pfad:** Adapter pollt einen echten Test-HTTP-Server, parsed die
  JSON-Antwort, `GetProperty` liefert den typisierten Wert zurück
  (`adapters/http/adapter_test.go`, außerdem verkettet durch
  `service.DeviceService` in `service/device_service_http_e2e_test.go`)
- **Webhook-Pfad:** eingehender Webhook-Call → Router → an einen
  `Subscribe`-Stream ausgeliefert (Pendant zum MQTT-Telemetrie-Pfad, der in
  #11 e2e getestet wurde) — `TestHTTPAdapter_EndToEnd` in
  `service/device_service_http_e2e_test.go`, mit einem echten
  `httpadapter.Adapter` (keinem Fake) durch eine echte `DeviceService`
- **mTLS-Pfad:** Adapter präsentiert beim Poll ein Client-Zertifikat gegen
  einen Test-HTTPS-Server mit Client-Cert-Verifikation (nicht nur gemockt) —
  `adapters/http/mtls_test.go`, inkl. Gegenprobe (kein Zertifikat →
  Server lehnt den Handshake ab)
- **Fehlerpfad:** HTTP 4xx/5xx vom Device-Endpoint → erwarteter gRPC-Status
  auf `GetProperty` — `adapters/http/adapter_test.go` (Adapter-Ebene,
  `*StatusError`) und `service/device_service_http_test.go`
  (`httpStatusError`-Mapping-Tabelle)

`go build ./...`, `go vet ./...`, `gofmt -l .` und `go test -race ./...`
(inkl. `-tags integration` Build-Check) sind grün.
`bausteinsicht validate --model architecture.jsonc` konnte in dieser Sandbox
nicht laufen (Binary nicht installiert) — sollte in CI verifiziert werden.
