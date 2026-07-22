//! Device-side-only SDK over MQTT (QR-08): `no_std`, no allocator, for
//! bare-metal/RTOS targets that can't carry a gRPC/HTTP2 stack. Talks the
//! same topic convention as the gateway's MQTT transport adapter (see
//! `code/gateway/internal/adapters/mqtt/topics.go`):
//!
//! ```text
//! udal/{deviceId}/props/{path}       device publishes value      (this SDK: publish_property)
//! udal/{deviceId}/props/{path}/get   gateway requests value      (this SDK: reported by poll(), answer with publish_property)
//! udal/{deviceId}/props/{path}/set   gateway writes value        (this SDK: reported by poll())
//! udal/{deviceId}/props/{path}/set/ack  device confirms a write  (this SDK: publish_set_ack)
//! udal/{deviceId}/status              device heartbeat           (this SDK: publish_status)
//! ```
//!
//! `SendCommand`/`Subscribe` (req42.adoc §7.3) aren't covered here — the
//! gateway's MQTT adapter doesn't route commands over MQTT yet. Nor is
//! `RegisterDevice`: there's no MQTT-side registration RPC, only the
//! heartbeat topic above (which the adapter itself currently documents as
//! "not consumed yet") — [`MqttDevice::publish_status`] is the closest
//! analog available on this transport. Devices that need real
//! registration are expected to get it out of band (e.g. via the `std`
//! build's [`crate::grpc::Client`] from a provisioning tool, or
//! pre-configured on the gateway).
//!
//! Generic over any transport implementing [`embedded_io::Read`] +
//! [`embedded_io::Write`] — this crate has no socket/TLS/Wi-Fi stack of
//! its own (there isn't one universal bare-metal choice), so the firmware
//! supplies an already-connected byte stream to the broker.

mod json;
mod packet;
mod value;

pub use value::{PropertyValue, BYTES_CAPACITY, STRING_CAPACITY};

use crate::error::{ErrorCode, UdalError};

/// Capacity of an outbound topic string this SDK builds (e.g.
/// `"udal/{device_id}/props/{path}/set/ack"`) — generous for a
/// short device ID and a nested property path.
const TOPIC_CAPACITY: usize = 96;

/// Which of the gateway's MQTT adapter's three property topic kinds an
/// incoming `PUBLISH` matched (see the module docs' topic table).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TopicKind {
    /// `.../props/{path}/get` — the gateway is requesting the current
    /// value; respond with [`MqttDevice::publish_property`].
    Get,
    /// `.../props/{path}/set` — the gateway is writing a new value;
    /// after applying it, respond with [`MqttDevice::publish_set_ack`].
    Set,
    /// `.../props/{path}` bare — not expected as an *incoming* message for
    /// this device's own topics (this device is the one who publishes
    /// those), but recognized in case the transport is shared/misconfigured.
    Bare,
}

/// Extracts `(path, kind)` from `topic`, if it's one of `device_id`'s own
/// property topics — mirrors
/// `code/gateway/internal/adapters/mqtt/topics.go`'s `parsePropsTopic`.
/// Returns `None` for any other topic (e.g. another device's, or this
/// device's own `/status`).
pub fn parse_property_topic<'a>(device_id: &str, topic: &'a str) -> Option<(&'a str, TopicKind)> {
    let mut parts = topic.splitn(4, '/');
    if parts.next()? != "udal" {
        return None;
    }
    if parts.next()? != device_id {
        return None;
    }
    if parts.next()? != "props" {
        return None;
    }
    let rest = parts.next()?;
    if rest.is_empty() {
        return None;
    }
    if let Some(path) = rest.strip_suffix("/get") {
        Some((path, TopicKind::Get))
    } else if let Some(path) = rest.strip_suffix("/set") {
        Some((path, TopicKind::Set))
    } else {
        Some((rest, TopicKind::Bare))
    }
}

/// One event surfaced by [`MqttDevice::poll`].
#[derive(Debug, PartialEq, Eq)]
pub enum Event<'a> {
    /// A `PUBLISH` on a topic matching one of this device's own property
    /// topics (see [`parse_property_topic`]) arrived, with the raw JSON
    /// payload (see [`PropertyValue`]'s codec in the `json` submodule) —
    /// decode it with [`decode_payload`].
    PropertyRequest {
        path: &'a str,
        kind: TopicKind,
        payload: &'a [u8],
    },
    /// The broker answered a [`MqttDevice::ping`].
    Pong,
    /// A recognized but not-otherwise-actionable packet (e.g. a `SUBACK`).
    Other,
}

fn io_err<E>(_e: E) -> UdalError {
    UdalError::new(ErrorCode::Unavailable, "I/O error")
}

fn topic_capacity_err() -> UdalError {
    UdalError::new(ErrorCode::ResourceExhausted, "topic exceeds capacity")
}

