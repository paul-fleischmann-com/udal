//! Device-side SDK (req42.adoc §7.3): registers with a gateway, publishes
//! property values, and handles incoming commands. Mirrors
//! `code/sdk/go/device.go` and `code/sdk/python/src/udal/device.py`.
//!
//! Devices using this (`std`) build connect directly over gRPC — there is
//! no transport adapter in between — so commands are delivered over
//! `StreamCommands` rather than through MQTT/HTTP/CAN. A `no_std`,
//! MQTT-only device build is available under the `mqtt` feature (see
//! [`crate::mqtt`]) for bare-metal targets that can't carry a gRPC/HTTP2
//! stack (QR-08).

use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use tonic::service::interceptor::InterceptedService;
use tonic::transport::Channel;
use tonic::{Code, Request};

use crate::error::UdalError;
use crate::value::Value;

use super::config::DeviceConfig;
use super::convert::value_to_proto;
use super::dial::{api_key_header, dial, AuthInterceptor};
use super::params::Params;
use super::pb;

type Stub =
    pb::device_service_client::DeviceServiceClient<InterceptedService<Channel, AuthInterceptor>>;

/// Handles one command and returns a result (`None` for no result), or
/// fails with an error message. A failure is reported to the gateway as a
/// device NACK (`FAILED_PRECONDITION` on the `send_command` caller's
/// side), with the message included.
///
/// Synchronous rather than `async` — an `async fn` in a trait object needs
/// boxed futures (the `async-trait` crate, or manual `Pin<Box<dyn
/// Future>>>`), which this SDK doesn't otherwise need. A handler that must
/// do async work can `tokio::spawn` its own task and reply via a channel
/// it owns.
pub type CommandHandler =
    Arc<dyn Fn(Params) -> Result<Option<prost_types::Value>, String> + Send + Sync>;

const BASE_BACKOFF: Duration = Duration::from_secs(1);
const MAX_BACKOFF: Duration = Duration::from_secs(30);
const HEALTHY_STREAM_THRESHOLD: Duration = Duration::from_secs(5);

/// The device-side SDK. Call [`Device::run`] to register and start
/// handling commands; [`Device::on_command`] may be called any time before
/// or after `run` starts.
pub struct Device {
    stub: Stub,
    config: DeviceConfig,
    device_id: Mutex<String>,
    handlers: Arc<Mutex<HashMap<String, CommandHandler>>>,
}

impl Device {
    /// Dials the gateway described by `config`.
    pub async fn connect(config: DeviceConfig) -> Result<Self, UdalError> {
        let channel = dial(&config.gateway_url, config.tls.as_ref()).await?;
        let interceptor = AuthInterceptor::new(api_key_header(&config.api_key));
        let stub =
            pb::device_service_client::DeviceServiceClient::with_interceptor(channel, interceptor);
        let device_id = Mutex::new(config.device_id.clone());
        Ok(Self {
            stub,
            config,
            device_id,
            handlers: Arc::new(Mutex::new(HashMap::new())),
        })
    }

    /// This device's ID: `config.device_id` if it was set, otherwise the
    /// gateway-assigned ID once [`Device::run`] has registered
    /// successfully.
    pub fn id(&self) -> String {
        self.device_id
            .lock()
            .expect("device_id mutex poisoned")
            .clone()
    }

    /// Registers `handler` for the named command, replacing any handler
    /// previously registered for that name.
    pub fn on_command(&self, name: impl Into<String>, handler: CommandHandler) {
        self.handlers
            .lock()
            .expect("handlers mutex poisoned")
            .insert(name.into(), handler);
    }

    /// Writes a value to one of this device's own properties.
    pub async fn publish_property(&self, path: &str, value: Value) -> Result<(), UdalError> {
        let mut stub = self.stub.clone();
        stub.set_property(pb::SetPropertyRequest {
            device_id: self.id(),
            property_path: path.to_string(),
            value: Some(value_to_proto(value)),
        })
        .await?;
        Ok(())
    }

