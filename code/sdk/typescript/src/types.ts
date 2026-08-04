import type { PropertyValue } from "./value.js";

/** One event delivered by `UdalClient.subscribe`. */
export interface PropertyUpdate {
  deviceId: string;
  propertyPath: string;
  value: PropertyValue | undefined;
  timestamp: Date;
}

/** A registered device, as returned by `UdalClient.getDevice`/`listDevices`.
 * Not part of req42.adoc §7.3's minimum SDK contract (that lists only
 * Connect/Disconnect/ReadProperty/WriteProperty/SendCommand/Subscribe/
 * RegisterDevice) — added because a device-listing web frontend (this
 * SDK's stated audience) needs it, and the gateway already exposes
 * GetDevice/ListDevices as DeviceService RPCs (mirrors the Python SDK's
 * issue #19 addition). */
export interface DeviceInfo {
  id: string;
  name: string;
  capability: string;
  transport: string;
  status: "unspecified" | "online" | "offline" | "unknown";
  lastSeen: Date;
  labels: Record<string, string>;
}

// Wire shapes as transcoded by grpc-gateway (v1Device, v1DeviceStatus in
// code/api/openapi/udal/v1/device.openapi.v3.json).
export interface DeviceJson {
  id?: string;
  name?: string;
  capability?: string;
  transport?: string;
  status?: string;
  lastSeen?: string;
  labels?: Record<string, string>;
}

const STATUS_NAMES: Record<string, DeviceInfo["status"]> = {
  DEVICE_STATUS_UNSPECIFIED: "unspecified",
  DEVICE_STATUS_ONLINE: "online",
  DEVICE_STATUS_OFFLINE: "offline",
  DEVICE_STATUS_UNKNOWN: "unknown",
};

export function deviceInfoFromJson(d: DeviceJson): DeviceInfo {
  return {
    id: d.id ?? "",
    name: d.name ?? "",
    capability: d.capability ?? "",
    transport: d.transport ?? "",
    status: STATUS_NAMES[d.status ?? ""] ?? "unspecified",
    lastSeen: d.lastSeen !== undefined ? new Date(d.lastSeen) : new Date(0),
    labels: d.labels ?? {},
  };
}
