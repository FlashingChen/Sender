package server

import (
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	authRateLimit  = 10
	authRateWindow = 5 * time.Minute
)

// RateLimiter is a tiny per-key sliding-window counter with no external
// dependencies. A nil *RateLimiter allows everything.
type RateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string][]time.Time
}

// NewRateLimiter returns a limiter that allows at most limit hits per key
// within a sliding window.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{limit: limit, window: window, hits: make(map[string][]time.Time)}
}

// Allow records a hit for key and reports whether it is within the budget.
func (rl *RateLimiter) Allow(key string) bool {
	if rl == nil {
		return true
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-rl.window)
	recent := rl.hits[key]
	kept := recent[:0]
	for _, hit := range recent {
		if hit.After(cutoff) {
			kept = append(kept, hit)
		}
	}
	if len(kept) == 0 {
		delete(rl.hits, key)
	}
	if len(kept) >= rl.limit {
		rl.hits[key] = kept
		return false
	}
	rl.hits[key] = append(kept, now)
	return true
}

// clientIP extracts the host part of the remote address; behind a proxy the
// operator is expected to run with a single trusted hop.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
