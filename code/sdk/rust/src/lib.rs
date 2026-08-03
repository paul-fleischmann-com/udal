#![cfg_attr(not(feature = "std"), no_std)]
// The `mqtt` (no_std, no-allocator) build's `UdalError` inlines its message
// into a fixed-capacity `heapless::String` (see error.rs) rather than
// boxing it — there's no allocator to box into. That makes `UdalError`
// itself "large" by clippy's default threshold, which normally suggests
// `Box`ing the error; not an option here, so the lint is silenced
// crate-wide rather than at every one of the mqtt module's call sites.
#![allow(clippy::result_large_err)]
//! Rust client SDK for UDAL (Universal Device Abstraction Layer,
//! req42.adoc §7.3, QR-08).
//!
//! Two independent builds, selected by Cargo feature:
//!
//! - **`std`** (default) — the full application- and device-side SDK over
//!   gRPC: [`grpc::Client`] (application side) and [`grpc::Device`] (device
//!   side), mirroring the Go (`code/sdk/go`) and Python
//!   (`code/sdk/python`) SDKs' operation set (`Connect`, `Disconnect`,
//!   `ReadProperty`, `WriteProperty`, `SendCommand`, `Subscribe`,
//!   `RegisterDevice`). Requires `tokio`/`tonic` — not available on
//!   `no_std` targets.
//! - **`mqtt`** — a device-side-only SDK over MQTT ([`mqtt::MqttDevice`]),
//!   compiling `no_std` with no allocator, for bare-metal/RTOS targets that
//!   can't carry a gRPC/HTTP2 stack. Covers `Connect`/`Disconnect`/
//!   `WriteProperty` (`PublishProperty`) plus responding to the gateway's
//!   MQTT adapter's `.../get` and `.../set` requests; `SendCommand`/
//!   `Subscribe` aren't wired up on the MQTT transport at all yet (see
//!   `code/gateway/internal/adapters/mqtt`'s own doc comment: "not wired up
//!   in this ticket"), and `RegisterDevice` has no MQTT-side RPC to call at
//!   all — see [`mqtt::MqttDevice::publish_status`] for the closest analog
//!   the wire protocol has.
//!
//! Both builds return `Result<_, `[`UdalError`]`>` from every fallible
//! operation (req42.adoc §7.3: "Rust: `Result<Value, UdalError>`").
//!
//! Enabling both features together (e.g. `--all-features`) builds fine —
//! `std` is opted out of via `#![cfg_attr(not(feature = "std"), no_std)]`,
//! not a hard crate-wide switch, so the two aren't mutually exclusive at
//! compile time. Only a build with `std` disabled actually produces a
//! `no_std` binary.

pub mod error;

#[cfg(feature = "std")]
pub mod value;

#[cfg(feature = "std")]
pub mod grpc;

#[cfg(feature = "mqtt")]
pub mod mqtt;

pub use error::{ErrorCode, UdalError};

#[cfg(feature = "std")]
pub use value::Value;
