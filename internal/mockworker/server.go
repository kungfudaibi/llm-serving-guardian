// Package mockworker provides a deterministic llama.cpp-compatible demo worker.
package mockworker

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

type Handler struct {
	name    string
	failing atomic.Bool
}

func NewHandler(name string) *Handler { return &Handler{name: name} }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/health":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if h.failing.Load() {
			http.Error(w, "mock worker failure enabled", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "worker": h.name})
	case "/admin/failure":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		enabled, err := strconv.ParseBool(r.URL.Query().Get("enabled"))
		if err != nil {
			http.Error(w, "enabled must be true or false", http.StatusBadRequest)
			return
		}
		h.failing.Store(enabled)
		writeJSON(w, http.StatusOK, map[string]bool{"isFailing": enabled})
	case "/v1/chat/completions":
		h.chat(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) chat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.failing.Load() {
		http.Error(w, "mock inference failure", http.StatusServiceUnavailable)
		return
	}
	var request struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if r.Body != nil {
		_ = decoder.Decode(&request)
	}
	if request.Model == "" {
		request.Model = "guardian-mock"
	}
	if request.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"guardian mock response\"},\"index\":0}]}\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": "chatcmpl-guardian-demo", "object": "chat.completion", "created": time.Now().Unix(), "model": request.Model,
		"choices": []any{map[string]any{"index": 0, "message": map[string]string{"role": "assistant", "content": "guardian mock response"}, "finish_reason": "stop"}},
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
