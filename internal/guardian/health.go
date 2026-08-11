package guardian

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type HealthObserver interface {
	ObserveHealth(worker string, success bool, duration time.Duration)
}

// HealthChecker actively probes workers and feeds the results into their circuits.
type HealthChecker struct {
	pool     *Pool
	client   *http.Client
	interval time.Duration
	timeout  time.Duration
	observer HealthObserver
}

func NewHealthChecker(pool *Pool, client *http.Client, interval, timeout time.Duration) *HealthChecker {
	if client == nil {
		client = http.DefaultClient
	}
	return &HealthChecker{pool: pool, client: client, interval: interval, timeout: timeout}
}

func (c *HealthChecker) SetObserver(observer HealthObserver) {
	c.observer = observer
}

// Run probes immediately and continues until the context is canceled.
func (c *HealthChecker) Run(ctx context.Context) {
	c.CheckOnce(ctx)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.CheckOnce(ctx)
		}
	}
}

// CheckOnce probes every worker currently allowed by its circuit cooldown.
func (c *HealthChecker) CheckOnce(ctx context.Context) {
	for _, worker := range c.pool.ProbeCandidates() {
		started := time.Now()
		err := c.probe(ctx, worker)
		if err == nil {
			c.pool.ReportSuccess(worker.Name)
		} else {
			c.pool.ReportFailure(worker.Name, err)
		}
		if c.observer != nil {
			c.observer.ObserveHealth(worker.Name, err == nil, time.Since(started))
		}
	}
}

func (c *HealthChecker) probe(parent context.Context, worker *Worker) error {
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()

	healthURL := worker.baseURL.ResolveReference(&url.URL{Path: "/health"})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL.String(), nil)
	if err != nil {
		return fmt.Errorf("create health request: %w", err)
	}
	if worker.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+worker.apiKey)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return errors.New("health check timed out")
		}
		return fmt.Errorf("health request: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("health status %d", resp.StatusCode)
	}
	return nil
}
