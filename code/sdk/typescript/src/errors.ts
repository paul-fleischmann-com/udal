// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Paul Fleischmann

// UdalError — rejected by every SDK operation that fails (req42.adoc §7.3:
// "TypeScript: Promise — rejects with UdalError").

/** Mirrors the gRPC status codes the gateway responds with, carried over
 * REST as a `google.rpc.Status`-shaped JSON error body (`{code, message}`,
 * numbered per `google.rpc.Code` — see `errorFromRpcStatus`). */
export enum UdalErrorCode {
  Unknown = "UNKNOWN",
  InvalidArgument = "INVALID_ARGUMENT",
  NotFound = "NOT_FOUND",
  AlreadyExists = "ALREADY_EXISTS",
  PermissionDenied = "PERMISSION_DENIED",
  Unauthenticated = "UNAUTHENTICATED",
  Unavailable = "UNAVAILABLE",
  FailedPrecondition = "FAILED_PRECONDITION",
  DeadlineExceeded = "DEADLINE_EXCEEDED",
  Internal = "INTERNAL",
  ResourceExhausted = "RESOURCE_EXHAUSTED",
  // Not part of the gRPC-code set mirrored by the other SDKs — used only by
  // the subscribe() stub until the WebSocket bridge (req42.adoc §7.1) exists.
  Unimplemented = "UNIMPLEMENTED",
}

/** Raised by every SDK operation that fails. `code`/`message` mirror the
 * shape Go (`*udal.Error`), Python (`UdalError`), and Rust (`UdalError`)
 * raise, and `message` matches their `"udal: CODE: message"` format. */
export class UdalError extends Error {
  readonly code: UdalErrorCode;

  constructor(code: UdalErrorCode, message: string) {
    super(`udal: ${code}: ${message}`);
    this.code = code;
    this.name = "UdalError";
    // Restores correct `instanceof` behavior when this class is
    // transpiled down for the CJS build (TS/Babel `Error` subclassing
    // pitfall — the prototype chain otherwise ends up as plain `Error`).
    Object.setPrototypeOf(this, UdalError.prototype);
  }

  // Error's own toString() prefixes "name: " (i.e. "UdalError: udal: ...");
  // override so it's exactly the "udal: CODE: message" format shared with
  // Go/Python/Rust.
  override toString(): string {
    return this.message;
  }
}

// google.rpc.Code numbering (https://github.com/googleapis/googleapis/blob/master/google/rpc/code.proto),
// the same numbering grpc-gateway's default error handler uses for the
// `code` field of the JSON error body it emits on non-2xx responses.
const RPC_CODE_TO_UDAL_CODE: Record<number, UdalErrorCode> = {
  3: UdalErrorCode.InvalidArgument,
  4: UdalErrorCode.DeadlineExceeded,
  5: UdalErrorCode.NotFound,
  6: UdalErrorCode.AlreadyExists,
  7: UdalErrorCode.PermissionDenied,
  8: UdalErrorCode.ResourceExhausted,
  9: UdalErrorCode.FailedPrecondition,
  12: UdalErrorCode.Unimplemented,
  13: UdalErrorCode.Internal,
  14: UdalErrorCode.Unavailable,
  16: UdalErrorCode.Unauthenticated,
};

/** Maps a `google.rpc.Status`-shaped error body's numeric `code` to a
 * `UdalError`. Codes outside the table above (Cancelled, Unknown, Aborted,
 * OutOfRange, DataLoss) fall back to `Unknown`, mirroring Rust's
 * `_ => ErrorCode::Unknown` fallback. */
export function errorFromRpcStatus(code: number, message: string): UdalError {
  return new UdalError(RPC_CODE_TO_UDAL_CODE[code] ?? UdalErrorCode.Unknown, message);
}

/** Fallback for a non-2xx response whose body isn't a parseable
 * `google.rpc.Status` JSON object — e.g. a reverse proxy or load balancer
 * in front of the gateway returning its own error page. */
export function errorFromHttpStatus(status: number, message: string): UdalError {
  let code: UdalErrorCode;
  switch (status) {
    case 400:
      code = UdalErrorCode.InvalidArgument;
      break;
    case 401:
      code = UdalErrorCode.Unauthenticated;
      break;
    case 403:
      code = UdalErrorCode.PermissionDenied;
      break;
    case 404:
      code = UdalErrorCode.NotFound;
      break;
    case 409:
      code = UdalErrorCode.AlreadyExists;
      break;
    case 412:
      code = UdalErrorCode.FailedPrecondition;
      break;
    case 429:
      code = UdalErrorCode.ResourceExhausted;
      break;
    case 503:
    case 504:
      code = UdalErrorCode.Unavailable;
      break;
    default:
      code = status >= 500 ? UdalErrorCode.Internal : UdalErrorCode.Unknown;
  }
  return new UdalError(code, message);
}

/** Maps a `fetch()` call throwing outright (DNS failure, connection
 * refused, TLS failure, an aborted request) to a `UdalError`. */
export function errorFromNetworkException(err: unknown): UdalError {
  const message = err instanceof Error ? err.message : String(err);
  return new UdalError(UdalErrorCode.Unavailable, message);
}
