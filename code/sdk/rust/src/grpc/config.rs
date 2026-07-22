//! Connection configuration for the application-side [`super::Client`] and
//! device-side [`super::Device`] (req42.adoc §7.3: "Connect(config) /
//! constructor") — mirrors `code/sdk/go/config.go` and
//! `code/sdk/python/src/udal/config.py`.

use std::collections::HashMap;

/// Transport security for the gRPC channel. All fields are raw PEM bytes,
/// mirroring `tonic::transport::{Certificate, Identity}`'s own
/// constructors — `ca_certificate` verifies the gateway's server
/// certificate; `client_certificate`/`client_key` are only needed for mTLS.
#[derive(Debug, Clone, Default)]
pub struct TlsConfig {
    pub ca_certificate: Option<Vec<u8>>,
    pub client_certificate: Option<Vec<u8>>,
    pub client_key: Option<Vec<u8>>,
}

/// Configures an application-side connection (see [`super::Client::connect`]).
#[derive(Debug, Clone)]
pub struct ClientConfig {
    /// Gateway's gRPC address, e.g. `"localhost:50051"` — no scheme; `tls`
    /// controls whether the connection is encrypted.
    pub gateway_url: String,
    /// Sent as the `x-api-key` header on every call, if non-empty.
    pub api_key: String,
    /// `None` means an insecure (plaintext) connection — only for local
    /// development against a gateway started with `UDAL_DEV_INSECURE=true`.
    pub tls: Option<TlsConfig>,
}

impl ClientConfig {
    /// A plaintext config with no API key — equivalent to Go's
    /// `ClientConfig{GatewayURL: gatewayURL}`.
    pub fn new(gateway_url: impl Into<String>) -> Self {
        Self {
            gateway_url: gateway_url.into(),
            api_key: String::new(),
            tls: None,
        }
    }
}

/// Configures a device-side connection (see [`super::Device::connect`]).
#[derive(Debug, Clone)]
pub struct DeviceConfig {
    /// Gateway's gRPC address, e.g. `"localhost:50051"`.
    pub gateway_url: String,
    /// If set, registers (or re-registers, across restarts) with a stable
    /// identity. Left empty, the gateway assigns one on first
    /// [`super::Device::run`] and [`super::Device::id`] reports it
    /// afterwards.
    pub device_id: String,
    /// Required for registration.
    pub name: String,
    /// Capability schema reference, e.g. `"temperature-sensor"`.
    pub capability: String,
    /// Reported to the gateway at registration time. Devices using this
    /// SDK's `std` (gRPC) build connect directly over gRPC — no transport
    /// adapter in between — so this is typically `"grpc"`.
    pub transport: String,
    /// Arbitrary key/value tags attached to the device record.
    pub labels: HashMap<String, String>,
    /// Sent as the `x-api-key` header on every call, if non-empty.
    pub api_key: String,
    /// `None` means an insecure (plaintext) connection.
    pub tls: Option<TlsConfig>,
}

impl DeviceConfig {
    /// A plaintext, unregistered (`device_id` empty) config with
    /// `transport: "grpc"` and no labels/API key — the common case.
    pub fn new(
        gateway_url: impl Into<String>,
        name: impl Into<String>,
        capability: impl Into<String>,
    ) -> Self {
        Self {
            gateway_url: gateway_url.into(),
            device_id: String::new(),
            name: name.into(),
            capability: capability.into(),
            transport: "grpc".to_string(),
            labels: HashMap::new(),
            api_key: String::new(),
            tls: None,
        }
    }
}
