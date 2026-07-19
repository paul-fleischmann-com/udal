import asyncio
from collections.abc import AsyncGenerator
from datetime import UTC

import grpc
import pytest

from tests.conftest import FakeDeviceService
from udal import Client, ClientConfig, UdalError
from udal.v1 import device_pb2


@pytest.fixture
async def client(gateway_url: str) -> AsyncGenerator[Client, None]:
    c = Client(ClientConfig(gateway_url=gateway_url, api_key="test-key"))
    yield c
    await c.close()


async def test_client_is_async_context_manager(gateway_url: str) -> None:
    async with Client(ClientConfig(gateway_url=gateway_url)) as c:
        assert isinstance(c, Client)


async def test_get_property_returns_value(client: Client, fake_service: FakeDeviceService) -> None:
    fake_service.properties[("dev-1", "temperature")] = device_pb2.PropertyValue(float_val=21.5)
    value = await client.get_property("dev-1", "temperature")
    assert value == 21.5


async def test_get_property_not_found_raises_udal_error(
    client: Client, fake_service: FakeDeviceService
) -> None:
    with pytest.raises(UdalError) as exc_info:
        await client.get_property("dev-1", "missing")
    assert exc_info.value.code == grpc.StatusCode.NOT_FOUND


async def test_write_property_sends_request(
    client: Client, fake_service: FakeDeviceService
) -> None:
    await client.write_property("dev-1", "setpoint", 19.5)
    assert len(fake_service.set_property_calls) == 1
    req = fake_service.set_property_calls[0]
    assert req.device_id == "dev-1"
    assert req.property_path == "setpoint"
    assert req.value.float_val == 19.5


async def test_write_property_unsupported_type_raises_before_any_call(
    client: Client, fake_service: FakeDeviceService
) -> None:
    with pytest.raises(UdalError):
        await client.write_property("dev-1", "x", object())  # type: ignore[arg-type]
    assert fake_service.set_property_calls == []


async def test_write_property_int_out_of_range_raises_udal_error_not_bare_value_error(
    client: Client, fake_service: FakeDeviceService
) -> None:
    # value_to_proto raises a bare ValueError for an out-of-int64-range
    # int; write_property must still translate that into a UdalError like
    # every other failure, per the SDK's documented contract (code review
    # finding, issue #18 — an earlier version only caught TypeError here).
    with pytest.raises(UdalError):
        await client.write_property("dev-1", "x", 2**63)
    assert fake_service.set_property_calls == []


async def test_send_command_returns_result(client: Client, fake_service: FakeDeviceService) -> None:
    from google.protobuf.struct_pb2 import Value

    fake_service.command_result = Value(string_value="ok")
    result = await client.send_command("dev-1", "reboot", {"delay": 5})
    assert result == "ok"
    assert len(fake_service.command_calls) == 1
    req = fake_service.command_calls[0]
    assert req.device_id == "dev-1"
    assert req.command == "reboot"
    assert req.params["delay"] == 5


async def test_send_command_no_result_returns_none(
    client: Client, fake_service: FakeDeviceService
) -> None:
    result = await client.send_command("dev-1", "noop")
    assert result is None


async def test_subscribe_yields_property_updates(
    client: Client, fake_service: FakeDeviceService
) -> None:
    from tests.conftest import now_timestamp

    fake_service.subscribe_events = [
        device_pb2.SubscribeResponse(
            device_id="dev-1",
            property_path="temperature",
            value=device_pb2.PropertyValue(float_val=22.0),
            timestamp=now_timestamp(),
        ),
        device_pb2.SubscribeResponse(
            device_id="dev-1",
            property_path="humidity",
            value=device_pb2.PropertyValue(float_val=55.0),
            timestamp=now_timestamp(),
        ),
    ]

    updates = [u async for u in client.subscribe("dev-1")]
    assert len(updates) == 2
    assert updates[0].property_path == "temperature"
    assert updates[0].value == 22.0
    assert updates[1].property_path == "humidity"
    assert updates[1].value == 55.0
    assert updates[0].timestamp.tzinfo == UTC


async def test_subscribe_closes_stream_promptly_on_early_break(
    client: Client, fake_service: FakeDeviceService
) -> None:
    from tests.conftest import now_timestamp

    # More events than the loop below actually consumes — without the
    # subscribe() generator's finally: call.cancel(), closing the
    # generator early wouldn't deterministically end the underlying gRPC
    # stream (code review finding, issue #18). asyncio.wait_for bounds how
    # long gen.aclose() is allowed to take; a stream left dangling here
    # would show up as this test hanging past the timeout.
    fake_service.subscribe_events = [
        device_pb2.SubscribeResponse(
            device_id="dev-1",
            property_path=f"p{i}",
            value=device_pb2.PropertyValue(float_val=float(i)),
            timestamp=now_timestamp(),
        )
        for i in range(5)
    ]

    gen = client.subscribe("dev-1")
    first = await anext(gen)
    assert first.property_path == "p0"
    await asyncio.wait_for(gen.aclose(), timeout=2.0)
