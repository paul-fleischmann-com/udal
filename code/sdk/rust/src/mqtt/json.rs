//! Minimal, `no_std`/no-`alloc` JSON codec for exactly one shape: a
//! single-field object holding a [`PropertyValue`] — `{"bool":true}`,
//! `{"int":42}`, `{"float":1.5}`, `{"string":"..."}`, or
//! `{"bytes":"<base64>"}`. Mirrors the gateway MQTT adapter's own
//! `wireValue` (`code/gateway/internal/adapters/mqtt/value.go`), which
//! encodes a Go `[]byte` as base64 via `encoding/json`'s default behavior —
//! this is a hand-rolled codec rather than `serde_json` because
//! `serde_json` needs an allocator, unavailable on the bare-metal targets
//! this build is for.

use crate::error::{ErrorCode, UdalError};

use super::value::PropertyValue;

fn malformed() -> UdalError {
    UdalError::new(ErrorCode::Internal, "malformed property value JSON")
}

const B64_ALPHABET: &[u8; 64] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

fn base64_encode(data: &[u8], out: &mut dyn core::fmt::Write) -> core::fmt::Result {
    let mut chunks = data.chunks_exact(3);
    for chunk in &mut chunks {
        let n = ((chunk[0] as u32) << 16) | ((chunk[1] as u32) << 8) | (chunk[2] as u32);
        out.write_char(B64_ALPHABET[(n >> 18 & 0x3F) as usize] as char)?;
        out.write_char(B64_ALPHABET[(n >> 12 & 0x3F) as usize] as char)?;
        out.write_char(B64_ALPHABET[(n >> 6 & 0x3F) as usize] as char)?;
        out.write_char(B64_ALPHABET[(n & 0x3F) as usize] as char)?;
    }
    match chunks.remainder() {
        [] => {}
        [a] => {
            let n = (*a as u32) << 16;
            out.write_char(B64_ALPHABET[(n >> 18 & 0x3F) as usize] as char)?;
            out.write_char(B64_ALPHABET[(n >> 12 & 0x3F) as usize] as char)?;
            out.write_str("==")?;
        }
        [a, b] => {
            let n = ((*a as u32) << 16) | ((*b as u32) << 8);
            out.write_char(B64_ALPHABET[(n >> 18 & 0x3F) as usize] as char)?;
            out.write_char(B64_ALPHABET[(n >> 12 & 0x3F) as usize] as char)?;
            out.write_char(B64_ALPHABET[(n >> 6 & 0x3F) as usize] as char)?;
            out.write_char('=')?;
        }
        _ => unreachable!("chunks_exact(3)'s remainder is always < 3 elements"),
    }
    Ok(())
}

fn base64_decode_char(c: u8) -> Option<u32> {
    match c {
        b'A'..=b'Z' => Some((c - b'A') as u32),
        b'a'..=b'z' => Some((c - b'a') as u32 + 26),
        b'0'..=b'9' => Some((c - b'0') as u32 + 52),
        b'+' => Some(62),
        b'/' => Some(63),
        _ => None,
    }
}

fn base64_decode(
    s: &str,
    out: &mut heapless::Vec<u8, { super::value::BYTES_CAPACITY }>,
) -> Result<(), UdalError> {
    let bytes = s.as_bytes();
    for group in bytes.chunks(4) {
        if group.len() < 2 {
            return Err(malformed());
        }
        let c0 = base64_decode_char(group[0]).ok_or_else(malformed)?;
        let c1 = base64_decode_char(group[1]).ok_or_else(malformed)?;
        let c2 = if group.len() > 2 && group[2] != b'=' {
            base64_decode_char(group[2])
        } else {
            None
        };
        let c3 = if group.len() > 3 && group[3] != b'=' {
            base64_decode_char(group[3])
        } else {
            None
        };

        let n = (c0 << 18) | (c1 << 12) | (c2.unwrap_or(0) << 6) | c3.unwrap_or(0);
        out.push(((n >> 16) & 0xFF) as u8).map_err(|_| {
            UdalError::new(ErrorCode::ResourceExhausted, "bytes value exceeds capacity")
        })?;
        if c2.is_some() {
            out.push(((n >> 8) & 0xFF) as u8).map_err(|_| {
                UdalError::new(ErrorCode::ResourceExhausted, "bytes value exceeds capacity")
            })?;
        }
        if c3.is_some() {
            out.push((n & 0xFF) as u8).map_err(|_| {
                UdalError::new(ErrorCode::ResourceExhausted, "bytes value exceeds capacity")
            })?;
        }
    }
    Ok(())
}

fn write_json_string(s: &str, out: &mut dyn core::fmt::Write) -> core::fmt::Result {
    out.write_char('"')?;
    for c in s.chars() {
        match c {
            '"' => out.write_str("\\\"")?,
            '\\' => out.write_str("\\\\")?,
            '\n' => out.write_str("\\n")?,
            '\r' => out.write_str("\\r")?,
            '\t' => out.write_str("\\t")?,
            c if (c as u32) < 0x20 => write!(out, "\\u{:04x}", c as u32)?,
            c => out.write_char(c)?,
        }
    }
    out.write_char('"')
}

