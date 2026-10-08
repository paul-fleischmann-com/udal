// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Paul Fleischmann

import { describe, expect, it } from "vitest";
import { UdalError, UdalErrorCode } from "./errors.js";
import { fromJson, toJson, type PropertyValue } from "./value.js";

describe("toJson / fromJson round trip", () => {
  const cases: PropertyValue[] = [true, false, 42n, -1n, 1.5, "hello", new Uint8Array([1, 2, 3])];

  it.each(cases)("round-trips %s", (value) => {
    const json = toJson(value);
    const back = fromJson(json);
    expect(back).toEqual(value);
  });
});

describe("toJson", () => {
  it("encodes bigint as a decimal string", () => {
    expect(toJson(42n)).toEqual({ intVal: "42" });
  });

  it("always encodes number as floatVal, even whole numbers", () => {
    expect(toJson(42)).toEqual({ floatVal: 42 });
  });

  it("throws InvalidArgument for a bigint outside int64 range", () => {
    const tooBig = 2n ** 63n;
    expect(() => toJson(tooBig)).toThrowError(UdalError);
    try {
      toJson(tooBig);
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(UdalError);
      expect((err as UdalError).code).toBe(UdalErrorCode.InvalidArgument);
    }
  });

  it("throws InvalidArgument for an unsupported type", () => {
    // @ts-expect-error deliberately passing an unsupported type
    expect(() => toJson(null)).toThrowError(UdalError);
    // @ts-expect-error deliberately passing an unsupported type
    expect(() => toJson({})).toThrowError(UdalError);
  });
});

describe("fromJson", () => {
  it("returns undefined for undefined input", () => {
    expect(fromJson(undefined)).toBeUndefined();
  });

  it("returns undefined for an empty object", () => {
    expect(fromJson({})).toBeUndefined();
  });

  it("returns undefined for a jsonVal-only response (unsupported structured value)", () => {
    expect(fromJson({ jsonVal: { nested: true } })).toBeUndefined();
  });
});
