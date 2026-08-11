package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kungfudaibi/llm-serving-guardian/internal/config"
	"github.com/kungfudaibi/llm-serving-guardian/internal/guardian"
	"github.com/kungfudaibi/llm-serving-guardian/internal/telemetry"
)

func main() {
	configPath := flag.String("config", "configs/local.json", "path to guardian JSON configuration")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("configuration failed", "event", "startup_failed", "error", err.Error())
		os.Exit(1)
	}
	if err := run(cfg, logger); err != nil {
		logger.Error("guardian stopped", "event", "server_failed", "error", err.Error())
		os.Exit(1)
	}
}

func run(cfg config.Config, logger *slog.Logger) error {
	pool, err := guardian.NewPool(cfg.Workers, cfg.Circuit.FailureThreshold, cfg.Circuit.Cooldown)
	if err != nil {
		return err
	}
	workerNames := make([]string, 0, len(cfg.Workers))
	for _, worker := range cfg.Workers {
		workerNames = append(workerNames, worker.Name)
	}
	metrics := telemetry.New(workerNames)

	transport := &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		DialContext:       (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2: true, MaxIdleConns: 100, MaxIdleConnsPerHost: 20,
		IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 5 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	client := &http.Client{Transport: transport}
	limiter := guardian.NewLimiter(cfg.Server.RateLimitRPS, cfg.Server.RateLimitBurst)
	proxy := guardian.NewProxy(pool, client, guardian.ProxyOptions{
		MaxAttempts: cfg.Proxy.MaxAttempts, MaxBodyBytes: cfg.Server.MaxBodyBytes,
		RequestTimeout: cfg.Server.RequestTimeout, Limiter: limiter, Logger: logger, Observer: metrics,
	})
	checker := guardian.NewHealthChecker(pool, client, cfg.Health.Interval, cfg.Health.Timeout)
	checker.SetObserver(metrics)
	handler := guardian.NewHandler(pool, proxy, metrics.Handler(), metrics, logger)
	server := &http.Server{
		Addr: cfg.Server.Listen, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go checker.Run(ctx)

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("guardian started", "event", "server_started", "listen", cfg.Server.Listen, "workers", len(cfg.Workers))
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		logger.Info("shutdown requested", "event", "shutdown_started")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		transport.CloseIdleConnections()
		logger.Info("shutdown complete", "event", "shutdown_completed")
		return nil
	}
}