fn unescape_json_string(
    raw: &str,
    out: &mut heapless::String<{ super::value::STRING_CAPACITY }>,
) -> Result<(), UdalError> {
    let mut chars = raw.chars();
    while let Some(c) = chars.next() {
        let c = if c == '\\' {
            match chars.next().ok_or_else(malformed)? {
                '"' => '"',
                '\\' => '\\',
                '/' => '/',
                'n' => '\n',
                'r' => '\r',
                't' => '\t',
                'u' => {
                    let hex: heapless::String<4> = chars.by_ref().take(4).collect();
                    if hex.len() != 4 {
                        return Err(malformed());
                    }
                    let code = u32::from_str_radix(&hex, 16).map_err(|_| malformed())?;
                    char::from_u32(code).ok_or_else(malformed)?
                }
                _ => return Err(malformed()),
            }
        } else {
            c
        };
        out.push(c).map_err(|_| {
            UdalError::new(
                ErrorCode::ResourceExhausted,
                "string value exceeds capacity",
            )
        })?;
    }
    Ok(())
}

/// Number of decimal places [`write_f64`] renders — plenty for sensor
/// telemetry, and small enough that `(v * SCALE)` stays well within `u32`
/// for any realistic property value.
const FLOAT_DECIMALS: u32 = 3;
const FLOAT_SCALE: f64 = 1_000.0; // 10^FLOAT_DECIMALS

/// Writes `v` as a fixed-point decimal with up to [`FLOAT_DECIMALS`] places
/// (trailing zeros trimmed), e.g. `23.5`, `-1.25`, `3`.
///
/// Deliberately *not* `core::fmt`'s `Display for f64` (used via `write!`) —
/// that implements the shortest-round-trip-representation algorithm
/// (Grisu3 + Dragon4 fallback, `core::num::fmt::float`), which alone is
/// upwards of 10 KB of `.text`/`.rodata` once linked in (measured via
/// `cargo size`/`cargo nm` on this crate's `examples/embedded-size-check`)
/// — blowing QR-08's 8 KB flash budget several times over for a single
/// format specifier. This trades round-trip exactness for a fixed decimal
/// precision, which is the right trade for sensor telemetry and fits in a
/// few dozen bytes instead.
///
/// Scales into `u32`, not `u64`: Cortex-M3/M4 has a hardware 32-bit
/// divide, but not a 64-bit one, so `u64` division/modulo pulls in
/// `compiler_builtins`' software `u64_div_rem` (measured: another ~850
/// bytes) — avoidable here since `u32::MAX / FLOAT_SCALE` (~4.29 million)
/// is already generous for sensor-telemetry-style property values.
fn write_f64(out: &mut dyn core::fmt::Write, v: f64) -> core::fmt::Result {
    if !v.is_finite() {
        // JSON has no NaN/Infinity literal; 0 is a safe, documented
        // fallback rather than emitting invalid JSON.
        return out.write_str("0");
    }
    let neg = v.is_sign_negative();
    // `as u32` is a saturating cast (defined behavior since Rust 1.45) —
    // caps at u32::MAX rather than the UB a C-style cast would risk, for
    // the (unrealistic, for sensor telemetry) case of |v| >= ~4.29 million.
    // `+ 0.5` rounds to nearest instead of calling `f64::round` (a
    // separate, if small, intrinsic this avoids pulling in at all).
    let scaled = (v.abs() * FLOAT_SCALE + 0.5) as u32;

    if neg && scaled != 0 {
        out.write_char('-')?;
    }
    let int_part = scaled / (FLOAT_SCALE as u32);
    let mut frac_part = scaled % (FLOAT_SCALE as u32);
    write!(out, "{int_part}")?;
    if frac_part != 0 {
        let mut digits = [0u8; FLOAT_DECIMALS as usize];
        for d in digits.iter_mut().rev() {
            *d = b'0' + (frac_part % 10) as u8;
            frac_part /= 10;
        }
        let mut end = digits.len();
        while end > 0 && digits[end - 1] == b'0' {
            end -= 1;
        }
        out.write_char('.')?;
        for &d in &digits[..end] {
            out.write_char(d as char)?;
        }
    }
    Ok(())
}

/// Encodes `value` as compact JSON (matching the gateway's own
/// `encoding/json` output byte-for-byte, modulo [`write_f64`]'s reduced
/// float precision) into `out`.
pub(crate) fn encode(value: &PropertyValue, out: &mut dyn core::fmt::Write) -> core::fmt::Result {
    match value {
        PropertyValue::Bool(b) => write!(out, "{{\"bool\":{b}}}"),
        PropertyValue::Int(i) => write!(out, "{{\"int\":{i}}}"),
        PropertyValue::Float(f) => {
            out.write_str("{\"float\":")?;
            write_f64(out, *f)?;
            out.write_char('}')
        }
        PropertyValue::String(s) => {
            out.write_str("{\"string\":")?;
            write_json_string(s, out)?;
            out.write_char('}')
        }
        PropertyValue::Bytes(b) => {
            out.write_str("{\"bytes\":\"")?;
            base64_encode(b, out)?;
            out.write_str("\"}")
        }
    }
}

