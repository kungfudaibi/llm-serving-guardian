# V100 baseline results

These measurements exercise the full streaming path through Guardian to vLLM. They were collected on 2026-08-11 with one Tesla V100 PCIe 32 GB, Qwen2.5-1.5B-Instruct in FP16, vLLM 0.6.6, and XFormers. The host CPU is an Intel Xeon Gold 6132. Each point contains two sequential warmups followed by 64 measured requests that each produced 64 output tokens.

| Concurrency | Requests/s | Output tokens/s | TTFT p50 / p95 / p99 (ms) | TPOT p50 (ms) | E2E p50 / p95 / p99 (ms) |
|---:|---:|---:|---:|---:|---:|
| 1 | 1.99 | 127.65 | 33.25 / 33.88 / 34.52 | 7.43 | 501.36 / 502.00 / 502.24 |
| 4 | 7.33 | 468.88 | 35.04 / 36.96 / 37.20 | 8.11 | 545.88 / 547.42 / 547.71 |
| 8 | 13.83 | 885.34 | 37.23 / 64.28 / 64.37 | 8.43 | 568.57 / 595.49 / 595.59 |
| 16 | 25.29 | 1618.37 | 77.78 / 78.59 / 78.77 | 8.80 | 632.41 / 633.82 / 634.25 |
| 32 | 40.71 | 2605.75 | 116.02 / 117.23 / 117.62 | 10.62 | 785.32 / 788.44 / 788.78 |

At concurrency 32, output-token throughput is 20.4 times the concurrency-1 baseline, while TTFT p99 rises from 34.52 ms to 117.62 ms and E2E p99 rises from 502.24 ms to 788.78 ms. This makes the throughput/latency trade-off visible instead of presenting throughput alone.

The five JSON files in `results/` are the source of truth and include all 320 raw samples. Guardian recorded 330 successful upstream attempts, exactly matching five runs of 64 measured requests plus two warmups, with no Guardian warnings or errors. This is an initial single-run baseline, not a production capacity guarantee; use the procedure in [BENCHMARKING.md](../docs/BENCHMARKING.md) for repetitions and comparison runs.
