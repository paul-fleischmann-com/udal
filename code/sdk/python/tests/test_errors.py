import grpc
import pytest

from udal.errors import UdalError, wrap_error


class _FakeAioRpcError(grpc.aio.AioRpcError):
    def __init__(self, code: grpc.StatusCode, details: str) -> None:
        self._code = code
        self._details = details

    def code(self) -> grpc.StatusCode:
        return self._code

    def details(self) -> str:
        return self._details


def test_wrap_error_aio_rpc_error() -> None:
    err = wrap_error(_FakeAioRpcError(grpc.StatusCode.NOT_FOUND, "device not found"))
    assert isinstance(err, UdalError)
    assert err.code == grpc.StatusCode.NOT_FOUND
    assert err.message == "device not found"


def test_wrap_error_non_grpc() -> None:
    err = wrap_error(ValueError("plain error"))
    assert isinstance(err, UdalError)
    assert err.code == grpc.StatusCode.UNKNOWN
    assert "plain error" in err.message


def test_udal_error_str_includes_code_and_message() -> None:
    err = UdalError(grpc.StatusCode.PERMISSION_DENIED, "nope")
    assert "PERMISSION_DENIED" in str(err)
    assert "nope" in str(err)


def test_udal_error_is_exception() -> None:
    with pytest.raises(UdalError):
        raise UdalError(grpc.StatusCode.INTERNAL, "boom")
