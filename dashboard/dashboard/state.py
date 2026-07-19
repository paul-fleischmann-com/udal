"""Dashboard state: talks to the gateway via the Python SDK (udal-sdk,
issue #18) and holds everything the UI displays."""

from __future__ import annotations

import asyncio
import dataclasses
import json
import os

import reflex as rx
from udal import Client, ClientConfig, UdalError

#: Gateway connection — env-var configured (no login form, this is a
#: reference demonstrator, not a production admin tool), same convention
#: as the gateway's own UDAL_* variables.
GATEWAY_URL = os.environ.get("UDAL_GATEWAY_URL", "localhost:50051")
API_KEY = os.environ.get("UDAL_API_KEY", "")

#: The gateway's ListDevices RPC has no streaming/push variant — Subscribe
#: (used for live telemetry below) requires a specific device_id, not "all
#: devices" — so "live" device status here means polling on an interval,
#: not a true server push. Documented honestly rather than implying more
#: than this actually does.
DEVICE_POLL_INTERVAL_SECONDS = 3.0

#: Caps memory/DOM growth for a long-running telemetry watch.
MAX_TELEMETRY_ROWS = 50


def _client() -> Client:
    return Client(ClientConfig(gateway_url=GATEWAY_URL, api_key=API_KEY))


@dataclasses.dataclass
class DeviceRow:
    """One row of the device list table — a display-formatted projection
    of udal.DeviceInfo (pre-formatted strings rather than a datetime,
    since that's all any view here does with it)."""

    id: str
    name: str
    capability: str
    transport: str
    status: str
    last_seen: str


