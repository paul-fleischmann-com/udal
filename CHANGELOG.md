# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Optional `gateway.yaml` config file (`gateway/internal/config`, req42.adoc §7.2), path
  configurable via `--config` or `UDAL_CONFIG_PATH` (default `./gateway.yaml`). A missing
  file is not an error — the gateway falls back to exactly its previous env-var-only
  defaults. Every YAML key is overridable by its own `UDAL_*` environment variable
  (`UDAL_GRPC_PORT`, `UDAL_HTTP_PORT`, `UDAL_METRICS_PORT`, plus the gateway's existing
  `UDAL_TLS_CERT`/`UDAL_TLS_KEY`/`UDAL_MTLS_CA_CERT`/`UDAL_REGISTRY_PATH`/
  `UDAL_MQTT_BROKER` for the keys that already had one, so current deployments are
  unaffected); the pre-existing flat env vars (`UDAL_GRPC_ADDR` etc.) still take priority
  over the config file if set. `metrics_port`, `auth.api_key_header`,
  `adapters.mqtt.client_id`, `adapters.http`/`adapters.can`, `heartbeat_interval` and
  `device_timeout` are parsed and overridable but not yet consumed by any running
  feature — see `docs/features/plans/41-yaml-config.md` for why. (#41)
- MQTT transport adapter (`gateway/internal/adapters/mqtt`), the first real transport
  adapter: request/response `ReadProperty`/`WriteProperty` over the topic convention
  `udal/{deviceId}/props/{path}[/get|/set|/set/ack]` (configurable timeout, default 5s),
  unsolicited property publishes fanned out through the existing `Broker` (`Subscribe`
  RPC), MQTT v5 (via `paho.golang`/`autopaho`, reconnect with exponential backoff 1s-60s)
  with automatic fallback to v3.1.1 (`paho.mqtt.golang`) if the broker rejects v5's
  CONNECT specifically over protocol version, and a circuit breaker (5 consecutive
  errors → open for 30s, then a half-open probe). `DeviceService.GetProperty`/
  `SetProperty` now branch on `Device.Transport`: `mqtt` devices route through the
  adapter, everything else keeps using the in-memory `PropertyStore` unchanged.
  Gateway-side, opt-in via `UDAL_MQTT_BROKER` (unset: no adapter, current behavior for
  all transports). SendCommand-over-MQTT isn't wired up yet — no acceptance criterion in
  this ticket required it. (#11)
- Go SDK (`code/sdk/go`, module `github.com/paulefl/udal/code/sdk/go`): device side
  (`NewDevice`/`Run`/`PublishProperty`/`OnCommand`, auto-reconnect with backoff) and
  application side (`NewClient`/`GetProperty`/`WriteProperty`/`SendCommand`/`Subscribe`),
  per req42.adoc §7.3. (#12)
- `StreamCommands` gRPC (bidi streaming): lets a directly gRPC-connected device (no
  transport adapter) receive commands dispatched via `SendCommand`, routed through a new
  `CommandRouter` (`gateway/internal/api`). (#12)
- `RegisterDeviceRequest.id` (optional): a device can now register with a caller-chosen,
  stable ID instead of always getting a server-generated one. (#12)
- Auth middleware for the gRPC/REST API: API-Key (`X-API-Key`, bcrypt-hashed,
  `UDAL_BOOTSTRAP_API_KEY` for initial provisioning), mTLS client certificates
  (`UDAL_MTLS_CA_CERT`/`UDAL_MTLS_REQUIRED`, CN becomes a device identity), and OAuth2
  JWT Bearer tokens (`UDAL_JWT_JWKS_URL`/`UDAL_JWT_AUDIENCE`/`UDAL_JWT_ISSUER`) — first
  match wins in that order. Every RPC is now authorized against the role x operation
  RBAC matrix, with per-device ACL entries (`Registry.UpdateACL`) able to override the
  RBAC decision in either direction. No management API/CLI for keys or ACLs yet — see
  the plan doc for this issue. (#9)
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
