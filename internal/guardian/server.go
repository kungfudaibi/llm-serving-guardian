package guardian

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type HTTPObserver interface {
	ObserveRequest(route, method string, status int, duration time.Duration)
}

type Handler struct {
	pool           *Pool
	proxy          http.Handler
	metricsHandler http.Handler
	observer       HTTPObserver
	logger         *slog.Logger
}

func NewHandler(pool *Pool, proxy, metricsHandler http.Handler, observer HTTPObserver, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	return &Handler{pool: pool, proxy: proxy, metricsHandler: metricsHandler, observer: observer, logger: logger}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	requestID := r.Header.Get("X-Request-Id")
	if !validRequestID(requestID) {
		requestID = newRequestID()
	}
	r.Header.Set("X-Request-Id", requestID)
	w.Header().Set("X-Request-Id", requestID)
	w.Header().Set("X-Content-Type-Options", "nosniff")

	recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	route := canonicalRoute(r.URL.Path)
	h.serveRoute(recorder, r, requestID)
	duration := time.Since(started)
	if h.observer != nil {
		h.observer.ObserveRequest(route, r.Method, recorder.status, duration)
	}
	h.logger.Info("request completed",
		"event", "request_completed", "requestId", requestID, "method", r.Method,
		"route", route, "status", recorder.status, "durationMs", float64(duration.Microseconds())/1000,
		"worker", recorder.Header().Get("X-Guardian-Worker"), "attempts", recorder.Header().Get("X-Guardian-Attempts"),
	)
}

func (h *Handler) serveRoute(w http.ResponseWriter, r *http.Request, requestID string) {
	switch {
	case r.URL.Path == "/healthz":
		if !requireGet(w, r, requestID) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	case r.URL.Path == "/readyz":
		if !requireGet(w, r, requestID) {
			return
		}
		healthy := h.pool.HealthyCount()
		status := http.StatusOK
		state := "ready"
		if healthy == 0 {
			status = http.StatusServiceUnavailable
			state = "not_ready"
		}
		writeJSON(w, status, map[string]any{"status": state, "healthyWorkers": healthy, "totalWorkers": h.pool.Len()})
	case r.URL.Path == "/admin/workers":
		if !requireGet(w, r, requestID) {
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, h.pool.Snapshot())
	case r.URL.Path == "/metrics":
		if !requireGet(w, r, requestID) {
			return
		}
		h.metricsHandler.ServeHTTP(w, r)
	case strings.HasPrefix(r.URL.Path, "/v1/"):
		h.proxy.ServeHTTP(w, r)
	default:
		writeError(w, http.StatusNotFound, "NOT_FOUND", "route not found", requestID)
	}
}

func requireGet(w http.ResponseWriter, r *http.Request, requestID string) bool {
	if r.Method == http.MethodGet {
		return true
	}
	w.Header().Set("Allow", http.MethodGet)
	writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "endpoint only accepts GET", requestID)
	return false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func canonicalRoute(path string) string {
	if strings.HasPrefix(path, "/v1/") {
		return "/v1/*"
	}
	switch path {
	case "/healthz", "/readyz", "/admin/workers", "/metrics":
		return path
	default:
		return "not_found"
	}
}

func validRequestID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return true
}

func newRequestID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(value[:])
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusRecorder) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

// Unwrap lets http.ResponseController discover optional interfaces on the real writer.
func (w *statusRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }
