# Plan: #22 — Capability Registry service (persistence, versioning, validation)

## Ausgangslage

F-13/F-14/F-15. Die JSON-Schema-Meta-Schema und Beispiele existieren bereits
(`schema/udal-capability.schema.json`, `schema/examples/*.json`, #3) und
werden in CI per Python (`check-jsonschema`) gegen die Beispiele validiert
(Job `schema-validate`). Dieses Ticket ist der **Laufzeit-Service**: Schemas
im Gateway selbst speichern/versionieren/servieren, und bei
`RegisterDevice`/`SetProperty` durchsetzen.

## Design-Entscheidungen

- **Neuer `CapabilityService` (neues Proto-File `capability.proto`, gleiches
  `udal.v1`-Package)**: Die AC nennen explizit gRPC-Status-Codes
  (`ALREADY_EXISTS`, `INVALID_ARGUMENT`, `NOT_FOUND`) für Publish/Get —
  das impliziert eine echte RPC, keine rein interne Go-API. `PublishSchema`/
  `GetSchema`/`ListSchemas`, REST via grpc-gateway wie bei `DeviceService`.
  Das CLI selbst (`udal schema publish/get/list`) ist #23, nicht Teil
  dieses Tickets — aber die RPCs, die das CLI später aufruft, schon.
- **Meta-Schema-Validierung**: `github.com/santhosh-tekuri/jsonschema/v5`
  (unterstützt Draft 2020-12, validiert Schemas gegen ein Meta-Schema,
  thread-safe). Recherchiert per `go doc` gegen das echte heruntergeladene
  Modul, nicht aus dem Gedächtnis.
- **Meta-Schema-Datei muss embedded werden, nicht zur Laufzeit gelesen
  werden**: Das finale Docker-Image (`deployments/docker/Dockerfile`) ist
  `distroless/static` und enthält NUR die kompilierte Binary — kein
  `schema/`-Verzeichnis. `go:embed` kann aber nicht aus dem Gateway-Modul
  (`code/gateway`) heraus auf `schema/udal-capability.schema.json` am
  Repo-Root zugreifen (embed-Pattern dürfen nicht über `..` aus dem
  Package-Verzeichnis hinaus). Lösung: eine Kopie unter
  `code/gateway/internal/capability/metaschema/udal-capability.schema.json`,
  eingebettet; ein Test liest **beide** Dateien (Test-Code darf beliebige
  relative Pfade lesen, embed nicht) und schlägt fehl, wenn sie
  auseinanderlaufen — verhindert stillen Drift zwischen der kanonischen
  Datei und der eingebetteten Kopie.
- **Referenzformat `name@version`**: passend zu F-13s "retrievable by
  `name@version`". `Device.Capability` wird, sofern eine
  `CapabilityRegistry` konfiguriert ist, als `name@version`-Referenz
  interpretiert.
- **Optionale, abschaltbare Integration in `DeviceService`** (gleiches
  Muster wie `MQTTAdapter`/`PresenceMonitor` aus #11/#42): ein neues
  `CapabilityRegistry`-Interface + `SetCapabilityRegistry`-Setter. Ohne
  Konfiguration verhält sich `RegisterDevice`/`SetProperty` exakt wie
  bisher — kein Breaking Change für die vielen bestehenden Tests, die
  Geräte mit beliebigen `Capability`-Strings ohne je ein Schema zu
  publizieren registrieren. Erst wenn `main.go` die Registry verdrahtet,
  greift die Validierung (F-14/F-15) tatsächlich.
- **Semver-Breaking-Change-Warnung** (beide AC-Listen erwähnen das): beim
  Publish einer neuen Version eines existierenden Namens wird die
  bisher aktuellste Version verglichen; entfernte Properties/Commands
  oder ein geänderter `type` gelten als "breaking" → `slog.Warn`, aber
  **kein Fehler** (Publish wird trotzdem angenommen — die AC sagt "warn",
  nicht "reject"). Kein vollständiger, allgemeiner Schema-Diff — ein
  pragmatischer, nützlicher Teilcheck.
- **Property-Validierung (F-15)** nutzt die geparsten `PropertyDef`s aus
  dem Schema: Typ-Match (bool/int/float/string/bytes/enum), Range
  (min/max) für numerische Typen, Enum-Mitgliedschaft für `enum`-Typen.

## Phasen

### Phase 1 — Proto
- `capability.proto`: `CapabilitySchema`-Message,
  `PublishSchema`/`GetSchema`/`ListSchemas` RPCs + Messages, neuer
  `CapabilityService`
- `buf generate`, `buf lint`, `buf breaking` (gegen main)

### Phase 2 — `internal/capability`: Domänentyp + Meta-Schema-Validierung
- `Schema`-Struct (Name, Version, Raw, geparste Properties/Commands/Events)
- Eingebettete Meta-Schema-Kopie + Drift-Test gegen die kanonische Datei
- `ValidateAgainstMetaSchema([]byte) error`
- Unit-Tests: alle `schema/examples/*.json` müssen valide sein (Regressions-
  schutz, teilt sich die Beispiele mit dem bestehenden Python-CI-Job)

### Phase 3 — Registry (Memory + Bbolt)
- `Registry`-Interface: `Publish`/`Get`/`List`
- `MemoryRegistry` (Tests), `BboltRegistry` (teilt sich die DB-Datei über
  `reg.DB()`, gleiches Muster wie `auth.NewAPIKeyStore`)
- Duplicate-`name@version`-Erkennung, Semver-Breaking-Change-Warnung
- Unit-Tests

### Phase 4 — Property-Validierung (F-15)
- `ValidateProperty(schema Schema, path string, v api.PropertyValue) error`
- Unit-Tests: Typ-Fehlpassung, Enum außerhalb der erlaubten Werte, Range-
  Verletzung, gültiger Wert durchgelassen

### Phase 5 — `CapabilityService`-gRPC-Handler + `DeviceService`-Integration
- Neuer Service-Handler (Publish/Get/List → richtige Status-Codes)
- `DeviceService`: `CapabilityRegistry`-Interface + `SetCapabilityRegistry`
  (optional, siehe Design-Entscheidung); `RegisterDevice` prüft Schema-
  Referenz (F-14), `SetProperty` validiert den Wert (F-15)
- Unit-Tests (inkl. "ohne konfigurierte Registry unverändertes Verhalten")

### Phase 6 — `main.go`-Wiring
- Bbolt-Capability-Registry aus der geteilten DB, gRPC-Service
  registrieren, in `DeviceService` verdrahten

### Phase 7 — Tests + manuelle Verifikation
- Volle Suite, `golangci-lint`, manuelle Verifikation gegen den echten
  Gateway-Prozess (Publish, Duplicate, invalides Schema, RegisterDevice
  mit/ohne bekanntes Schema, SetProperty Typ-/Enum-/Range-Verletzung)

### Phase 8 — Doku + Changelog
