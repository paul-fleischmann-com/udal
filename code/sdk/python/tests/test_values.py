import pytest

from udal._values import value_from_proto, value_to_proto
from udal.v1 import device_pb2


@pytest.mark.parametrize(
    "value",
    [True, False, 42, -7, 21.5, "hello", b"bytes"],
)
def test_value_round_trip(value: object) -> None:
    pv = value_to_proto(value)  # type: ignore[arg-type]
    assert value_from_proto(pv) == value


def test_value_to_proto_unsupported_type() -> None:
    with pytest.raises(TypeError):
        value_to_proto(object())  # type: ignore[arg-type]


def test_value_to_proto_bool_not_misencoded_as_int() -> None:
    # bool is a subclass of int in Python — a naive isinstance(x, int)
    # check without checking bool first would silently encode True as
    # int_val=1 instead of bool_val=True.
    pv = value_to_proto(True)
    assert pv.WhichOneof("value") == "bool_val"
    assert pv.bool_val is True


def test_value_from_proto_empty() -> None:
    assert value_from_proto(device_pb2.PropertyValue()) is None


def test_value_to_proto_int_out_of_int64_range_raises_value_error() -> None:
    # int_val is a proto3 int64 field; Python ints are unbounded. Without
    # an explicit range check, this would reach protobuf's own message
    # constructor and raise a bare ValueError there instead of a clear,
    # SDK-level one (code review finding, issue #18) — client.py/device.py
    # both catch (TypeError, ValueError) here and re-raise as UdalError.
    with pytest.raises(ValueError, match="int64 range"):
        value_to_proto(2**63)
    with pytest.raises(ValueError, match="int64 range"):
        value_to_proto(-(2**63) - 1)


def test_value_to_proto_int64_boundaries_are_accepted() -> None:
    value_to_proto(2**63 - 1)
    value_to_proto(-(2**63))
