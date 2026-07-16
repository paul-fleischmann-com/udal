# Plan: #12 — Go SDK (device + application side)

## Ausgangslage

Es existiert noch kein SDK-Code (`code/sdk/` existiert nicht). Die gRPC-API
(#7/#8/#9/#10 bereits gemerged) deckt Register/Get/List/Delete/GetProperty/
SetProperty/Subscribe bereits vollständig ab. `SendCommand` gibt aktuell immer
`Unimplemented` zurück ("transport adapter not yet connected").

## Architektur-Lücke: Command-Zustellung an gRPC-direkt-verbundene Devices

Die Issue-Pseudocode (`device.OnCommand(...)`) verlangt, dass ein Gerät, das
**direkt per gRPC** verbunden ist (kein MQTT/HTTP/CAN-Adapter dazwischen),
eingehende Commands empfangen kann. req42.adoc F-07 beschreibt Command-Routing
aber ausschließlich über einen Transport-Adapter — für ein reines gRPC-Gerät
gibt es dafür noch keinen Mechanismus. Das ist eine echte Lücke in der Spec,
nicht nur eine Implementierungsdetail-Frage.

**Lösung:** neue bidirektionale RPC `StreamCommands` (device-seitig, nach
`RegisterDevice` geöffnet). `SendCommand` prüft zuerst, ob für die Ziel-Device-ID
ein aktiver `StreamCommands`-Kanal existiert (`CommandRouter`, analog zum
bestehenden `Broker` für Property-Updates) und nutzt den, bevor es auf den
bisherigen "kein Adapter verbunden"-Unimplemented-Fallback zurückfällt — keine
Regression für zukünftige adapter-basierte Devices (#11/#24/#25).

## Scope-Entscheidungen

- **Kein Heartbeat-Senden im SDK.** F-04/Heartbeat-Protokoll ist #42, noch nicht
  spezifiziert — das SDK sendet keine proaktiven Heartbeats, bis das feststeht.
- **Reconnect** bezieht sich auf den `StreamCommands`-Kanal (und implizit
  `Subscribe` auf Client-Seite): grpc-go reconnected die zugrunde liegende
  HTTP/2-Verbindung für Unary-Calls bereits selbst; abgebrochene Streams müssen
  vom SDK selbst mit Backoff neu geöffnet werden — das ist der Teil, den dieses
  Ticket explizit testet.
- **`pkg.go.dev`-Veröffentlichung** passiert automatisch, sobald ein getaggter
  Release des öffentlichen Repos existiert (Teil von #32 GoReleaser) — das SDK
  wird so strukturiert/dokumentiert, dass das funktioniert, aber das tatsächliche
  Tag-Pushen ist außerhalb des Scopes dieses Tickets.
- **RBAC für `StreamCommands`**: nicht in F-19s Matrix — analog zu `SendCommand`
  behandelt (admin/operator/device-own erlaubt, reader verboten).
- **API-Key-Identitäten haben kein `DeviceID`** (`auth.APIKeyStore.Authenticate`
  liefert nur `Subject`+`Role`). Das bedeutet: eine per API-Key authentifizierte
  `RoleDevice`-Identität kann *nie* eine "own device"-Prüfung bestehen, da
  `Identity.DeviceID` dabei immer leer ist — `RoleDevice` ist faktisch nur über
  mTLS erreichbar. Im Integrationstest (`sdk_integration_test.go`) läuft das
  Device daher bewusst über mTLS (Cert-CN = eigene Device-ID) und der
  Application-Client über API-Key — deckt beide Auth-Methoden ab, ohne diese
  Lücke zu berühren. Kein Fix in diesem Ticket (wäre ein separates Feature:
  "Device-scoped API keys"), nur dokumentiert.

## Phasen

### Phase 1 — Proto-Erweiterung: `StreamCommands`
- `Command`/`CommandResult`-Messages, `rpc StreamCommands(stream CommandResult)
  returns (stream Command)`; Ziel-Device-ID über `x-device-id`-Metadata beim
  Stream-Aufbau (kein Envelope-Feld nötig, da bidi)
- `buf generate`, Doku im proto-File

### Phase 2 — Gateway: `CommandRouter` + `SendCommand`-Wiring
- `internal/api/commandrouter.go`: pro Device-ID ein registrierter Kanal,
  Korrelation über generierte Command-ID (mehrere gleichzeitige Commands möglich)
- `SendCommand`: Router zuerst versuchen (Timeout default 10s → `DEADLINE_EXCEEDED`
  laut F-07), sonst bisheriger Unimplemented-Fallback
- `StreamCommands`-Handler in `device_service.go`, RBAC via `auth.Authorize`

### Phase 3 — SDK-Modul-Grundgerüst (`code/sdk/go/`)
- Eigenes Go-Modul, `udal.Config`/`udal.ClientConfig`, Fehlertyp `*udal.Error`
  (Code + Message, aus dem gRPC-Status abgeleitet)
- TLS/mTLS/API-Key-Optionen in der Config (wieder verwendbar für Device + Client)

### Phase 4 — Application/Client SDK
- `NewClient`, `GetProperty`, `WriteProperty` (Spec nennt `WriteProperty`, Issue
  nennt es nicht explizit fürs Application-SDK, aber 7.3 fordert es generell —
  ergänzt), `SendCommand`, `Subscribe` (Channel-basiert)

### Phase 5 — Device SDK
- `NewDevice`, `Run(ctx)` (RegisterDevice + StreamCommands öffnen + Reconnect-
  Loop mit Backoff), `PublishProperty` (→ SetProperty), `OnCommand`-Registry
  (Name → Handler), Command-Result-Zustellung zurück über den Stream

### Phase 6 — Tests
- Unit-Tests: CommandRouter (Dispatch/Timeout/Korrelation), SDK-Fehler-Mapping
- Integrationstest: echter Client + echtes Device (SDK gegen SDK über einen
  echten Gateway-Prozess) — Register, PublishProperty, OnCommand-Roundtrip,
  Subscribe-Event, TLS + mTLS + API-Key, simulierter 30s-Ausfall + Reconnect

### Phase 7 — GoDoc + Doku + Changelog
