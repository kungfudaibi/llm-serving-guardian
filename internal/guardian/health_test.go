package guardian

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zhaowenjie/llm-serving-guardian/internal/config"
)

func TestHealthCheckerMarksSuccessAndFailure(t *testing.T) {
	status := http.StatusOK
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("health path = %q", r.URL.Path)
		}
		w.WriteHeader(status)
	}))
	defer upstream.Close()

	pool, err := NewPool([]config.Worker{{Name: "one", URL: upstream.URL}}, 1, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	checker := NewHealthChecker(pool, upstream.Client(), time.Second, time.Second)
	checker.CheckOnce(context.Background())
	if pool.HealthyCount() != 1 {
		t.Fatal("successful health check did not enable worker")
	}

	status = http.StatusServiceUnavailable
	checker.CheckOnce(context.Background())
	if pool.HealthyCount() != 0 {
		t.Fatal("failed health check did not disable worker")
	}
}

func TestHealthCheckerSendsConfiguredAuthorization(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer local-key" {
			t.Fatalf("Authorization = %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	pool, err := NewPool([]config.Worker{{Name: "one", URL: upstream.URL, APIKey: "local-key"}}, 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	NewHealthChecker(pool, upstream.Client(), time.Second, time.Second).CheckOnce(context.Background())
	if pool.HealthyCount() != 1 {
		t.Fatal("authenticated health check did not enable worker")
	}
}
