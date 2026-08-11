package benchmark

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunAvailabilityContinuesAfterIndividualFailuresAndPollsWorkers(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/admin/workers":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `[{"name":"gpu-zero","isHealthy":true,"circuitState":"CLOSED"}]`)
		case "/v1/chat/completions":
			time.Sleep(5 * time.Millisecond)
			w.Header().Set("X-Guardian-Worker", "gpu-zero")
			w.Header().Set("X-Guardian-Attempts", "1")
			if calls.Add(1)%2 == 0 {
				http.Error(w, "injected failure", http.StatusBadGateway)
				return
			}
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
			fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"completion_tokens\":1}}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	attempts, observations, err := RunAvailability(t.Context(), server.Client(), AvailabilityRunOptions{
		Request:       RequestOptions{Endpoint: server.URL + "/v1/chat/completions", Model: "test", Prompt: "test", MaxTokens: 1},
		AdminEndpoint: server.URL + "/admin/workers", Duration: 50 * time.Millisecond, Concurrency: 2, PollInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	var successes, failures int
	for _, attempt := range attempts {
		if attempt.Success {
			successes++
		} else {
			failures++
		}
	}
	if successes == 0 || failures == 0 || len(attempts) < 4 {
		t.Fatalf("attempts did not continue across errors: %+v", attempts)
	}
	for _, attempt := range attempts {
		if !attempt.Success && attempt.Error != "upstream_response_failed" {
			t.Fatalf("failure category = %q", attempt.Error)
		}
	}
	if len(observations) < 2 || len(observations[0].Workers) != 1 {
		t.Fatalf("worker observations = %+v", observations)
	}
	for index := 1; index < len(attempts); index++ {
		if attempts[index].StartedAt.Before(attempts[index-1].StartedAt) {
			t.Fatalf("attempts are not chronological: %+v", attempts)
		}
	}
}

func TestRunAvailabilityRejectsMissingWorkerEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/workers" {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"completion_tokens\":1}}\n\n")
	}))
	defer server.Close()

	_, _, err := RunAvailability(t.Context(), server.Client(), AvailabilityRunOptions{
		Request:       RequestOptions{Endpoint: server.URL + "/v1/chat/completions", Model: "test", Prompt: "test", MaxTokens: 1},
		AdminEndpoint: server.URL + "/admin/workers", Duration: 20 * time.Millisecond, Concurrency: 1, PollInterval: 5 * time.Millisecond,
	})
	if err == nil || err.Error() != "no successful worker observations" {
		t.Fatalf("error = %v", err)
	}
}

func TestAvailabilityAttemptDoesNotStoreRawErrors(t *testing.T) {
	attempt := availabilityAttempt(Sample{StartedAt: time.Now(), FinishedAt: time.Now(), StreamStarted: true}, errors.New("secret response body"))
	if attempt.Error != "stream_incomplete" || strings.Contains(attempt.Error, "secret") {
		t.Fatalf("error category = %q", attempt.Error)
	}
}

func TestRunAvailabilityStopsAtRequestLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/workers" {
			fmt.Fprint(w, `[{"name":"gpu-zero","isHealthy":true,"circuitState":"CLOSED"}]`)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"completion_tokens\":1}}\n\n")
	}))
	defer server.Close()

	attempts, _, err := RunAvailability(t.Context(), server.Client(), AvailabilityRunOptions{
		Request:       RequestOptions{Endpoint: server.URL + "/v1/chat/completions", Model: "test", Prompt: "test", MaxTokens: 1},
		AdminEndpoint: server.URL + "/admin/workers", Duration: time.Second, Concurrency: 2, PollInterval: 10 * time.Millisecond, MaxRequests: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 3 {
		t.Fatalf("attempts = %d, want 3", len(attempts))
	}
}

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
