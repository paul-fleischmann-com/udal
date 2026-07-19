"""UdalError — raised by every SDK operation that fails (req42.adoc §7.3:
"Python: raises UdalError(code, message)")."""

from __future__ import annotations

import grpc


class UdalError(Exception):
    """Raised by every SDK operation that fails against the gateway.

    ``code`` mirrors the gRPC status code the gateway responded with (see
    :class:`grpc.StatusCode`), letting callers distinguish e.g.
    ``NOT_FOUND`` from ``PERMISSION_DENIED`` without depending on the raw
    ``grpc.RpcError`` shape themselves.
    """

    def __init__(self, code: grpc.StatusCode, message: str) -> None:
        self.code = code
        self.message = message
        super().__init__(f"udal: {code.name}: {message}")


def wrap_error(exc: BaseException) -> UdalError:
    """Converts a gRPC (or any other) exception into a :class:`UdalError`."""
    if isinstance(exc, grpc.aio.AioRpcError):
        return UdalError(exc.code(), exc.details() or "")
    return UdalError(grpc.StatusCode.UNKNOWN, str(exc))
