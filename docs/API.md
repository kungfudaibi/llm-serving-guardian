# HTTP API Contract

All JSON error responses have one stable shape:

```json
{"error":{"code":"NO_HEALTHY_WORKER","message":"no healthy inference worker is available"},"requestId":"..."}
```

Every response includes `X-Request-Id`. A valid caller-provided ID is preserved; otherwise the gateway generates one.

## Proxy surface

`/v1/*` accepts the original method, query string, headers, and body expected by llama.cpp. Hop-by-hop headers are removed. The gateway preserves upstream status and content headers and streams the response body without buffering the complete generation.

Possible gateway errors are `400 INVALID_REQUEST`, `413 REQUEST_TOO_LARGE`, `429 RATE_LIMITED`, `502 UPSTREAM_FAILURE`, `503 NO_HEALTHY_WORKER`, and `504 REQUEST_TIMEOUT`.

## Guardian surface

- `GET /healthz` — process liveness; returns `200` while the HTTP process is running.
- `GET /readyz` — returns `200` with worker counts when at least one worker is eligible, otherwise `503`.
- `GET /admin/workers` — read-only snapshot of worker health, consecutive failures, circuit state, and last check. It never exposes configured authorization headers.
- `GET /metrics` — Prometheus exposition endpoint.

All other routes return `404 NOT_FOUND`. Methods unsupported by a guardian endpoint return `405 METHOD_NOT_ALLOWED`.
