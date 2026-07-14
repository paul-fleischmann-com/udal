# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- OpenAPI v3 spec for the Device API, generated from the existing Swagger 2.0 output
  via `swagger2openapi` and validated in CI (`redocly.yaml`, `make generate-openapi-v3`,
  `make validate-openapi-v3`). (#7)

### Changed

- Moved generated Go protobuf/gRPC stubs from `api/gen/go/` to `api/proto/gen/` to
  match the documented API layout; updated `buf.gen.yaml`, `go.work`, and gateway
  imports accordingly. (#7)
