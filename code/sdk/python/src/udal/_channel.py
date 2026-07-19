"""gRPC channel setup — mirrors code/sdk/go/dial.go."""

from __future__ import annotations

import grpc

from udal.config import TLSConfig


def dial(gateway_url: str, tls: TLSConfig | None) -> grpc.aio.Channel:
    """Opens an asyncio gRPC channel to gateway_url, encrypted with tls if
    given or plaintext otherwise."""
    if tls is None:
        return grpc.aio.insecure_channel(gateway_url)
    creds = grpc.ssl_channel_credentials(
        root_certificates=tls.root_certificates,
        private_key=tls.private_key,
        certificate_chain=tls.certificate_chain,
    )
    return grpc.aio.secure_channel(gateway_url, creds)


def auth_metadata(api_key: str) -> tuple[tuple[str, str], ...]:
    """Returns the X-API-Key metadata tuple for api_key, or an empty tuple
    if api_key is empty."""
    if not api_key:
        return ()
    return (("x-api-key", api_key),)
