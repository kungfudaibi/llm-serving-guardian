# Implementation Plan: LLM Serving Guardian

## Overview

Deliver a local-first Go inference gateway as thin, independently verifiable slices: contract and configuration, resilient worker selection, streaming proxy behavior, telemetry and operations, then packaging and runtime proof.

## Architecture Decisions

- Keep active health and passive circuit state in one concurrency-safe pool so routing reads one source of truth.
- Retry only before downstream headers are written; this makes streaming semantics correct and predictable.
- Use JSON configuration and environment expansion, keeping the binary simple and portable.
- Bind to loopback by default because the MVP has no management authentication.
- Use a mock OpenAI-compatible worker for repeatable CI and failure demonstrations; real llama.cpp remains an external process.

## Dependency Order

```text
configuration -> worker state/pool -> health loop -> proxy/retry
                                      |             |
                                      +-> status API +-> metrics/logging
all runtime slices -> scripts/docs/container assets -> end-to-end verification
```

## Phases

1. Foundation: module, configuration contract, validation, and CLI skeleton.
2. Resilience core: worker pool, health probes, passive failures, circuit recovery.
3. Request path: rate limit, OpenAI-compatible streaming proxy, bounded retry.
4. Operations: JSON logs, RED/upstream metrics, health/readiness/admin endpoints, graceful shutdown.
5. Demo and packaging: mock worker, Windows scripts, Docker/Prometheus assets, runbook and README.
6. Verification: focused tests per slice, full race/vet/build gates, and localhost smoke test.

## Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Streaming response is retried after bytes are sent | Corrupt client response | Retry only after `client.Do` and before copying headers/body |
| Concurrent checks and requests race on worker state | Incorrect routing | Mutex-protected state and `go test -race` |
| Retry duplicates non-idempotent generation work | Wasted compute | Retry only network/5xx failures before response commitment; cap attempts |
| Metrics cardinality grows unbounded | Prometheus instability | Fixed route/status/worker labels only |
| 4 GB VRAM cannot hold two real workers | Demo fails | One real llama.cpp worker plus lightweight mock worker |

## Checkpoints

- Foundation: configuration tests pass and binary builds.
- Resilience: deterministic pool and circuit tests pass under race detector.
- Request path: integration tests prove streaming and failover.
- Complete: all tests, vet, build, smoke test, docs, and secret scan pass.
