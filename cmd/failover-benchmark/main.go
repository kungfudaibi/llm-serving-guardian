package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/kungfudaibi/llm-serving-guardian/internal/benchmark"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "failover benchmark: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	endpoint := flag.String("endpoint", "http://127.0.0.1:8090/v1/chat/completions", "OpenAI-compatible streaming endpoint")
	adminEndpoint := flag.String("admin-endpoint", "http://127.0.0.1:8090/admin/workers", "Guardian worker snapshot endpoint")
	model := flag.String("model", "qwen2.5-1.5b-instruct", "served model name")
	prompt := flag.String("prompt", benchmark.DefaultPrompt, "experiment prompt; only its SHA-256 digest is reported")
	duration := flag.Duration("duration", 70*time.Second, "request generation window")
	concurrency := flag.Int("concurrency", 8, "concurrent request workers")
	maxRequests := flag.Int("max-requests", 100_000, "maximum raw request records retained")
	pollInterval := flag.Duration("poll-interval", 200*time.Millisecond, "worker snapshot interval")
	maxTokens := flag.Int("max-tokens", 64, "maximum generated tokens per request")
	temperature := flag.Float64("temperature", 0, "sampling temperature")
	targetWorker := flag.String("target-worker", "vllm-gpu0", "worker that will receive the injected fault")
	faultMarker := flag.String("fault-marker", "", "file containing the exact RFC3339Nano fault timestamp")
	hardware := flag.String("hardware", "", "hardware and serving-stack description")
	label := flag.String("label", "", "experiment label")
	output := flag.String("output", "", "new JSON report path; existing files are never overwritten")
	requestTimeout := flag.Duration("request-timeout", 2*time.Minute, "per-request timeout")
	flag.Parse()
	if *endpoint == "" || *adminEndpoint == "" || *model == "" || *prompt == "" || *targetWorker == "" || *faultMarker == "" {
		return errors.New("endpoint, admin-endpoint, model, prompt, target-worker, and fault-marker are required")
	}
	if *maxTokens <= 0 || *requestTimeout <= 0 {
		return errors.New("max-tokens and request-timeout must be greater than zero")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	client := &http.Client{Timeout: *requestTimeout}
	attempts, observations, err := benchmark.RunAvailability(ctx, client, benchmark.AvailabilityRunOptions{
		Request: benchmark.RequestOptions{
			Endpoint: *endpoint, APIKey: os.Getenv("BENCHMARK_API_KEY"), Model: *model,
			Prompt: *prompt, MaxTokens: *maxTokens, Temperature: *temperature,
		},
		AdminEndpoint: *adminEndpoint, Duration: *duration, Concurrency: *concurrency, PollInterval: *pollInterval, MaxRequests: *maxRequests,
	})
	if err != nil {
		return err
	}
	faultAt, err := readFaultMarker(*faultMarker)
	if err != nil {
		return err
	}
	if err := validateFaultTime(attempts, faultAt); err != nil {
		return err
	}
	report := benchmark.NewAvailabilityReport(benchmark.AvailabilityParameters{
		Endpoint: *endpoint, AdminEndpoint: *adminEndpoint, Model: *model, Prompt: *prompt,
		Concurrency: *concurrency, MaxRequests: *maxRequests, MaxTokens: *maxTokens, Temperature: *temperature, Duration: *duration,
		TargetWorker: *targetWorker, FaultInjectedAt: faultAt, Hardware: *hardware, Label: *label,
	}, attempts, observations)
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	encoded = append(encoded, '\n')
	if *output != "" {
		if err := benchmark.WriteReport(*output, encoded); err != nil {
			return fmt.Errorf("write report: %w", err)
		}
	}
	_, err = os.Stdout.Write(encoded)
	return err
}

func readFaultMarker(path string) (time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, fmt.Errorf("read fault marker: %w", err)
	}
	faultAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(data)))
	if err != nil {
		return time.Time{}, fmt.Errorf("parse fault marker: %w", err)
	}
	return faultAt, nil
}

func validateFaultTime(attempts []benchmark.AvailabilityAttempt, faultAt time.Time) error {
	if len(attempts) == 0 {
		return errors.New("workload completed without request attempts")
	}
	earliest := attempts[0].StartedAt
	latest := attempts[0].FinishedAt
	for _, attempt := range attempts[1:] {
		if attempt.StartedAt.Before(earliest) {
			earliest = attempt.StartedAt
		}
		if attempt.FinishedAt.After(latest) {
			latest = attempt.FinishedAt
		}
	}
	if faultAt.Before(earliest) || faultAt.After(latest) {
		return errors.New("fault marker is outside the measured workload window")
	}
	return nil
}
