import datetime

from google.api import annotations_pb2 as _annotations_pb2
from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class DeviceStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    DEVICE_STATUS_UNSPECIFIED: _ClassVar[DeviceStatus]
    DEVICE_STATUS_ONLINE: _ClassVar[DeviceStatus]
    DEVICE_STATUS_OFFLINE: _ClassVar[DeviceStatus]
    DEVICE_STATUS_UNKNOWN: _ClassVar[DeviceStatus]
DEVICE_STATUS_UNSPECIFIED: DeviceStatus
DEVICE_STATUS_ONLINE: DeviceStatus
DEVICE_STATUS_OFFLINE: DeviceStatus
DEVICE_STATUS_UNKNOWN: DeviceStatus

class Device(_message.Message):
    __slots__ = ("id", "name", "capability", "transport", "status", "last_seen", "labels")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    CAPABILITY_FIELD_NUMBER: _ClassVar[int]
    TRANSPORT_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    LAST_SEEN_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    id: str
    name: str
    capability: str
    transport: str
    status: DeviceStatus
    last_seen: _timestamp_pb2.Timestamp
    labels: _containers.ScalarMap[str, str]
    def __init__(self, id: _Optional[str] = ..., name: _Optional[str] = ..., capability: _Optional[str] = ..., transport: _Optional[str] = ..., status: _Optional[_Union[DeviceStatus, str]] = ..., last_seen: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., labels: _Optional[_Mapping[str, str]] = ...) -> None: ...

class PropertyValue(_message.Message):
    __slots__ = ("bool_val", "int_val", "float_val", "string_val", "bytes_val", "json_val")
    BOOL_VAL_FIELD_NUMBER: _ClassVar[int]
    INT_VAL_FIELD_NUMBER: _ClassVar[int]
    FLOAT_VAL_FIELD_NUMBER: _ClassVar[int]
    STRING_VAL_FIELD_NUMBER: _ClassVar[int]
    BYTES_VAL_FIELD_NUMBER: _ClassVar[int]
    JSON_VAL_FIELD_NUMBER: _ClassVar[int]
    bool_val: bool
    int_val: int
    float_val: float
    string_val: str
    bytes_val: bytes
    json_val: _struct_pb2.Value
    def __init__(self, bool_val: _Optional[bool] = ..., int_val: _Optional[int] = ..., float_val: _Optional[float] = ..., string_val: _Optional[str] = ..., bytes_val: _Optional[bytes] = ..., json_val: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ...) -> None: ...

class GetDeviceRequest(_message.Message):
    __slots__ = ("id",)
    ID_FIELD_NUMBER: _ClassVar[int]
    id: str
    def __init__(self, id: _Optional[str] = ...) -> None: ...

class GetDeviceResponse(_message.Message):
    __slots__ = ("device",)
    DEVICE_FIELD_NUMBER: _ClassVar[int]
    device: Device
    def __init__(self, device: _Optional[_Union[Device, _Mapping]] = ...) -> None: ...

class ListDevicesRequest(_message.Message):
    __slots__ = ("capability", "transport", "page_size", "page_token")
    CAPABILITY_FIELD_NUMBER: _ClassVar[int]
    TRANSPORT_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    capability: str
    transport: str
    page_size: int
    page_token: str
    def __init__(self, capability: _Optional[str] = ..., transport: _Optional[str] = ..., page_size: _Optional[int] = ..., page_token: _Optional[str] = ...) -> None: ...

class ListDevicesResponse(_message.Message):
    __slots__ = ("devices", "next_page_token")
    DEVICES_FIELD_NUMBER: _ClassVar[int]
    NEXT_PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    devices: _containers.RepeatedCompositeFieldContainer[Device]
    next_page_token: str
    def __init__(self, devices: _Optional[_Iterable[_Union[Device, _Mapping]]] = ..., next_page_token: _Optional[str] = ...) -> None: ...

class RegisterDeviceRequest(_message.Message):
    __slots__ = ("name", "capability", "transport", "labels", "transport_config", "id")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAME_FIELD_NUMBER: _ClassVar[int]
    CAPABILITY_FIELD_NUMBER: _ClassVar[int]
    TRANSPORT_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    TRANSPORT_CONFIG_FIELD_NUMBER: _ClassVar[int]
    ID_FIELD_NUMBER: _ClassVar[int]
    name: str
    capability: str
    transport: str
    labels: _containers.ScalarMap[str, str]
    transport_config: _struct_pb2.Struct
    id: str
    def __init__(self, name: _Optional[str] = ..., capability: _Optional[str] = ..., transport: _Optional[str] = ..., labels: _Optional[_Mapping[str, str]] = ..., transport_config: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., id: _Optional[str] = ...) -> None: ...

