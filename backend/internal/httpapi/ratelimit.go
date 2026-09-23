package httpapi

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// rateLimiter is an in-memory limiter (sliding window per key). Blocks brute-force/credential
// stuffing on login and on the 2nd factor within one instance. Multi-instance would require a
// shared store (Redis) — logged as a known limitation, not as a blocker for this fix (audit H5).
type rateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]int64 // key -> instants (unix nanos) still within the window
	limit  int
	window time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{hits: make(map[string][]int64), limit: limit, window: window}
}

// allow records an attempt for the key and returns false if it exceeds the limit within the window.
func (rl *rateLimiter) allow(key string, now time.Time) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := now.Add(-rl.window).UnixNano()
	// Filters the still-valid instants in place.
	kept := rl.hits[key][:0]
	for _, t := range rl.hits[key] {
		if t >= cutoff {
			kept = append(kept, t)
		}
	}
	if len(kept) >= rl.limit {
		rl.hits[key] = kept
		return false
	}
	rl.hits[key] = append(kept, now.UnixNano())
	// Lazy pruning: if the map has grown too large, remove keys that are fully expired.
	if len(rl.hits) > 10000 {
		for k, ts := range rl.hits {
			if len(ts) == 0 || ts[len(ts)-1] < cutoff {
				delete(rl.hits, k)
			}
		}
	}
	return true
}

// clientIP returns the client's IP. middleware.ClientIPFromXFFTrustedProxies(1), installed in
// Router(), already resolved it from X-Forwarded-For when behind our proxy (Caddy) -- reading the
// entry Caddy itself appended, not whatever a client's own X-Forwarded-For claims, so this can't
// be spoofed by an attacker forging the header. Outside of it (local dev with no proxy), it falls
// back to the raw RemoteAddr.
func clientIP(r *http.Request) string {
	if ip := middleware.GetClientIP(r.Context()); ip != "" {
		return ip
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
