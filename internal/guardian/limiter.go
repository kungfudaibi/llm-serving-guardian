package guardian

import (
	"net"
	"sync"
	"time"
)

type bucket struct {
	tokens   float64
	lastSeen time.Time
}

// Limiter is an in-memory token bucket isolated by client address.
type Limiter struct {
	mu          sync.Mutex
	rate        float64
	burst       float64
	buckets     map[string]bucket
	now         func() time.Time
	lastCleanup time.Time
}

func NewLimiter(rate float64, burst int) *Limiter {
	return &Limiter{
		rate: rate, burst: float64(burst), buckets: make(map[string]bucket), now: time.Now,
	}
}

func (l *Limiter) Allow(key string) bool {
	if l.rate == 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	b, exists := l.buckets[key]
	if !exists {
		b = bucket{tokens: l.burst, lastSeen: now}
	} else {
		b.tokens = min(l.burst, b.tokens+now.Sub(b.lastSeen).Seconds()*l.rate)
		b.lastSeen = now
	}
	allowed := b.tokens >= 1
	if allowed {
		b.tokens--
	}
	l.buckets[key] = b

	if l.lastCleanup.IsZero() || now.Sub(l.lastCleanup) >= time.Minute {
		for client, candidate := range l.buckets {
			if now.Sub(candidate.lastSeen) > time.Hour {
				delete(l.buckets, client)
			}
		}
		l.lastCleanup = now
	}
	return allowed
}

func clientKey(remoteAddress string) string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err == nil {
		return host
	}
	return remoteAddress
}