    async fn register(&self) -> Result<(), UdalError> {
        let mut stub = self.stub.clone();
        let req = pb::RegisterDeviceRequest {
            name: self.config.name.clone(),
            capability: self.config.capability.clone(),
            transport: self.config.transport.clone(),
            labels: self.config.labels.clone(),
            transport_config: None,
            id: self.config.device_id.clone(),
        };
        match stub.register_device(req).await {
            Ok(resp) => {
                let id = resp.into_inner().device.map(|d| d.id).unwrap_or_default();
                *self.device_id.lock().expect("device_id mutex poisoned") = id;
                Ok(())
            }
            Err(status) => {
                // Already registered under our own stable device_id (e.g.
                // this is a reconnect after a process restart) isn't a
                // failure to give up on.
                if status.code() == Code::AlreadyExists && !self.config.device_id.is_empty() {
                    *self.device_id.lock().expect("device_id mutex poisoned") =
                        self.config.device_id.clone();
                    Ok(())
                } else {
                    Err(status.into())
                }
            }
        }
    }

    /// Registers the device (if not already) and opens its command stream,
    /// re-registering and reconnecting with exponential backoff (1s up to
    /// 30s) if the connection is lost. Runs until the enclosing task is
    /// dropped or aborted — callers that need to stop it should
    /// `tokio::spawn` this call and `.abort()` the returned `JoinHandle`,
    /// mirroring how the Go SDK's `Run` stops on context cancellation and
    /// the Python SDK's `run` stops on task cancellation.
    pub async fn run(&self) -> Result<(), UdalError> {
        self.register().await?;

        let mut backoff = BASE_BACKOFF;
        loop {
            let connected_at = Instant::now();
            let result = self.run_command_stream().await;
            if connected_at.elapsed() > HEALTHY_STREAM_THRESHOLD {
                // The stream was healthy for a while before failing; treat
                // this as a fresh outage rather than compounding backoff
                // from a previous one.
                backoff = BASE_BACKOFF;
            }
            let _ = result; // best-effort reconnect; no logging facade wired up (kept dependency-free)

            tokio::time::sleep(backoff).await;
            let _ = self.register().await; // covers a gateway restart with a non-persistent registry
            backoff = (backoff * 2).min(MAX_BACKOFF);
        }
    }

    async fn run_command_stream(&self) -> Result<(), UdalError> {
        let device_id = self.id();
        let (tx, rx) = tokio::sync::mpsc::channel::<pb::CommandResult>(16);
        let outbound = tokio_stream::wrappers::ReceiverStream::new(rx);

        let mut stub = self.stub.clone();
        let mut request = Request::new(outbound);
        let device_id_value = device_id.parse().map_err(|_| {
            UdalError::new(
                crate::error::ErrorCode::InvalidArgument,
                "device id is not valid ASCII metadata",
            )
        })?;
        request
            .metadata_mut()
            .insert("x-device-id", device_id_value);

        let mut inbound = stub.stream_commands(request).await?.into_inner();

        while let Some(cmd) = inbound.message().await? {
            let tx = tx.clone();
            let handlers = Arc::clone(&self.handlers);
            tokio::spawn(async move {
                let result = handle_command(&handlers, cmd);
                // Best-effort; a broken stream surfaces via the next
                // `message()` call above, same as the Go/Python SDKs.
                let _ = tx.send(result).await;
            });
        }
        Ok(())
    }
}

fn handle_command(
    handlers: &Mutex<HashMap<String, CommandHandler>>,
    cmd: pb::Command,
) -> pb::CommandResult {
    let handler = handlers
        .lock()
        .expect("handlers mutex poisoned")
        .get(&cmd.name)
        .cloned();
    match handler {
        None => pb::CommandResult {
            id: cmd.id,
            success: false,
            error: format!("no handler registered for command {:?}", cmd.name),
            result: None,
        },
        Some(handler) => {
            let params = cmd.params.map(|s| s.fields).unwrap_or_default();
            match handler(params) {
                Ok(result) => pb::CommandResult {
                    id: cmd.id,
                    success: true,
                    error: String::new(),
                    result,
                },
                Err(error) => pb::CommandResult {
                    id: cmd.id,
                    success: false,
                    error,
                    result: None,
                },
            }
        }
    }
}
