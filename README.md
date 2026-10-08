# UDAL — Universal Device Abstraction Layer

[![CI](https://github.com/paul-fleischmann-com/udal/actions/workflows/ci.yml/badge.svg)](https://github.com/paul-fleischmann-com/udal/actions/workflows/ci.yml)
[![Docs](https://github.com/paul-fleischmann-com/udal/actions/workflows/docs.yml/badge.svg)](https://github.com/paul-fleischmann-com/udal/actions/workflows/docs.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/paul-fleischmann-com/udal)](https://github.com/paul-fleischmann-com/udal/releases/latest)

UDAL provides a **unified API surface for heterogeneous IoT devices** regardless of their underlying transport protocol. A single gRPC/REST endpoint replaces direct MQTT, HTTP-polling, and CAN integrations — devices and clients speak UDAL, the gateway translates.

```
App / CLI
    │  gRPC or REST
    ▼
┌─────────────┐
│ UDAL Gateway│
└──┬──┬──┬───┘
   │  │  │
  MQTT HTTP CAN
   │  │  │
Devices / Sensors
```

## Features

| | |
|---|---|
| **Device Registration** | Devices declare identity and capability schema on connect |
| **Property Read / Write** | Typed property access via a single API, any transport |
| **Command Dispatch** | Named commands with typed parameters to any device |
| **Streaming Telemetry** | Real-time property updates via server-side streaming |
| **Multi-Transport** | MQTT 3.1.1 + 5, HTTP poll + webhook, CAN (SocketCAN) |
| **Auth** | API-key, JWT, mTLS, RBAC |
| **Observability** | Prometheus metrics, structured JSON logging, OpenTelemetry tracing |
| **Single binary** | No external database required — embedded BoltDB |

## Quickstart

### Run the gateway

```bash
docker run --rm -p 50051:50051 -p 8080:8080 \
  ghcr.io/paul-fleischmann-com/udal:latest
```

Or download a pre-built binary from the [latest release](https://github.com/paul-fleischmann-com/udal/releases/latest) (Linux amd64 / arm64).

### Register a device and read a property

```python
# pip install udal-sdk
import asyncio
from udal import UDALClient

async def main():
    async with UDALClient("localhost:50051") as client:
        devices = await client.list_devices()
        print(devices)

asyncio.run(main())
```

```go
// go get github.com/paulefl/udal/code/sdk/go
import "github.com/paulefl/udal/code/sdk/go"

client, _ := udal.Dial("localhost:50051")
defer client.Close()
devices, _ := client.ListDevices(ctx)
```

```typescript
// npm install udal-sdk
import { UDALClient } from "udal-sdk";

const client = new UDALClient({ address: "http://localhost:8080" });
const devices = await client.listDevices();
```

```rust
// Cargo.toml: udal-sdk = "0.1"
use udal_sdk::grpc::UDALClient;

let mut client = UDALClient::connect("http://localhost:50051").await?;
let devices = client.list_devices(()).await?;
```

See [`examples/README.adoc`](examples/README.adoc) for a full walkthrough: Docker stack → device registration → property read/write → gRPC streaming → Reflex dashboard.

## SDKs

| Language | Package | Transport |
|---|---|---|
| Go | `github.com/paulefl/udal/code/sdk/go` | gRPC |
| Python | `udal-sdk` (PyPI) | gRPC (asyncio) |
| TypeScript / Node.js | `udal-sdk` (npm) | REST / HTTP |
| Rust | `udal-sdk` (crates.io) | gRPC (std) · MQTT-only (`no_std`, embedded) |

## API Documentation

| Format | File |
|---|---|
| **OpenAPI v3** (REST / grpc-gateway) | [`code/api/openapi/udal/v1/device.openapi.v3.json`](code/api/openapi/udal/v1/device.openapi.v3.json) |
| **OpenAPI v2 / Swagger** | [`code/api/openapi/udal/v1/device.swagger.json`](code/api/openapi/udal/v1/device.swagger.json) |
| **Protobuf** (gRPC source of truth) | [`code/api/proto/udal/v1/`](code/api/proto/udal/v1/) |
| **Capability schema API** | [`code/api/openapi/udal/v1/capability.swagger.json`](code/api/openapi/udal/v1/capability.swagger.json) |

The gateway also exposes gRPC reflection at runtime — no `.proto` file needed for tools like `grpcurl` or Postman.

## Architecture

Full arc42 architecture documentation is published as a PDF with every release.
See [Releases](https://github.com/paul-fleischmann-com/udal/releases/latest) → `udal-arc42.pdf`.

The architecture model lives in [`architecture.jsonc`](architecture.jsonc) and is validated in CI via [bausteinsicht](https://github.com/docToolchain/Bausteinsicht).

## Building from source

### Prerequisites

| Tool | Version |
|---|---|
| Go | ≥ 1.22 |
| Rust | stable |
| Python | ≥ 3.12 |
| Node.js | ≥ 20 |
| Docker | ≥ 24 |

```bash
git clone https://github.com/paul-fleischmann-com/udal.git
cd udal

# Gateway + Go SDK + CLI
go build ./...
go test ./...

# Rust SDK
cd code/sdk/rust && cargo build && cargo test

# Python SDK
cd code/sdk/python && pip install -e ".[dev]" && pytest

# TypeScript SDK
cd code/sdk/typescript && npm ci && npm test
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full development guide, component-specific guidelines, and PR process.

## Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a PR.
Found a security issue? See [SECURITY.md](SECURITY.md).

## License

Apache 2.0 — see [LICENSE](LICENSE).
