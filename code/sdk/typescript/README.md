# udal-sdk

TypeScript/Node.js client SDK for [UDAL](https://github.com/paul-fleischmann-com/udal) (Universal
Device Abstraction Layer, req42.adoc §7.3). Application-side only — for web frontends and Node.js
backend services, mirroring the operation set already defined for the Go, Python, and Rust SDKs.
No device-side registration (that's out of scope for browser/Node.js callers).

Calls the gateway's REST/`grpc-gateway`-transcoded API (`code/api/proto/udal/v1/device.proto`)
through a dependency-free, `fetch`-based HTTP client — isomorphic between Node (>=20) and
browsers, unlike the other three SDKs, which dial the gateway's gRPC port directly (something a
browser can't do).

```ts
import { UdalClient } from "udal-sdk";

const client = new UdalClient({ gatewayUrl: "http://localhost:8080", apiKey: "..." });

const temperature = await client.readProperty("dev-1", "temperature");
await client.writeProperty("dev-1", "setpoint", 21.5);
const result = await client.sendCommand("dev-1", "reboot", { delaySeconds: 5 });
const devices = await client.listDevices({ capability: "temperature-sensor" });
```

## Error handling

Every operation that fails rejects its returned `Promise` with a `UdalError` (`{code, message}`),
per req42.adoc §7.3 — mirroring Go's `*udal.Error`, Python's `UdalError`, and Rust's `UdalError`,
including the shared `"udal: CODE: message"` format:

```ts
import { UdalClient, UdalError, UdalErrorCode } from "udal-sdk";

try {
  await client.readProperty("missing-device", "temperature");
} catch (err) {
  if (err instanceof UdalError && err.code === UdalErrorCode.NotFound) {
    // ...
  }
}
```

## Property values

`PropertyValue` is `boolean | bigint | number | string | Uint8Array`. `bigint` (not `number`)
carries the wire protocol's `int64` field, since a JS `number` can't losslessly represent the full
int64 range — `number` always maps to the float branch, even for whole numbers. Structured (JSON)
property values aren't supported, matching the Go/Python SDKs' documented limitation.

## `subscribe()`

`subscribe(deviceId, path)` is typed as an `AsyncIterable<PropertyUpdate>`, but its first iteration
currently rejects with an `Unimplemented`-coded `UdalError`. Real streaming needs a WebSocket
bridge over the gateway's `Subscribe` RPC (req42.adoc §7.1) that doesn't exist yet — `grpc-gateway`
v2 doesn't support REST transcoding for server-streaming RPCs, so this can't be a thin REST wrapper
like the other operations here.

## Development

```sh
npm ci
npm run lint
npm run typecheck
npm test
npm run build   # dual ESM + CJS build via tsup
```
