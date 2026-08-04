// Conversion between the gateway's PropertyValue wire JSON and native
// TypeScript values — mirrors code/sdk/go/value.go's valueFromProto/
// valueToProto and code/sdk/python/src/udal/_values.py.

import { UdalError, UdalErrorCode } from "./errors.js";

/** TypeScript types a PropertyValue can be built from.
 *
 * `bigint` (not `number`) carries the proto's `int64 int_val` field: JS
 * `number` can't losslessly represent the full int64 range, and the
 * gateway's REST JSON encodes `intVal` as a decimal *string* for exactly
 * that reason. `number` therefore always maps to the `float_val` branch,
 * even for whole numbers.
 *
 * Structured (JSON) values aren't supported here — the gateway's own
 * property storage doesn't round-trip them correctly yet (same documented
 * limitation as the Go/Python SDKs), so accepting them here would silently
 * produce broken behavior rather than a clear error. */
export type PropertyValue = boolean | bigint | number | string | Uint8Array;

/** Wire shape of `PropertyValue` as transcoded by grpc-gateway — matches
 * the `v1PropertyValue` OpenAPI schema (code/api/openapi/udal/v1/device.openapi.v3.json). */
export interface PropertyValueJson {
  boolVal?: boolean;
  intVal?: string;
  floatVal?: number;
  stringVal?: string;
  bytesVal?: string;
  jsonVal?: unknown;
}

// int_val is a proto3 int64 field.
const INT64_MIN = -(2n ** 63n);
const INT64_MAX = 2n ** 63n - 1n;

function bytesToBase64(bytes: Uint8Array): string {
  let binary = "";
  for (const b of bytes) {
    binary += String.fromCharCode(b);
  }
  return btoa(binary);
}

function base64ToBytes(b64: string): Uint8Array {
  const binary = atob(b64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}

/** Converts a native TypeScript value into wire JSON for a `SetProperty`
 * request. Throws `UdalError(InvalidArgument, ...)` for an unsupported
 * type, or a `bigint` outside int64 range. */
export function toJson(value: PropertyValue): PropertyValueJson {
  if (typeof value === "boolean") {
    return { boolVal: value };
  }
  if (typeof value === "bigint") {
    if (value < INT64_MIN || value > INT64_MAX) {
      throw new UdalError(
        UdalErrorCode.InvalidArgument,
        `${value} is out of int64 range (${INT64_MIN}..${INT64_MAX})`,
      );
    }
    return { intVal: value.toString() };
  }
  if (typeof value === "number") {
    return { floatVal: value };
  }
  if (typeof value === "string") {
    return { stringVal: value };
  }
  if (value instanceof Uint8Array) {
    return { bytesVal: bytesToBase64(value) };
  }
  throw new UdalError(
    UdalErrorCode.InvalidArgument,
    "unsupported property value type (supported: boolean, bigint, number, string, Uint8Array)",
  );
}

/** Converts wire JSON from a `GetProperty`/`SetProperty`/`Subscribe`
 * response into a native TypeScript value, or `undefined` if it carries no
 * value at all (including a `jsonVal`-only response — see `PropertyValue`'s
 * doc comment). */
export function fromJson(json: PropertyValueJson | undefined): PropertyValue | undefined {
  if (json === undefined) {
    return undefined;
  }
  if (json.boolVal !== undefined) {
    return json.boolVal;
  }
  if (json.intVal !== undefined) {
    return BigInt(json.intVal);
  }
  if (json.floatVal !== undefined) {
    return json.floatVal;
  }
  if (json.stringVal !== undefined) {
    return json.stringVal;
  }
  if (json.bytesVal !== undefined) {
    return base64ToBytes(json.bytesVal);
  }
  return undefined;
}
