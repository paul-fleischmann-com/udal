//! Error type returned by every SDK operation that fails (req42.adoc §7.3:
//! "Rust: `Result<Value, UdalError>`").

#[cfg(feature = "std")]
use std::string::String;

#[cfg(all(not(feature = "std"), feature = "mqtt"))]
use heapless::String as HeaplessString;

/// Mirrors the gRPC status codes the gateway responds with on the `std`
/// (gRPC) path. On the `mqtt` (no_std) path there is no gateway round trip
/// to carry a server-assigned code, so these are assigned locally by the
/// transport (e.g. [`ErrorCode::Unavailable`] for a broken connection,
/// [`ErrorCode::ResourceExhausted`] for a fixed-capacity buffer overflow).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ErrorCode {
    Unknown,
    InvalidArgument,
    NotFound,
    AlreadyExists,
    PermissionDenied,
    Unauthenticated,
    Unavailable,
    FailedPrecondition,
    DeadlineExceeded,
    Internal,
    ResourceExhausted,
}

impl ErrorCode {
    pub fn as_str(&self) -> &'static str {
        match self {
            ErrorCode::Unknown => "UNKNOWN",
            ErrorCode::InvalidArgument => "INVALID_ARGUMENT",
            ErrorCode::NotFound => "NOT_FOUND",
            ErrorCode::AlreadyExists => "ALREADY_EXISTS",
            ErrorCode::PermissionDenied => "PERMISSION_DENIED",
            ErrorCode::Unauthenticated => "UNAUTHENTICATED",
            ErrorCode::Unavailable => "UNAVAILABLE",
            ErrorCode::FailedPrecondition => "FAILED_PRECONDITION",
            ErrorCode::DeadlineExceeded => "DEADLINE_EXCEEDED",
            ErrorCode::Internal => "INTERNAL",
            ErrorCode::ResourceExhausted => "RESOURCE_EXHAUSTED",
        }
    }
}

impl core::fmt::Display for ErrorCode {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        f.write_str(self.as_str())
    }
}

/// Capacity of the `mqtt` (no_std, no-allocator) build's [`UdalError`]
/// message buffer — long enough for this SDK's own fixed error strings; a
/// longer message (e.g. an unusually verbose broker CONNACK reason) is
/// reported as `"(message too long)"` rather than truncated mid-UTF-8.
#[cfg(all(not(feature = "std"), feature = "mqtt"))]
pub const MESSAGE_CAPACITY: usize = 128;

/// Raised by every SDK operation that fails.
///
/// `code` mirrors the gRPC status code the gateway responded with on the
/// `std` (gRPC) build (see [`ErrorCode`]), letting callers distinguish e.g.
/// `NotFound` from `PermissionDenied` without depending on `tonic::Status`
/// directly.
#[derive(Debug, Clone)]
pub struct UdalError {
    pub code: ErrorCode,
    #[cfg(feature = "std")]
    message: String,
    #[cfg(all(not(feature = "std"), feature = "mqtt"))]
    message: HeaplessString<MESSAGE_CAPACITY>,
}

impl UdalError {
    #[cfg(feature = "std")]
    pub fn new(code: ErrorCode, message: impl Into<String>) -> Self {
        Self {
            code,
            message: message.into(),
        }
    }

    #[cfg(all(not(feature = "std"), feature = "mqtt"))]
    pub fn new(code: ErrorCode, message: &str) -> Self {
        let mut m = HeaplessString::new();
        if m.push_str(message).is_err() {
            // All-or-nothing: heapless::String::push_str leaves the buffer
            // untouched on overflow rather than partially writing it, so
            // this never lands mid-codepoint.
            let _ = m.push_str("(message too long)");
        }
        Self { code, message: m }
    }

    pub fn message(&self) -> &str {
        &self.message
    }
}

impl core::fmt::Display for UdalError {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        write!(f, "udal: {}: {}", self.code, self.message())
    }
}

#[cfg(feature = "std")]
impl std::error::Error for UdalError {}

#[cfg(feature = "std")]
impl From<tonic::Status> for UdalError {
    fn from(status: tonic::Status) -> Self {
        use tonic::Code;
        let code = match status.code() {
            Code::InvalidArgument => ErrorCode::InvalidArgument,
            Code::NotFound => ErrorCode::NotFound,
            Code::AlreadyExists => ErrorCode::AlreadyExists,
            Code::PermissionDenied => ErrorCode::PermissionDenied,
            Code::Unauthenticated => ErrorCode::Unauthenticated,
            Code::Unavailable => ErrorCode::Unavailable,
            Code::FailedPrecondition => ErrorCode::FailedPrecondition,
            Code::DeadlineExceeded => ErrorCode::DeadlineExceeded,
            Code::Internal => ErrorCode::Internal,
            Code::ResourceExhausted => ErrorCode::ResourceExhausted,
            _ => ErrorCode::Unknown,
        };
        UdalError::new(code, status.message().to_string())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[cfg(feature = "std")]
    #[test]
    fn display_matches_go_python_format() {
        let err = UdalError::new(ErrorCode::NotFound, "device dev-1 not found");
        assert_eq!(err.code, ErrorCode::NotFound);
        assert_eq!(err.message(), "device dev-1 not found");
        assert_eq!(err.to_string(), "udal: NOT_FOUND: device dev-1 not found");
    }

    #[cfg(all(not(feature = "std"), feature = "mqtt"))]
    #[test]
    fn display_matches_go_python_format() {
        use core::fmt::Write as _;

        let err = UdalError::new(ErrorCode::NotFound, "device dev-1 not found");
        assert_eq!(err.code, ErrorCode::NotFound);
        assert_eq!(err.message(), "device dev-1 not found");

        // Exercises core::fmt::Display without std/alloc's `format!` macro.
        let mut buf: HeaplessString<64> = HeaplessString::new();
        write!(buf, "{err}").unwrap();
        assert_eq!(buf.as_str(), "udal: NOT_FOUND: device dev-1 not found");
    }

    #[cfg(all(not(feature = "std"), feature = "mqtt"))]
    #[test]
    fn overflowing_message_falls_back_without_truncating_midway() {
        let mut long = HeaplessString::<{ MESSAGE_CAPACITY * 2 }>::new();
        for _ in 0..MESSAGE_CAPACITY * 2 {
            long.push('x').unwrap();
        }
        let err = UdalError::new(ErrorCode::Internal, &long);
        assert_eq!(err.message(), "(message too long)");
    }
}
