# udal-sdk

Rust client SDK for [UDAL](https://github.com/paul-fleischmann-com/udal) (Universal Device
Abstraction Layer, req42.adoc §7.3, QR-08). Two independent builds, selected by Cargo feature:

- **`std`** (default) — full application- and device-side SDK over gRPC, mirroring the Go and
  Python SDKs' operation set.
- **`mqtt`** — device-side-only SDK over MQTT, `no_std` with no allocator, for bare-metal/RTOS
  targets (e.g. Cortex-M) that can't carry a gRPC/HTTP2 stack.

## `std`: application side

```rust
use udal_sdk::grpc::{Client, ClientConfig};
use udal_sdk::Value;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let mut client = Client::connect(ClientConfig::new("localhost:50051")).await?;
    let temperature = client.read_property("dev-1", "temperature").await?;
    client.write_property("dev-1", "setpoint", Value::Float(21.5)).await?;

    let mut sub = client.subscribe("dev-1", "").await?;
    while let Some(update) = sub.next().await {
        println!("{update:?}");
    }
    Ok(())
}
```

## `std`: device side

```rust
use std::sync::Arc;
use udal_sdk::grpc::{Device, DeviceConfig};

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let device = Device::connect(DeviceConfig::new("localhost:50051", "sensor-1", "temperature-sensor")).await?;
    device.on_command("reboot", Arc::new(|params| {
        println!("rebooting {params:?}");
        Ok(None)
    }));
    device.run().await?;
    Ok(())
}
```

## `mqtt`: bare-metal device side

```toml
[dependencies]
udal-sdk = { version = "0.1", default-features = false, features = ["mqtt"] }
```

```rust
use udal_sdk::mqtt::{MqttDevice, PropertyValue};

// `transport` is whatever byte stream your board's networking stack gives you,
// already connected to the broker — this crate has no socket stack of its own.
let mut device = MqttDevice::connect(transport, "sensor-1", 60)?;
device.publish_property("temperature", &PropertyValue::Float(23.5))?;
```

See `src/mqtt/mod.rs`'s module docs for the full topic convention and what's in/out of scope
(`SendCommand`/`Subscribe`/`RegisterDevice` aren't wired up on the MQTT transport yet).

## Embedded flash/RAM budget (QR-08)

`examples/embedded-size-check/` links the `mqtt` build into a minimal Cortex-M firmware image
(connect + publish one property) to verify it fits the QR-08 budget (flash < 8 KB, RAM < 2 KB).
It's a standalone crate (not a workspace member of this one — see `build.rs`'s doc comment for
why), built and measured in CI's `rust-ci` job via `cargo size`:

```bash
cd examples/embedded-size-check
cargo size --release -- -A
```

## Development

```bash
cargo build --all-features
cargo test --all-features
cargo clippy --all-features -- -D warnings
cargo fmt --check
```

See `docs/req42/req42.adoc` §7.3 for the full operation contract shared with the Go, Python, and
TypeScript SDKs.
