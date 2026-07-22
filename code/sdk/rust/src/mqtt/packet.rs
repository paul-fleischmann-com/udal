//! Minimal MQTT v3.1.1 packet encode/decode — just enough for
//! [`super::MqttDevice`]: `CONNECT`/`CONNACK`, `PUBLISH` (QoS 0 only, both
//! directions), `SUBSCRIBE`/`SUBACK`, and `PINGREQ`/`PINGRESP`. No `alloc`:
//! every function writes into (or reads from) a caller-provided `&mut
//! [u8]`.
//!
//! Deliberately not a general-purpose MQTT client: no QoS 1/2, no
//! will/username/password, no multi-frame reassembly (a `PUBLISH` payload
//! larger than the caller's read buffer is rejected rather than
//! reassembled — property values are expected to be small). See
//! `code/gateway/internal/adapters/mqtt/topics.go` for the topic
//! convention this talks to.

use crate::error::{ErrorCode, UdalError};

const CONNECT: u8 = 0x10;
const CONNACK: u8 = 0x20;
const PUBLISH_MASK: u8 = 0x30;
const SUBSCRIBE: u8 = 0x82;
const SUBACK: u8 = 0x90;
const PINGREQ: u8 = 0xC0;
const PINGRESP: u8 = 0xD0;
const DISCONNECT: u8 = 0xE0;

fn too_small() -> UdalError {
    UdalError::new(
        ErrorCode::ResourceExhausted,
        "buffer too small for MQTT packet",
    )
}

fn malformed() -> UdalError {
    UdalError::new(ErrorCode::Internal, "malformed MQTT packet")
}

/// Writes `len` as an MQTT variable-length integer (1-4 bytes) into `out`,
/// returning the number of bytes written.
fn encode_remaining_length(mut len: usize, out: &mut [u8]) -> Result<usize, UdalError> {
    let mut i = 0;
    loop {
        if i >= out.len() {
            return Err(too_small());
        }
        let mut byte = (len % 128) as u8;
        len /= 128;
        if len > 0 {
            byte |= 0x80;
        }
        out[i] = byte;
        i += 1;
        if len == 0 {
            return Ok(i);
        }
    }
}

/// Decodes an MQTT variable-length integer from the start of `buf`,
/// returning `(value, bytes_consumed)`, or `None` if `buf` doesn't yet
/// contain a complete one (need more bytes) — up to 4 bytes are inspected,
/// matching the protocol's own encoding limit.
fn decode_remaining_length(buf: &[u8]) -> Option<(usize, usize)> {
    let mut value = 0usize;
    let mut multiplier = 1usize;
    for (i, &byte) in buf.iter().enumerate().take(4) {
        value += (byte & 0x7F) as usize * multiplier;
        if byte & 0x80 == 0 {
            return Some((value, i + 1));
        }
        multiplier *= 128;
    }
    None
}

fn write_str(out: &mut [u8], pos: &mut usize, s: &str) -> Result<(), UdalError> {
    let bytes = s.as_bytes();
    if bytes.len() > u16::MAX as usize {
        return Err(UdalError::new(
            ErrorCode::InvalidArgument,
            "string exceeds 65535 bytes",
        ));
    }
    let needed = 2 + bytes.len();
    if out.len() < *pos + needed {
        return Err(too_small());
    }
    let len = bytes.len() as u16;
    out[*pos..*pos + 2].copy_from_slice(&len.to_be_bytes());
    out[*pos + 2..*pos + 2 + bytes.len()].copy_from_slice(bytes);
    *pos += needed;
    Ok(())
}

/// Encodes a `CONNECT` packet (clean session, no will/username/password)
/// into `out`, returning the number of bytes written.
pub(crate) fn encode_connect(
    out: &mut [u8],
    client_id: &str,
    keep_alive_secs: u16,
) -> Result<usize, UdalError> {
    // Variable header + payload, built first so its length is known before
    // the fixed header's remaining-length field is written.
    let mut body = [0u8; 256];
    let mut pos = 0;
    const PROTOCOL_NAME: &str = "MQTT";
    write_str(&mut body, &mut pos, PROTOCOL_NAME)?;
    if body.len() < pos + 4 {
        return Err(too_small());
    }
    body[pos] = 0x04; // protocol level: MQTT 3.1.1
    body[pos + 1] = 0x02; // connect flags: clean session
    body[pos + 2..pos + 4].copy_from_slice(&keep_alive_secs.to_be_bytes());
    pos += 4;
    write_str(&mut body, &mut pos, client_id)?;

    let mut header = [0u8; 5];
    let header_len = encode_remaining_length(pos, &mut header)?;
    let total = 1 + header_len + pos;
    if out.len() < total {
        return Err(too_small());
    }
    out[0] = CONNECT;
    out[1..1 + header_len].copy_from_slice(&header[..header_len]);
    out[1 + header_len..total].copy_from_slice(&body[..pos]);
    Ok(total)
}

