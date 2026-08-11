# Changelog

## [0.1.0] - 2026-08-11

### Added

- OpenAI-compatible streaming proxy for llama.cpp workers.
- Healthy round-robin routing, bounded retries, circuit opening, and automatic recovery.
- Per-client rate limiting, request-size limits, strict JSON configuration, and graceful shutdown.
- Structured JSON logs, Prometheus metrics, alerts, and read-only worker status.
- Windows llama.cpp launcher using an F-drive cache, deterministic fault demo, and non-root Docker stack.
