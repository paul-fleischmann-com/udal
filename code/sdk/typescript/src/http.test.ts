import { describe, expect, it, vi } from "vitest";
import { UdalError, UdalErrorCode } from "./errors.js";
import { encodePathSegments, HttpClient } from "./http.js";

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

describe("encodePathSegments", () => {
  it("encodes each segment individually, preserving separating slashes", () => {
    expect(encodePathSegments("a/b c/d")).toBe("a/b%20c/d");
  });
});

describe("HttpClient", () => {
  it("resolves with the parsed JSON body on a 2xx response", async () => {
    const fetchImpl = vi.fn().mockResolvedValue(jsonResponse(200, { value: { boolVal: true } }));
    const client = new HttpClient("http://localhost:8080", undefined, fetchImpl);
    const result = await client.request({ method: "GET", path: "/v1/devices/dev-1" });
    expect(result).toEqual({ value: { boolVal: true } });
  });

  it("sends the x-api-key header iff an apiKey is configured", async () => {
    const fetchImpl = vi.fn().mockResolvedValue(jsonResponse(200, {}));
    const client = new HttpClient("http://localhost:8080", "secret", fetchImpl);
    await client.request({ method: "GET", path: "/v1/devices" });
    const init = fetchImpl.mock.calls[0]?.[1] as RequestInit;
    expect((init.headers as Record<string, string>)["x-api-key"]).toBe("secret");
  });

  it("omits the x-api-key header when apiKey is not configured", async () => {
    const fetchImpl = vi.fn().mockResolvedValue(jsonResponse(200, {}));
    const client = new HttpClient("http://localhost:8080", undefined, fetchImpl);
    await client.request({ method: "GET", path: "/v1/devices" });
    const init = fetchImpl.mock.calls[0]?.[1] as RequestInit;
    expect((init.headers as Record<string, string>)["x-api-key"]).toBeUndefined();
  });

  it("rejects with a UdalError mapped from a google.rpc.Status error body", async () => {
    const fetchImpl = vi.fn().mockResolvedValue(jsonResponse(404, { code: 5, message: "device dev-1 not found" }));
    const client = new HttpClient("http://localhost:8080", undefined, fetchImpl);
    await expect(client.request({ method: "GET", path: "/v1/devices/dev-1" })).rejects.toMatchObject({
      code: UdalErrorCode.NotFound,
      message: "udal: NOT_FOUND: device dev-1 not found",
    });
  });

  it("falls back to HTTP-status mapping when the error body isn't a parseable rpc status", async () => {
    const fetchImpl = vi.fn().mockResolvedValue(
      new Response("<html>502 Bad Gateway</html>", { status: 502, statusText: "Bad Gateway" }),
    );
    const client = new HttpClient("http://localhost:8080", undefined, fetchImpl);
    await expect(client.request({ method: "GET", path: "/v1/devices/dev-1" })).rejects.toMatchObject({
      code: UdalErrorCode.Internal,
    });
  });

  it("rejects with UdalError(Unavailable) when fetch itself throws", async () => {
    const fetchImpl = vi.fn().mockRejectedValue(new TypeError("fetch failed"));
    const client = new HttpClient("http://localhost:8080", undefined, fetchImpl);
    await expect(client.request({ method: "GET", path: "/v1/devices/dev-1" })).rejects.toBeInstanceOf(UdalError);
    await expect(client.request({ method: "GET", path: "/v1/devices/dev-1" })).rejects.toMatchObject({
      code: UdalErrorCode.Unavailable,
    });
  });
});
