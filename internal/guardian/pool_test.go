package guardian

import (
	"errors"
	"testing"
	"time"

	"github.com/zhaowenjie/llm-serving-guardian/internal/config"
)

func TestPoolRotatesOnlyHealthyWorkers(t *testing.T) {
	pool, err := NewPool([]config.Worker{
		{Name: "one", URL: "http://127.0.0.1:8001"},
		{Name: "two", URL: "http://127.0.0.1:8002"},
	}, 2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	pool.ReportSuccess("one")
	pool.ReportSuccess("two")

	first, ok := pool.Next(nil)
	if !ok || first.Name != "one" {
		t.Fatalf("first worker = %v, %v", first, ok)
	}
	second, ok := pool.Next(nil)
	if !ok || second.Name != "two" {
		t.Fatalf("second worker = %v, %v", second, ok)
	}
	third, ok := pool.Next(map[string]bool{"one": true})
	if !ok || third.Name != "two" {
		t.Fatalf("excluded selection = %v, %v", third, ok)
	}
}

func TestPoolOpensCircuitAndRecoversAfterProbe(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	pool, err := NewPool([]config.Worker{{Name: "one", URL: "http://127.0.0.1:8001"}}, 2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	pool.now = func() time.Time { return now }
	pool.ReportSuccess("one")
	pool.ReportFailure("one", errors.New("first"))
	if _, ok := pool.Next(nil); !ok {
		t.Fatal("worker unavailable before threshold")
	}
	pool.ReportFailure("one", errors.New("second"))
	if _, ok := pool.Next(nil); ok {
		t.Fatal("worker selected with open circuit")
	}
	if got := pool.ProbeCandidates(); len(got) != 0 {
		t.Fatalf("ProbeCandidates before cooldown = %d", len(got))
	}

	now = now.Add(time.Minute)
	if got := pool.ProbeCandidates(); len(got) != 1 {
		t.Fatalf("ProbeCandidates after cooldown = %d", len(got))
	}
	if _, ok := pool.Next(nil); ok {
		t.Fatal("worker selected before successful recovery probe")
	}
	pool.ReportSuccess("one")
	if _, ok := pool.Next(nil); !ok {
		t.Fatal("worker unavailable after successful probe")
	}
}

func TestPoolSnapshotDoesNotExposeAPIKey(t *testing.T) {
	pool, err := NewPool([]config.Worker{{Name: "one", URL: "http://127.0.0.1:8001", APIKey: "secret"}}, 2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	pool.ReportSuccess("one")
	snapshot := pool.Snapshot()
	if len(snapshot) != 1 || snapshot[0].Name != "one" || !snapshot[0].IsHealthy {
		t.Fatalf("Snapshot() = %#v", snapshot)
	}
}

func TestNewPoolRejectsDuplicateNames(t *testing.T) {
	_, err := NewPool([]config.Worker{
		{Name: "same", URL: "http://127.0.0.1:8001"},
		{Name: "same", URL: "http://127.0.0.1:8002"},
	}, 2, time.Minute)
	if err == nil {
		t.Fatal("NewPool() error = nil")
	}
}
