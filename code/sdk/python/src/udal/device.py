"""Device-side SDK (req42.adoc §7.3): registers with a gateway, publishes
property values, and handles incoming commands.

Devices using this SDK connect directly over gRPC — there is no transport
adapter in between — so commands are delivered over StreamCommands rather
than through MQTT/HTTP/CAN.
"""

from __future__ import annotations

import asyncio
import logging
import time
from collections.abc import Awaitable, Callable
from typing import Any

import grpc
from google.protobuf.json_format import MessageToDict
from google.protobuf.struct_pb2 import Value

from udal._channel import auth_metadata, dial
from udal._values import PropertyValueInput, value_to_proto
from udal.config import DeviceConfig
from udal.errors import UdalError, wrap_error
from udal.v1 import device_pb2, device_pb2_grpc

logger = logging.getLogger("udal.device")

Params = dict[str, Any]
#: Handles one command and returns a result (may be None), or raises. A
#: raised exception is reported to the gateway as a device NACK
#: (FAILED_PRECONDITION on the SendCommand caller's side), with the
#: exception's message included. May be a plain function or a coroutine
#: function — Device.run awaits either.
CommandHandler = Callable[[Params], Any | Awaitable[Any]]

_BASE_BACKOFF = 1.0
_MAX_BACKOFF = 30.0
_HEALTHY_STREAM_THRESHOLD = 5.0


class Device:
    """The device-side SDK. Call :meth:`run` to register and start handling
    commands; :meth:`on_command` may be called any time before or after
    run starts."""

    def __init__(self, config: DeviceConfig) -> None:
        self._config = config
        self._channel = dial(config.gateway_url, config.tls)
        # See client.py's identical call site for why this needs its own
        # ignore despite udal.v1.* being excluded from strict mypy checking.
        self._stub = device_pb2_grpc.DeviceServiceStub(self._channel)  # type: ignore[no-untyped-call]
        self._device_id = config.device_id
        self._handlers: dict[str, CommandHandler] = {}

    async def close(self) -> None:
        """Closes the underlying gRPC channel."""
        await self._channel.close()

    @property
    def id(self) -> str:
        """This device's ID: config.device_id if it was set, otherwise the
        gateway-assigned ID once run has registered successfully."""
        return self._device_id

    def on_command(self, name: str, handler: CommandHandler) -> None:
        """Registers handler for the named command, replacing any handler
        previously registered for that name."""
        self._handlers[name] = handler

    def _metadata(self) -> tuple[tuple[str, str], ...]:
        return auth_metadata(self._config.api_key)

    async def _register(self) -> None:
        try:
            resp = await self._stub.RegisterDevice(
                device_pb2.RegisterDeviceRequest(
                    id=self._config.device_id,
                    name=self._config.name,
                    capability=self._config.capability,
                    transport=self._config.transport,
                    labels=self._config.labels,
                ),
                metadata=self._metadata(),
            )
        except grpc.RpcError as exc:
            # Already registered under our own stable device_id (e.g. this
            # is a reconnect after a process restart) isn't a failure to
            # give up on.
            if (
                isinstance(exc, grpc.aio.AioRpcError)
                and exc.code() == grpc.StatusCode.ALREADY_EXISTS
                and self._config.device_id
            ):
                self._device_id = self._config.device_id
                return
            raise wrap_error(exc) from exc
        self._device_id = resp.device.id

    async def publish_property(self, path: str, value: PropertyValueInput) -> None:
        """Writes a value to one of this device's own properties."""
        try:
            pv = value_to_proto(value)
        except TypeError as exc:
            raise wrap_error(exc) from exc
        try:
            await self._stub.SetProperty(
                device_pb2.SetPropertyRequest(device_id=self.id, property_path=path, value=pv),
                metadata=self._metadata(),
            )
        except grpc.RpcError as exc:
            raise wrap_error(exc) from exc

    async def run(self) -> None:
        """Registers the device (if not already) and opens its command
        stream, re-registering and reconnecting with exponential backoff
        (1s up to 30s) if the connection is lost, until cancelled. Runs
        until the enclosing task is cancelled."""
        await self._register()

        backoff = _BASE_BACKOFF
        while True:
            connected_at = time.monotonic()
            try:
                await self._run_command_stream()
                return  # stream ended cleanly (server closed it)
            except asyncio.CancelledError:
                raise
            except Exception as exc:
                if time.monotonic() - connected_at > _HEALTHY_STREAM_THRESHOLD:
                    # The stream was healthy for a while before failing;
                    # treat this as a fresh outage rather than compounding
                    # backoff from a previous one.
                    backoff = _BASE_BACKOFF
                logger.warning(
                    "command stream disconnected, reconnecting in %.0fs: %s", backoff, exc
                )

            await asyncio.sleep(backoff)
            try:
                await self._register()  # covers a gateway restart with a non-persistent registry
            except UdalError:
                pass  # best-effort, same reasoning as the Go SDK's Run
            backoff = min(backoff * 2, _MAX_BACKOFF)

    async def _run_command_stream(self) -> None:
        metadata = (*self._metadata(), ("x-device-id", self.id))
        call = self._stub.StreamCommands(metadata=metadata)
        async for cmd in call:
            asyncio.create_task(self._handle_command(call, cmd))

    async def _handle_command(self, call: Any, cmd: device_pb2.Command) -> None:
        handler = self._handlers.get(cmd.name)
        result = device_pb2.CommandResult(id=cmd.id)
        if handler is None:
            result.error = f'no handler registered for command "{cmd.name}"'
        else:
            try:
                params = MessageToDict(cmd.params) if cmd.HasField("params") else {}
                out = handler(params)
                if asyncio.iscoroutine(out):
                    out = await out
                result.success = True
                if out is not None:
                    result.result.CopyFrom(_to_value(out))
            except Exception as exc:
                result.success = False
                result.error = str(exc)
        try:
            await call.write(result)
        except grpc.RpcError:
            pass  # best-effort; a broken stream surfaces via the next Recv in _run_command_stream


def _to_value(out: Any) -> Value:
    """Converts a command handler's native return value into a
    google.protobuf.Value, mirroring structpb.NewValue's supported types
    in the Go SDK."""
    v = Value()
    if isinstance(out, bool):
        v.bool_value = out
    elif isinstance(out, (int, float)):
        v.number_value = out
    elif isinstance(out, str):
        v.string_value = out
    elif isinstance(out, dict):
        v.struct_value.update(out)
    elif isinstance(out, list):
        v.list_value.extend(out)
    else:
        v.string_value = str(out)
    return v
