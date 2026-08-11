# Spec: LLM Serving Guardian

## Objective

Build a portfolio-ready Go gateway for local LLM inference. It sits in front of one or more llama.cpp-compatible HTTP servers, keeps unhealthy workers out of rotation, retries requests before response streaming begins, and exposes enough telemetry to explain routing and failures.

The primary user is an AI-infrastructure engineer running local or lab inference workers. The MVP deliberately avoids databases, accounts, a web dashboard, and Kubernetes so it remains runnable on a 16 GB Windows laptop.

## Tech Stack

- Go 1.26, standard library first
- Prometheus Go client for metrics
- JSON configuration to avoid a configuration parser dependency
- llama.cpp's OpenAI-compatible HTTP server as the real backend
- Docker/Compose for an optional reproducible guardian and Prometheus environment

## Commands

```powershell
# Build
go build ./...

# Test, including data-race checks
go test -race ./...

# Format and static analysis
gofmt -w .
go vet ./...

# Run
go run ./cmd/guardian -config ./configs/local.json
```

## Project Structure

```text
cmd/guardian/       application entry point
internal/config/    validated JSON configuration
internal/guardian/  worker state, routing, proxying, health checks
internal/telemetry/ bounded-cardinality Prometheus metrics
configs/            example runtime configuration
deploy/             Prometheus and Docker assets
scripts/            Windows setup and failure-demo helpers
docs/               API, architecture, operations, and this spec
tasks/              implementation plan and completion checklist
```

## Code Style

Use small packages, explicit constructor validation, `context.Context` for cancellation, and errors that add operation context:

```go
func NewPool(workers []WorkerConfig) (*Pool, error) {
	if len(workers) == 0 {
		return nil, errors.New("worker pool requires at least one worker")
	}
	return &Pool{workers: workers}, nil
}
```

Names use standard Go conventions. Public identifiers have doc comments; JSON fields use camelCase. `gofmt` is authoritative.

## Testing Strategy

- Unit tests cover configuration validation, round-robin selection, failure thresholds, recovery, request IDs, and rate limiting.
- Integration tests use `httptest.Server` workers and exercise proxy success, retry, streaming, request-size limits, and readiness.
- Tests use only localhost and need no downloaded model.
- `go test -race ./...`, `go vet ./...`, and `go build ./...` are release gates.

## Boundaries

- Always: validate configuration and inbound sizes; propagate cancellation and request IDs; redact authorization values; keep metric labels bounded; use timeouts for health probes.
- Ask first: add authentication, persistent storage, Kubernetes manifests, or mutate worker processes through the API.
- Never: log prompts, model responses, tokens, secrets, or authorization headers; retry after response bytes have reached the client; expose management endpoints on a public address by default.

## Observability Questions

1. Which worker served a request, and did a retry occur?
2. Which workers are healthy, unhealthy, or held open by the circuit breaker?
3. What are request rate, error rate, and latency distributions by stable route and status class?
4. Are upstream failures or health-check failures causing degraded readiness?

## Success Criteria

- Proxy `/v1/*` to healthy llama.cpp-compatible workers and preserve streaming responses.
- Route healthy workers round-robin and never select an unhealthy/open-circuit worker.
- Retry connection errors and HTTP 5xx responses on another eligible worker before forwarding headers.
- Mark repeatedly failing workers unavailable, then allow automatic recovery after a cooldown and successful probe.
- Enforce configurable per-client rate and request-body limits.
- Expose `/healthz`, `/readyz`, `/admin/workers`, and Prometheus `/metrics`.
- Emit structured JSON logs with a safe request ID, route, status, duration, worker, and attempt count.
- Shut down gracefully and pass unit, integration, race, vet, and build checks.
- Include a Windows-native launcher using the installed llama.cpp path and F-drive cache, plus a reproducible failure demo.

## Assumptions

- Backends implement llama.cpp-compatible `/health` and `/v1/*` endpoints.
- The first real worker runs on `127.0.0.1:8081`; an included mock worker may run on `127.0.0.1:8082` for deterministic failure demos.
- No authentication is included in the local-only MVP; the default listener is `127.0.0.1`.
- Model downloads live under `F:\llm-serving-guardian\llama-cache` through `LLAMA_CACHE`.

## Open Questions

None block the MVP. Authentication, semantic routing, token-aware admission control, OpenTelemetry tracing, and Kubernetes integration are documented as follow-up milestones.
