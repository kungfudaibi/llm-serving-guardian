# LLM Serving Guardian

A local-first, production-shaped Go gateway for llama.cpp-compatible inference workers. Guardian keeps unhealthy workers out of rotation, retries safe pre-stream failures, preserves server-sent-event streaming, and exposes the evidence needed to operate the request path.

The project is intentionally small enough to run on a 16 GB Windows laptop with a 4 GB NVIDIA GPU: use one real llama.cpp worker and the included mock worker, or run the completely model-free two-worker demo.

## What it demonstrates

- Round-robin routing across healthy workers
- Active `/health` probes plus passive failure detection
- Circuit opening, cooldown, and automatic recovery
- Retry on connection errors and upstream 5xx before response commitment
- Streaming `/v1/*` proxy behavior compatible with llama.cpp's OpenAI-style API
- Per-client token-bucket limiting and request-body limits
- Structured JSON logs with correlation IDs and no prompt/body logging
- Prometheus RED metrics, worker health metrics, and symptom-based alerts
- Graceful shutdown, strict startup configuration, tests, race checks, and non-root containers

## Architecture

```mermaid
flowchart LR
    C[OpenAI-compatible client] -->|/v1/*| G[Go Guardian]
    G --> L[Rate limit and size guard]
    L --> P[Healthy round-robin pool]
    P --> W1[llama.cpp worker]
    P --> W2[Mock or second worker]
    H[Active health loop] --> W1
    H --> W2
    H --> P
    G --> M[/metrics]
    G --> A[/admin/workers]
    M --> PR[Prometheus]
```

Routing and circuit state share one mutex-protected pool. A failed attempt is retried only while no headers or body bytes have reached the client, so an SSE response is never duplicated mid-stream. See [architecture details](docs/ARCHITECTURE.md) and the [API contract](docs/API.md).

## Quick start: model-free failure demo

Requirements: Go 1.26+ and PowerShell.

```powershell
cd D:\code\简历\llm-serving-guardian
.\scripts\start-demo.ps1
.\scripts\demo-failure.ps1
.\scripts\stop-demo.ps1
```

The script starts two lightweight mock workers on ports `18081` and `18082`, then Guardian on `8090`. The failure script makes `mock-one` return 503, shows the request being served by `mock-two`, waits for circuit recovery, and prints worker state throughout.

Manual request:

```powershell
$body = @{
  model = 'guardian-mock'
  messages = @(@{ role = 'user'; content = 'hello' })
  stream = $false
} | ConvertTo-Json -Depth 4

Invoke-WebRequest `
  -Method Post `
  -Uri 'http://127.0.0.1:8090/v1/chat/completions' `
  -ContentType 'application/json' `
  -Body $body
```

Inspect the response headers `X-Guardian-Worker`, `X-Guardian-Attempts`, and `X-Request-Id`.

## Run with your llama.cpp installation

The provided launcher matches this machine's installation and stores Hugging Face downloads on F:

```powershell
.\scripts\start-llama.ps1
```

It invokes:

```text
D:\Downloads\llama-b10343-bin-win-cuda-12.4-x64\llama-server.exe
```

and loads `F:\llm-serving-guardian\models\gemma-3-1b-it-Q4_K_M.gguf`. If the file is absent, the launcher downloads the official 806 MB artifact through Windows HTTPS, verifies its length and official SHA-256 digest, and only then promotes the temporary file. In another terminal:

```powershell
go run ./cmd/mock-worker -listen 127.0.0.1:8082 -name demo-worker
go run ./cmd/guardian -config ./configs/local.json
```

Check readiness, workers, and metrics:

```powershell
Invoke-RestMethod http://127.0.0.1:8090/readyz
Invoke-RestMethod http://127.0.0.1:8090/admin/workers
Invoke-WebRequest http://127.0.0.1:8090/metrics
```

If the 1B Q4 model does not fit alongside other GPU applications, reduce `--n-gpu-layers` in [start-llama.ps1](scripts/start-llama.ps1) or close other GPU workloads.

## Docker and Prometheus

The Compose profile is model-free and uses two mock workers:

```powershell
docker compose up --build
```

- Guardian: http://127.0.0.1:8090
- Prometheus: http://127.0.0.1:9090

Every service has `restart: "no"`; Docker Desktop reopening will not automatically start this stack. Stop it with `docker compose down`. The images run as a non-root user.

## Configuration

Guardian accepts one strict JSON file through `-config`. Unknown fields, unsafe values, and references to unset environment variables fail startup. `${ENVIRONMENT_VARIABLE}` placeholders are expanded before decoding.

```json
{
  "server": {
    "listen": "127.0.0.1:8090",
    "requestTimeout": "5m",
    "shutdownTimeout": "10s",
    "maxBodyBytes": 10485760,
    "rateLimitRPS": 10,
    "rateLimitBurst": 20
  },
  "health": {"interval": "5s", "timeout": "2s"},
  "circuit": {"failureThreshold": 3, "cooldown": "15s"},
  "proxy": {"maxAttempts": 2},
  "workers": [{"name": "local", "url": "http://127.0.0.1:8081", "apiKey": "${WORKER_API_KEY}"}]
}
```

The default listener is loopback because this MVP has no management authentication. Do not bind it publicly without putting authentication and TLS in front of it.

## Development gates

```powershell
go test ./...
go vet ./...
go build ./...

# Windows' race detector needs a C compiler. The container command supplies one.
docker run --rm -v "${PWD}:/src" -w /src golang:1.26.5-alpine3.24 `
  sh -c "apk add --no-cache build-base && go test -race ./..."
```

Tests use only loopback `httptest` workers; they do not download a model. For load experiments, record the hardware, model, quantization, context size, concurrency, and p50/p95/p99 instead of publishing unrepeatable headline numbers.

The included benchmark clients produce raw samples for both steady-state performance and fault availability. See the [measured V100 baseline](benchmarks/README.md), [real dual-V100 failover result](benchmarks/FAILOVER.md), and [reproduction procedure](docs/BENCHMARKING.md).

## Operational endpoints

| Endpoint | Purpose |
|---|---|
| `GET /healthz` | Process liveness |
| `GET /readyz` | At least one eligible worker |
| `GET /admin/workers` | Read-only circuit and health snapshot |
| `GET /metrics` | Prometheus exposition |
| `/v1/*` | Guarded inference proxy |

See [RUNBOOK.md](docs/RUNBOOK.md) for induced-failure diagnosis and recovery.

## Roadmap

- Token-aware admission control and queueing
- OpenTelemetry spans across gateway and workers
- Authenticated management listener
- Semantic/model-capability routing
- Kubernetes discovery, Pod readiness integration, and load-shed policies

## Verified upstream patterns

- llama.cpp server API and monitoring behavior: https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md
- llama.cpp cache precedence (`LLAMA_CACHE`): https://github.com/ggml-org/llama.cpp/blob/master/common/hf-cache.cpp
- Prometheus custom registry and HTTP exposition: https://prometheus.io/docs/guides/go-application/
- Go graceful HTTP shutdown: https://pkg.go.dev/net/http@go1.26.5#Server.Shutdown
- Official Go container tags: https://hub.docker.com/_/golang

## License

MIT
