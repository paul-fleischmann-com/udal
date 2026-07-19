"""Connection configuration for the application-side Client and device-side
Device (req42.adoc §7.3: "Connect(config) / constructor")."""

from __future__ import annotations

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class TLSConfig:
    """Transport security for the gRPC channel. All fields are raw PEM
    bytes, mirroring grpc.ssl_channel_credentials' own parameters —
    root_certificates verifies the gateway's server certificate;
    private_key/certificate_chain are only needed for mTLS."""

    root_certificates: bytes | None = None
    private_key: bytes | None = None
    certificate_chain: bytes | None = None


@dataclass(frozen=True, slots=True)
class ClientConfig:
    """Configures an application-side connection (see :class:`udal.Client`)."""

    #: Gateway's gRPC address, e.g. "localhost:50051" — no scheme; tls
    #: controls whether the connection is encrypted.
    gateway_url: str
    #: Sent as the X-API-Key header on every call, if set.
    api_key: str = ""
    #: None means an insecure (plaintext) connection — only for local
    #: development against a gateway started with UDAL_DEV_INSECURE=true.
    tls: TLSConfig | None = None


@dataclass(frozen=True, slots=True)
class DeviceConfig:
    """Configures a device-side connection (see :class:`udal.Device`)."""

    #: Gateway's gRPC address, e.g. "localhost:50051".
    gateway_url: str
    #: Required for registration.
    name: str
    #: Capability schema reference, e.g. "temperature-sensor".
    capability: str
    #: If set, registers (or re-registers, across restarts) with a stable
    #: identity. Left empty, the gateway assigns one on first run and
    #: Device.id reports it afterwards.
    device_id: str = ""
    #: Reported to the gateway at registration time. Devices using this SDK
    #: connect directly over gRPC (no transport adapter in between), so
    #: this is typically "grpc".
    transport: str = "grpc"
    #: Arbitrary key/value tags attached to the device record.
    labels: dict[str, str] = field(default_factory=dict)
    #: Sent as the X-API-Key header on every call, if set.
    api_key: str = ""
    #: None means an insecure (plaintext) connection.
    tls: TLSConfig | None = None
