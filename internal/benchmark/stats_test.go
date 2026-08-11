package benchmark

import (
	"testing"
	"time"
)

func TestSummarizeCalculatesLatencyPercentilesAndThroughput(t *testing.T) {
	summary := Summarize([]Sample{
		{TTFT: 10 * time.Millisecond, E2E: 100 * time.Millisecond, CompletionTokens: 10},
		{TTFT: 20 * time.Millisecond, E2E: 200 * time.Millisecond, CompletionTokens: 20},
		{TTFT: 30 * time.Millisecond, E2E: 300 * time.Millisecond, CompletionTokens: 30},
		{TTFT: 40 * time.Millisecond, E2E: 400 * time.Millisecond, CompletionTokens: 40},
	}, 2*time.Second)

	if summary.Requests != 4 || summary.OutputTokens != 100 {
		t.Fatalf("counts = requests:%d tokens:%d", summary.Requests, summary.OutputTokens)
	}
	if summary.RequestsPerSecond != 2 || summary.OutputTokensPerSecond != 50 {
		t.Fatalf("throughput = requests:%f tokens:%f", summary.RequestsPerSecond, summary.OutputTokensPerSecond)
	}
	if summary.TTFT.P50MS != 25 || summary.TTFT.P95MS != 38.5 || summary.TTFT.P99MS != 39.7 {
		t.Fatalf("TTFT percentiles = %+v", summary.TTFT)
	}
	if summary.E2E.P50MS != 250 || summary.E2E.P95MS != 385 || summary.E2E.P99MS != 397 {
		t.Fatalf("E2E percentiles = %+v", summary.E2E)
	}
	if summary.TPOT.P50MS < 9.39 || summary.TPOT.P50MS > 9.40 {
		t.Fatalf("TPOT percentiles = %+v", summary.TPOT)
	}
}

func TestSummarizeHandlesNoSamples(t *testing.T) {
	summary := Summarize(nil, 0)
	if summary.Requests != 0 || summary.RequestsPerSecond != 0 || summary.TTFT.P99MS != 0 {
		t.Fatalf("empty summary = %+v", summary)
	}
}