class RegisterDeviceResponse(_message.Message):
    __slots__ = ("device",)
    DEVICE_FIELD_NUMBER: _ClassVar[int]
    device: Device
    def __init__(self, device: _Optional[_Union[Device, _Mapping]] = ...) -> None: ...

class DeleteDeviceRequest(_message.Message):
    __slots__ = ("id",)
    ID_FIELD_NUMBER: _ClassVar[int]
    id: str
    def __init__(self, id: _Optional[str] = ...) -> None: ...

class DeleteDeviceResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class GetPropertyRequest(_message.Message):
    __slots__ = ("device_id", "property_path")
    DEVICE_ID_FIELD_NUMBER: _ClassVar[int]
    PROPERTY_PATH_FIELD_NUMBER: _ClassVar[int]
    device_id: str
    property_path: str
    def __init__(self, device_id: _Optional[str] = ..., property_path: _Optional[str] = ...) -> None: ...

class GetPropertyResponse(_message.Message):
    __slots__ = ("value",)
    VALUE_FIELD_NUMBER: _ClassVar[int]
    value: PropertyValue
    def __init__(self, value: _Optional[_Union[PropertyValue, _Mapping]] = ...) -> None: ...

class SetPropertyRequest(_message.Message):
    __slots__ = ("device_id", "property_path", "value")
    DEVICE_ID_FIELD_NUMBER: _ClassVar[int]
    PROPERTY_PATH_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    device_id: str
    property_path: str
    value: PropertyValue
    def __init__(self, device_id: _Optional[str] = ..., property_path: _Optional[str] = ..., value: _Optional[_Union[PropertyValue, _Mapping]] = ...) -> None: ...

class SetPropertyResponse(_message.Message):
    __slots__ = ("new_value",)
    NEW_VALUE_FIELD_NUMBER: _ClassVar[int]
    new_value: PropertyValue
    def __init__(self, new_value: _Optional[_Union[PropertyValue, _Mapping]] = ...) -> None: ...

class SendCommandRequest(_message.Message):
    __slots__ = ("device_id", "command", "params")
    DEVICE_ID_FIELD_NUMBER: _ClassVar[int]
    COMMAND_FIELD_NUMBER: _ClassVar[int]
    PARAMS_FIELD_NUMBER: _ClassVar[int]
    device_id: str
    command: str
    params: _struct_pb2.Struct
    def __init__(self, device_id: _Optional[str] = ..., command: _Optional[str] = ..., params: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class SendCommandResponse(_message.Message):
    __slots__ = ("result",)
    RESULT_FIELD_NUMBER: _ClassVar[int]
    result: _struct_pb2.Value
    def __init__(self, result: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ...) -> None: ...

class SubscribeRequest(_message.Message):
    __slots__ = ("device_id", "property_path")
    DEVICE_ID_FIELD_NUMBER: _ClassVar[int]
    PROPERTY_PATH_FIELD_NUMBER: _ClassVar[int]
    device_id: str
    property_path: str
    def __init__(self, device_id: _Optional[str] = ..., property_path: _Optional[str] = ...) -> None: ...

class SubscribeResponse(_message.Message):
    __slots__ = ("device_id", "property_path", "value", "timestamp", "status")
    DEVICE_ID_FIELD_NUMBER: _ClassVar[int]
    PROPERTY_PATH_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    TIMESTAMP_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    device_id: str
    property_path: str
    value: PropertyValue
    timestamp: _timestamp_pb2.Timestamp
    status: DeviceStatus
    def __init__(self, device_id: _Optional[str] = ..., property_path: _Optional[str] = ..., value: _Optional[_Union[PropertyValue, _Mapping]] = ..., timestamp: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., status: _Optional[_Union[DeviceStatus, str]] = ...) -> None: ...

class Command(_message.Message):
    __slots__ = ("id", "name", "params")
    ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    PARAMS_FIELD_NUMBER: _ClassVar[int]
    id: str
    name: str
    params: _struct_pb2.Struct
    def __init__(self, id: _Optional[str] = ..., name: _Optional[str] = ..., params: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class CommandResult(_message.Message):
    __slots__ = ("id", "success", "error", "result")
    ID_FIELD_NUMBER: _ClassVar[int]
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    RESULT_FIELD_NUMBER: _ClassVar[int]
    id: str
    success: bool
    error: str
    result: _struct_pb2.Value
    def __init__(self, id: _Optional[str] = ..., success: _Optional[bool] = ..., error: _Optional[str] = ..., result: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ...) -> None: ...
