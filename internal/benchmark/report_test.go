package benchmark

import (
	"strings"
	"testing"
	"time"
)

func TestNewReportIncludesRawSamplesAndPromptDigest(t *testing.T) {
	samples := []Sample{{
		TTFT: 25 * time.Millisecond, E2E: 125 * time.Millisecond, CompletionTokens: 11,
	}}
	report := NewReport(ReportParameters{
		Endpoint: "http://127.0.0.1:8090/v1/chat/completions",
		Model:    "test-model", Prompt: "private prompt", Requests: 1, Concurrency: 1,
		Warmup: 2, MaxTokens: 32, Hardware: "test-gpu", Label: "smoke",
	}, samples, 200*time.Millisecond)

	if report.SchemaVersion != 1 || report.Summary.OutputTokens != 11 || len(report.Samples) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if report.Parameters.PromptSHA256 != "6fe06b970bb77bb96bee521acbebf7e932c2bbc684494ad299a7e1851347fc8e" {
		t.Fatalf("prompt digest = %q", report.Parameters.PromptSHA256)
	}
	if report.Samples[0].TTFTMS != 25 || report.Samples[0].E2EMS != 125 || report.Samples[0].CompletionTokens != 11 {
		t.Fatalf("raw sample = %+v", report.Samples[0])
	}
	if strings.Contains(report.Parameters.PromptSHA256, "private prompt") {
		t.Fatal("report contains the prompt instead of only its digest")
	}
}

func TestNewReportRedactsEndpointCredentialsAndQuery(t *testing.T) {
	report := NewReport(ReportParameters{
		Endpoint: "https://user:password@example.com/v1/chat/completions?api_key=secret#fragment",
		Prompt:   "test",
	}, nil, 0)

	if report.Parameters.Endpoint != "https://example.com/v1/chat/completions" {
		t.Fatalf("endpoint = %q", report.Parameters.Endpoint)
	}
}
