import asyncio
from collections.abc import AsyncGenerator, Mapping

import grpc
import pytest
from google.protobuf.struct_pb2 import Struct

from tests.conftest import FakeDeviceService
from udal import Device, DeviceConfig, UdalError
from udal.v1 import device_pb2


@pytest.fixture
async def device(gateway_url: str) -> AsyncGenerator[Device, None]:
    d = Device(
        DeviceConfig(gateway_url=gateway_url, name="sensor-1", capability="temperature-sensor")
    )
    yield d
    await d.close()


async def test_register_assigns_gateway_id(device: Device, fake_service: FakeDeviceService) -> None:
    fake_service.register_response_id = "dev-assigned"
    await device._register()
    assert device.id == "dev-assigned"
    assert len(fake_service.register_calls) == 1


async def test_register_already_exists_keeps_stable_id(
    gateway_url: str, fake_service: FakeDeviceService
) -> None:
    fake_service.register_error = grpc.StatusCode.ALREADY_EXISTS
    d = Device(
        DeviceConfig(
            gateway_url=gateway_url,
            name="sensor-1",
            capability="temperature-sensor",
            device_id="dev-stable",
        )
    )
    try:
        await d._register()
        assert d.id == "dev-stable"
    finally:
        await d.close()


async def test_register_other_error_raises(device: Device, fake_service: FakeDeviceService) -> None:
    fake_service.register_error = grpc.StatusCode.PERMISSION_DENIED
    with pytest.raises(UdalError) as exc_info:
        await device._register()
    assert exc_info.value.code == grpc.StatusCode.PERMISSION_DENIED


async def test_publish_property_sends_request(
    device: Device, fake_service: FakeDeviceService
) -> None:
    await device._register()
    await device.publish_property("temperature", 21.5)
    assert len(fake_service.set_property_calls) == 1
    assert fake_service.set_property_calls[0].value.float_val == 21.5


async def test_on_command_dispatches_and_writes_result(
    device: Device, fake_service: FakeDeviceService
) -> None:
    seen: dict[str, object] = {}

    def reboot(params: dict[str, object]) -> str:
        seen.update(params)
        return "rebooted"

    device.on_command("reboot", reboot)
    fake_service.commands_to_send = [
        device_pb2.Command(id="cmd-1", name="reboot", params=_struct({"delay": 3}))
    ]

    run_task = asyncio.ensure_future(device.run())
    for _ in range(50):
        if fake_service.received_results:
            break
        await asyncio.sleep(0.05)
    run_task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await run_task

    assert seen == {"delay": 3}
    assert len(fake_service.received_results) == 1
    result = fake_service.received_results[0]
    assert result.id == "cmd-1"
    assert result.success is True
    assert result.result.string_value == "rebooted"


async def test_unknown_command_reports_error_result(
    device: Device, fake_service: FakeDeviceService
) -> None:
    fake_service.commands_to_send = [device_pb2.Command(id="cmd-1", name="unregistered")]

    run_task = asyncio.ensure_future(device.run())
    for _ in range(50):
        if fake_service.received_results:
            break
        await asyncio.sleep(0.05)
    run_task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await run_task

    result = fake_service.received_results[0]
    assert result.success is False
    assert "unregistered" in result.error


async def test_run_reconnects_after_stream_failures(
    device: Device, fake_service: FakeDeviceService
) -> None:
    # Guards req42.adoc #12-equivalent AC for the Python SDK: "gateway
    # outage -> SDK reconnects, resumes without manual intervention" — see
    # code/sdk/go/device_internal_test.go's identical-in-spirit test.
    fake_service.stream_commands_fail_first_n = 2

    run_task = asyncio.ensure_future(device.run())
    try:
        for _ in range(100):
            if fake_service.stream_commands_attempts >= 3:
                break
            await asyncio.sleep(0.05)
        assert fake_service.stream_commands_attempts >= 3
    finally:
        run_task.cancel()
        with pytest.raises(asyncio.CancelledError):
            await run_task


def _struct(d: Mapping[str, int]) -> Struct:
    s = Struct()
    s.update(d)
    return s
