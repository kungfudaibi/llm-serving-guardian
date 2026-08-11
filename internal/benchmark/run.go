package benchmark

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type RunOptions struct {
	Request     RequestOptions
	Requests    int
	Concurrency int
	Warmup      int
}

func Run(ctx context.Context, client *http.Client, options RunOptions) ([]Sample, time.Duration, error) {
	if options.Requests <= 0 {
		return nil, 0, errors.New("requests must be greater than zero")
	}
	if options.Concurrency <= 0 {
		return nil, 0, errors.New("concurrency must be greater than zero")
	}
	if options.Warmup < 0 {
		return nil, 0, errors.New("warmup must not be negative")
	}
	if client == nil {
		client = http.DefaultClient
	}
	for index := 0; index < options.Warmup; index++ {
		if _, err := Request(ctx, client, options.Request); err != nil {
			return nil, 0, fmt.Errorf("warmup request %d: %w", index+1, err)
		}
	}

	requestContext, cancel := context.WithCancel(ctx)
	defer cancel()
	samples := make([]Sample, options.Requests)
	var next atomic.Int64
	var firstErr error
	var errOnce sync.Once
	var workers sync.WaitGroup
	started := time.Now()
	for range options.Concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				index := int(next.Add(1) - 1)
				if index >= len(samples) {
					return
				}
				if err := requestContext.Err(); err != nil {
					return
				}
				sample, err := Request(requestContext, client, options.Request)
				if err != nil {
					errOnce.Do(func() {
						firstErr = fmt.Errorf("measured request %d: %w", index+1, err)
						cancel()
					})
					return
				}
				samples[index] = sample
			}
		}()
	}
	workers.Wait()
	wall := time.Since(started)
	if firstErr != nil {
		return nil, wall, firstErr
	}
	return samples, wall, nil
}
