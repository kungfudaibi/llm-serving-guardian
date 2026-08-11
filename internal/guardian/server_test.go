package guardian

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhaowenjie/llm-serving-guardian/internal/config"
)

func TestHandlerHealthReadinessAndWorkerSnapshot(t *testing.T) {
	pool, err := NewPool([]config.Worker{{Name: "one", URL: "http://127.0.0.1:8081", APIKey: "must-not-leak"}}, 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(pool, http.NotFoundHandler(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("metric 1"))
	}), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	assertStatus(t, handler, "/healthz", http.StatusOK)
	assertStatus(t, handler, "/readyz", http.StatusServiceUnavailable)
	pool.ReportSuccess("one")
	assertStatus(t, handler, "/readyz", http.StatusOK)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/workers", nil))
	if strings.Contains(recorder.Body.String(), "must-not-leak") {
		t.Fatalf("admin response leaked API key: %s", recorder.Body.String())
	}
	var workers []Snapshot
	if err := json.Unmarshal(recorder.Body.Bytes(), &workers); err != nil || len(workers) != 1 || !workers[0].IsHealthy {
		t.Fatalf("workers response = %q, error = %v", recorder.Body.String(), err)
	}
}

func TestHandlerManagesRequestIDsAndStructuredLogs(t *testing.T) {
	pool, err := NewPool([]config.Worker{{Name: "one", URL: "http://127.0.0.1:8081"}}, 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	handler := NewHandler(pool, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Request-Id") == "" {
			t.Fatal("proxy did not receive request ID")
		}
		w.WriteHeader(http.StatusCreated)
	}), http.NotFoundHandler(), nil, slog.New(slog.NewJSONHandler(&logs, nil)))

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("private prompt"))
	req.Header.Set("X-Request-Id", "safe-id_123")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Header().Get("X-Request-Id") != "safe-id_123" {
		t.Fatalf("request ID = %q", recorder.Header().Get("X-Request-Id"))
	}
	if !strings.Contains(logs.String(), `"event":"request_completed"`) || strings.Contains(logs.String(), "private prompt") {
		t.Fatalf("unexpected log output: %s", logs.String())
	}

	invalid := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	invalid.Header.Set("X-Request-Id", "unsafe id with spaces")
	invalidRecorder := httptest.NewRecorder()
	handler.ServeHTTP(invalidRecorder, invalid)
	if got := invalidRecorder.Header().Get("X-Request-Id"); got == "" || got == "unsafe id with spaces" {
		t.Fatalf("invalid ID was not replaced: %q", got)
	}
}

func TestHandlerRejectsWrongMethodsAndUnknownRoutes(t *testing.T) {
	pool, err := NewPool([]config.Worker{{Name: "one", URL: "http://127.0.0.1:8081"}}, 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(pool, http.NotFoundHandler(), http.NotFoundHandler(), nil, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("method response = %d, Allow=%q", recorder.Code, recorder.Header().Get("Allow"))
	}
	assertStatus(t, handler, "/unknown", http.StatusNotFound)
}

func assertStatus(t *testing.T, handler http.Handler, path string, want int) {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if recorder.Code != want {
		t.Fatalf("GET %s status = %d, want %d; body=%s", path, recorder.Code, want, recorder.Body.String())
	}
}
