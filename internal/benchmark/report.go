package benchmark

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type ReportParameters struct {
	Endpoint     string  `json:"endpoint"`
	Model        string  `json:"model"`
	Prompt       string  `json:"-"`
	PromptSHA256 string  `json:"promptSha256"`
	Requests     int     `json:"requests"`
	Concurrency  int     `json:"concurrency"`
	Warmup       int     `json:"warmup"`
	MaxTokens    int     `json:"maxTokens"`
	Temperature  float64 `json:"temperature"`
	Hardware     string  `json:"hardware,omitempty"`
	Label        string  `json:"label,omitempty"`
}

type SampleMetrics struct {
	TTFTMS           float64 `json:"ttftMs"`
	TPOTMS           float64 `json:"tpotMs"`
	E2EMS            float64 `json:"e2eMs"`
	CompletionTokens int     `json:"completionTokens"`
}

type Report struct {
	SchemaVersion int              `json:"schemaVersion"`
	GeneratedAt   time.Time        `json:"generatedAt"`
	Parameters    ReportParameters `json:"parameters"`
	Summary       Summary          `json:"summary"`
	Samples       []SampleMetrics  `json:"samples"`
}

func NewReport(parameters ReportParameters, samples []Sample, wall time.Duration) Report {
	digest := sha256.Sum256([]byte(parameters.Prompt))
	parameters.Prompt = ""
	parameters.PromptSHA256 = hex.EncodeToString(digest[:])
	raw := make([]SampleMetrics, 0, len(samples))
	for _, sample := range samples {
		metrics := SampleMetrics{
			TTFTMS:           float64(sample.TTFT) / float64(time.Millisecond),
			E2EMS:            float64(sample.E2E) / float64(time.Millisecond),
			CompletionTokens: sample.CompletionTokens,
		}
		if sample.CompletionTokens > 1 && sample.E2E > sample.TTFT {
			metrics.TPOTMS = float64((sample.E2E-sample.TTFT)/time.Duration(sample.CompletionTokens-1)) / float64(time.Millisecond)
		}
		raw = append(raw, metrics)
	}
	return Report{
		SchemaVersion: 1,
		GeneratedAt:   time.Now().UTC(),
		Parameters:    parameters,
		Summary:       Summarize(samples, wall),
		Samples:       raw,
	}
}
