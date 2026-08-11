# Real dual-V100 failover experiment

This experiment tests the reason Guardian exists: maintaining one client endpoint while a real GPU inference worker disappears and later returns. It ran on 2026-08-11 with two Tesla V100 PCIe 32 GB GPUs, one vLLM 0.6.6 process per GPU, Qwen2.5-1.5B-Instruct in FP16, eight concurrent streaming clients, and 64 output tokens per request.

## Result

| Measure | Observed value |
|---|---:|
| Completed requests | 988 |
| Successful requests | 984 |
| User-visible failures | 4 |
| Request success rate | 99.595% |
| Successful pre-stream retries | 2 |
| Interrupted in-flight streams | 4 |
| GPU0 circuit detection | 0.802 s |
| GPU0 full recovery after fault | 60.002 s |
| Successful requests served by GPU0 / GPU1 | 129 / 855 |

Six request paths encountered the failure. Guardian transparently retried two requests whose downstream response had not started. Four requests were already streaming from GPU0 when it terminated, so retrying them would have duplicated or corrupted output; those four clients received an incomplete stream and reported failure. The remaining workload continued through GPU1.

The 60-second recovery measurement includes the intentional restart delay, model loading, CUDA graph capture, vLLM readiness, and Guardian's successful health probe. It is not 60 seconds of Guardian processing.

## Timeline

| Event | Time relative to injected fault |
|---|---:|
| SIGTERM sent to GPU0 vLLM and fault marker written | 0.000 s |
| Guardian first observed GPU0 unavailable | 0.802 s |
| Four existing GPU0 streams ended incomplete | approximately 5.056 s |
| Restarted GPU0 became healthy and rejoined rotation | 60.002 s |

The report contains 988 request records and 805 worker snapshots with no snapshot polling errors. Guardian's structured log independently contains four `upstream_stream_interrupted` events and two `upstream_attempt_failed` events, matching the client-visible failures and successful retries.

## Interpretation and limits

This demonstrates both halves of the gateway's policy: safe retry before response commitment and explicit failure after an SSE stream has begun. It also shows that a single surviving worker can keep the endpoint available while the failed model process reloads.

This is one controlled process-termination experiment with a small model and aggressive experiment-specific health settings (`500ms` probes and a failure threshold of one). It does not claim production capacity or cover network partitions, GPU hardware faults, correlated failures, or larger models. The raw source of truth is [v100-dual-vllm066-failover.json](results/v100-dual-vllm066-failover.json).
