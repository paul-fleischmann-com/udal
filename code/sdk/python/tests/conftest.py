"""Shared pytest fixtures: an in-process asyncio gRPC server implementing
DeviceServiceServicer, exercised by client/device tests instead of a real
network connection — the Python equivalent of the Go SDK's
fakeDeviceServiceClient (device_internal_test.go)."""

from __future__ import annotations

import asyncio
from collections.abc import AsyncIterator
from typing import Any

import grpc
import pytest_asyncio
from google.protobuf.timestamp_pb2 import Timestamp

from udal.v1 import device_pb2, device_pb2_grpc


class FakeDeviceService(device_pb2_grpc.DeviceServiceServicer):
    """A minimal, in-memory DeviceService double. Each test configures the
    handful of attributes it needs (see individual tests) before starting
    the server."""

    def __init__(self) -> None:
        self.properties: dict[tuple[str, str], device_pb2.PropertyValue] = {}
        self.get_property_error: grpc.StatusCode | None = None
        self.register_response_id = "dev-generated"
        self.register_error: grpc.StatusCode | None = None
        self.register_calls: list[device_pb2.RegisterDeviceRequest] = []
        self.set_property_calls: list[device_pb2.SetPropertyRequest] = []
        self.command_calls: list[device_pb2.SendCommandRequest] = []
        self.command_result: Any = None
        self.subscribe_events: list[device_pb2.SubscribeResponse] = []
        #: When set, StreamCommands sends this Command once, then waits for
        #: the device's CommandResult and records it here.
        self.commands_to_send: list[device_pb2.Command] = []
        self.received_results: list[device_pb2.CommandResult] = []
        self.received_device_id_header: str | bytes | None = None
        #: StreamCommands aborts with UNAVAILABLE for the first N call
        #: attempts, then behaves normally — for testing Device.run's
        #: reconnect-with-backoff loop without a real ~30s network outage
        #: (mirrors the Go SDK's fakeDeviceServiceClient.failFirstN).
        self.stream_commands_fail_first_n = 0
        self.stream_commands_attempts = 0

    async def GetProperty(
        self, request: device_pb2.GetPropertyRequest, context: grpc.aio.ServicerContext[Any, Any]
    ) -> device_pb2.GetPropertyResponse:
        if self.get_property_error is not None:
            await context.abort(self.get_property_error, "simulated failure")
        value = self.properties.get((request.device_id, request.property_path))
        if value is None:
            await context.abort(grpc.StatusCode.NOT_FOUND, "property not found")
        return device_pb2.GetPropertyResponse(value=value)

    async def SetProperty(
        self, request: device_pb2.SetPropertyRequest, context: grpc.aio.ServicerContext[Any, Any]
    ) -> device_pb2.SetPropertyResponse:
        self.set_property_calls.append(request)
        self.properties[(request.device_id, request.property_path)] = request.value
        return device_pb2.SetPropertyResponse(new_value=request.value)

    async def RegisterDevice(
        self, request: device_pb2.RegisterDeviceRequest, context: grpc.aio.ServicerContext[Any, Any]
    ) -> device_pb2.RegisterDeviceResponse:
        self.register_calls.append(request)
        if self.register_error is not None:
            await context.abort(self.register_error, "simulated failure")
        device_id = request.id or self.register_response_id
        return device_pb2.RegisterDeviceResponse(device=device_pb2.Device(id=device_id))

    async def SendCommand(
        self, request: device_pb2.SendCommandRequest, context: grpc.aio.ServicerContext[Any, Any]
    ) -> device_pb2.SendCommandResponse:
        self.command_calls.append(request)
        resp = device_pb2.SendCommandResponse()
        if self.command_result is not None:
            resp.result.CopyFrom(self.command_result)
        return resp

    async def Subscribe(
        self, request: device_pb2.SubscribeRequest, context: grpc.aio.ServicerContext[Any, Any]
    ) -> AsyncIterator[device_pb2.SubscribeResponse]:
        for ev in self.subscribe_events:
            yield ev

    async def StreamCommands(
        self,
        request_iterator: AsyncIterator[device_pb2.CommandResult],
        context: grpc.aio.ServicerContext[Any, Any],
    ) -> AsyncIterator[device_pb2.Command]:
        self.stream_commands_attempts += 1
        if self.stream_commands_attempts <= self.stream_commands_fail_first_n:
            await context.abort(grpc.StatusCode.UNAVAILABLE, "simulated outage")
            return
        md = dict(context.invocation_metadata() or ())
        self.received_device_id_header = md.get("x-device-id")
        for cmd in self.commands_to_send:
            yield cmd
        async for result in request_iterator:
            self.received_results.append(result)
        if not self.commands_to_send:
            # No commands queued: behave like a healthy, idle stream and
            # block until the client cancels it — same reasoning as the Go
            # SDK's fakeCommandStream.Recv blocking on ctx.Done().
            await asyncio.Event().wait()


@pytest_asyncio.fixture
async def fake_service() -> AsyncIterator[FakeDeviceService]:
    yield FakeDeviceService()


@pytest_asyncio.fixture
async def gateway_url(fake_service: FakeDeviceService) -> AsyncIterator[str]:
    server = grpc.aio.server()
    device_pb2_grpc.add_DeviceServiceServicer_to_server(  # type: ignore[no-untyped-call]
        fake_service, server
    )
    port = server.add_insecure_port("localhost:0")
    await server.start()
    try:
        yield f"localhost:{port}"
    finally:
        await server.stop(None)


def now_timestamp() -> Timestamp:
    ts = Timestamp()
    ts.GetCurrentTime()
    return ts
