"""Python client SDK for UDAL (Universal Device Abstraction Layer).

Two entry points, mirroring the Go SDK (code/sdk/go):

- :class:`Client` — the application side: read/write device properties,
  send commands, and subscribe to live property updates.
- :class:`Device` — the device side: register with a gateway, publish
  property values, and handle incoming commands.

Every operation raises :class:`UdalError` on failure (req42.adoc §7.3:
"Python: raises UdalError(code, message)"), wrapping the gRPC status code
the gateway returned so callers can branch on it without depending on
``grpc.RpcError`` directly.
"""

from udal.client import Client, DeviceInfo, PropertyUpdate
from udal.config import ClientConfig, DeviceConfig, TLSConfig
from udal.device import CommandHandler, Device, Params
from udal.errors import UdalError

__all__ = [
    "Client",
    "ClientConfig",
    "CommandHandler",
    "Device",
    "DeviceConfig",
    "DeviceInfo",
    "Params",
    "PropertyUpdate",
    "TLSConfig",
    "UdalError",
]
