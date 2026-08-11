package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadAppliesDefaultsAndExpandsEnvironment(t *testing.T) {
	t.Setenv("TEST_WORKER_KEY", "local-secret")
	path := filepath.Join(t.TempDir(), "guardian.json")
	data := `{
  "workers": [
    {"name": "local", "url": "http://127.0.0.1:8081", "apiKey": "${TEST_WORKER_KEY}"}
  ]
}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Listen != "127.0.0.1:8090" {
		t.Fatalf("Listen = %q", cfg.Server.Listen)
	}
	if cfg.Server.RequestTimeout != 5*time.Minute {
		t.Fatalf("RequestTimeout = %v", cfg.Server.RequestTimeout)
	}
	if cfg.Workers[0].APIKey != "local-secret" {
		t.Fatalf("APIKey was not expanded")
	}
}

func TestLoadRejectsInvalidConfigurations(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{"no workers", `{}`},
		{"duplicate worker", `{"workers":[{"name":"a","url":"http://127.0.0.1:1"},{"name":"a","url":"http://127.0.0.1:2"}]}`},
		{"unsupported scheme", `{"workers":[{"name":"a","url":"file:///tmp/model"}]}`},
		{"invalid listen", `{"server":{"listen":"not-an-address"},"workers":[{"name":"a","url":"http://127.0.0.1:1"}]}`},
		{"invalid duration", `{"health":{"interval":"soon"},"workers":[{"name":"a","url":"http://127.0.0.1:1"}]}`},
		{"invalid attempts", `{"proxy":{"maxAttempts":0},"workers":[{"name":"a","url":"http://127.0.0.1:1"}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "guardian.json")
			if err := os.WriteFile(path, []byte(tt.json), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("Load() error = nil, want validation error")
			}
		})
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guardian.json")
	if err := os.WriteFile(path, []byte(`{"workers":[{"name":"a","url":"http://127.0.0.1:1"}],"typo":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want unknown field error")
	}
}

func TestLoadRejectsTrailingJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guardian.json")
	data := `{"workers":[{"name":"a","url":"http://127.0.0.1:1"}]} {}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want trailing JSON error")
	}
}
