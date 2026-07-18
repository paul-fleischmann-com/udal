# Plan: #25 — CAN Adapter (SocketCAN)

## Ausgangslage

F-11. Wie bei #24 (HTTP) existiert die vollständige Spec-/Architektur-Doku
auf Beschreibungsebene bereits, aber noch keine Zeile Implementierung:

- `docs/req42/req42.adoc` F-11 (Beschreibung + AC) — deckungsgleich mit der
  Issue-Beschreibung; Constraint TC-01 ("Linux ≥ 5.10 required;
  macOS/Windows not supported for CAN in v1")
- `docs/arc42/arc42.adoc` §5.2 Building Block View: Zeile "CAN Adapter"
- `docs/arc42/arc42.adoc` §8.4 Configuration: `adapters.can.interface: can0`
- `docs/arc42/arc42.adoc` §11 Risks: "CAN adapter panic brings down entire
  gateway (structured monolith)"
- `architecture.jsonc`: Komponente `gateway.adapters.can_adapter`, bereits
  vollständig verdrahtete Relationships (`router → can_adapter`,
  `can_adapter → can_bus`, `iot_device → can_bus`) und in den Views
  `context`/`gateway_internal`/`adapters` enthalten
- `config.go`: `CANAdapter{Interface string}` + `UDAL_CAN_INTERFACE` war
  bereits vor #25 als Stub vorhanden (ungenutzt)

Es gab keinen Branch, keinen Commit und keine Codezeile zu CAN/DBC/SocketCAN
im Repo (`git log --all` leer). Anders als bei #11/#24 gibt es hier auch
keine Go-Bibliothek im Projekt, auf die aufgebaut werden könnte — SocketCAN-
und DBC-Handling mussten neu entschieden werden (siehe unten).

## Doku-Status (per doc-check, vor Implementierungsbeginn)

**Bereits vollständig — keine Änderung nötig:** Komponente, Relationships
und statische Views in `architecture.jsonc` für `can_adapter` waren schon
vollständig (ungewöhnlich — bei #11/#24 mussten diese erst nachgezogen
werden). Nur die `technology`-Angabe ("Go, go-socketcan") war ein Platzhalter
ohne echte Bibliothek dahinter.

**Nachgezogen nach Implementierung:**

- `architecture.jsonc`: `technology`-Feld von `can_adapter` korrigiert (echte
  Umsetzung: `golang.org/x/sys/unix` + handgeschriebener DBC-Parser, keine
  externe SocketCAN-/DBC-Bibliothek); zwei neue Runtime-Szenarien
  `can-property-read` und `can-property-write` ergänzt (Pendant zu
  `http-property-read`/`http-webhook-push`)
- `docs/arc42/arc42.adoc` §5.2: `technology`-Zelle für CAN Adapter
  korrigiert; §6.6/§6.7: Prosa-Pendant zu den beiden neuen Szenarien; §8.4:
  `adapters.can.dbc_file` ergänzt; §11 Risks: Mitigation-Text präzisiert
  (tatsächlich implementiert: `recover()` pro Frame im Read-Loop, bewusst
  **kein** Circuit Breaker — siehe Design-Entscheidungen)
- `docs/req42/req42.adoc` F-11: neue "Message Convention"-Tabelle (Pendant
  zu F-10s "Endpoint Convention"), `adapters.can.dbc_file` im
  Beispiel-YAML, alle sechs AC-Boxen auf `[x]`
- `CHANGELOG.md`: Eintrag unter `[Unreleased]` ergänzt

## Design-Entscheidungen

- **Kein neuer externer Dependency für SocketCAN/DBC.** `architecture.jsonc`
  nannte ursprünglich "go-socketcan" als Platzhalter-Technologie; es gibt
  aber kein Paket dieses exakten Namens im Ökosystem, und
  `golang.org/x/sys/unix` (bereits transitive Dependency im `go.sum`) bringt
  vollständige SocketCAN-Syscall-Unterstützung nativ mit (`AF_CAN`,
  `SockaddrCAN`, `CAN_RAW`, …). Für DBC-Parsing kam `github.com/einride/can-go`
  in Frage, wurde aber verworfen: das Subset, das F-11 tatsächlich braucht
  (`BO_`/`SG_`, MUX-Signale, Bit-Extraktion), ist klein und gut spezifiziert
  genug, um es selbst zu schreiben — ohne zusätzliches Supply-Chain-Risiko
  und ohne die Codegen-lastige API-Form, die can-go für den typischen
  Build-Time-Workflow vorsieht (hier wird zur Laufzeit geparst, nicht
  generiert).
- **Bit-Layout-Algorithmus für Motorola/Big-Endian-Signale**: DBC-Dateien
  kodieren den `start_bit` für `@0`(Motorola)- und `@1`(Intel)-Signale in
  derselben "Array-Position"-Konvention (`8*byteIndex + bitInByte`,
  `bitInByte` 0=LSB..7=MSB) — sie unterscheiden sich nur darin, welches Ende
  des Signals `start_bit` benennt (LSB bei Intel, MSB bei Motorola) und in
  welche Richtung die übrigen Bits gelesen werden. `bitPositions()`
  (`codec.go`) implementiert das für beide Fälle einheitlich und ist per
  Hand-Beispiel verifiziert (`TestDecode_Motorola`/`TestEncode_MotorolaRoundtrip`
  in `codec_test.go`), nicht nur gegen eine externe Referenzbibliothek
  abgeglichen — es gibt keine im Projekt.
- **`Device.Labels["can.message"]`** (Konstante `LabelMessage`) statt einer
  ID-Konvention (wie MQTT) oder eines einzelnen Endpoint-Labels (wie HTTP):
  Eine CAN-Signal-Adresse ist `(Arbitration-ID, Signalname)`, und eine DBC-
  Datei beschreibt typischerweise *alle* Nachrichten eines ganzen Busses,
  nicht ein Gerät. `property_path` ist der Signalname innerhalb der über
  `can.message` referenzierten `BO_`-Nachricht.
- **Ein gemeinsamer Read-Loop pro Interface entkoppelt von `WatchDevice`**:
  CAN ist ein Broadcast-Bus — jeder eingehende Frame mit bekannter Message-ID
  wird sofort dekodiert und gecacht, unabhängig davon, ob für dieses Gerät
  schon `WatchDevice` aufgerufen wurde. `ReadProperty` liefert daher immer
  aus dem Cache (AC: "returns decoded signal value from **last received**
  CAN frame"), nie durch eine Live-Anfrage — für CAN gibt es kein
  Request/Response für ein beliebiges Signal. `WatchDevice`s Rolle ist damit
  enger als bei MQTT/HTTP: sie validiert nur, dass `can.message` auf eine
  bekannte Nachricht zeigt, und registriert das Gerät für den
  `OnPropertyUpdate`-Fan-out.
- **Read-Modify-Write bei `WriteProperty`**: Ein DBC-Message-Payload trägt
  oft mehrere Signale. Ein einzelnes `WriteProperty` schreibt daher nicht
  einen leeren 8-Byte-Frame, sondern den zuletzt gesehenen Frame dieser
  Message-ID mit nur dem Ziel-Signal verändert — sonst würde jedes
  `WriteProperty` alle Geschwister-Signale im selben Frame auf 0 zurücksetzen
  (`TestAdapter_WriteProperty_PreservesSiblingSignal`). Ist das Ziel-Signal
  selbst ein `m<N>`-Signal, wird zusätzlich der Multiplexor-Selector der
  Message auf `N` gesetzt, damit der Frame für jeden Decoder selbstkonsistent
  bleibt.
- **Kein Circuit Breaker** (anders als #11/MQTT, wie #24/HTTP): Es gibt keine
  persistente Request/Response-Verbindung, die geschützt werden müsste — der
  Read-Loop drainiert den Socket unabhängig vom Ausgang eines einzelnen
  Frames weiter.
- **`recover()` pro Frame statt nur Logging danach**: `arc42.adoc`s Risk-
  Eintrag benennt explizit "CAN adapter panic brings down entire gateway
  (structured monolith)" als Risiko — ein `recover()`, das nur den ganzen
  Prozess absichert, aber den Read-Loop selbst sterben lässt, würde das Gerät
  effektiv stumm schalten. `handleFrame()` recovert daher um jeden einzelnen
  Frame herum, sodass der Loop nach einem Panic weiterläuft
  (`TestAdapter_ReadLoop_RecoversFromPanic`).
- **`Message.DecodeEach` (Callback, allokationsfrei) statt nur `Message.Decode`
  (Map) auf dem Hot Path**: Ein erstes Profiling der `<1µs`-AC zeigte, dass
  nicht die Bit-Extraktion selbst, sondern die Map-Allokation + GC-Scan von
  `Decode`s `map[string]api.PropertyValue`-Rückgabewert für ~70% der Latenz
  verantwortlich war (`mapassign_faststr`/`mallocgc` im CPU-Profil). Der
  Read-Loop (`Adapter.processFrame`) nutzt daher `DecodeEach` mit einem
  Callback; `Decode` bleibt als bequeme, Map-basierte Variante für Tests
  erhalten, ruft intern aber ebenfalls `DecodeEach` auf. Effekt:
  ~800-900ns/Frame → ~200-300ns/Frame, deutliche Sicherheitsmarge unter dem
  1µs-Budget.
- **Benchmark als echtes Gate, nicht nur Messwert**: `go test -bench` allein
  wird von nichts automatisch ausgewertet. `TestDecodeLatencyUnderOneMicrosecond`
  ruft `testing.Benchmark()` programmatisch auf und lässt `go test` fehlschlagen,
  wenn die Latenz die 1µs-AC verletzt — unter `-race` übersprungen (Skip, kein
  Fail), da der Race-Detector jeden Speicherzugriff instrumentiert und die
  gemessene Zeit um ein Vielfaches verzerrt (Standard-Idiom: Build-Tag-Datei
  `race_on_test.go`/`race_off_test.go` mit `raceDetectorEnabled`-Konstante).
- **`vcan0`-CI-Job als eigener Runner-Step, nicht als Docker-`services:`-
  Container** (anders als #11s Mosquitto-Service): Ein virtuelles CAN-
  Interface ist ein Kernel-Netdev, kein Netzwerkdienst — `sudo modprobe vcan
  && sudo ip link add … type vcan && … set up` läuft direkt auf dem
  `ubuntu-latest`-Runner (echter Linux-Kernel), analog möglich, weil TC-01
  ohnehin einen Linux-Kernel voraussetzt. Eigener Job `go-integration-can`,
  getrennt vom bestehenden `go-integration` (MQTT), da beide unterschiedliche
  Infrastruktur-Vorbereitung brauchen.
- **Linux-Build-Tag statt Ausschluss des ganzen Packages** (`socket_linux.go`
  / `socket_other.go`): TC-01 verbietet CAN auf macOS/Windows zur Laufzeit,
  nicht zur Compile-Zeit — der `canadapter`-Package muss auf einem
  Mac-Entwicklungsrechner weiter kompilierbar bleiben (`Adapter.Open` liefert
  dort schlicht `ErrLinuxOnly`), damit der Rest des Gateways dort weiter
  buildet und getestet werden kann.

## E2E-/Testabdeckung

- **DBC-Parser**: `dbc_test.go` — Messages/Signals/DLC/IDs, Feld-für-Feld-
  Verifikation eines Signals, MUX-Erkennung (`M`/`m<N>`), inkl. Extended-ID
  (0x80000000-Bit) über den ganzen Bereich (uint32, nicht int32).
- **Codec**: `codec_test.go` — Hand-verifizierte Encode/Decode-Beispiele für
  Little-Endian (unsigned skaliert, signed mit negativem Offset, inkl.
  Zwei-Komplement-Rundtrip), Big-Endian/Motorola (Bit für Bit von Hand
  durchgerechnet), MUX-Selektion in beide Richtungen, Fehlerfälle
  (unbekanntes Signal, nicht-numerischer Wert).
- **Adapter**: `adapter_test.go` mit `fakeSocket` (kein reales Interface
  nötig, analog zu `mqtt/adapter_faketransport_test.go`) — `WatchDevice`-
  Validierung, `ReadProperty` vor/nach Frame-Empfang, `WriteProperty`
  inkl. Read-Modify-Write-Erhaltung von Geschwister-Signalen, unbekannte
  Frame-IDs werden verworfen, Panic-Recovery im Read-Loop, `Close()`
  idempotent — alle unter `-race` grün.
- **`device_service.go`-Wiring**: `device_service_can_test.go` (Fake-
  Adapter, analog zu `device_service_http_test.go`) — Routing für
  `RegisterDevice`/`GetProperty`/`SetProperty` bei `transport=can`,
  Nicht-CAN-Geräte bleiben unberührt, `canStatusError`-Mapping-Tabelle.
- **Benchmark/Gate**: `codec_bench_test.go` — `TestDecodeLatencyUnderOneMicrosecond`
  (s. Design-Entscheidungen).
- **Integration**: `integration_test.go` (`//go:build integration`, Skip
  ohne `UDAL_TEST_CAN_INTERFACE`) gegen ein echtes SocketCAN-Interface
  (`vcan0` in CI) — Device-Seite wird durch einen zweiten, unabhängigen
  echten SocketCAN-Socket simuliert (kein Fake), sowohl Bus→Gateway
  (Device schreibt, Adapter liest) als auch Gateway→Bus (Adapter schreibt,
  ein frisch geöffneter Socket liest zurück).

`go build ./...`, `go vet ./...`, `gofmt -l .`, `go test -race ./...` und
`go build -tags integration ./...` sind grün. `bausteinsicht validate
--model architecture.jsonc` lief in dieser Sandbox (anders als bei #24)
erfolgreich (`go install .../bausteinsicht@latest` hatte Netzwerkzugriff) und
meldet "Model is valid."
