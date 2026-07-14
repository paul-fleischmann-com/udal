# Plan: #7 — Protobuf API definitions (gRPC + OpenAPI)

## Ausgangslage

Issue #7 überschneidet sich stark mit dem bereits per PR #13 gemergten Issue #2
("Component: Unified Device API"). Nach Merge von PR #13 auf `main` existiert bereits:

- `api/proto/udal/v1/device.proto` — vollständige `DeviceService` (RegisterDevice, GetDevice,
  ListDevices, DeleteDevice, GetProperty, SetProperty, SendCommand, Subscribe streaming)
- `buf.yaml` / `buf.gen.yaml` — buf v2 Setup mit Remote-Plugins (protoc-gen-go, grpc-go,
  grpc-gateway, openapiv2)
- `api/gen/go/udal/v1/*.pb.go` — eingecheckter generierter Go-Code
- `api/openapi/udal/v1/device.swagger.json` — generierte Spec, aber **Swagger 2.0**, keine OpenAPI v3
- `buf breaking` bereits in `.github/workflows/ci.yml` (`proto-ci` Job) verankert
- `PropertyValue` oneof deckt bool/int64/double/string/bytes ab; `google.protobuf.Value`
  (json_val) deckt strukturierte Werte und `null` ab

## Abgleich mit Acceptance Criteria (Issue #7)

| AC | Status | Anmerkung |
|----|--------|-----------|
| Proto compiles mit protoc + grpc-gateway | ✅ erfüllt | `proto-ci` Job führt `buf generate` aus |
| Generierter Go-Code eingecheckt in `api/proto/gen/` | ✅ erfüllt (abweichender Pfad `api/gen/go/`) | Pfad ist Projektkonvention aus PR #13 / CONTRIBUTING.md — nicht ändern, nur im PR dokumentieren |
| OpenAPI v3 Spec generiert und validiert | ❌ **Lücke** | aktuell nur Swagger 2.0 — muss ergänzt werden |
| `buf breaking` in CI | ✅ erfüllt | bereits vorhanden |
| Value type: bool, int64, float64, string, bytes, null | ✅ erfüllt | `PropertyValue` oneof + `google.protobuf.Value` |

Die einzige echte Lücke ist die fehlende OpenAPI **v3** Spec samt Validierung in CI.
Der Rest von Issue #7 ist bereits durch PR #13 abgedeckt.

## Phasen

### Phase 1 — OpenAPI v3 Generierung
- Swagger 2.0 → OpenAPI 3.0 Konvertierung als Build-Schritt ergänzen (`swagger2openapi` via npx,
  deterministisch, keine zusätzliche buf-Remote-Plugin-Abhängigkeit nötig)
- Makefile-Target `generate` erweitern: nach `buf generate` automatisch
  `api/openapi/udal/v1/device.openapi.v3.json` erzeugen
- Generierte v3-Datei einchecken

### Phase 2 — Validierung in CI
- `proto-ci` Job in `.github/workflows/ci.yml` um einen Schritt "Validate OpenAPI v3" erweitern
  (z. B. `npx --yes @redocly/cli lint`)
- Sicherstellen, dass der Schritt bei ungültiger Spec fehlschlägt

### Phase 3 — Dokumentation
- CONTRIBUTING.md / arc42 kurz ergänzen: OpenAPI v3 Datei-Pfad und Regenerierungs-Workflow
- Issue-AC-Tabelle im PR-Description dokumentieren (inkl. Begründung für abweichenden Go-Gen-Pfad)

## Risiken / offene Punkte

- `swagger2openapi` deckt i. d. R. alle Swagger-2.0-Konstrukte ab, die grpc-gateway erzeugt;
  falls Konvertierung Informationen verliert, alternativ auf einen proto-nativen OpenAPI-v3-Generator
  (z. B. `google/gnostic` `protoc-gen-openapi`) wechseln — höherer Umbauaufwand, daher nur Fallback.
