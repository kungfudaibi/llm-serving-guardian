package telemetry

import (
	"testing"
	"time"
)

func TestMetricsExposeBoundedRequestAndWorkerSignals(t *testing.T) {
	metrics := New([]string{"one", "two"})
	metrics.ObserveRequest("/v1/*", "POST", 200, 150*time.Millisecond)
	metrics.ObserveUpstream("one", "success", 100*time.Millisecond)
	metrics.ObserveHealth("one", true, 10*time.Millisecond)

	families, err := metrics.Registry().Gather()
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]bool, len(families))
	for _, family := range families {
		names[family.GetName()] = true
	}
	for _, name := range []string{
		"llm_guardian_http_requests_total",
		"llm_guardian_http_request_duration_seconds",
		"llm_guardian_upstream_attempts_total",
		"llm_guardian_worker_healthy",
		"llm_guardian_health_checks_total",
	} {
		if !names[name] {
			t.Errorf("metric %q is missing", name)
		}
	}
}
