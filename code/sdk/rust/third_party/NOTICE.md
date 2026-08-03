# Vendored files

This directory vendors a handful of `.proto` files this crate's `build.rs`
needs as a `protoc` include path (see its doc comment), from:

- https://github.com/googleapis/googleapis (`google/api/annotations.proto`,
  `google/api/http.proto`) — Apache License 2.0.
- The `protoc` v28.3 release distribution's bundled well-known types
  (`google/protobuf/struct.proto`, `google/protobuf/timestamp.proto`,
  `google/protobuf/descriptor.proto`) — part of
  https://github.com/protocolbuffers/protobuf, BSD-3-Clause.

Not used at runtime — only `protoc` (invoked from `build.rs`) reads these,
to resolve `code/api/proto/udal/v1/{device,capability}.proto`'s imports.
