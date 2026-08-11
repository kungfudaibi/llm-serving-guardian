package benchmark

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type RequestOptions struct {
	Endpoint    string
	APIKey      string
	Model       string
	Prompt      string
	MaxTokens   int
	Temperature float64
}

type chatRequest struct {
	Model         string          `json:"model"`
	Messages      []chatMessage   `json:"messages"`
	MaxTokens     int             `json:"max_tokens"`
	Temperature   float64         `json:"temperature"`
	Stream        bool            `json:"stream"`
	StreamOptions map[string]bool `json:"stream_options"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *struct {
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func Request(ctx context.Context, client *http.Client, options RequestOptions) (Sample, error) {
	payload, err := json.Marshal(chatRequest{
		Model:         options.Model,
		Messages:      []chatMessage{{Role: "user", Content: options.Prompt}},
		MaxTokens:     options.MaxTokens,
		Temperature:   options.Temperature,
		Stream:        true,
		StreamOptions: map[string]bool{"include_usage": true},
	})
	if err != nil {
		return Sample{}, fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, options.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return Sample{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if options.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+options.APIKey)
	}
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return Sample{}, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Sample{}, fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var firstToken time.Time
	completionTokens := -1
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}
		var chunk chatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return Sample{}, fmt.Errorf("decode stream event: %w", err)
		}
		for _, choice := range chunk.Choices {
			if firstToken.IsZero() && choice.Delta.Content != "" {
				firstToken = time.Now()
			}
		}
		if chunk.Usage != nil {
			completionTokens = chunk.Usage.CompletionTokens
		}
	}
	finished := time.Now()
	if err := scanner.Err(); err != nil {
		return Sample{}, fmt.Errorf("read stream: %w", err)
	}
	if firstToken.IsZero() {
		return Sample{}, errors.New("stream did not include a content token")
	}
	if completionTokens < 0 {
		return Sample{}, errors.New("stream did not include completion token usage")
	}
	return Sample{TTFT: firstToken.Sub(started), E2E: finished.Sub(started), CompletionTokens: completionTokens}, nil
}
