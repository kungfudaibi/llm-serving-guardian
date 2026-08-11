package guardian

import (
	"testing"
	"time"
)

func TestLimiterEnforcesBurstAndRefills(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	limiter := NewLimiter(2, 2)
	limiter.now = func() time.Time { return now }

	if !limiter.Allow("client-a") || !limiter.Allow("client-a") {
		t.Fatal("initial burst was rejected")
	}
	if limiter.Allow("client-a") {
		t.Fatal("request beyond burst was allowed")
	}
	if !limiter.Allow("client-b") {
		t.Fatal("one client exhausted another client's limit")
	}

	now = now.Add(500 * time.Millisecond)
	if !limiter.Allow("client-a") {
		t.Fatal("token did not refill")
	}
}

func TestLimiterCanBeDisabled(t *testing.T) {
	limiter := NewLimiter(0, 1)
	for range 100 {
		if !limiter.Allow("client") {
			t.Fatal("disabled limiter rejected request")
		}
	}
}

func TestClientKeyUsesRemoteAddressNotForwardedHeader(t *testing.T) {
	if got := clientKey("192.0.2.10:54321"); got != "192.0.2.10" {
		t.Fatalf("clientKey() = %q", got)
	}
	if got := clientKey("malformed"); got != "malformed" {
		t.Fatalf("clientKey() fallback = %q", got)
	}
}
