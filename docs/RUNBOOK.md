# Operations Runbook

## First queries

```powershell
Invoke-RestMethod http://127.0.0.1:8090/readyz
Invoke-RestMethod http://127.0.0.1:8090/admin/workers
Invoke-WebRequest http://127.0.0.1:8090/metrics
Get-Content .run\guardian.log -Tail 50
```

Use `requestId` to correlate `request_completed`, `upstream_attempt_failed`, and `upstream_stream_interrupted` events. Logs intentionally do not contain prompts or responses.

## No healthy workers

Meaning: `sum(llm_guardian_worker_healthy) == 0` for one minute; clients receive 503.

1. Inspect `/admin/workers` for `lastError`, `circuitState`, and `circuitOpenUntil`.
2. Query each worker's `/health` endpoint directly.
3. Check llama.cpp output for model loading, CUDA allocation failure, or an occupied port.
4. Confirm the configured URL is reachable from Guardian's network namespace. Docker uses service DNS names; a Windows-native Guardian uses `127.0.0.1`.
5. Restore the worker. After cooldown, a successful probe automatically closes its circuit; no Guardian restart is required.

## High server error rate

Meaning: more than 5% of `/v1/*` requests returned a 5xx class for five minutes.

1. Compare `llm_guardian_upstream_attempts_total` by `worker` and `outcome`.
2. Inspect latency histograms to distinguish slow headers from immediate 5xx responses.
3. Search JSON logs by the affected worker and request ID.
4. If only one worker fails, leave it out of rotation and repair it. If all workers fail, reduce concurrency or model/context memory pressure.
5. Confirm recovery with `/readyz`, `/admin/workers`, and a real streaming request.

## Deliberate fault test

```powershell
.\scripts\start-demo.ps1
.\scripts\demo-failure.ps1
```

Expected evidence:

- The client still receives 200 from the other worker.
- `X-Guardian-Attempts` becomes `2` when the failing worker is selected first.
- `mock-one` becomes unhealthy/open after the configured failure threshold.
- Metrics increment the `5xx` outcome and health gauge changes to zero.
- Once failure mode is disabled and cooldown passes, a probe restores `mock-one`.
