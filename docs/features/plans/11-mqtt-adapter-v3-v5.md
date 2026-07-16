# Plan: #11 — MQTT Adapter (v3.1.1 + v5)

## Ausgangslage

Erster echter Transport-Adapter. Aktuell laufen `GetProperty`/`SetProperty` für
JEDES Device (unabhängig vom `Transport`-Feld) über den In-Memory
`api.PropertyStore` — es gibt keine Unterscheidung nach Transport-Typ.

## Bibliotheks-Entscheidung: kein einzelner Client deckt v3.1.1 + v5 ab

Recherche (via `go doc` gegen die tatsächlichen Module, nicht aus dem
Gedächtnis):
- `github.com/eclipse/paho.golang` (`paho`-Package) — **nur MQTT v5**
- `github.com/eclipse/paho.mqtt.golang` — laut eigenem Package-Kommentar
  "provides an MQTT v3.1.1 client library" — **nur v3.1.1**

Es gibt keine einzelne, aktiv gepflegte Go-Bibliothek, die beide Versionen in
einem Client abdeckt. "Auto-negotiate" wird daher so umgesetzt: Verbindung
zuerst mit `paho.golang` (v5) versuchen; scheitert das CONNECT spezifisch an
der Protokollversion (CONNACK-Reason-Code "Unsupported Protocol Version" bzw.
gleichwertiges Fehlverhalten beim v3.1.1-Broker), Fallback auf
`paho.mqtt.golang` (v3.1.1). Mosquitto 2.x (auch das CI-Service-Image)
unterstützt beide Versionen gleichzeitig — der v5-Versuch wird im
Normalfall also bereits erfolgreich sein; der Fallback-Pfad wird gegen eine
gezielt v3.1.1-only konfigurierte Mosquitto-Instanz getestet (`--protocol
mqttv311` lässt sich nicht direkt erzwingen, daher wird der Fallback-Pfad
stattdessen durch einen Unit-Test mit einem Fake-v5-Connector simuliert, der
den "unsupported version"-Fehler erzeugt — echtes Verhalten gegen einen
reinen v3.1.1-Broker ist mit den hier verfügbaren Mitteln nicht abbildbar,
da kein solcher Broker isoliert bereitsteht).

## Scope-Entscheidungen

- **Kein Command-Dispatch über MQTT in diesem Ticket.** F-09/Issue-AC listen
  nur ReadProperty/WriteProperty/Subscribe/Reconnect/Version/Circuit-Breaker
  — SendCommand-Routing über MQTT (Topic `udal/{deviceId}/cmds/{name}`) ist
  hier bewusst nicht angebunden (kein AC verlangt es), auch wenn das Topic in
  der Konvention steht. Die Konstante wird dokumentiert, aber ungenutzt
  gelassen — Folge-Ticket bei Bedarf.
- **Gateway-Routing**: `DeviceService.GetProperty`/`SetProperty` müssen künftig
  nach `Device.Transport` verzweigen (mqtt → Adapter; alles andere → bisheriger
  `PropertyStore`-Pfad, unverändert für direkt-gRPC-Geräte aus #12).
- **Kein Docker in dieser Sandbox** — lokale Verifikation läuft gegen einen
  direkt installierten Mosquitto-Broker (`apt install mosquitto`), CI nutzt
  weiterhin den bestehenden Docker-Service.

## Phasen

### Phase 1 — Adapter-Grundgerüst + v5-Client
- `code/gateway/internal/adapters/mqtt/adapter.go`: `Adapter` mit
  Connect/Disconnect, Topic-Konvention aus der Issue
- v5-Verbindung via `paho.golang`, Request/Response-Pattern für ReadProperty
  (`.../get` → Antwort auf `.../props/{path}`, Timeout default 5s) und
  WriteProperty (`.../set` → Bestätigung auf `.../set/ack`)
- Eingehende `.../props/{path}`-Publishes (unaufgefordert) → Callback für
  Gateway-Fan-out (Subscribe)

### Phase 2 — Reconnect + Circuit Breaker
- Exponentielles Backoff 1s–60s bei Broker-Trennung
- Circuit Breaker: 5 aufeinanderfolgende Fehler → 30s offen, dann Half-Open-Probe

### Phase 3 — v3.1.1-Fallback
- `paho.mqtt.golang`-basierte Implementierung derselben internen
  Adapter-Schnittstelle; Auswahl beim Connect basierend auf v5-Fehlschlag

### Phase 4 — Gateway-Wiring
- `DeviceService.GetProperty`/`SetProperty`: Transport-Verzweigung
- `main.go`: Adapter optional starten (`UDAL_MQTT_BROKER` env var), Callback
  ans `Broker` (Property-Fan-out) verdrahten

### Phase 5 — Tests
- Unit-Tests: Circuit Breaker, Backoff-Berechnung, Topic-Parsing,
  Request/Response-Korrelation (Fake-MQTT-Transport)
- Integrationstest gegen echten Mosquitto (lokal via `apt`, CI via
  vorhandenen Docker-Service): Read/Write-Roundtrip, Subscribe-Fan-out

### Phase 6 — Doku + Changelog