class DashboardState(rx.State):
    """The dashboard's entire UI state."""

    devices: list[DeviceRow] = []
    devices_error: str = ""
    watching_devices: bool = False

    selected_device_id: str = ""

    property_path: str = ""
    property_value: str = ""
    property_write_value: str = ""
    property_error: str = ""

    command_name: str = ""
    command_params_json: str = "{}"
    command_result: str = ""
    command_error: str = ""

    telemetry: list[str] = []
    watching_telemetry: bool = False
    telemetry_error: str = ""

    @rx.event
    def select_device(self, device_id: str) -> None:
        """Switches the property/command/telemetry panels to device_id,
        clearing anything left over from a previously selected device."""
        self.selected_device_id = device_id
        self.property_path = ""
        self.property_value = ""
        self.property_error = ""
        self.command_result = ""
        self.command_error = ""
        self.telemetry = []
        self.telemetry_error = ""
        self.watching_telemetry = False

    # Explicit setters, one per form input, rather than relying on
    # Reflex's auto-generated `set_<varname>` event handlers: those are
    # added dynamically at class-creation time, which mypy's static
    # analysis can't see (CONTRIBUTING.md requires strict mode) — an
    # explicit method is both statically visible and self-documenting.
    @rx.event
    def set_property_path(self, value: str) -> None:
        self.property_path = value

    @rx.event
    def set_property_write_value(self, value: str) -> None:
        self.property_write_value = value

    @rx.event
    def set_command_name(self, value: str) -> None:
        self.command_name = value

    @rx.event
    def set_command_params_json(self, value: str) -> None:
        self.command_params_json = value

    @rx.event(background=True)
    async def watch_devices(self) -> None:
        """Polls ListDevices every DEVICE_POLL_INTERVAL_SECONDS until
        stop_watching_devices is called. Idempotent — a second click while
        already running is a no-op, not a second concurrent poll loop."""
        async with self:
            if self.watching_devices:
                return
            self.watching_devices = True
        try:
            while True:
                async with self:
                    if not self.watching_devices:
                        return
                try:
                    async with _client() as client:
                        devices = await client.list_devices()
                except UdalError as exc:
                    async with self:
                        self.devices_error = str(exc)
                else:
                    async with self:
                        self.devices = [
                            DeviceRow(
                                id=d.id,
                                name=d.name,
                                capability=d.capability,
                                transport=d.transport,
                                status=d.status,
                                last_seen=d.last_seen.strftime("%Y-%m-%d %H:%M:%S UTC"),
                            )
                            for d in devices
                        ]
                        self.devices_error = ""
                await asyncio.sleep(DEVICE_POLL_INTERVAL_SECONDS)
        finally:
            async with self:
                self.watching_devices = False

    @rx.event
    def stop_watching_devices(self) -> None:
        self.watching_devices = False

    async def _refresh_property(self) -> None:
        """Fetches the currently-selected property and stores it — shared
        by read_property and write_property (the latter re-reads after a
        successful write, to show the value actually now stored, not just
        an echo of what was requested). A plain async method, not itself
        an @rx.event, specifically so it can be awaited directly from
        write_property without going through Reflex's event dispatch."""
        async with self:
            device_id, path = self.selected_device_id, self.property_path
        if not device_id or not path:
            return
        try:
            async with _client() as client:
                value = await client.get_property(device_id, path)
        except UdalError as exc:
            async with self:
                self.property_error = str(exc)
                self.property_value = ""
        else:
            async with self:
                self.property_value = str(value)
                self.property_error = ""

    @rx.event(background=True)
    async def read_property(self) -> None:
        await self._refresh_property()

    @rx.event(background=True)
    async def write_property(self) -> None:
        async with self:
            device_id = self.selected_device_id
            path = self.property_path
            raw = self.property_write_value
        if not device_id or not path:
            return
        value = _parse_scalar(raw)
        try:
            async with _client() as client:
                await client.write_property(device_id, path, value)
        except UdalError as exc:
            async with self:
                self.property_error = str(exc)
            return
        async with self:
            self.property_error = ""
        await self._refresh_property()

    @rx.event(background=True)
    async def send_command(self) -> None:
        async with self:
            device_id = self.selected_device_id
            name = self.command_name
            raw = self.command_params_json
        if not device_id or not name:
            return
        try:
            params = json.loads(raw) if raw.strip() else {}
        except json.JSONDecodeError as exc:
            async with self:
                self.command_error = f"invalid JSON params: {exc}"
                self.command_result = ""
            return
        try:
            async with _client() as client:
                result = await client.send_command(device_id, name, params)
        except UdalError as exc:
            async with self:
                self.command_error = str(exc)
                self.command_result = ""
        else:
            async with self:
                self.command_result = json.dumps(result) if result is not None else "(no result)"
                self.command_error = ""

    @rx.event(background=True)
    async def watch_telemetry(self) -> None:
        """Streams live property updates for the selected device via
        Subscribe — the one part of this dashboard that's a true server
        push, not polling (see DEVICE_POLL_INTERVAL_SECONDS's doc comment
        for why the device list can't work the same way)."""
        async with self:
            device_id = self.selected_device_id
            if not device_id:
                return
            self.watching_telemetry = True
            self.telemetry_error = ""
        try:
            async with _client() as client:
                async for update in client.subscribe(device_id):
                    async with self:
                        if not self.watching_telemetry or self.selected_device_id != device_id:
                            return
                        row = f"{update.timestamp:%H:%M:%S} {update.property_path} = {update.value}"
                        self.telemetry = [row, *self.telemetry][:MAX_TELEMETRY_ROWS]
        except UdalError as exc:
            async with self:
                self.telemetry_error = str(exc)
        finally:
            async with self:
                self.watching_telemetry = False

    @rx.event
    def stop_watching_telemetry(self) -> None:
        self.watching_telemetry = False


def _parse_scalar(raw: str) -> bool | int | float | str:
    """Best-effort parse of a form text input into the narrowest matching
    PropertyValueInput type — bool/int/float if it looks like one,
    otherwise the raw string. There's no way for the UI to know a
    property's declared type ahead of time (the gateway's API has no
    "describe this property" operation), so this is a pragmatic guess,
    not a validated conversion."""
    if raw in ("true", "false"):
        return raw == "true"
    try:
        return int(raw)
    except ValueError:
        pass
    try:
        return float(raw)
    except ValueError:
        pass
    return raw