fn build_topic(
    parts: core::fmt::Arguments<'_>,
) -> Result<heapless::String<TOPIC_CAPACITY>, UdalError> {
    use core::fmt::Write as _;
    let mut s = heapless::String::new();
    s.write_fmt(parts).map_err(|_| topic_capacity_err())?;
    Ok(s)
}

/// The `mqtt`-feature device-side SDK: a thin, `no_std`, no-allocator MQTT
/// v3.1.1 client bound to one device ID.
pub struct MqttDevice<T> {
    transport: T,
    device_id: heapless::String<64>,
    next_packet_id: u16,
}

impl<T> MqttDevice<T>
where
    T: embedded_io::Read + embedded_io::Write,
{
    /// Opens an MQTT session over `transport` (already connected to the
    /// broker by the caller) with a clean session and no
    /// will/username/password, registering as `device_id`.
    pub fn connect(
        mut transport: T,
        device_id: &str,
        keep_alive_secs: u16,
    ) -> Result<Self, UdalError> {
        let mut buf = [0u8; 128];
        let n = packet::encode_connect(&mut buf, device_id, keep_alive_secs)?;
        transport.write_all(&buf[..n]).map_err(io_err)?;

        let mut resp = [0u8; 8];
        let mut filled = 0;
        loop {
            if filled >= resp.len() {
                return Err(UdalError::new(
                    ErrorCode::Internal,
                    "CONNACK longer than expected",
                ));
            }
            let read = transport.read(&mut resp[filled..]).map_err(io_err)?;
            if read == 0 {
                return Err(UdalError::new(
                    ErrorCode::Unavailable,
                    "connection closed during CONNECT",
                ));
            }
            filled += read;
            if let Some((pkt, _consumed)) = packet::decode(&resp[..filled])? {
                match pkt {
                    packet::Incoming::ConnAck { accepted: true } => break,
                    packet::Incoming::ConnAck { accepted: false } => {
                        return Err(UdalError::new(
                            ErrorCode::PermissionDenied,
                            "broker rejected CONNECT",
                        ))
                    }
                    _ => return Err(UdalError::new(ErrorCode::Internal, "expected CONNACK")),
                }
            }
        }

        let device_id = heapless::String::try_from(device_id)
            .map_err(|_| UdalError::new(ErrorCode::InvalidArgument, "device id too long"))?;
        Ok(Self {
            transport,
            device_id,
            next_packet_id: 1,
        })
    }

    /// Publishes `online` to `udal/{device_id}/status` — the gateway MQTT
    /// adapter's heartbeat topic (`code/gateway/internal/adapters/mqtt/
    /// topics.go`'s `topicStatus`, currently documented there as "not
    /// consumed yet"). There's no MQTT-side registration RPC (unlike the
    /// `std`/gRPC build's `RegisterDevice`) for this to report through
    /// instead, so this is the closest analog to "announce this device is
    /// online" the existing wire protocol has — call it once after
    /// [`MqttDevice::connect`] succeeds.
    pub fn publish_status(&mut self, online: bool) -> Result<(), UdalError> {
        let topic = build_topic(format_args!("udal/{}/status", self.device_id))?;
        self.publish_raw(&topic, &PropertyValue::Bool(online))
    }

    /// Gracefully closes the session (`DISCONNECT`), consuming `self` and
    /// returning the underlying transport.
    pub fn disconnect(mut self) -> Result<T, UdalError> {
        let mut buf = [0u8; 2];
        let n = packet::encode_disconnect(&mut buf)?;
        self.transport.write_all(&buf[..n]).map_err(io_err)?;
        Ok(self.transport)
    }

    /// Publishes `value` to this device's property at `path`
    /// (`udal/{device_id}/props/{path}`), QoS 0.
    pub fn publish_property(&mut self, path: &str, value: &PropertyValue) -> Result<(), UdalError> {
        let topic = build_topic(format_args!("udal/{}/props/{path}", self.device_id))?;
        self.publish_raw(&topic, value)
    }

    /// Acknowledges a `.../set` write at `path` — call after applying the
    /// value from an [`Event::PropertyRequest`] with
    /// [`TopicKind::Set`].
    pub fn publish_set_ack(&mut self, path: &str, value: &PropertyValue) -> Result<(), UdalError> {
        let topic = build_topic(format_args!("udal/{}/props/{path}/set", self.device_id))?;
        self.publish_raw(&topic, value)
    }

    fn publish_raw(&mut self, topic: &str, value: &PropertyValue) -> Result<(), UdalError> {
        let mut payload: heapless::String<160> = heapless::String::new();
        json::encode(value, &mut payload).map_err(|_| topic_capacity_err())?;

        let mut buf = [0u8; 256];
        let n = packet::encode_publish(&mut buf, topic, payload.as_bytes(), false)?;
        self.transport.write_all(&buf[..n]).map_err(io_err)
    }

    /// Subscribes to `topic` (QoS 0) — typically one of this device's own
    /// `.../get` or `.../set` topics, built with e.g.
    /// `format_args!("udal/{device_id}/props/{path}/set")`.
    pub fn subscribe(&mut self, topic: &str) -> Result<(), UdalError> {
        let mut buf = [0u8; 128];
        let packet_id = self.next_packet_id;
        self.next_packet_id = self.next_packet_id.wrapping_add(1).max(1);
        let n = packet::encode_subscribe(&mut buf, packet_id, topic)?;
        self.transport.write_all(&buf[..n]).map_err(io_err)
    }

    /// Sends a `PINGREQ` — call periodically (within the broker's
    /// keep-alive interval passed to [`MqttDevice::connect`]) to keep the
    /// session alive; the caller drives its own timer, since `no_std` has
    /// no universal one.
    pub fn ping(&mut self) -> Result<(), UdalError> {
        let mut buf = [0u8; 2];
        let n = packet::encode_pingreq(&mut buf)?;
        self.transport.write_all(&buf[..n]).map_err(io_err)
    }

    /// Reads and decodes the next packet out of `buf` (a caller-owned
    /// scratch buffer — sized for the largest `PUBLISH` payload the
    /// caller expects), if a complete one is available. Returns `Ok(None)`
    /// on a would-block/no-data read of `0` bytes from a non-blocking
    /// transport, or `Ok(Some(Event::Other))` for a recognized packet this
    /// device has no reaction to (e.g. a `SUBACK`) — either way, call
    /// again after the transport has more data.
    ///
    /// `buf` accumulates across calls internally via `read`'s return
    /// value, so pass the *same* buffer on every call for one connection.
    pub fn poll<'b>(&mut self, buf: &'b mut [u8]) -> Result<Option<Event<'b>>, UdalError> {
        let read = self.transport.read(buf).map_err(io_err)?;
        if read == 0 {
            return Ok(None);
        }
        let Some((pkt, _consumed)) = packet::decode(&buf[..read])? else {
            return Ok(None);
        };
        Ok(Some(match pkt {
            packet::Incoming::Publish { topic, payload } => {
                match parse_property_topic(&self.device_id, topic) {
                    Some((path, kind)) => Event::PropertyRequest {
                        path,
                        kind,
                        payload,
                    },
                    None => Event::Other,
                }
            }
            packet::Incoming::PingResp => Event::Pong,
            _ => Event::Other,
        }))
    }
}

