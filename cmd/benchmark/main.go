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
	"time"

	"github.com/kungfudaibi/llm-serving-guardian/internal/benchmark"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "benchmark: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	endpoint := flag.String("endpoint", "http://127.0.0.1:8090/v1/chat/completions", "OpenAI-compatible chat completions endpoint")
	model := flag.String("model", "qwen2.5-1.5b-instruct", "served model name")
	prompt := flag.String("prompt", benchmark.DefaultPrompt, "benchmark prompt (the report stores only its SHA-256 digest)")
	requests := flag.Int("requests", 64, "number of measured requests")
	concurrency := flag.Int("concurrency", 1, "number of concurrent request workers")
	warmup := flag.Int("warmup", 2, "number of sequential warmup requests")
	maxTokens := flag.Int("max-tokens", 64, "maximum generated tokens per request")
	temperature := flag.Float64("temperature", 0, "sampling temperature")
	hardware := flag.String("hardware", "", "hardware and serving-stack description stored in the report")
	label := flag.String("label", "", "experiment label stored in the report")
	output := flag.String("output", "", "optional new JSON report path; existing files are never overwritten")
	requestTimeout := flag.Duration("request-timeout", 2*time.Minute, "per-request HTTP timeout")
	flag.Parse()

	if *endpoint == "" || *model == "" || *prompt == "" {
		return errors.New("endpoint, model, and prompt must not be empty")
	}
	if *maxTokens <= 0 {
		return errors.New("max-tokens must be greater than zero")
	}
	if *requestTimeout <= 0 {
		return errors.New("request-timeout must be greater than zero")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	requestOptions := benchmark.RequestOptions{
		Endpoint:    *endpoint,
		APIKey:      os.Getenv("BENCHMARK_API_KEY"),
		Model:       *model,
		Prompt:      *prompt,
		MaxTokens:   *maxTokens,
		Temperature: *temperature,
	}
	samples, wall, err := benchmark.Run(ctx, &http.Client{Timeout: *requestTimeout}, benchmark.RunOptions{
		Request:     requestOptions,
		Requests:    *requests,
		Concurrency: *concurrency,
		Warmup:      *warmup,
	})
	if err != nil {
		return err
	}
	report := benchmark.NewReport(benchmark.ReportParameters{
		Endpoint:    *endpoint,
		Model:       *model,
		Prompt:      *prompt,
		Requests:    *requests,
		Concurrency: *concurrency,
		Warmup:      *warmup,
		MaxTokens:   *maxTokens,
		Temperature: *temperature,
		Hardware:    *hardware,
		Label:       *label,
	}, samples, wall)
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
	if _, err := os.Stdout.Write(encoded); err != nil {
		return fmt.Errorf("print report: %w", err)
	}
	return nil
}
