//! Application-side SDK (req42.adoc §7.3): reads/writes device properties,
//! sends commands, and subscribes to live property updates. Mirrors
//! `code/sdk/go/client.go` and `code/sdk/python/src/udal/client.py`.

use std::time::SystemTime;

use tonic::service::interceptor::InterceptedService;
use tonic::transport::Channel;
use tonic::Request;

use crate::error::UdalError;
use crate::value::Value;

use super::config::ClientConfig;
use super::convert::{timestamp_from_proto, value_from_proto, value_to_proto};
use super::dial::{api_key_header, dial, AuthInterceptor};
use super::params::Params;
use super::pb;

type Stub =
    pb::device_service_client::DeviceServiceClient<InterceptedService<Channel, AuthInterceptor>>;

/// One event delivered by [`Client::subscribe`].
#[derive(Debug, Clone, PartialEq)]
pub struct PropertyUpdate {
    pub device_id: String,
    pub property_path: String,
    pub value: Option<Value>,
    pub timestamp: SystemTime,
}

impl PropertyUpdate {
    fn from_proto(ev: pb::SubscribeResponse) -> Self {
        Self {
            device_id: ev.device_id,
            property_path: ev.property_path,
            value: ev.value.and_then(value_from_proto),
            timestamp: timestamp_from_proto(ev.timestamp),
        }
    }
}

/// A live [`Client::subscribe`] stream. Drives to completion via
/// [`Subscription::next`] rather than implementing `futures_core::Stream`
/// directly — avoids pulling in the `futures` crate for a single trait this
/// SDK doesn't otherwise need.
pub struct Subscription {
    inner: tonic::Streaming<pb::SubscribeResponse>,
}

impl Subscription {
    /// Returns the next update, `None` once the stream has ended cleanly,
    /// or `Some(Err(_))` if it ended with an error.
    pub async fn next(&mut self) -> Option<Result<PropertyUpdate, UdalError>> {
        match self.inner.message().await {
            Ok(Some(ev)) => Some(Ok(PropertyUpdate::from_proto(ev))),
            Ok(None) => None,
            Err(status) => Some(Err(status.into())),
        }
    }
}

/// The application-side SDK: reads/writes device properties, sends
/// commands, and subscribes to live property updates.
#[derive(Clone)]
pub struct Client {
    stub: Stub,
}

impl Client {
    /// Dials the gateway described by `config`.
    pub async fn connect(config: ClientConfig) -> Result<Self, UdalError> {
        let channel = dial(&config.gateway_url, config.tls.as_ref()).await?;
        let interceptor = AuthInterceptor::new(api_key_header(&config.api_key));
        let stub =
            pb::device_service_client::DeviceServiceClient::with_interceptor(channel, interceptor);
        Ok(Self { stub })
    }

    /// Reads `device_id`'s current value at `path`. Returns `None` if the
    /// property has never been set.
    pub async fn read_property(
        &mut self,
        device_id: &str,
        path: &str,
    ) -> Result<Option<Value>, UdalError> {
        let resp = self
            .stub
            .get_property(pb::GetPropertyRequest {
                device_id: device_id.to_string(),
                property_path: path.to_string(),
            })
            .await?
            .into_inner();
        Ok(resp.value.and_then(value_from_proto))
    }

    /// Writes `value` to `device_id`'s property at `path`.
    pub async fn write_property(
        &mut self,
        device_id: &str,
        path: &str,
        value: Value,
    ) -> Result<(), UdalError> {
        self.stub
            .set_property(pb::SetPropertyRequest {
                device_id: device_id.to_string(),
                property_path: path.to_string(),
                value: Some(value_to_proto(value)),
            })
            .await?;
        Ok(())
    }

    /// Sends a named command with `params` to `device_id` and returns its
    /// result (`Kind::NullValue` if the device returned none).
    pub async fn send_command(
        &mut self,
        device_id: &str,
        command: &str,
        params: Params,
    ) -> Result<prost_types::Value, UdalError> {
        let resp = self
            .stub
            .send_command(pb::SendCommandRequest {
                device_id: device_id.to_string(),
                command: command.to_string(),
                params: Some(prost_types::Struct { fields: params }),
            })
            .await?
            .into_inner();
        Ok(resp.result.unwrap_or_else(super::params::null_value))
    }

    /// Streams property updates for `device_id` (every property if `path`
    /// is `""`), until the caller stops polling [`Subscription::next`] or
    /// the stream ends.
    pub async fn subscribe(
        &mut self,
        device_id: &str,
        path: &str,
    ) -> Result<Subscription, UdalError> {
        let stream = self
            .stub
            .subscribe(Request::new(pb::SubscribeRequest {
                device_id: device_id.to_string(),
                property_path: path.to_string(),
            }))
            .await?
            .into_inner();
        Ok(Subscription { inner: stream })
    }
}
