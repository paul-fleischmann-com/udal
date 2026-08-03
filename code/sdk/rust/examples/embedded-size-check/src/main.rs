//! QR-08 flash/RAM budget check: links `udal-sdk`'s `no_std`, MQTT-only
//! (`mqtt` feature) build into a minimal Cortex-M firmware image and
//! exercises its main code paths (connect, publish a property) so the
//! linked size reflects real usage rather than an empty `main`. Measured
//! with `cargo size` in CI (see `.github/workflows/ci.yml`'s `rust-ci`
//! job) against the acceptance criterion: flash < 8 KB, RAM < 2 KB.
//!
//! `NullTransport` stands in for a real board's already-connected
//! TCP/TLS/Wi-Fi byte stream (this SDK has no socket stack of its own —
//! see `code/sdk/rust/src/mqtt/mod.rs`'s module docs) — just enough to let
//! `MqttDevice::connect` and `publish_property` run their real encoding
//! logic against something.
#![no_std]
#![no_main]

use cortex_m_rt::entry;
use panic_halt as _;

use udal_sdk::mqtt::{MqttDevice, PropertyValue};

struct NullTransport;

impl embedded_io::ErrorType for NullTransport {
    type Error = core::convert::Infallible;
}

impl embedded_io::Read for NullTransport {
    fn read(&mut self, buf: &mut [u8]) -> Result<usize, Self::Error> {
        // A canned CONNACK (accepted) — enough to let `connect` complete
        // its handshake without a real broker.
        const CONNACK: [u8; 4] = [0x20, 0x02, 0x00, 0x00];
        let n = CONNACK.len().min(buf.len());
        buf[..n].copy_from_slice(&CONNACK[..n]);
        Ok(n)
    }
}

impl embedded_io::Write for NullTransport {
    fn write(&mut self, buf: &[u8]) -> Result<usize, Self::Error> {
        Ok(buf.len())
    }

    fn flush(&mut self) -> Result<(), Self::Error> {
        Ok(())
    }
}

#[entry]
fn main() -> ! {
    if let Ok(mut device) = MqttDevice::connect(NullTransport, "dev-1", 60) {
        let _ = device.publish_property("temperature", &PropertyValue::Float(23.5));
    }
    loop {
        cortex_m::asm::wfi();
    }
}
