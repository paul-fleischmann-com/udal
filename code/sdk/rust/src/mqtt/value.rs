//! Fixed-capacity property value type for the `mqtt` (no_std, no
//! allocator) build. Mirrors [`crate::value::Value`] but bounds
//! `String`/`Bytes` to a small, compile-time capacity via `heapless`
//! instead of owning a heap-allocated buffer.

/// Capacity of [`PropertyValue::String`] — generous for a device
/// identifier, sensor unit label, or short status string; a longer value
/// is rejected at construction time (see [`PropertyValue::string`]) rather
/// than silently truncated.
pub const STRING_CAPACITY: usize = 64;

/// Capacity of [`PropertyValue::Bytes`] — sized for e.g. a short sensor
/// calibration blob; see [`PropertyValue::String`]'s doc comment for why
/// this is a hard cap rather than a truncation.
pub const BYTES_CAPACITY: usize = 32;

/// A typed device property value, sized to fit in a bare-metal device's
/// static memory budget (QR-08).
#[derive(Debug, Clone, PartialEq)]
pub enum PropertyValue {
    Bool(bool),
    Int(i64),
    Float(f64),
    String(heapless::String<STRING_CAPACITY>),
    Bytes(heapless::Vec<u8, BYTES_CAPACITY>),
}

impl PropertyValue {
    /// Builds a [`PropertyValue::String`], or `None` if `s` exceeds
    /// [`STRING_CAPACITY`] bytes.
    pub fn string(s: &str) -> Option<Self> {
        heapless::String::try_from(s)
            .ok()
            .map(PropertyValue::String)
    }

    /// Builds a [`PropertyValue::Bytes`], or `None` if `b` exceeds
    /// [`BYTES_CAPACITY`] bytes.
    pub fn bytes(b: &[u8]) -> Option<Self> {
        heapless::Vec::from_slice(b).ok().map(PropertyValue::Bytes)
    }
}

impl From<bool> for PropertyValue {
    fn from(v: bool) -> Self {
        PropertyValue::Bool(v)
    }
}

impl From<i64> for PropertyValue {
    fn from(v: i64) -> Self {
        PropertyValue::Int(v)
    }
}

impl From<f64> for PropertyValue {
    fn from(v: f64) -> Self {
        PropertyValue::Float(v)
    }
}
