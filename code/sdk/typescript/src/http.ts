// Minimal fetch-based HTTP client for the gateway's REST/grpc-gateway
// transcoding — the transport layer underneath UdalClient. Isomorphic
// between Node (>=20, built-in fetch) and browsers; the injectable
// `fetchImpl` exists purely as a test seam (see http.test.ts).

import { errorFromHttpStatus, errorFromNetworkException, errorFromRpcStatus } from "./errors.js";

export interface RequestOptions {
  method: "GET" | "PUT" | "POST";
  path: string;
  body?: unknown;
  signal?: AbortSignal;
}

// Shape of the JSON error body grpc-gateway's default error handler emits
// on a non-2xx response — a `google.rpc.Status` message (see the
// `rpcStatus` OpenAPI schema in code/api/openapi/udal/v1/device.openapi.v3.json).
interface RpcStatusBody {
  code?: number;
  message?: string;
}

/** Splits a `{property_path=**}`-style grpc-gateway wildcard path segment
 * on `/` and percent-encodes each piece individually, so the path's own
 * separating slashes survive (encoding the whole string as one unit would
 * turn them into `%2F` and break the route match). */
export function encodePathSegments(path: string): string {
  return path
    .split("/")
    .map((segment) => encodeURIComponent(segment))
    .join("/");
}

export class HttpClient {
  private readonly baseUrl: string;
  private readonly apiKey: string | undefined;
  private readonly fetchImpl: typeof fetch;

  constructor(baseUrl: string, apiKey: string | undefined, fetchImpl: typeof fetch = fetch) {
    this.baseUrl = baseUrl;
    this.apiKey = apiKey;
    this.fetchImpl = fetchImpl;
  }

  async request<T>(opts: RequestOptions): Promise<T> {
    const headers: Record<string, string> = { "content-type": "application/json" };
    if (this.apiKey) {
      headers["x-api-key"] = this.apiKey;
    }

    let res: Response;
    try {
      res = await this.fetchImpl(new URL(opts.path, this.baseUrl), {
        method: opts.method,
        headers,
        body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
        signal: opts.signal,
      });
    } catch (err) {
      throw errorFromNetworkException(err);
    }

    const text = await res.text();
    let json: unknown;
    if (text.length > 0) {
      try {
        json = JSON.parse(text);
      } catch {
        // Not JSON at all — e.g. a reverse proxy/load balancer in front of
        // the gateway returning its own HTML error page. Fall through to
        // the HTTP-status mapping below rather than surfacing a raw
        // SyntaxError.
        json = undefined;
      }
    }

    if (!res.ok) {
      const body = json as RpcStatusBody | undefined;
      if (typeof body?.code === "number") {
        throw errorFromRpcStatus(body.code, body.message ?? "");
      }
      throw errorFromHttpStatus(res.status, (json as { message?: string } | undefined)?.message ?? res.statusText);
    }

    return json as T;
  }
}