/// Encodes a QoS-0 `PUBLISH` packet (topic + payload, no packet
/// identifier) into `out`, returning the number of bytes written.
pub(crate) fn encode_publish(
    out: &mut [u8],
    topic: &str,
    payload: &[u8],
    retain: bool,
) -> Result<usize, UdalError> {
    let topic_bytes = topic.as_bytes();
    if topic_bytes.len() > u16::MAX as usize {
        return Err(UdalError::new(
            ErrorCode::InvalidArgument,
            "topic exceeds 65535 bytes",
        ));
    }
    let var_header_len = 2 + topic_bytes.len();
    let remaining = var_header_len + payload.len();

    let mut header = [0u8; 5];
    let header_len = encode_remaining_length(remaining, &mut header)?;
    let total = 1 + header_len + remaining;
    if out.len() < total {
        return Err(too_small());
    }

    out[0] = PUBLISH_MASK | if retain { 0x01 } else { 0x00 };
    out[1..1 + header_len].copy_from_slice(&header[..header_len]);
    let mut pos = 1 + header_len;
    out[pos..pos + 2].copy_from_slice(&(topic_bytes.len() as u16).to_be_bytes());
    pos += 2;
    out[pos..pos + topic_bytes.len()].copy_from_slice(topic_bytes);
    pos += topic_bytes.len();
    out[pos..pos + payload.len()].copy_from_slice(payload);
    Ok(total)
}

/// Encodes a `SUBSCRIBE` packet requesting QoS 0 for a single `topic`,
/// into `out`, returning the number of bytes written.
pub(crate) fn encode_subscribe(
    out: &mut [u8],
    packet_id: u16,
    topic: &str,
) -> Result<usize, UdalError> {
    let topic_bytes = topic.as_bytes();
    if topic_bytes.len() > u16::MAX as usize {
        return Err(UdalError::new(
            ErrorCode::InvalidArgument,
            "topic exceeds 65535 bytes",
        ));
    }
    let remaining = 2 + 2 + topic_bytes.len() + 1;
    let mut header = [0u8; 5];
    let header_len = encode_remaining_length(remaining, &mut header)?;
    let total = 1 + header_len + remaining;
    if out.len() < total {
        return Err(too_small());
    }

    out[0] = SUBSCRIBE;
    out[1..1 + header_len].copy_from_slice(&header[..header_len]);
    let mut pos = 1 + header_len;
    out[pos..pos + 2].copy_from_slice(&packet_id.to_be_bytes());
    pos += 2;
    out[pos..pos + 2].copy_from_slice(&(topic_bytes.len() as u16).to_be_bytes());
    pos += 2;
    out[pos..pos + topic_bytes.len()].copy_from_slice(topic_bytes);
    pos += topic_bytes.len();
    out[pos] = 0x00; // requested QoS 0
    Ok(total)
}

/// Encodes a `PINGREQ` packet into `out`, returning the number of bytes
/// written (always 2).
pub(crate) fn encode_pingreq(out: &mut [u8]) -> Result<usize, UdalError> {
    if out.len() < 2 {
        return Err(too_small());
    }
    out[0] = PINGREQ;
    out[1] = 0x00;
    Ok(2)
}

/// Encodes a `DISCONNECT` packet into `out`, returning the number of bytes
/// written (always 2).
pub(crate) fn encode_disconnect(out: &mut [u8]) -> Result<usize, UdalError> {
    if out.len() < 2 {
        return Err(too_small());
    }
    out[0] = DISCONNECT;
    out[1] = 0x00;
    Ok(2)
}

/// One decoded incoming packet, borrowing from the buffer it was decoded
/// from.
#[derive(Debug, PartialEq, Eq)]
pub(crate) enum Incoming<'a> {
    ConnAck {
        accepted: bool,
    },
    Publish {
        topic: &'a str,
        payload: &'a [u8],
    },
    SubAck,
    PingResp,
    /// Recognized packet type this client never needs to act on (e.g. a
    /// `PUBACK` for a QoS-1 publish this client never sends).
    Other,
}

