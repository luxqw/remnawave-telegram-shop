package webapp

import (
	"sync"
	"time"
)

// rateLimiter is a simple in-memory per-key (per client IP) fixed-window token bucket. Good
// enough for a single-admin login endpoint behind a reverse proxy — no external store needed.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	limit   int
	window  time.Duration
}

type bucket struct {
	count   int
	resetAt time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{buckets: make(map[string]*bucket), limit: limit, window: window}
}

// Allow reports whether a request from key should proceed, and opportunistically prunes expired
// buckets so the map doesn't grow unbounded under a sustained low-traffic single-admin workload.
func (rl *rateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	if len(rl.buckets) > 1000 {
		for k, b := range rl.buckets {
			if now.After(b.resetAt) {
				delete(rl.buckets, k)
			}
		}
	}

	b, ok := rl.buckets[key]
	if !ok || now.After(b.resetAt) {
		rl.buckets[key] = &bucket{count: 1, resetAt: now.Add(rl.window)}
		return true
	}
	if b.count >= rl.limit {
		return false
	}
	b.count++
	return true
}

// clientIP extracts a best-effort client identifier for rate limiting from X-Forwarded-For (the
// app runs behind a reverse proxy per the project's deployment model) falling back to RemoteAddr.
func clientIP(remoteAddr, forwardedFor string) string {
	if forwardedFor != "" {
		for i, c := range forwardedFor {
			if c == ',' {
				return forwardedFor[:i]
			}
		}
		return forwardedFor
	}
	return remoteAddr
}
