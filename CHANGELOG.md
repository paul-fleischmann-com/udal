# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Reflex reference dashboard (`dashboard`, issue #19): device list (polled, with
  online/offline status), a property browser (read/write a named property — the
  gateway has no "list properties" operation, so this needs the path already known),
  command dispatch (JSON-encoded params), and live telemetry via `Client.subscribe`
  (a genuine server push, updating the UI with no page reload) — all built on the
  Python SDK (#18). Gateway connection is env-var configured
  (`UDAL_GATEWAY_URL`/`UDAL_API_KEY`), no login form, matching this being a reference
  demonstrator rather than a production admin tool. `ruff`/`mypy --strict` pass, with
  two narrowly-scoped, documented exceptions for Reflex's own typing gaps
  (`no-any-return` on component-builder functions; `operator`/`misc` on parametrized
  event handlers) — `dashboard/dashboard/state.py`, the actual business logic, has no
  such exception. `reflex export --no-zip` succeeds, producing a working production
  build. The Python SDK gained `Client.list_devices`/`get_device` (and a `DeviceInfo`
  type) to support this — not part of req42.adoc §7.3's minimum SDK contract, added
  because the dashboard (and any real device-listing UI) needs it and the gateway
  already exposes the underlying RPCs. (#19)
- Python client SDK (req42.adoc §7.3, `code/sdk/python`): asyncio-based, application
  (`Client`) and device (`Device`) side, mirroring the Go SDK's operation set —
  `get_property`/`write_property`/`send_command`/`subscribe` (async iterator) on the
  application side, `run`/`publish_property`/`on_command` (registration + a
  StreamCommands-backed command loop with 1s–30s exponential-backoff reconnect,
  mirroring the Go SDK's `Device.Run`) on the device side. Every failing operation
  raises `UdalError(code, message)` wrapping the gateway's gRPC status, per spec.
  gRPC/protobuf stubs are generated from `code/api/proto/udal/v1/*.proto` via
  `grpcio-tools` (checked in under `src/udal/v1/`, `# do not edit manually` like the Go
  stubs) rather than buf's remote Python plugins, which need network access to
  buf.build at generation time. `ruff`/`mypy --strict`/`pytest` all pass (93% coverage,
  ≥80% required, verified in a clean venv); verified manually against a running
  gateway: device registration, `publish_property`→`get_property` round-trip,
  `write_property`, `send_command` dispatched through a real command handler, and
  `subscribe` receiving a live property update all worked end-to-end. `run()`
  reconnects on any command-stream disconnect, including a clean server-initiated
  close (e.g. graceful gateway shutdown), not just an outright failure; `subscribe()`
  cancels its underlying gRPC call if the caller stops iterating early. (#18)
- Pluggable transport adapter interface (F-12/QR-09, `code/gateway/internal/adapter`):
  a new public `Transport` interface (`ReadProperty`/`WriteProperty`/`WatchDevice`)
  unifies the three built-in MQTT/HTTP/CAN adapters' operations behind one
  contract, so `DeviceService.GetProperty`/`SetProperty` can dispatch to a
  third-party adapter through the same code path used for the built-ins.
  Third-party adapters register themselves via `adapter.Register(name, transport)`
  in their own `init()`; `gateway.yaml`'s new `adapters.custom` list (or
  `UDAL_CUSTOM_ADAPTERS`, comma-separated) activates a registered transport by
  name for a given gateway process — a device whose `transport` matches an
  activated name routes through it, watched at `RegisterDevice` time and at
  startup for already-registered devices, exactly like the built-ins — a
  custom adapter can't be activated under a reserved built-in name
  (`mqtt`/`http`/`can`), which the gateway now rejects at startup rather than
  silently shadowing one or the other. Read-only transports return the new
  `adapter.ErrWriteNotSupported` from `WriteProperty` (mapped to
  `Unimplemented`) instead of omitting the method; `adapter.ErrNotFound`/
  `adapter.ErrInvalidArgument` let a third-party adapter opt into the same
  precise `NotFound`/`InvalidArgument` status mapping the built-ins have,
  instead of every unrecognized error defaulting to `Internal`. A Go-native
  `plugin.Open(".so")` loader was considered and not built — Linux-only,
  requires an exact host/plugin toolchain match, and works against the
  single-binary portability goal. `internal/adapter/adaptertest` is a shared
  conformance test suite any `Transport` implementation can run against itself;
  `code/gateway/examples/adapters/echo` is the reference third-party adapter
  (an in-memory echo, blank-imported and registered by default) proving the
  interface, registry, and conformance suite all work end-to-end — verified
  manually against a running gateway with `UDAL_CUSTOM_ADAPTERS=echo`: a
  `RegisterDevice`/`SetProperty`/`GetProperty` round-trip via REST returned the
  just-written value. (#26)
- OpenTelemetry distributed tracing (F-24, `code/gateway/internal/tracing`):
  `tracing.NewProvider` builds and globally registers a real
  `*sdktrace.TracerProvider` on startup — always, regardless of whether
  `UDAL_OTEL_ENDPOINT` is set, since the default sampler produces a real trace
  ID for every span even with no exporter attached (this is what lets #28's
  per-request `trace_id` keep working when tracing export is "disabled"; only
  the OTLP network traffic itself is conditional on the endpoint being set). A
  new `tracing.Interceptor`, running first in the gRPC interceptor chain (ahead
  of `logging.Interceptor` and `auth.Authenticator`), starts an `"api"` span
  for every request; `logging.contextHandler` now reads its real `TraceID`
  back out of the request context instead of generating its own placeholder
  one, making #28's `trace_id` field an actual OTEL span ID for the first
  time. `auth.Authenticator` wraps authentication in a sibling `"auth"` span;
  `DeviceService.GetProperty`/`SetProperty` wrap transport-adapter dispatch in
  nested `"router"`/`"adapter"` spans (only for those two RPCs, and only when
  actually routed to a transport adapter — the `PropertyStore` fallback gets a
  `"router"` span but no `"adapter"` span), using named return values so the
  `"router"` span's error status reflects every exit path (adapter dispatch,
  the `PropertyStore` fallback, and the HTTP-unsupported/encode-failure
  paths alike), not just the transport-adapter branches. `UDAL_OTEL_ENDPOINT` accepts either
  a bare `host:port` (plaintext OTLP/gRPC) or a full URL with scheme (TLS via
  `https://`). The provider is flushed via `Shutdown` during graceful
  shutdown, after the HTTP servers stop, so any spans still buffered in the
  batch processor are exported before the process exits. Verified end-to-end
  against a running gateway: `RegisterDevice`/`SetProperty`/`GetProperty` via
  REST each produced a request log line with a distinct, valid `trace_id`.
  (#29)
- Health + Prometheus metrics endpoints (F-21/F-22, `code/gateway/internal/health`
  + `code/gateway/internal/metrics`), served on the metrics listener
  (`adapters.metrics_port`/`UDAL_METRICS_PORT`, first given a real listener by #28)
  alongside `/debug/log-level`: `GET /health` returns `503 {"status":"starting"}`
  until every listener has actually bound its port (REST/webhook/metrics listeners
  now bind synchronously before startup proceeds, matching the gRPC listener, so a
  bind failure can't race a false-positive `SetReady(true)`), then `200
  {"status":"ok"}` (plus a per-adapter `"degraded"` status, still HTTP 200, for any
  adapter implementing the new `health.Reporter` interface — MQTT's circuit breaker
  being open, or CAN's read loop having stopped on a real socket error; HTTP
  doesn't implement it, having no comparable persistent-connection failure mode;
  non-`GET` requests get `405`). `GET /metrics` exposes all four required
  Prometheus collectors (`udal_devices_online` gauge,
  `udal_requests_total{operation,status}` counter,
  `udal_request_duration_seconds{operation}` histogram,
  `udal_adapter_errors_total{adapter}` counter) via `promhttp.Handler()` — a new
  `metrics.Interceptor` records the first two on every gRPC request (also covering
  REST, proxied through the same server; stream duration isn't recorded for
  long-lived streaming RPCs like `StreamCommands`, which would otherwise always
  land in the latency histogram's `+Inf` bucket), `DeviceService` increments
  adapter errors at its three transport call sites, and `heartbeat.Monitor` gained
  an optional `WithOnStatusChange` callback that re-counts online devices from the
  registry and sets the devices-online gauge to that exact value on every
  transition (self-correcting — avoids drift from a process restart or from
  `Touch`'s already-documented non-atomic race under concurrent calls for the same
  device) off its existing online/offline transition point (#42). Verified
  end-to-end against a running gateway: a real REST request produced
  `udal_requests_total{operation="ListDevices",status="Unauthenticated"}` and a
  populated duration histogram. (#27)
- Structured JSON logging (F-23, `code/gateway/internal/logging`): every log
  line is now JSON (`slog.NewJSONHandler`, `time` renamed to `timestamp` per
  spec) instead of the previous plain-text handler. `component` comes from a
  per-subsystem child logger (`mqtt_adapter`, `http_adapter`,
  `capability_registry`, `gateway.api`, `gateway` for main.go's own
  top-level messages). A new `logging.Interceptor`, running first in the
  gRPC interceptor chain (before auth, so even an auth failure gets logged
  with a trace ID), generates a per-request `trace_id` (16 random bytes
  hex-encoded — the OpenTelemetry `TraceID` format, ahead of OTEL tracing
  itself, issue #29, actually being wired in) and logs one JSON line per
  request; any handler-side log call scoped to that request's context picks
  up the same `trace_id` automatically via a context-aware handler wrapper.
  `UDAL_LOG_LEVEL` (`debug`/`info`/`warn`/`error`) sets the level a
  freshly-started gateway begins at; a new `PUT /debug/log-level` endpoint
  on the metrics listener (`adapters.metrics_port`/`UDAL_METRICS_PORT` —
  parsed since #41, wired to a real listener for the first time here) is
  the actual "without restart" mechanism, since a running process can't
  observe its own environment changing without a restart. (#28)
- CAN transport adapter (F-11, `code/gateway/internal/adapters/can`, Linux-only —
  `req42.adoc` TC-01): a shared read-loop goroutine per SocketCAN interface
  (`golang.org/x/sys/unix`, no new external dependency) decodes every incoming frame
  matching a message in a DBC file loaded at startup (hand-rolled parser: `BO_`/`SG_`,
  including multiplexed `M`/`m<N>` signals) into an in-memory cache. `GetProperty` for
  `transport=can` devices answers from that cache — never a live bus request, since CAN
  has no request/response semantics for an arbitrary signal — while `SetProperty`
  encodes the value into the message's last-seen frame (read-modify-write, so sibling
  signals sharing the payload survive) and writes it to the interface, unlike the HTTP
  adapter's read-only scope (#24). A device's DBC message comes from its `can.message`
  label (`Device.Labels`); `property_path` names a signal within it. Decode is
  allocation-free on the per-frame hot path (`Message.DecodeEach`, ~200-300ns/frame,
  comfortably under the 1µs/frame AC — profiling showed the map-returning `Decode`
  convenience wrapper's own allocation, not the bit-extraction math, was the actual
  cost). A panic while decoding one frame is recovered per-frame inside the read loop,
  not just logged after the fact, so a single malformed frame can't take down the
  gateway process. Integration-tested against a real `vcan0` interface in CI
  (`go-integration-can` job — a runner step, not a Docker `services:` container, since
  a virtual CAN interface is a kernel netdev, not a network service). (#25)
- Capability Registry CLI (F-13, `code/cli/cmd/udal`, new module in `go.work`):
  `udal schema publish <file.json>` / `get <name>@<version>` / `list [<name>]` against a
  running gateway's `CapabilityService` (#22). `publish` does no local schema validation —
  it forwards the server's exact error (`INVALID_ARGUMENT`/`ALREADY_EXISTS` messages
  included verbatim), since re-validating client-side would risk drifting from the meta-schema
  logic in #22. `get` pretty-prints the stored document as JSON. `list` sorts newest-first
  client-side, since `ListSchemas`/`capability.Registry.List` make no ordering guarantee.
  Connects via `-gateway`/`-api-key`/`-ca`/`-insecure` (env `UDAL_GATEWAY_ADDR`/`UDAL_API_KEY`/
  `UDAL_TLS_CA`/`UDAL_DEV_INSECURE`) — API-Key auth only, no CLI-side mTLS client cert or
  OAuth2/JWT support (out of scope, see the plan doc). CI's Go build/test/lint/security/
  integration jobs now also cover `code/cli/...` alongside `code/gateway/...`. (#23)
- HTTP transport adapter (F-10, `code/gateway/internal/adapters/http`): `GetProperty`
  for `transport=http` devices issues a synchronous `GET {endpoint}/properties/{path}`
  against the device's `http.endpoint` label and decodes the JSON response; a
  background poll loop (`WatchDevice`, started automatically on `RegisterDevice`, or
  at startup for pre-existing devices) periodically `GET`s a bulk `/properties`
  snapshot and fans out only the properties that actually changed, keeping
  `Subscribe` live without a broker. A device can also push a value ahead of its
  next scheduled poll via `POST {webhook_addr}/devices/{deviceId}/events` — a
  dedicated webhook receiver (`adapters.http.webhook_port`/`UDAL_HTTP_WEBHOOK_ADDR`,
  default `:8090`), separate from the client-facing REST gateway. Poll interval is
  configurable per device (`http.poll_interval` label) or gateway-wide
  (`adapters.http.poll_interval`, default 5s, #41's existing config key, now wired).
  mTLS: when `adapters.http.mtls.cert`/`.key` (or `UDAL_HTTP_MTLS_CERT`/`_KEY`) are
  set, the adapter presents that client certificate on every outbound request.
  HTTP 4xx/5xx responses map to the matching gRPC status (404→`NOT_FOUND`,
  401→`UNAUTHENTICATED`, 403→`PERMISSION_DENIED`, 408→`DEADLINE_EXCEEDED`,
  429→`RESOURCE_EXHAUSTED`, other 5xx→`UNAVAILABLE`, other 4xx→`INVALID_ARGUMENT`).
  `SetProperty` for `transport=http` devices returns `UNIMPLEMENTED` — issue #24's
  AC has no write path, and silently falling through to the in-memory
  `PropertyStore` would be invisible to every subsequent `GetProperty` (which
  always polls the adapter once one is configured), a worse footgun than a clear
  "not supported" error. No circuit breaker (unlike the MQTT adapter, #11): there's
  no persistent connection to protect, and each request already carries its own
  timeout. (#24)
- Capability Registry service (F-13/F-14/F-15): `CapabilityService` gRPC/REST API
  (`PublishSchema`/`GetSchema`/`ListSchemas`) stores, versions, and serves capability
  schemas (`code/gateway/internal/capability`), validated against the UDAL meta-schema
  on publish (`INVALID_ARGUMENT` if non-conforming, `ALREADY_EXISTS` for a duplicate
  `name@version` — schemas are immutable once published) and persisted across restarts
  (bbolt, sharing the device registry's existing database file). Publishing a new
  version of an existing schema within the same major version logs a warning if it
  looks like it removed or retyped something the previous version declared (a
  pragmatic heuristic, not exhaustive). `DeviceService` optionally enforces schemas
  against devices — opt-in via `UDAL_CAPABILITY_ENFORCEMENT` (default off, so existing
  deployments are unaffected): `RegisterDevice` rejects an unknown `name@version`
  capability reference with `NOT_FOUND`, and `SetProperty` validates values against the
  declared property type/range/enum with `INVALID_ARGUMENT`. New RBAC operations
  (`PublishSchema`/`GetSchema`/`ListSchemas`) aren't in req42.adoc's F-19 table (predates
  this service) — publish is admin/operator only, read is any non-device role, a
  documented judgment call matching the existing `DeleteDevice` precedent. (#22)
- Load/soak test harness (QR-02, `code/gateway/internal/adapters/mqtt/loadtest_test.go`,
  build tag `loadtest`): simulates N MQTT devices publishing on an interval through one
  real `Adapter` + `Broker` fan-out, then checks heap/CPU/goroutine usage against the
  requirement's thresholds. Device count/publish interval/duration are env-var
  configurable; a new manual-only (`workflow_dispatch`) CI job `go-loadtest` runs the
  literal AC configuration (1,000 devices, 10s interval, 30 min) on demand — it's
  intentionally not part of `ci-gate` since a 30-minute run is too slow to gate every
  push/PR. Verified locally with the exact AC config: 11.3 MB heap, 0.4% CPU, goroutine
  count flat throughout the full 30 minutes (2208 → 2208), clean teardown to baseline —
  see `docs/features/plans/43-load-soak-test.md` for the full results. (#43)
- Heartbeat-based device online/offline detection (F-04): a device silent for
  longer than `device_timeout` (default 90s) is automatically marked offline, and
  a `SubscribeResponse.status` event (new, additive proto field) is fanned out
  through the existing `Broker`/`Subscribe` mechanism on every online/offline
  transition. Two heartbeat sources feed it: MQTT devices' `udal/{deviceId}/status`
  topic (documented since #11, now actually consumed), and an open `StreamCommands`
  connection for direct-gRPC devices, treated as a continuous implicit heartbeat
  since that transport has no separate heartbeat message. `RegisterDevice` also
  touches presence immediately, so a device re-registering after a restart is
  online right away rather than waiting for its first heartbeat. Interval/timeout
  come from `gateway.heartbeat_interval`/`device_timeout` (#41), defaulting to
  30s/90s. (#42)
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
