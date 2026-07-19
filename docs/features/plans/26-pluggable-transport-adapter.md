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

## Nachgezogen aus dem Review (vor PR-Eröffnung)

Ein High-Effort-Multi-Agent-Review (8 Finder-Winkel + Verifikation) lief
gegen den vollständigen Diff. Vier Findings wurden behoben, der Rest
(vier weitere parallele Dispatch-Wege statt einer echten Vereinheitlichung
von mqtt/http/can auf `Transport`, unbedingter Blank-Import des
`echo`-Beispiels in jedes Produktions-Binary, `Register`s Panic-bei-
Duplikat als harter, unabgefangener Prozessabsturz vor `main()`) bewusst
als dokumentierte, bereits im Design-Abschnitt oben begründete Trade-offs
belassen:

- **`ApplyEnv()` überging `UDAL_CUSTOM_ADAPTERS`** (ein Review-Winkel
  fand das): jedes andere `adapters.*`-Feld hat eine passende
  `overrideString`/`overrideInt`/`overrideDuration`-Zeile in `ApplyEnv()`,
  passend zu dessen eigenem Doc-Kommentar-Versprechen ("overrides every
  Config field from its documented UDAL_* environment variable"); `Custom
  []string` wurde stattdessen nur ad hoc in `main.go` per
  `strings.Join`+`strings.Split`-Umweg um das string-only
  `config.ResolveString` herum aufgelöst — ein Bruch dieses Versprechens,
  von `TestApplyEnv_OverridesEverySettableField` nicht erkannt, da der
  Test eine feste Env-Var-Liste prüft, nicht Reflection-basiert über alle
  Felder geht. Fix: ein neuer `overrideStringSlice`-Helper in `ApplyEnv()`
  (Komma-getrennt, whitespace-getrimmt, leere Segmente verworfen);
  `main.go` liest jetzt einfach `cfg.Gateway.Adapters.Custom` direkt —
  behebt gleichzeitig einen zweiten, unabhängig gefundenen
  Simplification-Finding (den überflüssigen Join-dann-Split-Umweg).
- **Custom-Transport unter einem reservierten Namen ("mqtt"/"http"/"can")
  konnte ein Gerät doppelt `WatchDevice`n** (zwei Review-Winkel fanden das
  unabhängig): `RegisterDevice`s vier `if`-Blöcke für
  mqtt/http/can/custom waren unabhängig voneinander, nicht
  gegenseitig exklusiv wie `GetProperty`/`SetProperty`s eigene
  Switch/If-Kette es für denselben Fall bereits war (dort gewinnt der
  eingebaute Adapter einfach durch Case-Reihenfolge, `custom` wird nie
  erreicht) — ein Gerät mit `transport: "mqtt"` hätte bei einer
  Namenskollision sowohl den eingebauten MQTT-Adapter als auch den
  kollidierenden Custom-Transport beim Registrieren beobachtet. Fix:
  `RegisterDevice`s vier `if`s zu einem `switch` mit `default`-Fall
  umgebaut, exakt spiegelbildlich zu `GetProperty`/`SetProperty`s
  Struktur; zusätzlich lehnt `main.go` einen Custom-Adapter mit
  reserviertem Namen jetzt beim Start klar ab (`os.Exit(1)`, statt
  still zu shadowen) — verifiziert per manuellem Smoke-Test
  (`UDAL_CUSTOM_ADAPTERS=mqtt` → Prozess beendet sich mit Exit-Code 1 und
  klarer Fehlermeldung).
- **`adapter.Register(name, nil)` hätte erst später, mitten in einem
  Request, mit einem Nil-Interface-Panic gecrasht** statt sofort bei der
  Registrierung selbst: `Register` prüft jetzt explizit auf `t == nil` und
  panict dort mit einer klaren, den Transport-Namen nennenden Meldung —
  fail-fast am eigentlichen Fehlerort (dem fehlerhaften `init()` eines
  Adapter-Pakets), nicht erst beim ersten `ReadProperty`/`WriteProperty`-
  Aufruf für ein zufällig passendes Gerät.
- **`customStatusError` bildete jeden nicht erkannten Fehler eines
  Drittanbieter-Transports pauschal auf `codes.Internal` ab**, im
  Gegensatz zu den eingebauten Adaptern, die je eigene, präzise
  Status-Zuordnungen haben (ein Review-Winkel fand das): ein
  Drittanbieter-Adapter hatte keine Möglichkeit, für eine harmlose
  "Property nicht gefunden"-Situation `codes.NotFound` statt eines
  alarmierenden `codes.Internal` auszulösen. Fix: zwei neue,
  `errors.Is`-kompatible Sentinel-Fehler `adapter.ErrNotFound`/
  `adapter.ErrInvalidArgument`, die ein Transport optional zurückgeben
  kann, um dieselbe präzise Status-Zuordnung zu bekommen wie die
  eingebauten Adapter — ohne Sentinel funktioniert ein Transport weiterhin
  unverändert, nur mit dem gröberen `Internal`-Default.

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