/// Decodes a single-field property value object (see module docs) from
/// `json`.
pub(crate) fn decode(json: &str) -> Result<PropertyValue, UdalError> {
    let s = json.trim();
    let s = s
        .strip_prefix('{')
        .and_then(|s| s.strip_suffix('}'))
        .ok_or_else(malformed)?
        .trim();
    let s = s.strip_prefix('"').ok_or_else(malformed)?;
    let (key, rest) = s.split_once('"').ok_or_else(malformed)?;
    let rest = rest.trim().strip_prefix(':').ok_or_else(malformed)?.trim();

    match key {
        "bool" => rest
            .parse::<bool>()
            .map(PropertyValue::Bool)
            .map_err(|_| malformed()),
        "int" => rest
            .parse::<i64>()
            .map(PropertyValue::Int)
            .map_err(|_| malformed()),
        "float" => rest
            .parse::<f64>()
            .map(PropertyValue::Float)
            .map_err(|_| malformed()),
        "string" => {
            let raw = rest
                .strip_prefix('"')
                .and_then(|s| s.strip_suffix('"'))
                .ok_or_else(malformed)?;
            let mut out = heapless::String::new();
            unescape_json_string(raw, &mut out)?;
            Ok(PropertyValue::String(out))
        }
        "bytes" => {
            let raw = rest
                .strip_prefix('"')
                .and_then(|s| s.strip_suffix('"'))
                .ok_or_else(malformed)?;
            let mut out = heapless::Vec::new();
            base64_decode(raw, &mut out)?;
            Ok(PropertyValue::Bytes(out))
        }
        _ => Err(malformed()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn encode_to_string(value: &PropertyValue) -> heapless::String<128> {
        let mut out = heapless::String::new();
        encode(value, &mut out).unwrap();
        out
    }

    #[test]
    fn bool_matches_go_encoding() {
        assert_eq!(
            encode_to_string(&PropertyValue::Bool(true)),
            "{\"bool\":true}"
        );
    }

    #[test]
    fn int_matches_go_encoding() {
        assert_eq!(encode_to_string(&PropertyValue::Int(-42)), "{\"int\":-42}");
    }

    #[test]
    fn float_trims_trailing_zeros() {
        assert_eq!(
            encode_to_string(&PropertyValue::Float(23.5)),
            "{\"float\":23.5}"
        );
        assert_eq!(
            encode_to_string(&PropertyValue::Float(3.0)),
            "{\"float\":3}"
        );
    }

    #[test]
    fn float_negative_and_small_fraction() {
        assert_eq!(
            encode_to_string(&PropertyValue::Float(-1.25)),
            "{\"float\":-1.25}"
        );
        assert_eq!(
            encode_to_string(&PropertyValue::Float(-0.5)),
            "{\"float\":-0.5}"
        );
    }

    #[test]
    fn float_nan_and_infinite_fall_back_to_zero() {
        assert_eq!(
            encode_to_string(&PropertyValue::Float(f64::NAN)),
            "{\"float\":0}"
        );
        assert_eq!(
            encode_to_string(&PropertyValue::Float(f64::INFINITY)),
            "{\"float\":0}"
        );
    }

    #[test]
    fn string_escapes_quotes_and_control_chars() {
        let v = PropertyValue::string("a\"b\nc").unwrap();
        assert_eq!(encode_to_string(&v), "{\"string\":\"a\\\"b\\nc\"}");
    }

    #[test]
    fn bytes_encodes_as_standard_base64() {
        let v = PropertyValue::bytes(b"foobar").unwrap();
        // Known-good vector (RFC 4648 test vectors).
        assert_eq!(encode_to_string(&v), "{\"bytes\":\"Zm9vYmFy\"}");
    }

    #[test]
    fn bytes_padding_one_and_two_bytes_remainder() {
        assert_eq!(
            encode_to_string(&PropertyValue::bytes(b"fo").unwrap()),
            "{\"bytes\":\"Zm8=\"}"
        );
        assert_eq!(
            encode_to_string(&PropertyValue::bytes(b"f").unwrap()),
            "{\"bytes\":\"Zg==\"}"
        );
    }

    #[test]
    fn decode_roundtrips_every_variant() {
        for v in [
            PropertyValue::Bool(false),
            PropertyValue::Int(123456789),
            PropertyValue::Float(2.5),
            PropertyValue::string("hello").unwrap(),
            PropertyValue::bytes(b"foobar").unwrap(),
        ] {
            let encoded = encode_to_string(&v);
            assert_eq!(decode(&encoded).unwrap(), v);
        }
    }

    #[test]
    fn decode_rejects_unknown_key() {
        assert!(decode("{\"nope\":1}").is_err());
    }

    #[test]
    fn decode_rejects_malformed_json() {
        assert!(decode("not json").is_err());
    }
}
