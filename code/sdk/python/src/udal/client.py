"""Application-side SDK (req42.adoc §7.3): reads/writes device properties,
sends commands, and subscribes to live property updates."""

from __future__ import annotations

from collections.abc import AsyncGenerator
from dataclasses import dataclass
from datetime import UTC, datetime
from types import TracebackType
from typing import Any

import grpc
from google.protobuf import struct_pb2
from google.protobuf.json_format import MessageToDict

from udal._channel import auth_metadata, dial
from udal._values import PropertyValueInput, value_from_proto, value_to_proto
from udal.config import ClientConfig
from udal.errors import wrap_error
from udal.v1 import device_pb2, device_pb2_grpc


@dataclass(frozen=True, slots=True)
class PropertyUpdate:
    """One event delivered by :meth:`Client.subscribe`."""

    device_id: str
    property_path: str
    value: Any
    timestamp: datetime


class Client:
    """The application-side SDK. Use as an async context manager, or call
    :meth:`close` explicitly when done::

        async with Client(ClientConfig(gateway_url="localhost:50051")) as client:
            value = await client.get_property("dev-1", "temperature")
    """

    def __init__(self, config: ClientConfig) -> None:
        self._config = config
        self._channel = dial(config.gateway_url, config.tls)
        # Generated code has no type annotations at all (excluded from
        # strict mypy checking, see pyproject.toml) — this one call site
        # into it needs its own ignore since the exclusion only covers
        # errors reported *within* the generated module's own file.
        self._stub = device_pb2_grpc.DeviceServiceStub(self._channel)  # type: ignore[no-untyped-call]

    async def close(self) -> None:
        """Closes the underlying gRPC channel."""
        await self._channel.close()

    async def __aenter__(self) -> Client:
        return self

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        await self.close()

    def _metadata(self) -> tuple[tuple[str, str], ...]:
        return auth_metadata(self._config.api_key)

    async def get_property(self, device_id: str, path: str) -> Any:
        """Reads device_id's current value at path."""
        try:
            resp = await self._stub.GetProperty(
                device_pb2.GetPropertyRequest(device_id=device_id, property_path=path),
                metadata=self._metadata(),
            )
        except grpc.RpcError as exc:
            raise wrap_error(exc) from exc
        return value_from_proto(resp.value)

    async def write_property(self, device_id: str, path: str, value: PropertyValueInput) -> None:
        """Writes value to device_id's property at path."""
        try:
            pv = value_to_proto(value)
        except (TypeError, ValueError) as exc:
            raise wrap_error(exc) from exc
        try:
            await self._stub.SetProperty(
                device_pb2.SetPropertyRequest(device_id=device_id, property_path=path, value=pv),
                metadata=self._metadata(),
            )
        except grpc.RpcError as exc:
            raise wrap_error(exc) from exc

    async def send_command(
        self, device_id: str, command: str, params: dict[str, Any] | None = None
    ) -> Any:
        """Sends a named command with params to device_id and returns its result."""
        s = struct_pb2.Struct()
        s.update(params or {})
        try:
            resp = await self._stub.SendCommand(
                device_pb2.SendCommandRequest(device_id=device_id, command=command, params=s),
                metadata=self._metadata(),
            )
        except grpc.RpcError as exc:
            raise wrap_error(exc) from exc
        if not resp.HasField("result"):
            return None
        return MessageToDict(resp.result)

    async def subscribe(
        self, device_id: str, path: str = ""
    ) -> AsyncGenerator[PropertyUpdate, None]:
        """Streams property updates for device_id (every property if path
        is ""), until the caller stops iterating or the stream ends."""
        call = self._stub.Subscribe(
            device_pb2.SubscribeRequest(device_id=device_id, property_path=path),
            metadata=self._metadata(),
        )
        try:
            async for ev in call:
                yield PropertyUpdate(
                    device_id=ev.device_id,
                    property_path=ev.property_path,
                    value=value_from_proto(ev.value),
                    timestamp=ev.timestamp.ToDatetime(tzinfo=UTC),
                )
        except grpc.RpcError as exc:
            raise wrap_error(exc) from exc
        finally:
            # A caller that stops iterating early (`break`, or the
            # generator otherwise being closed) throws GeneratorExit in
            # here rather than raising grpc.RpcError — without this, the
            # underlying gRPC stream stays open (leaking a stream/goroutine
            # on the gateway) until Python's refcounting or asyncgen
            # finalizer hooks eventually get around to closing it, which
            # isn't guaranteed to be prompt (code review finding, issue
            # #18). call.cancel() is a no-op if the call already finished.
            call.cancel()
