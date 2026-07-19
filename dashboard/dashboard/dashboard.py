"""UDAL reference dashboard (issue #19): device list, property browser,
command dispatch, and live telemetry — built on the Python SDK (#18)."""

from typing import Any

import reflex as rx

from dashboard.state import DashboardState, DeviceRow
from rxconfig import config


def _error_callout(message: str) -> rx.Component:
    """A red callout shown only while message is non-empty — the same
    error-display shape repeated across every panel below (code review
    finding, issue #19: collapsed 4 copy-pasted rx.cond/rx.callout blocks
    into this one helper). Typed as a plain str/bool/Any below, not
    rx.Var[...] — every call site passes a DashboardState class attribute
    access (e.g. DashboardState.devices_error), which mypy's own inference
    already treats as the plain underlying type, not a wrapped Var."""
    return rx.cond(message != "", rx.callout(message, color_scheme="red", size="1"))


def _labeled_value(label: str, value: str) -> rx.Component:
    """ "<label> <code>value</code>", shown only while value is non-empty —
    shared by the property browser's "Value:" and the command panel's
    "Result:" displays (code review finding, issue #19)."""
    return rx.cond(value != "", rx.text(label, rx.code(value)))


def _watch_toggle_button(
    watching: bool, start_label: str, on_start: Any, on_stop: Any
) -> rx.Component:
    """A button that reads "<start_label>" and starts on_start while not
    watching, or "Stop" and starts on_stop while watching — the shared
    shape behind "Watch devices"/"Stop watching" and "Start watching"/
    "Stop" (code review finding, issue #19). on_start/on_stop are typed
    Any: Reflex's own event-handler type varies by decoration
    (EventNamespace for a background handler, EventHandler for a plain
    one) in ways its stubs don't expose a common supertype for."""
    return rx.cond(
        watching,
        rx.button("Stop", on_click=on_stop, color_scheme="red", variant="soft", size="2"),
        rx.button(start_label, on_click=on_start, size="2"),
    )


def _device_row(row: DeviceRow) -> rx.Component:
    is_selected = DashboardState.selected_device_id == row.id
    return rx.table.row(
        rx.table.cell(row.id),
        rx.table.cell(row.name),
        rx.table.cell(row.capability),
        rx.table.cell(row.transport),
        rx.table.cell(
            rx.badge(
                row.status,
                color_scheme=rx.cond(row.status == "online", "green", "gray"),
            )
        ),
        rx.table.cell(row.last_seen),
        rx.table.cell(
            rx.button(
                rx.cond(is_selected, "Selected", "Select"),
                size="1",
                variant=rx.cond(is_selected, "solid", "outline"),
                on_click=DashboardState.select_device(row.id),
            )
        ),
        background=rx.cond(is_selected, rx.color("accent", 3), "transparent"),
    )


def device_list() -> rx.Component:
    return rx.vstack(
        rx.hstack(
            rx.heading("Devices", size="5"),
            rx.spacer(),
            _watch_toggle_button(
                DashboardState.watching_devices,
                "Watch devices",
                DashboardState.watch_devices,
                DashboardState.stop_watching_devices,
            ),
            width="100%",
            align="center",
        ),
        _error_callout(DashboardState.devices_error),
        rx.table.root(
            rx.table.header(
                rx.table.row(
                    rx.table.column_header_cell("ID"),
                    rx.table.column_header_cell("Name"),
                    rx.table.column_header_cell("Capability"),
                    rx.table.column_header_cell("Transport"),
                    rx.table.column_header_cell("Status"),
                    rx.table.column_header_cell("Last Seen"),
                    rx.table.column_header_cell(""),
                )
            ),
            rx.table.body(rx.foreach(DashboardState.devices, _device_row)),
            width="100%",
        ),
        width="100%",
        spacing="3",
    )


def property_browser() -> rx.Component:
    return rx.vstack(
        rx.heading("Property Browser", size="4"),
        rx.text(
            'The gateway has no "list properties" operation — enter a path you '
            "already know (e.g. from the device's capability schema).",
            size="1",
            color="gray",
        ),
        rx.hstack(
            rx.input(
                placeholder="property path, e.g. temperature",
                value=DashboardState.property_path,
                on_change=DashboardState.set_property_path,
                width="100%",
            ),
            rx.button("Read", on_click=DashboardState.read_property),
            width="100%",
        ),
        _labeled_value("Value: ", DashboardState.property_value),
        rx.hstack(
            rx.input(
                placeholder="new value (bool/int/float auto-detected, else string)",
                value=DashboardState.property_write_value,
                on_change=DashboardState.set_property_write_value,
                width="100%",
            ),
            rx.button("Write", on_click=DashboardState.write_property, color_scheme="grass"),
            width="100%",
        ),
        _error_callout(DashboardState.property_error),
        width="100%",
        spacing="2",
    )


def command_dispatch() -> rx.Component:
    return rx.vstack(
        rx.heading("Send Command", size="4"),
        rx.hstack(
            rx.input(
                placeholder="command name, e.g. reboot",
                value=DashboardState.command_name,
                on_change=DashboardState.set_command_name,
                width="40%",
            ),
            rx.input(
                placeholder='params as JSON, e.g. {"delay": 5}',
                value=DashboardState.command_params_json,
                on_change=DashboardState.set_command_params_json,
                width="60%",
            ),
            width="100%",
        ),
        rx.button("Send", on_click=DashboardState.send_command),
        _labeled_value("Result: ", DashboardState.command_result),
        _error_callout(DashboardState.command_error),
        width="100%",
        spacing="2",
    )


def live_telemetry() -> rx.Component:
    return rx.vstack(
        rx.hstack(
            rx.heading("Live Telemetry", size="4"),
            rx.spacer(),
            _watch_toggle_button(
                DashboardState.watching_telemetry,
                "Start watching",
                DashboardState.watch_telemetry,
                DashboardState.stop_watching_telemetry,
            ),
            width="100%",
            align="center",
        ),
        _error_callout(DashboardState.telemetry_error),
        rx.scroll_area(
            rx.vstack(
                rx.foreach(
                    DashboardState.telemetry,
                    lambda row: rx.text(row, font_family="monospace", size="1"),
                ),
                align="start",
                spacing="1",
            ),
            height="200px",
            width="100%",
            border="1px solid var(--gray-6)",
            border_radius="var(--radius-3)",
            padding="0.5em",
        ),
        width="100%",
        spacing="2",
    )


def device_panel() -> rx.Component:
    return rx.cond(
        DashboardState.selected_device_id != "",
        rx.vstack(
            rx.heading("Device: ", DashboardState.selected_device_id, size="6"),
            rx.divider(),
            property_browser(),
            rx.divider(),
            command_dispatch(),
            rx.divider(),
            live_telemetry(),
            width="100%",
            spacing="4",
        ),
        rx.text(
            "Select a device above to browse its properties, send commands, "
            "and watch live updates.",
            color="gray",
        ),
    )


def index() -> rx.Component:
    return rx.container(
        rx.color_mode.button(position="top-right"),
        rx.vstack(
            rx.heading("UDAL Dashboard", size="8"),
            rx.text(
                "Reference demonstrator for the UDAL gateway "
                "(req42.adoc F-03/F-04/F-05/F-06/F-07/F-08).",
                color="gray",
            ),
            device_list(),
            rx.divider(),
            device_panel(),
            spacing="5",
            width="100%",
            padding_y="2em",
        ),
        max_width="900px",
    )


app = rx.App()
app.add_page(index, title=f"{config.app_name} — UDAL")
