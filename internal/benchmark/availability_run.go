package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

type AvailabilityRunOptions struct {
	Request       RequestOptions
	AdminEndpoint string
	Duration      time.Duration
	Concurrency   int
	PollInterval  time.Duration
	MaxRequests   int
}

func RunAvailability(ctx context.Context, client *http.Client, options AvailabilityRunOptions) ([]AvailabilityAttempt, []WorkerObservation, error) {
	if options.Duration <= 0 {
		return nil, nil, errors.New("duration must be greater than zero")
	}
	if options.Concurrency <= 0 {
		return nil, nil, errors.New("concurrency must be greater than zero")
	}
	if options.AdminEndpoint == "" || options.PollInterval <= 0 {
		return nil, nil, errors.New("admin endpoint and positive poll interval are required")
	}
	if options.MaxRequests == 0 {
		options.MaxRequests = 100_000
	}
	if options.MaxRequests < 0 || options.MaxRequests > 1_000_000 {
		return nil, nil, errors.New("max requests must be between 1 and 1000000")
	}
	if client == nil {
		client = http.DefaultClient
	}
	stopAt := time.Now().Add(options.Duration)
	var attempts []AvailabilityAttempt
	var observations []WorkerObservation
	var attemptsMu sync.Mutex
	var observationsMu sync.Mutex
	stopPolling := make(chan struct{})
	var polling sync.WaitGroup
	polling.Add(1)
	go func() {
		defer polling.Done()
		ticker := time.NewTicker(options.PollInterval)
		defer ticker.Stop()
		for {
			observation := observeWorkers(ctx, client, options.AdminEndpoint)
			observationsMu.Lock()
			observations = append(observations, observation)
			observationsMu.Unlock()
			select {
			case <-ticker.C:
			case <-stopPolling:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	var workers sync.WaitGroup
	var nextRequest atomic.Int64
	for range options.Concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for time.Now().Before(stopAt) {
				if ctx.Err() != nil {
					return
				}
				if nextRequest.Add(1) > int64(options.MaxRequests) {
					return
				}
				sample, err := Request(ctx, client, options.Request)
				attempt := availabilityAttempt(sample, err)
				attemptsMu.Lock()
				attempts = append(attempts, attempt)
				attemptsMu.Unlock()
			}
		}()
	}
	workers.Wait()
	close(stopPolling)
	polling.Wait()
	slices.SortFunc(attempts, func(left, right AvailabilityAttempt) int { return left.StartedAt.Compare(right.StartedAt) })
	slices.SortFunc(observations, func(left, right WorkerObservation) int { return left.ObservedAt.Compare(right.ObservedAt) })
	if !hasWorkerEvidence(observations) {
		return attempts, observations, errors.New("no successful worker observations")
	}
	return attempts, observations, nil
}

func availabilityAttempt(sample Sample, requestErr error) AvailabilityAttempt {
	attempt := AvailabilityAttempt{
		StartedAt: sample.StartedAt, FinishedAt: sample.FinishedAt, Success: requestErr == nil,
		Worker: sample.Worker, Attempts: sample.Attempts, RequestID: sample.RequestID,
		StreamStarted: sample.StreamStarted, TTFTMS: float64(sample.TTFT) / float64(time.Millisecond),
		E2EMS: float64(sample.E2E) / float64(time.Millisecond),
	}
	if requestErr != nil {
		switch {
		case sample.StreamStarted:
			attempt.Error = "stream_incomplete"
		case sample.Worker != "":
			attempt.Error = "upstream_response_failed"
		default:
			attempt.Error = "request_failed"
		}
	}
	return attempt
}

func hasWorkerEvidence(observations []WorkerObservation) bool {
	for _, observation := range observations {
		if observation.Error == "" && len(observation.Workers) > 0 {
			return true
		}
	}
	return false
}

func observeWorkers(ctx context.Context, client *http.Client, endpoint string) WorkerObservation {
	observation := WorkerObservation{ObservedAt: time.Now().UTC()}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		observation.Error = "create_admin_request"
		return observation
	}
	resp, err := client.Do(req)
	if err != nil {
		observation.Error = "admin_request_failed"
		return observation
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		observation.Error = fmt.Sprintf("admin endpoint returned %d", resp.StatusCode)
		return observation
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&observation.Workers); err != nil {
		observation.Error = "decode_admin_response"
	}
	return observation
}
