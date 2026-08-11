package benchmark

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestMeasuresStreamingResponseAndReadsUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("request = %s %s content-type=%q", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["model"] != "test-model" || payload["stream"] != true {
			t.Fatalf("payload = %#v", payload)
		}
		streamOptions, ok := payload["stream_options"].(map[string]any)
		if !ok || streamOptions["include_usage"] != true {
			t.Fatalf("stream_options = %#v", payload["stream_options"])
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Guardian-Worker", "gpu-one")
		w.Header().Set("X-Guardian-Attempts", "2")
		w.Header().Set("X-Request-Id", "request-one")
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"completion_tokens\":2}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	sample, err := Request(t.Context(), server.Client(), RequestOptions{
		Endpoint: server.URL + "/v1/chat/completions",
		Model:    "test-model", Prompt: "hello", MaxTokens: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sample.CompletionTokens != 2 || sample.TTFT <= 0 || sample.E2E < sample.TTFT {
		t.Fatalf("sample = %+v", sample)
	}
	if sample.Worker != "gpu-one" || sample.Attempts != 2 || sample.RequestID != "request-one" {
		t.Fatalf("routing metadata = %+v", sample)
	}
	if sample.StartedAt.IsZero() || sample.FinishedAt.Before(sample.StartedAt) {
		t.Fatalf("request timestamps = %+v", sample)
	}
}

func TestRequestRejectsSuccessfulStreamWithoutUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	_, err := Request(t.Context(), server.Client(), RequestOptions{
		Endpoint: server.URL, Model: "test-model", Prompt: "hello", MaxTokens: 16,
	})
	if err == nil || err.Error() != "stream did not include completion token usage" {
		t.Fatalf("error = %v", err)
	}
}

func TestRequestPreservesMetadataWhenStreamFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Guardian-Worker", "gpu-zero")
		w.Header().Set("X-Guardian-Attempts", "1")
		w.Header().Set("X-Request-Id", "request-failed")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
	}))
	defer server.Close()

	sample, err := Request(t.Context(), server.Client(), RequestOptions{
		Endpoint: server.URL, Model: "test-model", Prompt: "hello", MaxTokens: 16,
	})
	if err == nil {
		t.Fatal("Request() unexpectedly accepted a stream without usage")
	}
	if sample.Worker != "gpu-zero" || sample.Attempts != 1 || sample.RequestID != "request-failed" {
		t.Fatalf("routing metadata = %+v", sample)
	}
	if sample.StartedAt.IsZero() || sample.FinishedAt.Before(sample.StartedAt) || !sample.StreamStarted {
		t.Fatalf("failed request timestamps = %+v", sample)
	}
}
