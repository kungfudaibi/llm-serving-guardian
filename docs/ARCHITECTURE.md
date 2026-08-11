# Architecture

## Request lifecycle

1. The HTTP boundary validates or generates `X-Request-Id` and assigns a fixed route label.
2. The token bucket isolates rate capacity by the TCP peer address; forwarded headers are not trusted.
3. The body is read once under the configured byte limit so a pre-response retry can replay the same request.
4. The pool selects the next healthy worker that has not already been attempted.
5. Guardian sends the request with hop-by-hop headers removed and the worker-specific authorization value, when configured.
6. Connection errors and 5xx headers update passive circuit state and may select another worker.
7. Once a non-5xx response is selected, headers are committed and the body is copied and flushed incrementally. No retry is possible after this point.

## Worker state machine

```text
PROBING --successful health probe--> CLOSED/healthy
CLOSED  --failure below threshold--> CLOSED/healthy
CLOSED  --threshold reached-------> OPEN/unhealthy
OPEN    --cooldown expires--------> PROBING/unhealthy
PROBING --successful health probe--> CLOSED/healthy
PROBING --failed health probe-----> OPEN/unhealthy
```

Workers start unavailable. This prevents traffic from racing ahead of the first real health result. Both request failures and health failures count toward the same threshold; any successful health check or upstream response resets consecutive failures.

## Concurrency

Worker configuration is immutable after startup. A single `sync.RWMutex` protects health, circuit, and round-robin cursor state. HTTP handlers, probes, and Prometheus collectors never expose or mutate the worker API key. The release gate includes the Go race detector in Linux because Windows requires an external C toolchain.

## Failure semantics

- No eligible worker before any attempt: `503 NO_HEALTHY_WORKER`.
- Every attempted worker fails transport or returns 5xx: `502 UPSTREAM_FAILURE`.
- Request deadline expires: `504 REQUEST_TIMEOUT`.
- Stream breaks after response commitment: the connection ends and a structured warning is emitted; Guardian cannot safely replace the response.

## Security boundaries

- JSON config is strict and validates schemes, durations, sizes, and unique worker names.
- The default bind address is loopback.
- Prompt bodies, completions, API keys, and authorization headers are never logged or returned by the admin API.
- Request IDs accept only a bounded safe character set.
- Containers run without root privileges.
- This MVP is not an internet-facing API gateway; deploy authentication, TLS, and trusted-proxy handling before widening the listener.
