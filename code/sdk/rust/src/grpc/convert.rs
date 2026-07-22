//! Conversion between `PropertyValue` protobuf messages and [`Value`] —
//! mirrors `code/sdk/go/value.go`'s `valueFromProto`/`valueToProto`.

use crate::value::Value;

use super::pb;

/// Converts a `PropertyValue` to a [`Value`], or `None` if it carries no
/// value at all, or carries the structured `json_val` variant — the
/// gateway's own property storage doesn't round-trip that one correctly
/// yet (see `device_service.go`'s `toProtoValue`/`fromProtoValue`), matching
/// the same scope note in the Go and Python SDKs.
pub(crate) fn value_from_proto(pv: pb::PropertyValue) -> Option<Value> {
    match pv.value? {
        pb::property_value::Value::BoolVal(b) => Some(Value::Bool(b)),
        pb::property_value::Value::IntVal(i) => Some(Value::Int(i)),
        pb::property_value::Value::FloatVal(f) => Some(Value::Float(f)),
        pb::property_value::Value::StringVal(s) => Some(Value::String(s)),
        pb::property_value::Value::BytesVal(b) => Some(Value::Bytes(b)),
        pb::property_value::Value::JsonVal(_) => None,
    }
}

/// Converts a [`Value`] into a `PropertyValue`.
pub(crate) fn value_to_proto(value: Value) -> pb::PropertyValue {
    let inner = match value {
        Value::Bool(b) => pb::property_value::Value::BoolVal(b),
        Value::Int(i) => pb::property_value::Value::IntVal(i),
        Value::Float(f) => pb::property_value::Value::FloatVal(f),
        Value::String(s) => pb::property_value::Value::StringVal(s),
        Value::Bytes(b) => pb::property_value::Value::BytesVal(b),
    };
    pb::PropertyValue { value: Some(inner) }
}

/// Converts a protobuf `Timestamp` into a `SystemTime`, clamping to
/// `UNIX_EPOCH` for an (unrealistic, pre-1970) negative-seconds value
/// rather than panicking on the `Duration` underflow.
pub(crate) fn timestamp_from_proto(ts: Option<prost_types::Timestamp>) -> std::time::SystemTime {
    let ts = ts.unwrap_or_default();
    if ts.seconds < 0 {
        return std::time::UNIX_EPOCH;
    }
    std::time::UNIX_EPOCH + std::time::Duration::new(ts.seconds as u64, ts.nanos.max(0) as u32)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn bool_roundtrips() {
        let pv = value_to_proto(Value::Bool(true));
        assert_eq!(value_from_proto(pv), Some(Value::Bool(true)));
    }

    #[test]
    fn bytes_roundtrips() {
        let pv = value_to_proto(Value::Bytes(vec![1, 2, 3]));
        assert_eq!(value_from_proto(pv), Some(Value::Bytes(vec![1, 2, 3])));
    }

    #[test]
    fn unset_value_is_none() {
        assert_eq!(value_from_proto(pb::PropertyValue { value: None }), None);
    }

    #[test]
    fn json_val_is_unsupported() {
        let pv = pb::PropertyValue {
            value: Some(pb::property_value::Value::JsonVal(prost_types::Value {
                kind: Some(prost_types::value::Kind::NullValue(0)),
            })),
        };
        assert_eq!(value_from_proto(pv), None);
    }

    #[test]
    fn negative_timestamp_clamps_to_epoch() {
        let ts = Some(prost_types::Timestamp {
            seconds: -1,
            nanos: 0,
        });
        assert_eq!(timestamp_from_proto(ts), std::time::UNIX_EPOCH);
    }
}
