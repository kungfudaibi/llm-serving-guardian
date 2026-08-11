# Reproducible benchmarks

The benchmark path measures the complete client-to-Guardian-to-worker streaming path. It waits for a non-empty content delta for time to first token (TTFT), uses the final usage event for output-token counts, and saves every sample so the summary can be audited.

Reports never contain the API key or prompt text. They contain the prompt's SHA-256 digest, experiment parameters, UTC timestamp, aggregate percentiles, and raw measurements. An output path must not already exist.

## Pinned s3 environment

The current school-server baseline uses one Tesla V100 PCIe 32 GB, Qwen2.5-1.5B-Instruct in FP16, and vLLM 0.6.6. This older vLLM release is deliberate: current vLLM CUDA binaries require compute capability 7.5, while the V100 is compute capability 7.0.

All downloaded environments, models, caches, and runtime logs live below the user's home directory:

```bash
source "$HOME/.venvs/llm-serving-guardian-vllm066/bin/activate"
export HF_HOME="$HOME/.cache/llm-serving-guardian/huggingface"
export VLLM_NO_USAGE_STATS=1
export CUDA_VISIBLE_DEVICES=0
export HF_HUB_OFFLINE=1

vllm serve Qwen/Qwen2.5-1.5B-Instruct \
  --host 127.0.0.1 \
  --port 8000 \
  --dtype half \
  --max-model-len 4096 \
  --gpu-memory-utilization 0.80 \
  --served-model-name qwen2.5-1.5b-instruct
```

In a second shell, start Guardian:

```bash
go run ./cmd/guardian -config ./configs/s3-vllm.json
```

## Run the matrix

Use a fixed prompt, token limit, model configuration, and request count. Change only concurrency between runs. The recommended initial matrix is `1, 4, 8, 16, 32`, with 64 measured requests and two warmups per point.

```bash
mkdir -p benchmarks/results

go run ./cmd/benchmark \
  -endpoint http://127.0.0.1:8090/v1/chat/completions \
  -model qwen2.5-1.5b-instruct \
  -requests 64 \
  -concurrency 1 \
  -warmup 2 \
  -max-tokens 64 \
  -temperature 0 \
  -hardware "inspur3; 1x Tesla V100-PCIE-32GB; Xeon Gold 6132; vLLM 0.6.6; FP16; XFormers" \
  -label vllm066-qwen25-1.5b-fp16-guardian-c1 \
  -output benchmarks/results/v100-vllm066-c1.json
```

If authentication is enabled, set `BENCHMARK_API_KEY`; it is sent as a bearer token and is never written to the report. Keep server and Guardian logs alongside the experiment metadata when diagnosing an outlier, but do not commit secrets or prompt bodies.

For claims beyond an initial baseline, run at least three independent repetitions per concurrency after a fresh warmup and report the variation. A single run is useful evidence, but it is not a capacity guarantee.

## Inject a real worker failure

On the configured s3 host, the following script checks that both GPUs and ports are idle, starts one vLLM worker per V100, runs continuous streaming traffic, terminates GPU0, restarts it, and cleans up only the exact processes it created:

```bash
./scripts/run-s3-failover.sh
```

The availability client continues after individual request failures and polls `/admin/workers` throughout the experiment. Its report records request-level routing, retries, stream state, worker snapshots, the exact fault timestamp, circuit detection, and recovery. Runtime binaries and logs remain under `$HOME/.cache/llm-serving-guardian/failover/`; the auditable JSON report is written under `benchmarks/results/` without overwriting an existing result.
