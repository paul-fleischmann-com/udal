//! Helper constructors for [`super::Client::send_command`]'s `params` and
//! [`super::CommandHandler`]'s return value.
//!
//! Command params/results are arbitrary JSON-shaped data (per req42.adoc
//! §7.3, `SendCommand(deviceId, command, params) -> Result`) — richer than
//! [`crate::value::Value`] (which only covers the five scalar types a
//! *property* can hold). `prost_types::Value`/`prost_types::Struct` already
//! model exactly this shape (the same types `google.protobuf.Struct`
//! decodes to), so this SDK reuses them directly rather than introducing a
//! second, parallel JSON value tree — orphan rules mean `From` impls can't
//! be added onto them from here, hence these free functions instead.

use std::collections::BTreeMap;

use prost_types::value::Kind;
use prost_types::Value;

/// Parameters for [`super::Client::send_command`] — mirrors Go's
/// `map[string]any` / Python's `dict[str, Any]`. A `BTreeMap` because
/// that's what `prost_types::Struct::fields` itself is.
pub type Params = BTreeMap<String, Value>;

pub fn null_value() -> Value {
    Value {
        kind: Some(Kind::NullValue(0)),
    }
}

pub fn bool_value(b: bool) -> Value {
    Value {
        kind: Some(Kind::BoolValue(b)),
    }
}

pub fn number_value(n: f64) -> Value {
    Value {
        kind: Some(Kind::NumberValue(n)),
    }
}

pub fn string_value(s: impl Into<String>) -> Value {
    Value {
        kind: Some(Kind::StringValue(s.into())),
    }
}
