# Contributing to UDAL

Thank you for contributing! This guide covers everything you need to get started.

## Table of Contents

- [Development Environment](#development-environment)
- [Repository Structure](#repository-structure)
- [Branching Strategy](#branching-strategy)
- [Commit Messages](#commit-messages)
- [Pull Request Process](#pull-request-process)
- [Component-Specific Guidelines](#component-specific-guidelines)
- [Architecture Changes](#architecture-changes)

---

## Development Environment

### Prerequisites

| Tool | Version | Install |
|------|---------|---------|
| Go | ≥ 1.22 | https://go.dev/dl/ |
| Rust | stable | `rustup toolchain install stable` |
| Python | ≥ 3.12 | https://python.org |
| Node.js | ≥ 20 | https://nodejs.org |
| buf | ≥ 1.32 | https://buf.build/docs/installation |
| Docker | ≥ 24 | https://docs.docker.com/get-docker/ |
| bausteinsicht | latest | `go install github.com/docToolchain/Bausteinsicht/cmd/bausteinsicht@latest` |

### Quick Start

```bash
git clone https://github.com/paulefl/udal.git
cd udal

# Go (gateway + adapters + Go SDK)
go build ./...
go test ./...

# Rust SDK
cd sdk/rust && cargo build && cargo test

# Python SDK
cd sdk/python && pip install -e ".[dev]" && pytest

# TypeScript SDK
cd sdk/typescript && npm ci && npm test

# Reflex Dashboard
cd dashboard && pip install -e ".[dev]"

# Start local MQTT broker (for integration tests)
docker run -d -p 1883:1883 eclipse-mosquitto:2

# Validate architecture model
bausteinsicht validate --model architecture.jsonc
```

---

## Repository Structure

```
udal/
├── architecture.jsonc     # Bausteinsicht architecture model — update when architecture changes
├── gateway/               # Go — central runtime service
├── adapters/              # Go — MQTT, HTTP, CAN transport adapters
├── sdk/go/                # Go SDK (device + app)
├── sdk/rust/              # Rust SDK (embedded)
├── sdk/python/            # Python SDK (asyncio)
├── sdk/typescript/        # TypeScript / Node.js SDK
├── dashboard/             # Python Reflex reference dashboard
├── api/proto/             # Protobuf definitions (source of truth for API)
├── api/proto/gen/         # Generated Go stubs (protoc-gen-go, grpc, grpc-gateway) — do not edit manually
├── api/openapi/           # Generated OpenAPI v2 (Swagger) + v3 spec — do not edit manually
├── docs/
│   ├── arc42/             # arc42 architecture document
│   │   └── ADRs/          # Architecture Decision Records
│   ├── req42/             # req42 requirements document
│   └── spec/SRS.adoc      # Software Requirements Specification
├── deployments/           # Docker, Kubernetes (Helm), GoReleaser
└── examples/              # Runnable end-to-end demos
```

---

## Branching Strategy

| Branch | Purpose |
|--------|---------|
| `main` | Always deployable, protected |
| `feat/<issue>-<short-desc>` | New features |
| `fix/<issue>-<short-desc>` | Bug fixes |
| `chore/<desc>` | Maintenance, tooling |
| `docs/<desc>` | Documentation only |
| `release/v<x.y.z>` | Release preparation |

Example: `feat/12-go-sdk-device-registration`

---

## Commit Messages

We use **Conventional Commits**:

```
<type>(<scope>): <short description>

[optional body]

[optional footer]
```

**Types:** `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `perf`, `ci`

**Scopes:** `gateway`, `adapter:mqtt`, `adapter:http`, `adapter:can`, `sdk:go`, `sdk:rust`, `sdk:python`, `sdk:ts`, `dashboard`, `api`, `docs`, `ci`

**Examples:**
```
feat(adapter:mqtt): implement circuit breaker with exponential backoff
fix(gateway): handle nil pointer when device has no schema ref
docs(arc42): add runtime view for telemetry publish flow
chore(ci): pin buf to v1.32.0
```

Breaking changes: append `!` after scope and add `BREAKING CHANGE:` in footer.

---

## Pull Request Process

1. **Open an issue first** for non-trivial changes — discuss approach before coding
2. **Branch from `main`**, name it `feat/<issue>-<desc>` or `fix/<issue>-<desc>`
3. **Keep PRs focused** — one concern per PR
4. **Update `architecture.jsonc`** if you add/remove/rename components (see [Architecture Changes](#architecture-changes))
5. **Write tests** — new code needs unit tests; integration tests for adapters
6. **All CI checks must pass** before review
7. **PR checklist** (filled in by author):

```markdown
## Checklist
- [ ] Tests added / updated
- [ ] `bausteinsicht validate --model architecture.jsonc` passes
- [ ] ADR created for significant architecture decisions
- [ ] BREAKING CHANGE noted in commit footer (if applicable)
- [ ] Docs updated (if user-facing change)
```

---

## Component-Specific Guidelines

### Go (gateway, adapters, SDK)

- `go test -race ./...` must pass — no data races
- `golangci-lint run` must pass — use project `.golangci.yml`
- Error wrapping: `fmt.Errorf("doing X: %w", err)` — always wrap with context
- No `panic()` in adapter code — return errors, use `recover()` in goroutines
- Public API: full GoDoc comment on every exported symbol
- Integration tests: use `//go:build integration` build tag, require `UDAL_TEST_MQTT_BROKER` env var

### Rust SDK

- `cargo clippy -- -D warnings` must pass
- `cargo fmt` before commit
- `no_std` compatibility: do not use `std::` in paths reachable from `no_std` feature flag
- `unsafe` blocks require a safety comment explaining invariants

### Python SDK + Reflex Dashboard

- `ruff check` + `ruff format --check` must pass
- `mypy` in strict mode
- `pytest` coverage ≥ 80% for SDK (not required for dashboard)
- Type annotations on all public functions

### TypeScript SDK

- `eslint` must pass (config in `sdk/typescript/.eslintrc.json`)
- `tsc --noEmit` (no TypeScript errors)
- `npm audit` — no high/critical vulnerabilities
- Exports: ESM + CJS dual build via `tsup`

### Protobuf API

- All changes to `api/proto/` must pass `buf lint`
- Breaking changes require a new major version and an ADR
- `buf breaking` is enforced in CI — you will not accidentally break the API
- After changing a `.proto` file, run `make generate` to regenerate:
  - Go stubs in `api/proto/gen/`
  - OpenAPI v2 spec in `api/openapi/udal/v1/device.swagger.json` (via buf's `grpc-ecosystem/openapiv2` plugin)
  - OpenAPI v3 spec in `api/openapi/udal/v1/device.openapi.v3.json` (converted from the v2 spec via `swagger2openapi`)
- `make validate-openapi-v3` checks the v3 spec is structurally valid (`redocly.yaml`, minimal ruleset — the
  security/summary style rules are intentionally off since gateway-wide auth isn't documented per-operation yet)
- Commit all regenerated files together with the `.proto` change

---

## Architecture Changes

When you add, remove, rename, or reconnect components:

1. **Update `architecture.jsonc`** — this is the single source of truth for the architecture diagram
2. **Validate:** `bausteinsicht validate --model architecture.jsonc`
3. **Write an ADR** in `docs/arc42/ADRs/ADR-NNN-Title.adoc` for decisions with significant trade-offs (use Nygard format with Pugh Matrix)
4. **Update arc42 doc** (`docs/arc42/arc42.adoc`) in the affected section(s)

ADR naming: `ADR-005-Next-Decision.adoc` (sequential, never reuse a number).
