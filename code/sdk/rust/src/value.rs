//! Native property value type for the `std` (gRPC) build — converts to/from
//! `udal.v1.PropertyValue` in [`crate::grpc::convert`]. Mirrors
//! `code/sdk/go/value.go` and `code/sdk/python/src/udal/_values.py`.
//!
//! The `mqtt` (no_std) build has its own fixed-capacity
//! [`crate::mqtt::PropertyValue`] instead — this type owns a heap-allocated
//! `String`/`Vec<u8>`, which isn't available without an allocator.

/// A typed device property value.
#[derive(Debug, Clone, PartialEq)]
pub enum Value {
    Bool(bool),
    Int(i64),
    Float(f64),
    String(String),
    Bytes(Vec<u8>),
}

impl From<bool> for Value {
    fn from(v: bool) -> Self {
        Value::Bool(v)
    }
}

impl From<i64> for Value {
    fn from(v: i64) -> Self {
        Value::Int(v)
    }
}

impl From<f64> for Value {
    fn from(v: f64) -> Self {
        Value::Float(v)
    }
}

impl From<String> for Value {
    fn from(v: String) -> Self {
        Value::String(v)
    }
}

impl From<&str> for Value {
    fn from(v: &str) -> Self {
        Value::String(v.to_string())
    }
}

impl From<Vec<u8>> for Value {
    fn from(v: Vec<u8>) -> Self {
        Value::Bytes(v)
    }
}
