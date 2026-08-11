package guardian

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kungfudaibi/llm-serving-guardian/internal/config"
)

func TestProxyForwardsRequestAndConfiguredAuthorization(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/v1/chat/completions?stream=true" {
			t.Fatalf("request URI = %q", r.URL.RequestURI())
		}
		if got := r.Header.Get("Authorization"); got != "Bearer worker-key" {
			t.Fatalf("Authorization = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer upstream.Close()

	proxy, pool := testProxy(t, []config.Worker{{Name: "one", URL: upstream.URL, APIKey: "worker-key"}}, 2, 1024)
	pool.ReportSuccess("one")
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?stream=true", strings.NewReader(`{"model":"demo"}`))
	req.Header.Set("Authorization", "Bearer client-key")
	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK || recorder.Body.String() != `{"model":"demo"}` {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("X-Guardian-Worker") != "one" {
		t.Fatalf("worker header = %q", recorder.Header().Get("X-Guardian-Worker"))
	}
}

func TestProxyRetriesFiveHundredOnAnotherWorker(t *testing.T) {
	firstCalls := 0
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		firstCalls++
		http.Error(w, "failed", http.StatusServiceUnavailable)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("recovered"))
	}))
	defer second.Close()

	proxy, pool := testProxy(t, []config.Worker{
		{Name: "one", URL: first.URL}, {Name: "two", URL: second.URL},
	}, 2, 1024)
	pool.ReportSuccess("one")
	pool.ReportSuccess("two")
	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("same-body")))

	if firstCalls != 1 || recorder.Code != http.StatusOK || recorder.Body.String() != "recovered" {
		t.Fatalf("failover response = calls:%d status:%d body:%q", firstCalls, recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("X-Guardian-Attempts") != "2" {
		t.Fatalf("attempt header = %q", recorder.Header().Get("X-Guardian-Attempts"))
	}
}

func TestProxyDoesNotFollowUpstreamRedirects(t *testing.T) {
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("guardian followed an upstream redirect")
	}))
	defer redirectTarget.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, redirectTarget.URL, http.StatusTemporaryRedirect)
	}))
	defer upstream.Close()

	proxy, pool := testProxy(t, []config.Worker{{Name: "one", URL: upstream.URL}}, 1, 1024)
	pool.ReportSuccess("one")
	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	if recorder.Code != http.StatusTemporaryRedirect || recorder.Header().Get("Location") != redirectTarget.URL {
		t.Fatalf("redirect response = %d Location=%q", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestProxyRejectsLargeRequest(t *testing.T) {
	proxy, pool := testProxy(t, []config.Worker{{Name: "one", URL: "http://127.0.0.1:1"}}, 1, 4)
	pool.ReportSuccess("one")
	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("12345")))
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", recorder.Code)
	}
	var response errorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.Error.Code != "REQUEST_TOO_LARGE" {
		t.Fatalf("response = %q, error = %v", recorder.Body.String(), err)
	}
}

func TestProxyReturnsUnavailableWithoutHealthyWorker(t *testing.T) {
	proxy, _ := testProxy(t, []config.Worker{{Name: "one", URL: "http://127.0.0.1:1"}}, 1, 1024)
	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestProxyFlushesStreamingChunks(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte("data: one\n\n"))
		flusher.Flush()
		time.Sleep(10 * time.Millisecond)
		_, _ = w.Write([]byte("data: two\n\n"))
		flusher.Flush()
	}))
	defer upstream.Close()

	proxy, pool := testProxy(t, []config.Worker{{Name: "one", URL: upstream.URL}}, 1, 1024)
	pool.ReportSuccess("one")
	recorder := &flushRecorder{header: make(http.Header)}
	proxy.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	if recorder.flushes < 2 || recorder.body.String() != "data: one\n\ndata: two\n\n" {
		t.Fatalf("flushes = %d, body = %q", recorder.flushes, recorder.body.String())
	}
}

func testProxy(t *testing.T, workers []config.Worker, attempts int, maxBody int64) (*Proxy, *Pool) {
	t.Helper()
	pool, err := NewPool(workers, 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewProxy(pool, &http.Client{}, ProxyOptions{
		MaxAttempts: attempts, MaxBodyBytes: maxBody, RequestTimeout: time.Second,
		Limiter: NewLimiter(0, 1), Logger: logger,
	}), pool
}

type flushRecorder struct {
	mu      sync.Mutex
	header  http.Header
	body    bytes.Buffer
	status  int
	flushes int
}

func (r *flushRecorder) Header() http.Header    { return r.header }
func (r *flushRecorder) WriteHeader(status int) { r.status = status }
func (r *flushRecorder) Write(data []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(data)
}
func (r *flushRecorder) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushes++
}

func TestProxyStopsWhenRequestContextIsCanceled(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer upstream.Close()
	proxy, pool := testProxy(t, []config.Worker{{Name: "one", URL: upstream.URL}}, 1, 1024)
	pool.ReportSuccess("one")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx))
	if recorder.Code != http.StatusBadGateway && recorder.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d", recorder.Code)
	}
}
