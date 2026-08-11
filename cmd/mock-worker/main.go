package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/kungfudaibi/llm-serving-guardian/internal/mockworker"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8082", "HTTP listen address")
	name := flag.String("name", "demo-worker", "worker name")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	server := &http.Server{
		Addr: *listen, Handler: mockworker.NewHandler(*name),
		ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second,
	}
	logger.Info("mock worker started", "event", "mock_worker_started", "listen", *listen)
	if err := server.ListenAndServe(); err != nil {
		logger.Error("mock worker stopped", "event", "mock_worker_stopped", "error", err.Error())
	}
}
