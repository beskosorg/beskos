package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

// withClientIPMiddleware wraps h the same way Router() does: exactly one trusted reverse proxy
// (Caddy) in front, so clientIP must resolve from the X-Forwarded-For entry the middleware trusts,
// not directly from the raw header.
func withClientIPMiddleware(h http.HandlerFunc) http.Handler {
	return middleware.ClientIPFromXFFTrustedProxies(1)(h)
}

func TestClientIPResolvesTrustedProxyHop(t *testing.T) {
	var got string
	h := withClientIPMiddleware(func(w http.ResponseWriter, r *http.Request) {
		got = clientIP(r)
	})
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.5:443" // Caddy's own address, as seen at the TCP layer
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if got != "203.0.113.7" {
		t.Errorf("clientIP = %q, want the single X-Forwarded-For entry Caddy appended (203.0.113.7)", got)
	}
}

// TestClientIPIgnoresSpoofedPrefix is the actual security property: a client can send whatever it
// wants ahead of the entry our own trusted proxy appends, and it must not be trusted. This is the
// exact vulnerability middleware.RealIP had (GHSA-3fxj-6jh8-hvhx) -- it read the LEFTMOST entry,
// which is attacker-controlled, instead of the one appended by our own infrastructure.
func TestClientIPIgnoresSpoofedPrefix(t *testing.T) {
	var got string
	h := withClientIPMiddleware(func(w http.ResponseWriter, r *http.Request) {
		got = clientIP(r)
	})
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.5:443"
	// An attacker sends a forged first entry, hoping to be attributed a different IP than their
	// real one (e.g. to dodge a per-IP rate limit on login).
	req.Header.Set("X-Forwarded-For", "198.51.100.1, 203.0.113.7")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if got == "198.51.100.1" {
		t.Fatal("clientIP trusted the attacker-forged leftmost X-Forwarded-For entry")
	}
	if got != "203.0.113.7" {
		t.Errorf("clientIP = %q, want 203.0.113.7 (the entry our own proxy appended)", got)
	}
}

func TestClientIPFallsBackToRemoteAddrWithoutProxy(t *testing.T) {
	// No middleware in the chain here: simulates hitting the app directly (local dev), where
	// there's no X-Forwarded-For to resolve from at all.
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:54321"

	got := clientIP(req)
	if got != "127.0.0.1" {
		t.Errorf("clientIP = %q, want 127.0.0.1 from RemoteAddr", got)
	}
}
