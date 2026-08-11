// Package telemetry exposes bounded-cardinality Prometheus metrics.
package telemetry

import (
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics implements the guardian health and upstream observer interfaces.
type Metrics struct {
	registry         *prometheus.Registry
	httpRequests     *prometheus.CounterVec
	httpDuration     *prometheus.HistogramVec
	upstreamAttempts *prometheus.CounterVec
	upstreamDuration *prometheus.HistogramVec
	workerHealthy    *prometheus.GaugeVec
	healthChecks     *prometheus.CounterVec
	healthDuration   *prometheus.HistogramVec
}

func New(workerNames []string) *Metrics {
	metrics := &Metrics{
		registry: prometheus.NewPedanticRegistry(),
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "llm_guardian_http_requests_total", Help: "Total HTTP requests handled by the guardian.",
		}, []string{"route", "method", "status_class"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "llm_guardian_http_request_duration_seconds", Help: "End-to-end guardian HTTP request duration.",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 15, 60, 300},
		}, []string{"route", "method", "status_class"}),
		upstreamAttempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "llm_guardian_upstream_attempts_total", Help: "Total inference worker attempts by outcome.",
		}, []string{"worker", "outcome"}),
		upstreamDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "llm_guardian_upstream_request_duration_seconds", Help: "Time until an inference worker returns response headers.",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 15, 60},
		}, []string{"worker", "outcome"}),
		workerHealthy: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "llm_guardian_worker_healthy", Help: "Whether an inference worker passed its latest eligible health probe.",
		}, []string{"worker"}),
		healthChecks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "llm_guardian_health_checks_total", Help: "Total inference worker health checks by result.",
		}, []string{"worker", "result"}),
		healthDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "llm_guardian_health_check_duration_seconds", Help: "Inference worker health-check duration.",
			Buckets: prometheus.DefBuckets,
		}, []string{"worker", "result"}),
	}
	metrics.registry.MustRegister(
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		metrics.httpRequests, metrics.httpDuration, metrics.upstreamAttempts, metrics.upstreamDuration,
		metrics.workerHealthy, metrics.healthChecks, metrics.healthDuration,
	)
	for _, worker := range workerNames {
		metrics.workerHealthy.WithLabelValues(worker).Set(0)
	}
	return metrics
}

func (m *Metrics) Registry() *prometheus.Registry { return m.registry }

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Metrics) ObserveRequest(route, method string, status int, duration time.Duration) {
	statusClass := fmt.Sprintf("%dxx", status/100)
	m.httpRequests.WithLabelValues(route, method, statusClass).Inc()
	m.httpDuration.WithLabelValues(route, method, statusClass).Observe(duration.Seconds())
}

func (m *Metrics) ObserveUpstream(worker, outcome string, duration time.Duration) {
	m.upstreamAttempts.WithLabelValues(worker, outcome).Inc()
	m.upstreamDuration.WithLabelValues(worker, outcome).Observe(duration.Seconds())
}

func (m *Metrics) ObserveHealth(worker string, success bool, duration time.Duration) {
	result := "failure"
	healthy := 0.0
	if success {
		result = "success"
		healthy = 1
	}
	m.workerHealthy.WithLabelValues(worker).Set(healthy)
	m.healthChecks.WithLabelValues(worker, result).Inc()
	m.healthDuration.WithLabelValues(worker, result).Observe(duration.Seconds())
}
