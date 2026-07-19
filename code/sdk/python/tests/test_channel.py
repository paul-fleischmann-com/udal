import grpc

from udal._channel import auth_metadata, dial
from udal.config import TLSConfig


def test_dial_insecure_returns_channel() -> None:
    channel = dial("localhost:1", None)
    assert isinstance(channel, grpc.aio.Channel)


def test_dial_tls_returns_channel() -> None:
    channel = dial("localhost:1", TLSConfig())
    assert isinstance(channel, grpc.aio.Channel)


def test_auth_metadata_empty_key() -> None:
    assert auth_metadata("") == ()


def test_auth_metadata_with_key() -> None:
    assert auth_metadata("secret") == (("x-api-key", "secret"),)
