package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kungfudaibi/llm-serving-guardian/internal/benchmark"
)

func TestReadFaultMarkerParsesRFC3339Nano(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fault-at.txt")
	want := time.Date(2026, 8, 11, 8, 1, 2, 345678901, time.UTC)
	if err := os.WriteFile(path, []byte(want.Format(time.RFC3339Nano)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := readFaultMarker(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(want) {
		t.Fatalf("fault time = %s, want %s", got, want)
	}
}

func TestValidateFaultTimeRequiresMarkerInsideWorkload(t *testing.T) {
	started := time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC)
	attempts := []benchmark.AvailabilityAttempt{{StartedAt: started, FinishedAt: started.Add(10 * time.Second)}}
	if err := validateFaultTime(attempts, started.Add(5*time.Second)); err != nil {
		t.Fatalf("valid fault marker rejected: %v", err)
	}
	if err := validateFaultTime(attempts, started.Add(-time.Second)); err == nil {
		t.Fatal("fault marker before workload unexpectedly accepted")
	}
}
