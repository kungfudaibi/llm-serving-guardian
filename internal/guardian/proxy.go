package guardian

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type ProxyObserver interface {
	ObserveUpstream(worker, outcome string, duration time.Duration)
}

type ProxyOptions struct {
	MaxAttempts    int
	MaxBodyBytes   int64
	RequestTimeout time.Duration
	Limiter        *Limiter
	Logger         *slog.Logger
	Observer       ProxyObserver
}

// Proxy forwards an OpenAI-compatible request to a healthy worker.
type Proxy struct {
	pool    *Pool
	client  *http.Client
	options ProxyOptions
}

type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	RequestID string `json:"requestId"`
}

func NewProxy(pool *Pool, client *http.Client, options ProxyOptions) *Proxy {
	if client == nil {
		client = http.DefaultClient
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	if options.Limiter == nil {
		options.Limiter = NewLimiter(0, 1)
	}
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	return &Proxy{pool: pool, client: &clientCopy, options: options}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := r.Header.Get("X-Request-Id")
	if !p.options.Limiter.Allow(clientKey(r.RemoteAddr)) {
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "client request rate exceeded", requestID)
		return
	}
	if r.ContentLength > p.options.MaxBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "request body exceeds configured limit", requestID)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, p.options.MaxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "request body exceeds configured limit", requestID)
			return
		}
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "request body could not be read", requestID)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), p.options.RequestTimeout)
	defer cancel()
	attempted := make(map[string]bool, p.options.MaxAttempts)
	var lastFailure error

	for attempt := 1; attempt <= p.options.MaxAttempts; attempt++ {
		worker, ok := p.pool.Next(attempted)
		if !ok {
			break
		}
		attempted[worker.Name] = true
		started := time.Now()
		resp, err := p.attempt(ctx, r, body, worker)
		duration := time.Since(started)
		if err != nil {
			lastFailure = err
			p.pool.ReportFailure(worker.Name, err)
			p.observe(worker.Name, "transport_error", duration)
			p.options.Logger.Warn("upstream attempt failed", "event", "upstream_attempt_failed", "requestId", requestID, "worker", worker.Name, "attempt", attempt, "error", err.Error())
			continue
		}
		if resp.StatusCode >= 500 {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 32<<10))
			_ = resp.Body.Close()
			lastFailure = fmt.Errorf("upstream status %d", resp.StatusCode)
			p.pool.ReportFailure(worker.Name, lastFailure)
			p.observe(worker.Name, "5xx", duration)
			p.options.Logger.Warn("upstream attempt failed", "event", "upstream_attempt_failed", "requestId", requestID, "worker", worker.Name, "attempt", attempt, "status", resp.StatusCode, "error", lastFailure.Error())
			continue
		}

		p.pool.ReportSuccess(worker.Name)
		p.observe(worker.Name, "success", duration)
		copyResponseHeaders(w.Header(), resp.Header)
		w.Header().Set("X-Guardian-Worker", worker.Name)
		w.Header().Set("X-Guardian-Attempts", strconv.Itoa(attempt))
		w.WriteHeader(resp.StatusCode)
		copyErr := copyStreaming(w, resp.Body)
		_ = resp.Body.Close()
		if copyErr != nil {
			p.pool.ReportFailure(worker.Name, copyErr)
			p.observe(worker.Name, "stream_error", time.Since(started))
			p.options.Logger.Warn("upstream stream interrupted", "event", "upstream_stream_interrupted", "requestId", requestID, "worker", worker.Name, "error", copyErr.Error())
		}
		return
	}

	if len(attempted) == 0 {
		writeError(w, http.StatusServiceUnavailable, "NO_HEALTHY_WORKER", "no healthy inference worker is available", requestID)
		return
	}
	if errors.Is(lastFailure, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		writeError(w, http.StatusGatewayTimeout, "REQUEST_TIMEOUT", "upstream request timed out", requestID)
		return
	}
	writeError(w, http.StatusBadGateway, "UPSTREAM_FAILURE", "all attempted inference workers failed", requestID)
}

func (p *Proxy) attempt(ctx context.Context, incoming *http.Request, body []byte, worker *Worker) (*http.Response, error) {
	target := *incoming.URL
	target.Scheme = worker.baseURL.Scheme
	target.Host = worker.baseURL.Host
	target.Path = singleJoiningSlash(worker.baseURL.Path, incoming.URL.Path)
	target.RawPath = ""

	req, err := http.NewRequestWithContext(ctx, incoming.Method, target.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}
	req.Header = incoming.Header.Clone()
	removeHopHeaders(req.Header)
	if worker.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+worker.apiKey)
	}
	return p.client.Do(req)
}

func (p *Proxy) observe(worker, outcome string, duration time.Duration) {
	if p.options.Observer != nil {
		p.options.Observer.ObserveUpstream(worker, outcome, duration)
	}
}

func writeError(w http.ResponseWriter, status int, code, message, requestID string) {
	response := errorResponse{RequestID: requestID}
	response.Error.Code = code
	response.Error.Message = message
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}

func copyResponseHeaders(destination, source http.Header) {
	cloned := source.Clone()
	removeHopHeaders(cloned)
	for key, values := range cloned {
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}

func removeHopHeaders(header http.Header) {
	for _, connectionValue := range header.Values("Connection") {
		for token := range strings.SplitSeq(connectionValue, ",") {
			header.Del(strings.TrimSpace(token))
		}
	}
	for _, key := range []string{
		"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
		"Te", "Trailer", "Transfer-Encoding", "Upgrade",
	} {
		header.Del(key)
	}
}

func copyStreaming(w http.ResponseWriter, body io.Reader) error {
	buffer := make([]byte, 32<<10)
	controller := http.NewResponseController(w)
	for {
		count, readErr := body.Read(buffer)
		if count > 0 {
			if _, err := w.Write(buffer[:count]); err != nil {
				return fmt.Errorf("write downstream response: %w", err)
			}
			_ = controller.Flush()
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return fmt.Errorf("read upstream response: %w", readErr)
		}
	}
}

func singleJoiningSlash(left, right string) string {
	return strings.TrimRight(left, "/") + "/" + strings.TrimLeft(right, "/")
}
