//! gRPC channel setup — mirrors `code/sdk/go/dial.go` and
//! `code/sdk/python/src/udal/_channel.py`.

use tonic::metadata::{Ascii, MetadataValue};
use tonic::transport::{Certificate, Channel, Endpoint, Identity};

use crate::error::{ErrorCode, UdalError};

use super::config::TlsConfig;

/// Opens a gRPC channel to `gateway_url`, encrypted with `tls` if given or
/// plaintext otherwise.
pub(crate) async fn dial(gateway_url: &str, tls: Option<&TlsConfig>) -> Result<Channel, UdalError> {
    let scheme = if tls.is_some() { "https" } else { "http" };
    let uri = format!("{scheme}://{gateway_url}");
    let mut endpoint = Endpoint::from_shared(uri)
        .map_err(|e| UdalError::new(ErrorCode::InvalidArgument, e.to_string()))?;

    if let Some(tls) = tls {
        let mut tls_config = tonic::transport::ClientTlsConfig::new();
        if let Some(ca) = &tls.ca_certificate {
            tls_config = tls_config.ca_certificate(Certificate::from_pem(ca));
        }
        if let (Some(cert), Some(key)) = (&tls.client_certificate, &tls.client_key) {
            tls_config = tls_config.identity(Identity::from_pem(cert, key));
        }
        endpoint = endpoint
            .tls_config(tls_config)
            .map_err(|e| UdalError::new(ErrorCode::InvalidArgument, e.to_string()))?;
    }

    endpoint
        .connect()
        .await
        .map_err(|e| UdalError::new(ErrorCode::Unavailable, e.to_string()))
}

/// Returns the `x-api-key` ASCII metadata value for `api_key`, or `None` if
/// `api_key` is empty.
pub(crate) fn api_key_header(api_key: &str) -> Option<MetadataValue<Ascii>> {
    if api_key.is_empty() {
        return None;
    }
    api_key.parse().ok()
}

/// A `tonic` client interceptor that attaches the `x-api-key` metadata
/// header (see [`api_key_header`]) to every outgoing call, if one was
/// configured.
#[derive(Clone)]
pub(crate) struct AuthInterceptor {
    api_key: Option<MetadataValue<Ascii>>,
}

impl AuthInterceptor {
    pub(crate) fn new(api_key: Option<MetadataValue<Ascii>>) -> Self {
        Self { api_key }
    }
}

impl tonic::service::Interceptor for AuthInterceptor {
    fn call(&mut self, mut req: tonic::Request<()>) -> Result<tonic::Request<()>, tonic::Status> {
        if let Some(v) = &self.api_key {
            req.metadata_mut().insert("x-api-key", v.clone());
        }
        Ok(req)
    }
}
