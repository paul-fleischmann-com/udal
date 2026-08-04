import { describe, expect, it, vi } from "vitest";
import { UdalClient } from "./client.js";
import { UdalErrorCode } from "./errors.js";

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

function makeClient(fetchImpl: typeof fetch): UdalClient {
  return new UdalClient({ gatewayUrl: "http://localhost:8080", fetch: fetchImpl });
}

describe("UdalClient", () => {
  it("readProperty sends GET and unwraps .value", async () => {
    const fetchImpl = vi.fn().mockResolvedValue(jsonResponse(200, { value: { floatVal: 21.5 } }));
    const client = makeClient(fetchImpl);
    const value = await client.readProperty("dev-1", "temperature");
    expect(value).toBe(21.5);
    const [url, init] = fetchImpl.mock.calls[0] as [URL, RequestInit];
    expect(url.pathname).toBe("/v1/devices/dev-1/properties/temperature");
    expect(init.method).toBe("GET");
  });

  it("readProperty encodes a multi-segment property path", async () => {
    const fetchImpl = vi.fn().mockResolvedValue(jsonResponse(200, { value: { boolVal: true } }));
    const client = makeClient(fetchImpl);
    await client.readProperty("dev-1", "sensors/outdoor/temp");
    const [url] = fetchImpl.mock.calls[0] as [URL];
    expect(url.pathname).toBe("/v1/devices/dev-1/properties/sensors/outdoor/temp");
  });

  it("writeProperty sends PUT with the raw PropertyValue JSON as the body", async () => {
    const fetchImpl = vi.fn().mockResolvedValue(jsonResponse(200, { newValue: { floatVal: 22 } }));
    const client = makeClient(fetchImpl);
    await client.writeProperty("dev-1", "temperature", 22);
    const [url, init] = fetchImpl.mock.calls[0] as [URL, RequestInit];
    expect(url.pathname).toBe("/v1/devices/dev-1/properties/temperature");
    expect(init.method).toBe("PUT");
    expect(JSON.parse(init.body as string)).toEqual({ floatVal: 22 });
  });

  it("sendCommand sends POST and unwraps .result", async () => {
    const fetchImpl = vi.fn().mockResolvedValue(jsonResponse(200, { result: { ok: true } }));
    const client = makeClient(fetchImpl);
    const result = await client.sendCommand("dev-1", "reboot", { delaySeconds: 5 });
    expect(result).toEqual({ ok: true });
    const [url, init] = fetchImpl.mock.calls[0] as [URL, RequestInit];
    expect(url.pathname).toBe("/v1/devices/dev-1/commands/reboot");
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toEqual({ delaySeconds: 5 });
  });

  it("getDevice unwraps and maps device status", async () => {
    // The real response is { "device": {...} } (v1GetDeviceResponse wraps
    // the Device message — code/api/openapi/udal/v1/device.openapi.v3.json)
    // — a bare device object here would silently mask the unwrap being
    // missing, exactly how this shipped broken the first time.
    const fetchImpl = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        device: {
          id: "dev-1",
          name: "Sensor",
          capability: "temperature-sensor",
          transport: "mqtt",
          status: "DEVICE_STATUS_ONLINE",
          lastSeen: "2026-08-01T00:00:00Z",
          labels: { room: "kitchen" },
        },
      }),
    );
    const client = makeClient(fetchImpl);
    const device = await client.getDevice("dev-1");
    expect(device.status).toBe("online");
    expect(device.labels).toEqual({ room: "kitchen" });
  });

  it("listDevices applies capability/transport filters as query params", async () => {
    const fetchImpl = vi.fn().mockResolvedValue(jsonResponse(200, { devices: [] }));
    const client = makeClient(fetchImpl);
    await client.listDevices({ capability: "temperature-sensor", transport: "mqtt" });
    const [url] = fetchImpl.mock.calls[0] as [URL];
    expect(url.searchParams.get("capability")).toBe("temperature-sensor");
    expect(url.searchParams.get("transport")).toBe("mqtt");
  });

  it("propagates a UdalError from a failed request", async () => {
    const fetchImpl = vi.fn().mockResolvedValue(jsonResponse(404, { code: 5, message: "not found" }));
    const client = makeClient(fetchImpl);
    await expect(client.readProperty("dev-1", "temperature")).rejects.toMatchObject({
      code: UdalErrorCode.NotFound,
    });
  });

  it("rejects further calls after close()", async () => {
    const fetchImpl = vi.fn().mockResolvedValue(jsonResponse(200, { value: {} }));
    const client = makeClient(fetchImpl);
    await client.close();
    await expect(client.readProperty("dev-1", "temperature")).rejects.toMatchObject({
      code: UdalErrorCode.FailedPrecondition,
    });
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  describe("subscribe", () => {
    it("does not throw synchronously and never calls fetch", () => {
      const fetchImpl = vi.fn();
      const client = makeClient(fetchImpl);
      expect(() => client.subscribe("dev-1", "temperature")).not.toThrow();
      expect(fetchImpl).not.toHaveBeenCalled();
    });

    it("rejects on first iteration with an Unimplemented UdalError referencing the WebSocket bridge", async () => {
      const fetchImpl = vi.fn();
      const client = makeClient(fetchImpl);
      const iterable = client.subscribe("dev-1", "temperature");

      await expect(
        (async () => {
          for await (const _update of iterable) {
            // never reached
          }
        })(),
      ).rejects.toMatchObject({
        code: UdalErrorCode.Unimplemented,
        message: expect.stringMatching(/WebSocket bridge/i) as string,
      });
      expect(fetchImpl).not.toHaveBeenCalled();
    });
  });
});