/// Attempts to decode one complete packet from the start of `buf`.
/// Returns `Ok(None)` if `buf` doesn't yet contain a full packet (the
/// caller should read more bytes and retry) rather than an error — this
/// is the normal, expected state while streaming bytes off a socket.
pub(crate) fn decode(buf: &[u8]) -> Result<Option<(Incoming<'_>, usize)>, UdalError> {
    if buf.is_empty() {
        return Ok(None);
    }
    let packet_type = buf[0];
    let Some((remaining_len, header_len)) = decode_remaining_length(&buf[1..]) else {
        return Ok(None);
    };
    let header_len = 1 + header_len;
    let total = header_len + remaining_len;
    if buf.len() < total {
        return Ok(None);
    }
    let body = &buf[header_len..total];

    let incoming = match packet_type {
        CONNACK => {
            if body.len() < 2 {
                return Err(malformed());
            }
            Incoming::ConnAck {
                accepted: body[1] == 0x00,
            }
        }
        t if t & 0xF0 == PUBLISH_MASK => {
            if body.len() < 2 {
                return Err(malformed());
            }
            let topic_len = u16::from_be_bytes([body[0], body[1]]) as usize;
            let qos = (t >> 1) & 0x03;
            let mut pos = 2 + topic_len;
            if body.len() < pos {
                return Err(malformed());
            }
            let topic = core::str::from_utf8(&body[2..pos]).map_err(|_| malformed())?;
            if qos > 0 {
                pos += 2; // skip packet identifier — this client never subscribes above QoS 0
            }
            if body.len() < pos {
                return Err(malformed());
            }
            Incoming::Publish {
                topic,
                payload: &body[pos..],
            }
        }
        SUBACK => Incoming::SubAck,
        PINGRESP => Incoming::PingResp,
        _ => Incoming::Other,
    };
    Ok(Some((incoming, total)))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn remaining_length_roundtrips_small_and_large() {
        for len in [0usize, 1, 127, 128, 16383, 16384, 2097151] {
            let mut buf = [0u8; 4];
            let n = encode_remaining_length(len, &mut buf).unwrap();
            assert_eq!(decode_remaining_length(&buf[..n]), Some((len, n)));
        }
    }

    #[test]
    fn connect_then_decode_connack() {
        let mut buf = [0u8; 64];
        let n = encode_connect(&mut buf, "dev-1", 60).unwrap();
        assert_eq!(buf[0], CONNECT);

        let connack = [CONNACK, 0x02, 0x00, 0x00];
        let (pkt, consumed) = decode(&connack).unwrap().unwrap();
        assert_eq!(pkt, Incoming::ConnAck { accepted: true });
        assert_eq!(consumed, 4);
        let _ = n;
    }

    #[test]
    fn connack_refused_is_not_accepted() {
        let connack = [CONNACK, 0x02, 0x00, 0x05]; // 0x05 = not authorized
        let (pkt, _) = decode(&connack).unwrap().unwrap();
        assert_eq!(pkt, Incoming::ConnAck { accepted: false });
    }

    #[test]
    fn publish_roundtrips_topic_and_payload() {
        let mut buf = [0u8; 128];
        let n = encode_publish(&mut buf, "udal/dev-1/props/temperature", b"23.5", false).unwrap();
        let (pkt, consumed) = decode(&buf[..n]).unwrap().unwrap();
        assert_eq!(consumed, n);
        match pkt {
            Incoming::Publish { topic, payload } => {
                assert_eq!(topic, "udal/dev-1/props/temperature");
                assert_eq!(payload, b"23.5");
            }
            other => panic!("expected Publish, got {other:?}"),
        }
    }

    #[test]
    fn incomplete_packet_returns_none_not_error() {
        // A CONNACK's fixed header claims 2 remaining bytes, but only 1 is
        // actually present yet.
        let partial = [CONNACK, 0x02, 0x00];
        assert_eq!(decode(&partial).unwrap(), None);
    }

    #[test]
    fn subscribe_encodes_expected_bytes() {
        let mut buf = [0u8; 64];
        let n = encode_subscribe(&mut buf, 1, "udal/dev-1/props/+/set").unwrap();
        assert_eq!(buf[0], SUBSCRIBE);
        // packet id
        assert_eq!(
            &buf[n - "udal/dev-1/props/+/set".len() - 1..n - 1],
            b"udal/dev-1/props/+/set"
        );
        assert_eq!(buf[n - 1], 0x00); // requested QoS
    }

    #[test]
    fn pingreq_is_two_bytes() {
        let mut buf = [0u8; 8];
        let n = encode_pingreq(&mut buf).unwrap();
        assert_eq!(&buf[..n], &[PINGREQ, 0x00]);
    }

    #[test]
    fn pingresp_decodes() {
        let resp = [PINGRESP, 0x00];
        let (pkt, consumed) = decode(&resp).unwrap().unwrap();
        assert_eq!(pkt, Incoming::PingResp);
        assert_eq!(consumed, 2);
    }

    #[test]
    fn buffer_too_small_is_an_error_not_a_panic() {
        let mut buf = [0u8; 4];
        let err = encode_connect(&mut buf, "a-fairly-long-client-id", 60).unwrap_err();
        assert_eq!(err.code, ErrorCode::ResourceExhausted);
    }
}
