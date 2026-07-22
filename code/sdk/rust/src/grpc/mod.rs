//! Application- and device-side SDK over gRPC (req42.adoc §7.3, `std`
//! feature) — mirrors `code/sdk/go` and `code/sdk/python`'s shape: a
//! [`Client`] (application side) and a [`Device`] (device side), both
//! dialing the gateway's `DeviceService` via `tonic`.

mod client;
mod config;
mod convert;
mod device;
mod dial;
pub mod params;

pub use client::{Client, PropertyUpdate, Subscription};
pub use config::{ClientConfig, DeviceConfig, TlsConfig};
pub use device::{CommandHandler, Device};
pub use params::Params;

/// Generated `udal.v1` protobuf/gRPC stubs (see build.rs) — mirrors how the
/// Go SDK keeps `code/api/proto/gen/` internal to its own package boundary
/// rather than re-exporting it; only [`client`]/[`device`]/[`convert`] reach
/// into this directly.
///
/// `clippy::all` is silenced here, not project-wide: this is
/// tonic-prost-build's output, not code this SDK controls the shape of —
/// same reasoning the Go SDK's `code/api/proto/gen/` is excluded from
/// golangci-lint and the Python SDK's `udal.v1` stubs are excluded from
/// ruff/mypy.
#[allow(clippy::all)]
pub(crate) mod pb {
    tonic::include_proto!("udal.v1");
}
