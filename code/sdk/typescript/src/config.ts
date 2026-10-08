// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Paul Fleischmann

export interface ClientConfig {
  /** Base URL of the gateway's REST endpoint, e.g. "https://gateway.example.com" or "http://localhost:8080". */
  gatewayUrl: string;
  /** Sent as the X-API-Key header on every request, if set. */
  apiKey?: string;
  /** Overrides the fetch implementation used for every request — mainly for
   * tests; defaults to the runtime global `fetch`. Not a TLS/mTLS knob:
   * unlike the Go/Python/Rust SDKs (which hand-roll a raw gRPC transport
   * and so need to supply mTLS materials themselves), this SDK relies on
   * the browser/OS trust store via the URL scheme (`https://` vs
   * `http://`), keeping the API surface identical between Node and
   * browsers. */
  fetch?: typeof fetch;
}
