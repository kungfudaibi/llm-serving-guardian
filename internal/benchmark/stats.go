package benchmark

import (
	"slices"
	"time"
)

type Sample struct {
	TTFT             time.Duration
	E2E              time.Duration
	CompletionTokens int
	StartedAt        time.Time
	FinishedAt       time.Time
	Worker           string
	Attempts         int
	RequestID        string
}

type LatencyStats struct {
	P50MS float64 `json:"p50Ms"`
	P95MS float64 `json:"p95Ms"`
	P99MS float64 `json:"p99Ms"`
}

type Summary struct {
	Requests              int          `json:"requests"`
	OutputTokens          int          `json:"outputTokens"`
	WallSeconds           float64      `json:"wallSeconds"`
	RequestsPerSecond     float64      `json:"requestsPerSecond"`
	OutputTokensPerSecond float64      `json:"outputTokensPerSecond"`
	TTFT                  LatencyStats `json:"ttft"`
	TPOT                  LatencyStats `json:"tpot"`
	E2E                   LatencyStats `json:"e2e"`
}

func Summarize(samples []Sample, wall time.Duration) Summary {
	summary := Summary{Requests: len(samples), WallSeconds: wall.Seconds()}
	if len(samples) == 0 {
		return summary
	}
	ttft := make([]time.Duration, 0, len(samples))
	tpot := make([]time.Duration, 0, len(samples))
	e2e := make([]time.Duration, 0, len(samples))
	for _, sample := range samples {
		summary.OutputTokens += sample.CompletionTokens
		ttft = append(ttft, sample.TTFT)
		e2e = append(e2e, sample.E2E)
		if sample.CompletionTokens > 1 && sample.E2E > sample.TTFT {
			tpot = append(tpot, (sample.E2E-sample.TTFT)/time.Duration(sample.CompletionTokens-1))
		}
	}
	if wall > 0 {
		summary.RequestsPerSecond = float64(summary.Requests) / wall.Seconds()
		summary.OutputTokensPerSecond = float64(summary.OutputTokens) / wall.Seconds()
	}
	summary.TTFT = latencyStats(ttft)
	summary.TPOT = latencyStats(tpot)
	summary.E2E = latencyStats(e2e)
	return summary
}

func latencyStats(values []time.Duration) LatencyStats {
	if len(values) == 0 {
		return LatencyStats{}
	}
	slices.Sort(values)
	return LatencyStats{
		P50MS: percentileMilliseconds(values, 0.50),
		P95MS: percentileMilliseconds(values, 0.95),
		P99MS: percentileMilliseconds(values, 0.99),
	}
}

func percentileMilliseconds(values []time.Duration, quantile float64) float64 {
	position := float64(len(values)-1) * quantile
	lower := int(position)
	upper := min(lower+1, len(values)-1)
	fraction := position - float64(lower)
	value := float64(values[lower]) + (float64(values[upper])-float64(values[lower]))*fraction
	return value / float64(time.Millisecond)
}
