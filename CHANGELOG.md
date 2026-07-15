# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Mandatory TLS for the gateway's gRPC and REST listeners, configured via
  `UDAL_TLS_CERT`/`UDAL_TLS_KEY`; explicit `UDAL_DEV_INSECURE=true` opt-out for local
  development. (#8)
- `Subscribe` RPC: a new fan-out `Broker` (`gateway/internal/api`) delivers live
  `PropertyUpdate` events to any number of subscribers per device, published from
  `SetProperty`. (#8)
- Persistent, bbolt-backed device registry (`gateway/internal/registry.BboltRegistry`),
  replacing the in-memory-only registry as the gateway's default. Configurable via
  `UDAL_REGISTRY_PATH` (default `./udal-registry.db`). (#10)
- `Registry.List` now supports filtering by tag (label presence) and online status,
  in addition to capability and transport. (#10)
- OpenAPI v3 spec for the Device API, generated from the existing Swagger 2.0 output
  via `swagger2openapi` and validated in CI (`redocly.yaml`, `make generate-openapi-v3`,
  `make validate-openapi-v3`). (#7)

### Changed

- Moved `gateway/`, `api/` (and the future `adapters/`, `sdk/`) under a new top-level
  `code/` directory, per the updated Repository Structure in `README.adoc`. Go module
  paths, `buf.yaml`/`buf.gen.yaml`, `go.work`, the Dockerfile, CI path filters/commands,
  and doc references all updated accordingly.
- Moved generated Go protobuf/gRPC stubs from `api/gen/go/` to `api/proto/gen/` (later
  `code/api/proto/gen/`, see above) to match the documented API layout; updated
  `buf.gen.yaml`, `go.work`, and gateway imports accordingly. (#7)
