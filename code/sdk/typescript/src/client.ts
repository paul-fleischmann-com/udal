// UdalClient is the application-side SDK (req42.adoc §7.3): reads/writes
// device properties, sends commands, and looks up registered devices —
// over the gateway's REST/grpc-gateway transcoding
// (code/api/proto/udal/v1/device.proto). No device-side registration
// (RegisterDevice/OnCommand/publishProperty) — this SDK targets web
// frontends and Node.js application services, not devices themselves.

import type { ClientConfig } from "./config.js";
import { UdalError, UdalErrorCode } from "./errors.js";
import { HttpClient, encodePathSegments } from "./http.js";
import type { DeviceInfo, PropertyUpdate } from "./types.js";
import { deviceInfoFromJson, type DeviceJson } from "./types.js";
import { fromJson, toJson, type PropertyValue, type PropertyValueJson } from "./value.js";

interface GetDeviceResponseJson {
  device?: DeviceJson;
}
interface GetPropertyResponseJson {
  value?: PropertyValueJson;
}
interface SetPropertyResponseJson {
  newValue?: PropertyValueJson;
}
interface SendCommandResponseJson {
  result?: unknown;
}
interface ListDevicesResponseJson {
  devices?: DeviceJson[];
  nextPageToken?: string;
}

function devicePropertyPath(deviceId: string, path: string): string {
  return `/v1/devices/${encodeURIComponent(deviceId)}/properties/${encodePathSegments(path)}`;
}

export class UdalClient {
  private readonly http: HttpClient;
  private closed = false;

  constructor(config: ClientConfig) {
    this.http = new HttpClient(config.gatewayUrl, config.apiKey, config.fetch);
  }

  /** Marks this client closed. There's no persistent connection to tear
   * down (unlike the gRPC-based Go/Python/Rust SDKs) — REST requests are
   * independent — so this only guards against further use; any in-flight
   * or subsequent call rejects with a FailedPrecondition UdalError. */
  // eslint-disable-next-line @typescript-eslint/require-await
  async close(): Promise<void> {
    this.closed = true;
  }

  private checkOpen(): void {
    if (this.closed) {
      throw new UdalError(UdalErrorCode.FailedPrecondition, "client is closed");
    }
  }

  async getDevice(id: string, opts?: { signal?: AbortSignal }): Promise<DeviceInfo> {
    this.checkOpen();
    const json = await this.http.request<GetDeviceResponseJson>({
      method: "GET",
      path: `/v1/devices/${encodeURIComponent(id)}`,
      signal: opts?.signal,
    });
    return deviceInfoFromJson(json.device ?? {});
  }

  async listDevices(
    filter?: { capability?: string; transport?: string },
    opts?: { signal?: AbortSignal },
  ): Promise<DeviceInfo[]> {
    this.checkOpen();
    const params = new URLSearchParams();
    if (filter?.capability) params.set("capability", filter.capability);
    if (filter?.transport) params.set("transport", filter.transport);
    const query = params.toString();
    const json = await this.http.request<ListDevicesResponseJson>({
      method: "GET",
      path: `/v1/devices${query ? `?${query}` : ""}`,
      signal: opts?.signal,
    });
    return (json.devices ?? []).map(deviceInfoFromJson);
  }

  async readProperty(
    deviceId: string,
    path: string,
    opts?: { signal?: AbortSignal },
  ): Promise<PropertyValue | undefined> {
    this.checkOpen();
    const json = await this.http.request<GetPropertyResponseJson>({
      method: "GET",
      path: devicePropertyPath(deviceId, path),
      signal: opts?.signal,
    });
    return fromJson(json.value);
  }

  async writeProperty(
    deviceId: string,
    path: string,
    value: PropertyValue,
    opts?: { signal?: AbortSignal },
  ): Promise<void> {
    this.checkOpen();
    await this.http.request<SetPropertyResponseJson>({
      method: "PUT",
      path: devicePropertyPath(deviceId, path),
      body: toJson(value),
      signal: opts?.signal,
    });
  }

  async sendCommand(
    deviceId: string,
    command: string,
    params?: Record<string, unknown>,
    opts?: { signal?: AbortSignal },
  ): Promise<unknown> {
    this.checkOpen();
    const json = await this.http.request<SendCommandResponseJson>({
      method: "POST",
      path: `/v1/devices/${encodeURIComponent(deviceId)}/commands/${encodeURIComponent(command)}`,
      body: params ?? {},
      signal: opts?.signal,
    });
    return json.result;
  }

  /** Streams property updates for deviceId (every property if path is
   * omitted). Genuinely typed as an `AsyncIterable<PropertyUpdate>` so it
   * type-checks anywhere a real subscription is expected, but the gateway's
   * Subscribe RPC has no REST transcoding (grpc-gateway v2 doesn't support
   * a streaming REST mapping — see the proto comment on `Subscribe` in
   * code/api/proto/udal/v1/device.proto) and there is no WebSocket bridge
   * yet (req42.adoc §7.1). The first iteration always rejects with an
   * `Unimplemented` UdalError; no network call is attempted. */
  subscribe(_deviceId: string, _path = ""): AsyncIterable<PropertyUpdate> {
    return {
      [Symbol.asyncIterator](): AsyncIterator<PropertyUpdate> {
        return {
          // eslint-disable-next-line @typescript-eslint/require-await
          async next(): Promise<IteratorResult<PropertyUpdate>> {
            throw new UdalError(
              UdalErrorCode.Unimplemented,
              "subscribe() is not implemented yet — pending a WebSocket bridge over the " +
                "gateway's Subscribe RPC (req42.adoc §7.1); grpc-gateway v2 does not support " +
                "REST transcoding for server-streaming RPCs, so this cannot be a thin REST " +
                "wrapper like the other UdalClient methods",
            );
          },
        };
      },
    };
  }
}
