package benchmark

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNewAvailabilityReportSummarizesFaultImpactAndRecovery(t *testing.T) {
	started := time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC)
	faultAt := started.Add(5 * time.Second)
	attempts := []AvailabilityAttempt{
		{StartedAt: started, FinishedAt: started.Add(time.Second), Success: true, Worker: "gpu-zero", Attempts: 1},
		{StartedAt: started.Add(4 * time.Second), FinishedAt: started.Add(6 * time.Second), Worker: "gpu-zero", Attempts: 1, StreamStarted: true, Error: "stream interrupted"},
		{StartedAt: started.Add(7 * time.Second), FinishedAt: started.Add(8 * time.Second), Success: true, Worker: "gpu-one", Attempts: 2},
		{StartedAt: started.Add(21 * time.Second), FinishedAt: started.Add(22 * time.Second), Success: true, Worker: "gpu-zero", Attempts: 1},
	}
	observations := []WorkerObservation{
		{ObservedAt: started.Add(4 * time.Second), Workers: []ObservedWorker{{Name: "gpu-zero", Healthy: true}}},
		{ObservedAt: started.Add(6 * time.Second), Workers: []ObservedWorker{{Name: "gpu-zero", Healthy: false, CircuitState: "OPEN"}}},
		{ObservedAt: started.Add(20 * time.Second), Workers: []ObservedWorker{{Name: "gpu-zero", Healthy: true, CircuitState: "CLOSED"}}},
	}

	report := NewAvailabilityReport(AvailabilityParameters{
		Endpoint: "http://127.0.0.1:8090/v1/chat/completions", Model: "test", Prompt: "private prompt",
		Concurrency: 4, Duration: 30 * time.Second, TargetWorker: "gpu-zero", FaultInjectedAt: faultAt,
	}, attempts, observations)

	if report.Summary.Total != 4 || report.Summary.Successful != 3 || report.Summary.Failed != 1 {
		t.Fatalf("request counts = %+v", report.Summary)
	}
	if report.Summary.SuccessRate != 0.75 || report.Summary.InterruptedStreams != 1 || report.Summary.RetriedSuccesses != 1 {
		t.Fatalf("fault outcomes = %+v", report.Summary)
	}
	if report.Summary.DetectionSeconds != 1 || report.Summary.RecoverySeconds != 15 {
		t.Fatalf("fault timing = %+v", report.Summary)
	}
	if report.Summary.WorkerSuccesses["gpu-zero"] != 2 || report.Summary.WorkerSuccesses["gpu-one"] != 1 {
		t.Fatalf("worker successes = %+v", report.Summary.WorkerSuccesses)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private prompt") || report.Parameters.PromptSHA256 == "" {
		t.Fatalf("report leaked prompt or omitted digest: %s", encoded)
	}
}
