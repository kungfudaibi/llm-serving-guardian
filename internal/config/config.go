// Package config loads and validates guardian runtime configuration.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

// Config is the validated runtime configuration.
type Config struct {
	Server  Server
	Health  Health
	Circuit Circuit
	Proxy   Proxy
	Workers []Worker
}

type Server struct {
	Listen          string
	RequestTimeout  time.Duration
	ShutdownTimeout time.Duration
	MaxBodyBytes    int64
	RateLimitRPS    float64
	RateLimitBurst  int
}

type Health struct {
	Interval time.Duration
	Timeout  time.Duration
}

type Circuit struct {
	FailureThreshold int
	Cooldown         time.Duration
}

type Proxy struct {
	MaxAttempts int
}

type Worker struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	APIKey string `json:"apiKey,omitempty"`
}

type rawConfig struct {
	Server struct {
		Listen          string  `json:"listen"`
		RequestTimeout  string  `json:"requestTimeout"`
		ShutdownTimeout string  `json:"shutdownTimeout"`
		MaxBodyBytes    int64   `json:"maxBodyBytes"`
		RateLimitRPS    float64 `json:"rateLimitRPS"`
		RateLimitBurst  int     `json:"rateLimitBurst"`
	} `json:"server"`
	Health struct {
		Interval string `json:"interval"`
		Timeout  string `json:"timeout"`
	} `json:"health"`
	Circuit struct {
		FailureThreshold int    `json:"failureThreshold"`
		Cooldown         string `json:"cooldown"`
	} `json:"circuit"`
	Proxy struct {
		MaxAttempts int `json:"maxAttempts"`
	} `json:"proxy"`
	Workers []Worker `json:"workers"`
}

func defaultRawConfig() rawConfig {
	var cfg rawConfig
	cfg.Server.Listen = "127.0.0.1:8090"
	cfg.Server.RequestTimeout = "5m"
	cfg.Server.ShutdownTimeout = "10s"
	cfg.Server.MaxBodyBytes = 10 << 20
	cfg.Server.RateLimitRPS = 10
	cfg.Server.RateLimitBurst = 20
	cfg.Health.Interval = "5s"
	cfg.Health.Timeout = "2s"
	cfg.Circuit.FailureThreshold = 3
	cfg.Circuit.Cooldown = "15s"
	cfg.Proxy.MaxAttempts = 2
	return cfg
}

// Load reads a strict JSON file, expands environment variables, and validates it.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	raw := defaultRawConfig()
	decoder := json.NewDecoder(bytes.NewReader([]byte(os.ExpandEnv(string(data)))))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("decode config: multiple JSON values")
	}

	requestTimeout, err := positiveDuration("server.requestTimeout", raw.Server.RequestTimeout)
	if err != nil {
		return Config{}, err
	}
	shutdownTimeout, err := positiveDuration("server.shutdownTimeout", raw.Server.ShutdownTimeout)
	if err != nil {
		return Config{}, err
	}
	healthInterval, err := positiveDuration("health.interval", raw.Health.Interval)
	if err != nil {
		return Config{}, err
	}
	healthTimeout, err := positiveDuration("health.timeout", raw.Health.Timeout)
	if err != nil {
		return Config{}, err
	}
	cooldown, err := positiveDuration("circuit.cooldown", raw.Circuit.Cooldown)
	if err != nil {
		return Config{}, err
	}

	if _, _, err := net.SplitHostPort(raw.Server.Listen); err != nil {
		return Config{}, fmt.Errorf("server.listen: %w", err)
	}
	if raw.Server.MaxBodyBytes <= 0 {
		return Config{}, errors.New("server.maxBodyBytes must be positive")
	}
	if raw.Server.RateLimitRPS < 0 || raw.Server.RateLimitBurst <= 0 {
		return Config{}, errors.New("server rate limit must have non-negative RPS and positive burst")
	}
	if raw.Circuit.FailureThreshold <= 0 {
		return Config{}, errors.New("circuit.failureThreshold must be positive")
	}
	if raw.Proxy.MaxAttempts <= 0 {
		return Config{}, errors.New("proxy.maxAttempts must be positive")
	}
	if len(raw.Workers) == 0 {
		return Config{}, errors.New("workers must contain at least one worker")
	}

	seen := make(map[string]struct{}, len(raw.Workers))
	for i := range raw.Workers {
		worker := &raw.Workers[i]
		worker.Name = strings.TrimSpace(worker.Name)
		worker.URL = strings.TrimRight(strings.TrimSpace(worker.URL), "/")
		if worker.Name == "" {
			return Config{}, fmt.Errorf("workers[%d].name is required", i)
		}
		if _, ok := seen[worker.Name]; ok {
			return Config{}, fmt.Errorf("worker name %q is duplicated", worker.Name)
		}
		seen[worker.Name] = struct{}{}
		parsed, err := url.Parse(worker.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return Config{}, fmt.Errorf("workers[%d].url must be an absolute HTTP(S) URL", i)
		}
	}

	return Config{
		Server: Server{
			Listen: raw.Server.Listen, RequestTimeout: requestTimeout,
			ShutdownTimeout: shutdownTimeout, MaxBodyBytes: raw.Server.MaxBodyBytes,
			RateLimitRPS: raw.Server.RateLimitRPS, RateLimitBurst: raw.Server.RateLimitBurst,
		},
		Health:  Health{Interval: healthInterval, Timeout: healthTimeout},
		Circuit: Circuit{FailureThreshold: raw.Circuit.FailureThreshold, Cooldown: cooldown},
		Proxy:   Proxy{MaxAttempts: raw.Proxy.MaxAttempts},
		Workers: raw.Workers,
	}, nil
}

func positiveDuration(field, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", field)
	}
	return duration, nil
}