/// Decodes an [`Event::PropertyRequest`]'s raw JSON payload (see the
/// module docs' `json` codec). A free function, not an associated one on
/// [`MqttDevice`] — it doesn't touch the transport, so callers shouldn't
/// need to name `T` just to call it.
pub fn decode_payload(payload: &[u8]) -> Result<PropertyValue, UdalError> {
    let text = core::str::from_utf8(payload)
        .map_err(|_| UdalError::new(ErrorCode::Internal, "payload is not valid UTF-8"))?;
    json::decode(text)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_get_topic() {
        assert_eq!(
            parse_property_topic("dev-1", "udal/dev-1/props/temperature/get"),
            Some(("temperature", TopicKind::Get))
        );
    }

    #[test]
    fn parses_set_topic_with_nested_path() {
        assert_eq!(
            parse_property_topic("dev-1", "udal/dev-1/props/sensor/temperature/set"),
            Some(("sensor/temperature", TopicKind::Set))
        );
    }

    #[test]
    fn parses_bare_topic() {
        assert_eq!(
            parse_property_topic("dev-1", "udal/dev-1/props/temperature"),
            Some(("temperature", TopicKind::Bare))
        );
    }

    #[test]
    fn rejects_other_devices_topics() {
        assert_eq!(
            parse_property_topic("dev-1", "udal/dev-2/props/temperature"),
            None
        );
    }

    #[test]
    fn rejects_status_topic() {
        assert_eq!(parse_property_topic("dev-1", "udal/dev-1/status"), None);
    }

    /// An in-memory `embedded_io::Read + Write` for testing [`MqttDevice`]
    /// without a real socket: `read` hands back whatever's been queued via
    /// `enqueue` (FIFO, one whole queue per call — good enough for tests
    /// that control exactly what "arrives" and when), and `write` records
    /// everything sent so a test can decode it back with [`packet::decode`].
    struct MockTransport {
        written: heapless::Vec<u8, 512>,
        read_queue: heapless::Vec<u8, 256>,
    }

    impl MockTransport {
        fn new() -> Self {
            Self {
                written: heapless::Vec::new(),
                read_queue: heapless::Vec::new(),
            }
        }

        fn enqueue(&mut self, bytes: &[u8]) {
            self.read_queue
                .extend_from_slice(bytes)
                .expect("test read queue capacity");
        }
    }

    impl embedded_io::ErrorType for MockTransport {
        type Error = core::convert::Infallible;
    }

    impl embedded_io::Read for MockTransport {
        fn read(&mut self, buf: &mut [u8]) -> Result<usize, Self::Error> {
            let n = self.read_queue.len().min(buf.len());
            buf[..n].copy_from_slice(&self.read_queue[..n]);
            let remaining: heapless::Vec<u8, 256> = self.read_queue[n..].iter().copied().collect();
            self.read_queue = remaining;
            Ok(n)
        }
    }

    impl embedded_io::Write for MockTransport {
        fn write(&mut self, buf: &[u8]) -> Result<usize, Self::Error> {
            self.written
                .extend_from_slice(buf)
                .expect("test write buffer capacity");
            Ok(buf.len())
        }

        fn flush(&mut self) -> Result<(), Self::Error> {
            Ok(())
        }
    }

    const CONNACK_ACCEPTED: [u8; 4] = [0x20, 0x02, 0x00, 0x00];

    #[test]
    fn connect_rejects_a_refused_connack() {
        let mut t = MockTransport::new();
        t.enqueue(&[0x20, 0x02, 0x00, 0x05]); // CONNACK, not authorized
        let err = match MqttDevice::connect(t, "dev-1", 60) {
            Err(e) => e,
            Ok(_) => panic!("expected connect to fail"),
        };
        assert_eq!(err.code, ErrorCode::PermissionDenied);
    }

    #[test]
    fn publish_property_and_status_are_written_and_decodable() {
        let mut t = MockTransport::new();
        t.enqueue(&CONNACK_ACCEPTED);
        let mut device = MqttDevice::connect(t, "dev-1", 60).unwrap();

        device
            .publish_property("temperature", &PropertyValue::Float(23.5))
            .unwrap();
        device.publish_status(true).unwrap();
        let transport = device.disconnect().unwrap();

        let mut buf = &transport.written[..];
        let mut publishes =
            heapless::Vec::<(heapless::String<64>, heapless::Vec<u8, 64>), 4>::new();
        while let Some((pkt, consumed)) = packet::decode(buf).unwrap() {
            if let packet::Incoming::Publish { topic, payload } = pkt {
                publishes
                    .push((
                        heapless::String::try_from(topic).unwrap(),
                        heapless::Vec::from_slice(payload).unwrap(),
                    ))
                    .unwrap();
            }
            buf = &buf[consumed..];
        }

        assert_eq!(publishes.len(), 2);
        assert_eq!(publishes[0].0.as_str(), "udal/dev-1/props/temperature");
        assert_eq!(
            decode_payload(&publishes[0].1).unwrap(),
            PropertyValue::Float(23.5)
        );
        assert_eq!(publishes[1].0.as_str(), "udal/dev-1/status");
        assert_eq!(
            decode_payload(&publishes[1].1).unwrap(),
            PropertyValue::Bool(true)
        );
    }

    #[test]
    fn poll_surfaces_a_set_request_for_this_devices_topic() {
        let mut t = MockTransport::new();
        t.enqueue(&CONNACK_ACCEPTED);
        let mut device = MqttDevice::connect(t, "dev-1", 60).unwrap();

        let mut publish_buf = [0u8; 128];
        let n = packet::encode_publish(
            &mut publish_buf,
            "udal/dev-1/props/temperature/set",
            b"{\"float\":21}",
            false,
        )
        .unwrap();
        device.transport.enqueue(&publish_buf[..n]);

        let mut poll_buf = [0u8; 128];
        match device.poll(&mut poll_buf).unwrap().unwrap() {
            Event::PropertyRequest {
                path,
                kind,
                payload,
            } => {
                assert_eq!(path, "temperature");
                assert_eq!(kind, TopicKind::Set);
                assert_eq!(decode_payload(payload).unwrap(), PropertyValue::Float(21.0));
            }
            other => panic!("expected PropertyRequest, got {other:?}"),
        }
    }

    #[test]
    fn poll_ignores_another_devices_topic() {
        let mut t = MockTransport::new();
        t.enqueue(&CONNACK_ACCEPTED);
        let mut device = MqttDevice::connect(t, "dev-1", 60).unwrap();

        let mut publish_buf = [0u8; 128];
        let n = packet::encode_publish(
            &mut publish_buf,
            "udal/dev-2/props/temperature",
            b"{\"float\":1}",
            false,
        )
        .unwrap();
        device.transport.enqueue(&publish_buf[..n]);

        let mut poll_buf = [0u8; 128];
        assert_eq!(device.poll(&mut poll_buf).unwrap(), Some(Event::Other));
    }
}
