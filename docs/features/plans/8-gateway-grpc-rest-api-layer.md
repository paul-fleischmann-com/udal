# Plan: #8 — Gateway gRPC + REST API Layer

## Ausgangslage

`code/gateway/internal/service/device_service.go` implementiert bereits GetDevice,
ListDevices, RegisterDevice, DeleteDevice, GetProperty, SetProperty (SendCommand ist
bewusst `Unimplemented`, da Transport-Adapter noch fehlen — außerhalb dieses Tickets).
`main.go` startet den gRPC-Server komplett ohne TLS und dialt den grpc-gateway-Mux
ebenfalls unverschlüsselt. `Subscribe` ist nicht implementiert (nur der von
`UnimplementedDeviceServiceServer` geerbte `Unimplemented`-Stub).

## Abgleich mit Acceptance Criteria (Issue #8)

| AC | Status | Anmerkung |
|----|--------|-----------|
| gRPC server starts with TLS, responds to all defined RPCs | ❌ **Lücke** | kein TLS irgendwo |
| REST gateway correctly translates HTTP ↔ gRPC for all endpoints | ✅ erfüllt | grpc-gateway bereits verdrahtet |
| Subscribe returns server-side stream; events < 50 ms p99 | ❌ **Lücke** | nicht implementiert |
| Invalid requests return structured gRPC error codes | ✅ weitgehend erfüllt | konsistente `status.Errorf` Nutzung, wird verifiziert |
| Unit tests for all RPC handlers | ⚠️ fehlt für Subscribe | Rest bereits abgedeckt |
| Integration test: Go client connects, reads property, receives stream event | ❌ **Lücke** | existiert nicht |

## Phasen

### Phase 1 — TLS für gRPC- und HTTP-Listener
- Config: `UDAL_TLS_CERT` / `UDAL_TLS_KEY` (PEM-Dateipfade), `UDAL_DEV_INSECURE`
  (explizites Opt-out, analog TC-03 "TLS mandatory; plain-text only via explicit --dev
  flag")
- Weder Zertifikat noch Dev-Insecure gesetzt → Startfehler mit klarer Meldung
- gRPC-Server: `credentials.NewTLS(...)` via `grpc.Creds(...)`
- HTTP-Server: `ListenAndServeTLS(cert, key)`
- Interner grpc-gateway-Dial (selber Prozess, Loopback) braucht ebenfalls TLS-Credentials
  passend zum Server-Zertifikat — `InsecureSkipVerify` dort mit Begründung
  (`#nosec G402`), da Server und Client hier derselbe Prozess sind und keine
  Angreifer-Position zwischen ihnen existieren kann

### Phase 2 — Subscribe-Streaming (Fan-out Broker)
- Neuer `Broker` (in `internal/api`): pro `deviceID` mehrere Subscriber-Channels,
  `Publish`/`Subscribe`/`Unsubscribe` — nutzt den bereits vorhandenen, bisher
  ungenutzten `PropertyUpdate`-Typ
- `SetProperty`-Handler publiziert nach erfolgreichem Schreiben ans Broker
- `Subscribe`-RPC: validiert Device, abonniert beim Broker, streamt Events (gefiltert
  nach `property_path`, falls gesetzt) bis Client-Disconnect (`ctx.Done()`)
- Unit-Tests mit Fake-`grpc.ServerStream` (Context()/Send() überschrieben)

### Phase 3 — Fehlercode-Audit
- Bestehende Handler auf konsistente `codes.*`-Nutzung durchsehen; wo nötig ergänzen
  (kein großer Umbau erwartet, da AC schon weitgehend erfüllt)

### Phase 4 — Integrationstest
- `//go:build integration` Test: echter Go-Client dialt TLS-Server, `RegisterDevice`,
  `SetProperty` + `Subscribe` parallel, empfängt Event innerhalb eines Test-Timeouts
  (funktionale Verifikation des Mechanismus — echtes p99-Lasttest-Benchmark ist
  außerhalb des Scopes eines einzelnen Integrationstests)

## Risiken / offene Punkte
- Kein internes CA-Setup — Betreiber muss eigenes Zertifikat bereitstellen (passt zu
  TC-03 "Certificates must be provisioned before first start")
- Fan-out-Puffergröße pro Subscriber-Channel ist ein Kompromiss zwischen Backpressure
  und Verlustfreiheit; für v1 ausreichend gepuffert, kein Slow-Consumer-Handling
