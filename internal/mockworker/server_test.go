package mockworker

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesOpenAIResponseAndFailureToggle(t *testing.T) {
	handler := NewHandler("demo")

	chat := httptest.NewRecorder()
	handler.ServeHTTP(chat, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"mock","messages":[]}`)))
	if chat.Code != http.StatusOK || !strings.Contains(chat.Body.String(), "guardian mock response") {
		t.Fatalf("chat response = %d %q", chat.Code, chat.Body.String())
	}

	toggle := httptest.NewRecorder()
	handler.ServeHTTP(toggle, httptest.NewRequest(http.MethodPost, "/admin/failure?enabled=true", nil))
	if toggle.Code != http.StatusOK {
		t.Fatalf("toggle status = %d", toggle.Code)
	}
	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if health.Code != http.StatusServiceUnavailable {
		t.Fatalf("health status = %d", health.Code)
	}
	failedChat := httptest.NewRecorder()
	handler.ServeHTTP(failedChat, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	if failedChat.Code != http.StatusServiceUnavailable {
		t.Fatalf("failed chat status = %d", failedChat.Code)
	}
}

func TestHandlerStreamsServerSentEvents(t *testing.T) {
	handler := NewHandler("demo")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"stream":true}`)))
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream response = %d %q", recorder.Code, recorder.Header().Get("Content-Type"))
	}
	if !strings.Contains(recorder.Body.String(), "data: [DONE]") {
		t.Fatalf("stream body = %q", recorder.Body.String())
	}
}
