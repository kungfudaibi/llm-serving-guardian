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

const defaultPrompt = "Explain in concise English why fault-tolerant LLM serving matters. Give four numbered points."

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
	prompt := flag.String("prompt", defaultPrompt, "experiment prompt; only its SHA-256 digest is reported")
	duration := flag.Duration("duration", 70*time.Second, "request generation window")
	concurrency := flag.Int("concurrency", 8, "concurrent request workers")
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
		AdminEndpoint: *adminEndpoint, Duration: *duration, Concurrency: *concurrency, PollInterval: *pollInterval,
	})
	if err != nil {
		return err
	}
	faultAt, err := readFaultMarker(*faultMarker)
	if err != nil {
		return err
	}
	report := benchmark.NewAvailabilityReport(benchmark.AvailabilityParameters{
		Endpoint: *endpoint, AdminEndpoint: *adminEndpoint, Model: *model, Prompt: *prompt,
		Concurrency: *concurrency, MaxTokens: *maxTokens, Temperature: *temperature, Duration: *duration,
		TargetWorker: *targetWorker, FaultInjectedAt: faultAt, Hardware: *hardware, Label: *label,
	}, attempts, observations)
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	encoded = append(encoded, '\n')
	if *output != "" {
		file, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return fmt.Errorf("create report: %w", err)
		}
		if _, err := file.Write(encoded); err != nil {
			_ = file.Close()
			return fmt.Errorf("write report: %w", err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close report: %w", err)
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
