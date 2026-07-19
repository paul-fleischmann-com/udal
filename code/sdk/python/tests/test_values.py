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
