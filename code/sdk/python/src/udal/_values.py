"""Conversion between PropertyValue protobuf messages and native Python
values — mirrors code/sdk/go/value.go's valueFromProto/valueToProto."""

from __future__ import annotations

from typing import Any

from udal.v1 import device_pb2

#: Python types a PropertyValue can be built from. Structured (JSON) values
#: aren't supported here — the gateway's own property storage doesn't
#: round-trip them correctly yet (see the gateway's device_service.go
#: toProtoValue/fromProtoValue), so accepting them here would silently
#: produce broken behavior rather than a clear error.
PropertyValueInput = bool | int | float | str | bytes


def value_from_proto(pv: device_pb2.PropertyValue) -> Any:
    """Converts a PropertyValue to a native Python value: bool, int, float,
    str, or bytes — or None if pv carries no value at all."""
    which = pv.WhichOneof("value")
    if which is None:
        return None
    return getattr(pv, which)


def value_to_proto(value: PropertyValueInput) -> device_pb2.PropertyValue:
    """Converts a native Python value into a PropertyValue.

    Raises TypeError for an unsupported type (bool is checked before int,
    since bool is a subclass of int in Python and would otherwise be
    silently misencoded as int_val).
    """
    if isinstance(value, bool):
        return device_pb2.PropertyValue(bool_val=value)
    if isinstance(value, int):
        return device_pb2.PropertyValue(int_val=value)
    if isinstance(value, float):
        return device_pb2.PropertyValue(float_val=value)
    if isinstance(value, str):
        return device_pb2.PropertyValue(string_val=value)
    if isinstance(value, bytes):
        return device_pb2.PropertyValue(bytes_val=value)
    raise TypeError(
        f"unsupported property value type {type(value).__name__} "
        "(supported: bool, int, float, str, bytes)"
    )
