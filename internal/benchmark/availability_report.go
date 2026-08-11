package benchmark

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type AvailabilityAttempt struct {
	StartedAt     time.Time `json:"startedAt"`
	FinishedAt    time.Time `json:"finishedAt"`
	Success       bool      `json:"success"`
	Worker        string    `json:"worker,omitempty"`
	Attempts      int       `json:"attempts,omitempty"`
	RequestID     string    `json:"requestId,omitempty"`
	StreamStarted bool      `json:"streamStarted"`
	TTFTMS        float64   `json:"ttftMs,omitempty"`
	E2EMS         float64   `json:"e2eMs"`
	Error         string    `json:"error,omitempty"`
}

type ObservedWorker struct {
	Name                string `json:"name"`
	Healthy             bool   `json:"isHealthy"`
	CircuitState        string `json:"circuitState"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
	LastError           string `json:"lastError,omitempty"`
}

type WorkerObservation struct {
	ObservedAt time.Time        `json:"observedAt"`
	Workers    []ObservedWorker `json:"workers"`
}

type AvailabilityParameters struct {
	Endpoint        string        `json:"endpoint"`
	Model           string        `json:"model"`
	Prompt          string        `json:"-"`
	PromptSHA256    string        `json:"promptSha256"`
	Concurrency     int           `json:"concurrency"`
	Duration        time.Duration `json:"-"`
	DurationSeconds float64       `json:"durationSeconds"`
	TargetWorker    string        `json:"targetWorker"`
	FaultInjectedAt time.Time     `json:"faultInjectedAt"`
}

type AvailabilitySummary struct {
	Total              int            `json:"total"`
	Successful         int            `json:"successful"`
	Failed             int            `json:"failed"`
	SuccessRate        float64        `json:"successRate"`
	FailuresAfterFault int            `json:"failuresAfterFault"`
	InterruptedStreams int            `json:"interruptedStreams"`
	RetriedSuccesses   int            `json:"retriedSuccesses"`
	WorkerSuccesses    map[string]int `json:"workerSuccesses"`
	DetectedAt         time.Time      `json:"detectedAt,omitempty"`
	RecoveredAt        time.Time      `json:"recoveredAt,omitempty"`
	DetectionSeconds   float64        `json:"detectionSeconds,omitempty"`
	RecoverySeconds    float64        `json:"recoverySeconds,omitempty"`
}

type AvailabilityReport struct {
	SchemaVersion int                    `json:"schemaVersion"`
	GeneratedAt   time.Time              `json:"generatedAt"`
	Parameters    AvailabilityParameters `json:"parameters"`
	Summary       AvailabilitySummary    `json:"summary"`
	Attempts      []AvailabilityAttempt  `json:"attempts"`
	Observations  []WorkerObservation    `json:"observations"`
}

func NewAvailabilityReport(parameters AvailabilityParameters, attempts []AvailabilityAttempt, observations []WorkerObservation) AvailabilityReport {
	digest := sha256.Sum256([]byte(parameters.Prompt))
	parameters.Prompt = ""
	parameters.PromptSHA256 = hex.EncodeToString(digest[:])
	parameters.Endpoint = reportEndpoint(parameters.Endpoint)
	parameters.DurationSeconds = parameters.Duration.Seconds()
	parameters.Duration = 0
	summary := AvailabilitySummary{Total: len(attempts), WorkerSuccesses: make(map[string]int)}
	for _, attempt := range attempts {
		if attempt.Success {
			summary.Successful++
			summary.WorkerSuccesses[attempt.Worker]++
			if attempt.Attempts > 1 {
				summary.RetriedSuccesses++
			}
			continue
		}
		summary.Failed++
		if attempt.StreamStarted {
			summary.InterruptedStreams++
		}
		if !parameters.FaultInjectedAt.IsZero() && !attempt.FinishedAt.Before(parameters.FaultInjectedAt) {
			summary.FailuresAfterFault++
		}
	}
	if summary.Total > 0 {
		summary.SuccessRate = float64(summary.Successful) / float64(summary.Total)
	}
	for _, observation := range observations {
		if observation.ObservedAt.Before(parameters.FaultInjectedAt) {
			continue
		}
		worker, ok := observedWorker(observation.Workers, parameters.TargetWorker)
		if !ok {
			continue
		}
		if summary.DetectedAt.IsZero() && !worker.Healthy {
			summary.DetectedAt = observation.ObservedAt
			summary.DetectionSeconds = observation.ObservedAt.Sub(parameters.FaultInjectedAt).Seconds()
			continue
		}
		if !summary.DetectedAt.IsZero() && summary.RecoveredAt.IsZero() && worker.Healthy {
			summary.RecoveredAt = observation.ObservedAt
			summary.RecoverySeconds = observation.ObservedAt.Sub(parameters.FaultInjectedAt).Seconds()
		}
	}
	return AvailabilityReport{SchemaVersion: 1, GeneratedAt: time.Now().UTC(), Parameters: parameters, Summary: summary, Attempts: attempts, Observations: observations}
}

func observedWorker(workers []ObservedWorker, name string) (ObservedWorker, bool) {
	for _, worker := range workers {
		if worker.Name == name {
			return worker, true
		}
	}
	return ObservedWorker{}, false
}
