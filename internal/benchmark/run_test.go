package benchmark

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunUsesConfiguredConcurrencyAndExcludesWarmup(t *testing.T) {
	var active atomic.Int64
	var maxActive atomic.Int64
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		current := active.Add(1)
		defer active.Add(-1)
		for current > maxActive.Load() && !maxActive.CompareAndSwap(maxActive.Load(), current) {
		}
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"completion_tokens\":1}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	samples, wall, err := Run(t.Context(), server.Client(), RunOptions{
		Request:  RequestOptions{Endpoint: server.URL, Model: "test", Prompt: "test", MaxTokens: 1},
		Requests: 6, Concurrency: 3, Warmup: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 6 || calls.Load() != 8 {
		t.Fatalf("samples=%d calls=%d", len(samples), calls.Load())
	}
	if maxActive.Load() < 2 {
		t.Fatalf("max concurrent requests = %d", maxActive.Load())
	}
	if wall < 40*time.Millisecond || wall > 200*time.Millisecond {
		t.Fatalf("measured wall time = %s", wall)
	}
}

func TestRunRejectsInvalidWorkload(t *testing.T) {
	_, _, err := Run(t.Context(), http.DefaultClient, RunOptions{Requests: 0, Concurrency: 1})
	if err == nil || err.Error() != "requests must be greater than zero" {
		t.Fatalf("error = %v", err)
	}
}
