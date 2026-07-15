# Plan: #10 — Device Registry (bbolt embedded)

## Ausgangslage

`gateway/internal/registry/registry.go` definiert bereits das `Registry`-Interface
(Register/Get/List/Delete/UpdateStatus) und eine `MemoryRegistry`-Implementierung
(In-Memory, thread-safe via `sync.RWMutex`). Der Package-Kommentar behauptet bereits
"bbolt-backed", das existiert aber nicht — `main.go` verwendet ausschließlich
`registry.NewMemoryRegistry()`. Registrierte Geräte gehen beim Neustart verloren.

## Abgleich mit Acceptance Criteria (Issue #10)

| AC | Status | Anmerkung |
|----|--------|-----------|
| Register → Get roundtrip preserves all fields | ✅ (Memory) / ❌ (bbolt fehlt) | gilt es auf die neue bbolt-Implementierung zu übertragen |
| List mit Filter nach transport/tag/**online** | ❌ **Lücke** | `List(capability, transport string)` unterstützt weder Tag- noch Online-Filter |
| Entries survive gateway restart | ❌ **Lücke** | Kernstück dieses Tickets — bbolt-Persistenz |
| Concurrent access passes `go test -race` | ⚠️ nur für Memory geprüft | neue bbolt-Implementierung muss ebenfalls race-frei sein |
| DB path configurable; default `./udal-registry.db` | ❌ **Lücke** | kein Config-Mechanismus vorhanden; `.gitignore` hat aber schon einen Eintrag für `udal-registry.db` |

## Scope-Entscheidung: kein neues `TransportConfig`-Feld

Die Issue-Beschreibung nennt "schema reference, transport config, tags" als zu
speichernde Felder. `transport_config` wird zwar in `RegisterDeviceRequest` (proto)
mitgeschickt, aber von `device_service.go` schon heute verworfen, und die `Device`-Proto-
Message hat gar kein Feld, um es zurückzugeben. Ein neues, nur intern gespeichertes und
nie auslesbares Feld wäre totes Gewicht — das gehört in den Scope von #8/#9, sobald die
API das exponieren soll. "Tags" wird als das bereits vorhandene `Labels`-Feld
interpretiert (Tag-Filter = Prüfung auf Vorhandensein eines Label-Keys), keine
Datenmodell-Duplikation.

## Phasen

### Phase 1 — Registry-Interface um Tag-/Online-Filter erweitern (Memory)
- `ListFilter{Capability, Transport, Tag string; Online *bool}` einführen
- `Registry.List(filter ListFilter)` statt `List(capability, transport string)`
- `MemoryRegistry` entsprechend anpassen, Tests erweitern (Tag-Filter = Label-Key
  vorhanden; Online-Filter = `Status == DeviceStatusOnline`)
- Aufrufstelle in `device_service.go` (`ListDevices`) anpassen (aktuell nur
  capability/transport aus dem proto Request — Tag/Online bleiben leer/nil, unverändertes
  Verhalten, da `ListDevicesRequest` proto diese Felder noch nicht hat)

### Phase 2 — BboltRegistry
- Neue Datei `gateway/internal/registry/bbolt.go`: `BboltRegistry` implementiert
  `Registry` via `go.etcd.io/bbolt`, ein Bucket `devices`, JSON-codierte `api.Device`-Werte
- `NewBboltRegistry(path string) (*BboltRegistry, error)` + `Close() error`
- Restart-Test: DB schließen, neu öffnen, Daten noch da
- `-race`-Test für parallele Register/Get/List-Zugriffe

### Phase 3 — Wiring in main.go
- `UDAL_REGISTRY_PATH` env var, Default `./udal-registry.db` (passend zum bestehenden
  `.gitignore`-Eintrag)
- `main.go`: `registry.NewBboltRegistry(...)` statt `NewMemoryRegistry()`, Fehlerbehandlung
  beim Öffnen, `Close()` beim Shutdown
- `gateway/go.mod`: Abhängigkeit `go.etcd.io/bbolt` ergänzen

### Phase 4 — Doku + Verifikation
- CONTRIBUTING.md / req42.adoc falls nötig (Registry-Pfad ist dort schon als
  `/var/udal/registry.db`-Beispiel in `gateway.yaml` dokumentiert — Default hier bewusst
  `./udal-registry.db` gemäß Issue-AC, kein Widerspruch da Beispiel-Config einen anderen
  Pfad *konfigurieren* kann)
- `go test -race ./gateway/...` grün, manueller Restart-Test, CHANGELOG-Eintrag
