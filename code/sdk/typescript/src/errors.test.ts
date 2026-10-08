// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Paul Fleischmann

import { describe, expect, it } from "vitest";
import {
  errorFromHttpStatus,
  errorFromNetworkException,
  errorFromRpcStatus,
  UdalError,
  UdalErrorCode,
} from "./errors.js";

describe("UdalError", () => {
  it("formats message as udal: CODE: message, matching Go/Python/Rust", () => {
    const err = new UdalError(UdalErrorCode.NotFound, "device dev-1 not found");
    expect(err.code).toBe(UdalErrorCode.NotFound);
    expect(err.message).toBe("udal: NOT_FOUND: device dev-1 not found");
    expect(err.toString()).toBe("udal: NOT_FOUND: device dev-1 not found");
  });

  it("is an instanceof Error and UdalError", () => {
    const err = new UdalError(UdalErrorCode.Internal, "boom");
    expect(err).toBeInstanceOf(Error);
    expect(err).toBeInstanceOf(UdalError);
    expect(err.name).toBe("UdalError");
  });
});

describe("errorFromRpcStatus", () => {
  const cases: [number, UdalErrorCode][] = [
    [3, UdalErrorCode.InvalidArgument],
    [4, UdalErrorCode.DeadlineExceeded],
    [5, UdalErrorCode.NotFound],
    [6, UdalErrorCode.AlreadyExists],
    [7, UdalErrorCode.PermissionDenied],
    [8, UdalErrorCode.ResourceExhausted],
    [9, UdalErrorCode.FailedPrecondition],
    [12, UdalErrorCode.Unimplemented],
    [13, UdalErrorCode.Internal],
    [14, UdalErrorCode.Unavailable],
    [16, UdalErrorCode.Unauthenticated],
  ];

  it.each(cases)("maps rpc code %i to %s", (rpcCode, expected) => {
    expect(errorFromRpcStatus(rpcCode, "msg").code).toBe(expected);
  });

  it.each([1, 2, 10, 11, 15, 999])("falls back to Unknown for unmapped code %i", (rpcCode) => {
    expect(errorFromRpcStatus(rpcCode, "msg").code).toBe(UdalErrorCode.Unknown);
  });
});

describe("errorFromHttpStatus", () => {
  it("maps well-known statuses", () => {
    expect(errorFromHttpStatus(400, "x").code).toBe(UdalErrorCode.InvalidArgument);
    expect(errorFromHttpStatus(401, "x").code).toBe(UdalErrorCode.Unauthenticated);
    expect(errorFromHttpStatus(403, "x").code).toBe(UdalErrorCode.PermissionDenied);
    expect(errorFromHttpStatus(404, "x").code).toBe(UdalErrorCode.NotFound);
    expect(errorFromHttpStatus(409, "x").code).toBe(UdalErrorCode.AlreadyExists);
    expect(errorFromHttpStatus(412, "x").code).toBe(UdalErrorCode.FailedPrecondition);
    expect(errorFromHttpStatus(429, "x").code).toBe(UdalErrorCode.ResourceExhausted);
    expect(errorFromHttpStatus(503, "x").code).toBe(UdalErrorCode.Unavailable);
    expect(errorFromHttpStatus(504, "x").code).toBe(UdalErrorCode.Unavailable);
  });

  it("maps other 5xx to Internal and other statuses to Unknown", () => {
    expect(errorFromHttpStatus(500, "x").code).toBe(UdalErrorCode.Internal);
    expect(errorFromHttpStatus(418, "x").code).toBe(UdalErrorCode.Unknown);
  });
});

describe("errorFromNetworkException", () => {
  it("maps a thrown Error to Unavailable", () => {
    const err = errorFromNetworkException(new Error("connection refused"));
    expect(err.code).toBe(UdalErrorCode.Unavailable);
    expect(err.message).toBe("udal: UNAVAILABLE: connection refused");
  });

  it("stringifies a non-Error throw", () => {
    const err = errorFromNetworkException("weird");
    expect(err.message).toBe("udal: UNAVAILABLE: weird");
  });
});
