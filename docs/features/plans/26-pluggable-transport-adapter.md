# Plan: #26 — Pluggable Transport Adapter Interface

## Ausgangslage

F-12/QR-09. `docs/arc42/arc42.adoc` hatte bereits an mehreren Stellen
(§4.2 "Open extension"-Entscheidung, §5.2s Komponentenliste, QS-06) die
Architektur-Absicht dokumentiert, bevor dieses Ticket sie umgesetzt hat:
"Transport interface as public API seam", "Third-party adapters can be
compiled in without forking the gateway". Die drei eingebauten Adapter
(mqtt/http/can, issues #11/#24/#25) hatten bislang aber je eine eigene,
schmale Interface-Definition direkt in `internal/service/device_service.go`
(`MQTTAdapter`/`HTTPAdapter`/`CANAdapter`), nicht öffentlich und nicht
untereinander vereinheitlicht — mit unterschiedlichen Methodensignaturen
(MQTT nimmt `deviceID string`, HTTP/CAN nehmen das volle `api.Device`).

## Design-Entscheidungen

- **`Transport`-Interface vereinheitlicht, ersetzt aber die drei
  eingebauten Adapter-Interfaces nicht**: `internal/adapter.Transport`
  (`ReadProperty`/`WriteProperty`/`WatchDevice`, alle mit vollem
  `api.Device`) ist ein *zusätzlicher* Dispatch-Pfad in
  `GetProperty`/`SetProperty`, neben den bestehenden mqtt/http/can-Zweigen,
  nicht deren Ersatz. Ein Refactoring der drei konkreten Adapter-Pakete
  (`internal/adapters/mqtt` müsste seine `ReadProperty`/`WriteProperty`/
  `WatchDevice`-Signaturen von `deviceID string` auf `api.Device` umstellen,
  um `Transport` direkt zu implementieren) wäre ein großer, risikoreicher
  Diff über drei Pakete plus deren Tests gewesen, ohne dass F-12s
  Akzeptanzkriterien das verlangen — bewusst nicht Teil dieses Tickets.
- **`code/gateway/internal/adapter/transport.go`** — exakter Pfad aus der
  Issue-Beschreibung übernommen. Lesetransporte (kein Schreibpfad) geben
  `adapter.ErrWriteNotSupported` aus `WriteProperty` zurück statt die
  Methode wegzulassen, damit `Transport` für jeden Aufrufer dieselbe
  Drei-Methoden-Form hat; `DeviceService` bildet den Sentinel-Fehler auf
  `codes.Unimplemented` ab, analog zu `HTTPAdapter`s bestehendem
  Unimplemented-Sonderfall für SetProperty.
- **Compiled-in Registration statt `.so`-Plugin**: F-12s dritte AC bietet
  "binary plugin **or** compiled-in registration" als Alternativen an.
  Ein `plugin.Open(".so")`-Loader wurde erwogen und verworfen: Go-Plugins
  sind Linux-only, verlangen exakt übereinstimmende Toolchain-/Build-Flags
  zwischen Host und Plugin, und würden QR-07s "Single binary"-Ziel
  unterlaufen (ein separat verteiltes, ABI-brüchiges Artefakt pro Adapter).
  Stattdessen: `adapter.Register(name, transport)` — vom Adapter-Paket
  selbst in seinem `init()` aufgerufen — plus `adapter.Lookup(name)` in
  `main.go`. Der einzige Integrationspunkt für einen neuen Drittanbieter-
  Adapter ist ein Blank-Import in `cmd/gateway/main.go` — dieselbe
  Ein-Zeilen-Integration, die jedes Go-Programm mit diesem
  Registrierungs-Idiom braucht (z. B. `database/sql`-Treiber); das ist
  bewusst als das akzeptierte "keine Änderung an Kern-Paketen"-Verständnis
  dokumentiert (siehe req42.adoc F-12s "Registration Convention"), nicht
  stillschweigend als stärkere Garantie behauptet, als tatsächlich
  eingehalten wird.
- **Beispiel-Adapter lebt unter `code/gateway/`, nicht unter
  `code/adapters/`** (wie ursprünglich in der Issue-Beschreibung
  angedeutet): Go's Internal-Package-Sichtbarkeitsregel erlaubt den Import
  von `.../internal/...` nur für Code, der unterhalb desselben
  Eltern-Verzeichnisses von `internal` verwurzelt ist — unabhängig von
  Modul-Grenzen. Da `Transport` laut Issue-AC zwingend unter
  `internal/adapter/transport.go` liegen muss, kann ein Paket außerhalb von
  `code/gateway/` es gar nicht importieren; ein erster Versuch, das
  Beispiel unter `code/adapters/community/echo/` zu platzieren, scheiterte
  entsprechend beim Build und wurde nach `code/gateway/examples/adapters/
  echo/` verschoben. Das deckt sich mit QR-09s eigenem Stimulus-Text
  ("Contributor adds J1939 adapter") — Pluggability für einen Mitwirkenden
  an diesem Repository, nicht für ein echtes, separat versioniertes
  externes Modul.
- **`gateway.yaml`s `adapters.custom` (Liste von Namen) statt eines
  einzelnen `type: custom`-Feldes**: die Issue-Beschreibung nennt `type:
  custom` im Singular, aber mehrere registrierte Third-Party-Adapter
  gleichzeitig zu aktivieren ist die naheliegendere Erweiterung, und
  passt zum bereits etablierten Muster (`adapters.mqtt`/`http`/`can` sind
  je durch einen optionalen Konfigurationswert gated). Aktivierung ist rein
  konfigurationsgetrieben — der registrierte Name selbst *ist* der Wert,
  den ein Gerät in seinem `transport`-Feld trägt (z. B. `"echo"`), nicht das
  Literal `"custom"`.
- **Kein Callback-Mechanismus für asynchrone Property-Updates in
  `Transport`** (anders als die eingebauten Adapter, die eine
  `OnPropertyUpdate`-artige Callback-Funktion bei Konstruktion übergeben
  bekommen, um MQTT-Publishes/CAN-Frames asynchron in den `Broker` zu
  fanout-en): `SetProperty`s custom-Zweig published stattdessen direkt an
  `s.broker` nach einem erfolgreichen `WriteProperty`, genau wie der
  `PropertyStore`-Fallback. Das deckt den synchronen Schreibpfad zuverlässig
  ab, aber ein Custom-Transport kann (in dieser Iteration) keine
  eigeninitiierten, asynchronen Werteänderungen an `Subscribe`-Abonnenten
  fanout-en — ehrlich als bekannte Grenze dokumentiert statt stillschweigend
  offen gelassen, da F-12s AC weder ein solches Fanout noch überhaupt
  Subscribe für Custom-Transports verlangt.
- **`internal/adapter/adaptertest`**: F-12s zweite AC verlangt explizit
  "passes the common adapter test suite" — `adaptertest.Run(t,
  newTransport)` prüft `Name()` nicht-leer, `WatchDevice` fehlerfrei für
  ein einfaches Gerät, und dass `WriteProperty` gefolgt von `ReadProperty`
  entweder den geschriebenen Wert zurückgibt oder `WriteProperty`
  `ErrWriteNotSupported` liefert (für reine Lese-Transports).

## Testabdeckung

- **`internal/adapter`**: `registry_test.go` (Register/Lookup, unbekannter
  Name, doppelte Registrierung panict).
- **`code/gateway/examples/adapters/echo`**: `TestEcho_ConformsToTransport`
  (nutzt `adaptertest.Run`), `TestEcho_RegistersItselfUnderItsName`
  (verifiziert, dass der Blank-Import-Registrierungspfad tatsächlich
  funktioniert).
- **`internal/service`**: `device_service_custom_test.go` — Routing zu
  einem Custom-Transport für `RegisterDevice`(→`WatchDevice`)/
  `GetProperty`/`SetProperty`, Fehler-Mapping (`ErrWriteNotSupported` →
  `Unimplemented`, unbekannter Fehler → `Internal`), und dass ein Gerät mit
  einem *nicht* aktivierten Transport-Namen weiterhin zum
  `PropertyStore`-Fallback geht statt fälschlich zu einem unabhängig
  konfigurierten Custom-Transport zu routen.
- **Manueller End-to-End-Smoke-Test** (kein automatisierter Test, da er
  einen echten Prozessstart braucht): Gateway mit `UDAL_DEV_INSECURE=true`
  und `UDAL_CUSTOM_ADAPTERS=echo` gestartet → Log-Zeile "custom adapters
  activated" mit `names: ["echo"]`; ein Gerät mit `transport: "echo"`
  registriert; `SetProperty`/`GetProperty` per REST → der geschriebene Wert
  (`19.9`) kommt unverändert zurück, bestätigt den kompletten Pfad REST →
  gRPC → `DeviceService` → registrierten `echo`-Transport.

`go build ./...`, `go vet ./...`, `gofmt -l .` und `go test -race ./...`
sind grün.
