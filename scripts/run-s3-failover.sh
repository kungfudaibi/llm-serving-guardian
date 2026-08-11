#!/usr/bin/env bash
set -euo pipefail

project_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
user_root=/home/inspur/nfs/home/qiegewala
venv="$user_root/.venvs/llm-serving-guardian-vllm066"
cache_root="$user_root/.cache/llm-serving-guardian"
run_id=$(date -u +%Y%m%dT%H%M%SZ)
runtime="$cache_root/failover/$run_id"
result_path=${1:-"$project_root/benchmarks/results/v100-dual-vllm066-failover.json"}
fault_marker="$runtime/fault-at.txt"
hardware="inspur3; 2x Tesla V100-PCIE-32GB; Xeon Gold 6132; vLLM 0.6.6; FP16; XFormers"

vllm0_pid=
vllm1_pid=
guardian_pid=
benchmark_pid=

stop_pid() {
  local pid=$1
  if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
    kill -TERM "$pid" 2>/dev/null || true
    for _ in $(seq 1 20); do
      kill -0 "$pid" 2>/dev/null || return
      sleep 0.25
    done
    kill -KILL "$pid" 2>/dev/null || true
  fi
}

cleanup() {
  stop_pid "$benchmark_pid"
  stop_pid "$guardian_pid"
  stop_pid "$vllm0_pid"
  stop_pid "$vllm1_pid"
}
trap cleanup EXIT INT TERM

for command in curl go jq nvidia-smi ss; do
  command -v "$command" >/dev/null || { echo "missing command: $command" >&2; exit 1; }
done
[[ -x "$venv/bin/vllm" ]] || { echo "missing vLLM environment: $venv" >&2; exit 1; }
[[ ! -e "$result_path" ]] || { echo "result already exists: $result_path" >&2; exit 1; }
if ss -ltn | grep -Eq ':(8000|8001|8090)[[:space:]]'; then
  echo "ports 8000, 8001, or 8090 are already in use" >&2
  exit 1
fi
if nvidia-smi --query-compute-apps=pid --format=csv,noheader,nounits | grep -Eq '[0-9]'; then
  echo "a GPU compute process is already running" >&2
  exit 1
fi

mkdir -p "$runtime" "$(dirname "$result_path")"
export HF_HOME="$cache_root/huggingface"
export VLLM_NO_USAGE_STATS=1
export HF_HUB_OFFLINE=1
export GOCACHE="$cache_root/go-build"
export GOMODCACHE="$cache_root/go-mod"
export GOTOOLCHAIN=auto

start_vllm() {
  local gpu=$1
  local port=$2
  local log=$3
  local pid_variable=$4
  CUDA_VISIBLE_DEVICES="$gpu" "$venv/bin/vllm" serve Qwen/Qwen2.5-1.5B-Instruct \
    --host 127.0.0.1 --port "$port" --dtype half --max-model-len 4096 \
    --gpu-memory-utilization 0.80 --served-model-name qwen2.5-1.5b-instruct \
    >"$log" 2>&1 &
  printf -v "$pid_variable" '%s' "$!"
}

wait_ready() {
  local url=$1
  local log=$2
  for _ in $(seq 1 60); do
    curl -fsS --max-time 2 "$url" >/dev/null && return
    sleep 1
  done
  tail -n 80 "$log" >&2
  return 1
}

cd "$project_root"
go build -o "$runtime/guardian" ./cmd/guardian
go build -o "$runtime/failover-benchmark" ./cmd/failover-benchmark

start_vllm 0 8000 "$runtime/vllm-gpu0.log" vllm0_pid
start_vllm 1 8001 "$runtime/vllm-gpu1.log" vllm1_pid
wait_ready http://127.0.0.1:8000/v1/models "$runtime/vllm-gpu0.log"
wait_ready http://127.0.0.1:8001/v1/models "$runtime/vllm-gpu1.log"

"$runtime/guardian" -config "$project_root/configs/s3-vllm-dual.json" >"$runtime/guardian.log" 2>&1 &
guardian_pid=$!
for _ in $(seq 1 30); do
  if [[ $(curl -fsS --max-time 2 http://127.0.0.1:8090/admin/workers 2>/dev/null | jq '[.[] | select(.isHealthy)] | length') == 2 ]]; then
    break
  fi
  sleep 0.5
done
[[ $(curl -fsS http://127.0.0.1:8090/admin/workers | jq '[.[] | select(.isHealthy)] | length') == 2 ]]

"$runtime/failover-benchmark" \
  -duration 80s -concurrency 8 -poll-interval 100ms -max-tokens 64 \
  -target-worker vllm-gpu0 -fault-marker "$fault_marker" \
  -hardware "$hardware" -label dual-v100-kill-and-recover \
  -output "$result_path" >"$runtime/report-stdout.json" &
benchmark_pid=$!

sleep 10
date -u +'%Y-%m-%dT%H:%M:%S.%NZ' >"$fault_marker"
stop_pid "$vllm0_pid"
vllm0_pid=

sleep 10
start_vllm 0 8000 "$runtime/vllm-gpu0-restarted.log" vllm0_pid
wait_ready http://127.0.0.1:8000/v1/models "$runtime/vllm-gpu0-restarted.log"

wait "$benchmark_pid"
benchmark_pid=
jq '.summary' "$result_path"
echo "runtime evidence: $runtime"
echo "report: $result_path"
