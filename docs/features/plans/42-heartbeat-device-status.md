# Plan: #42 — Heartbeat-based device online/offline detection

## Ausgangslage

Registry-Persistenz (#10) und `Registry.UpdateStatus` existieren bereits;
nichts markiert aktuell ein Gerät als offline nach Inaktivität — das ist
laut Issue explizit der fehlende Teil. Es gibt aktuell **keinen** expliziten
Heartbeat-Mechanismus im Proto/SDK:

- Für MQTT-Geräte ist `udal/{deviceId}/status` bereits als "device heartbeat"
  im Topic-Schema dokumentiert (#11), aber nirgends konsumiert.
- Für direkt-gRPC-Geräte (`code/sdk/go`) gibt es keine Heartbeat-RPC; die
  offene `StreamCommands`-Verbindung selbst ist der einzige
  Lebendigkeits-Indikator, aber `LastSeen` wird während einer bloß offenen,
  aber inaktiven Verbindung aktuell nirgends aktualisiert (nur
  `SetProperty` ruft `UpdateStatus` auf).

## Design-Entscheidungen

- **Neue `DeviceStatusEvent`-Fan-out über den bestehenden Broker, nicht über
  einen neuen Mechanismus** (Issue: "fan-out mechanism already exists, see
  #8's Broker"). `api.PropertyUpdate` bekommt ein neues, additives Feld
  `Status *api.DeviceStatus` (nil = normales Property-Update, gesetzt = ein
  Status-Wechsel-Event). Kein Broker-Rewrite nötig.
- **Proto-Änderung ist additiv, nicht breaking**: `SubscribeResponse`
  bekommt ein neues Feld `DeviceStatus status = 5;` (unset =
  Property-Event, gesetzt = Status-Event). `buf breaking` prüft in CI genau
  das — ein neues Feld ist unter den Standard-Regeln nicht breaking, eine
  `oneof`-Umstrukturierung der bestehenden Felder wäre es gewesen.
- **Neues `internal/heartbeat`-Package** (`Monitor`) statt Logik direkt in
  `registry` oder `service`:
  - `Touch(deviceID)`: markiert ein Gerät als jetzt-lebendig; Transition
    von jedem Nicht-Online-Status zu Online löst ein Event aus.
  - `Sweep()`: einmaliger Durchlauf über alle Geräte; jedes aktuell
    *online* Gerät, dessen `LastSeen` älter als `Timeout` ist, wird auf
    offline gesetzt (Event ausgelöst). Geräte im Zustand `Unknown`
    (frisch registriert, nie "touched") werden von `Sweep` ignoriert —
    es gibt keinen sinnvollen "War online, jetzt offline"-Übergang für sie.
  - `Run(ctx, interval)`: ruft `Sweep` periodisch bis `ctx` endet.
  - Get-dann-UpdateStatus in `Touch`/`Sweep` ist nicht atomar; ein
    seltenes doppeltes Event bei zeitgleichen Touches ist tolerierbar
    (gleiche Größenordnung wie Brokers bestehendes "skip if full"-Verhalten),
    kein Grund für Per-Device-Locking in diesem Ticket.
- **Zwei Heartbeat-Quellen, gleicher `Monitor`**:
  - MQTT: `Adapter.WatchDevice` abonniert zusätzlich `udal/{deviceId}/status`
    (bisher undokumentiert *ungenutzt*); ein neuer `OnHeartbeat`-Callback
    (Option `WithOnHeartbeat`) feuert bei jeder Nachricht darauf, verdrahtet
    in `main.go` auf `Monitor.Touch`.
  - Direkt-gRPC (`StreamCommands`): die offene Verbindung selbst ist der
    Heartbeat — ein `PresenceMonitor`-Interface
    (`Touch(deviceID) error`, `Interval() time.Duration`) wird beim
    Verbindungsaufbau sofort einmal getouched, danach über einen Ticker im
    bestehenden `select`-Loop der Handler-Funktion (kein zusätzlicher
    Goroutine nötig — ein `nil`-Channel im `select` blockiert einfach für
    immer, wenn kein `PresenceMonitor` konfiguriert ist).
  - `RegisterDevice` touched ebenfalls sofort (deckt AC3 "reconnect → online"
    für den Fall eines Neustarts mit erneuter Registrierung ab).
- **Defaults**: 30s Intervall / 90s Timeout (AC), verwendet wenn
  `cfg.Gateway.HeartbeatInterval`/`DeviceTimeout` aus #41 auf 0 stehen
  (nicht gesetzt).
- **Scope-Grenze**: kein neuer HTTP/CAN-Heartbeat (diese Adapter existieren
  noch nicht); kein Ändern der SDK, `StreamCommands` selbst bereits
  ausreichend als Lebendigkeitssignal für direkt-gRPC-Geräte.

## Phasen

### Phase 1 — Proto-Erweiterung
- `SubscribeResponse.status` (Feld 5, additiv) in `device.proto`
- `buf generate`, `buf lint`, `buf breaking` (gegen main) lokal prüfen

### Phase 2 — Broker/Subscribe-Fan-out für Status-Events
- `api.PropertyUpdate.Status *api.DeviceStatus` (neues, additives Feld)
- `DeviceService.Subscribe`: Status-Events werden unabhängig vom
  `property_path`-Filter immer weitergeleitet; Property-Events unverändert

### Phase 3 — `internal/heartbeat`-Package
- `Monitor` mit `Touch`/`Sweep`/`Run`, konfigurierbarem Timeout/Intervall
- Unit-Tests: Online-Transition (Event ja/nein je nach vorherigem Status),
  Offline-Transition nach Timeout, kein Sweep-Effekt auf `Unknown`-Geräte,
  `LastSeen` bleibt bei Offline-Transition unverändert

### Phase 4 — Wiring
- MQTT: `topicStatus`/`parseStatusTopic` in `topics.go`;
  `Adapter.WatchDevice` abonniert zusätzlich den Status-Topic;
  `WithOnHeartbeat`-Option; `dispatch` ruft den Callback bei
  Status-Topic-Treffern
- `DeviceService`: `PresenceMonitor`-Interface + `SetPresenceMonitor`;
  `RegisterDevice` touched; `StreamCommands` touched initial + per Ticker
- `main.go`: `heartbeat.Monitor` aus `cfg.Gateway.HeartbeatInterval`/
  `DeviceTimeout` (mit Defaults) konstruieren, `Run` als Hintergrund-Goroutine
  starten, an MQTT-Adapter und `DeviceService` verdrahten, beim Shutdown
  stoppen

### Phase 5 — Tests
- Unit-Tests für alle obigen Komponenten
- Manuelle Verifikation: echter Gateway-Prozess, MQTT-Status-Publish löst
  Online-Transition + Subscribe-Event aus; Timeout-Sweep löst
  Offline-Transition aus; `StreamCommands`-Verbindung hält ein Gerät online

### Phase 6 — Doku + Changelog
