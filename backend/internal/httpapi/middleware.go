package httpapi

// Cross-cutting request handling: what every route passes through, rather than what any one route
// does. Kept apart from server.go so the route table there stays readable as a table.

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/beskosorg/beskos/backend/internal/rbac"
)

const sessionTTL = 7 * 24 * time.Hour

// redactSensitiveQuery returns the URL with credential parameters masked (for logs).
func redactSensitiveQuery(u *url.URL) string {
	if u.RawQuery == "" {
		return u.Path
	}
	q := u.Query()
	redacted := false
	for _, k := range []string{"access_token", "token"} {
		if q.Has(k) {
			q.Set(k, "REDACTED")
			redacted = true
		}
	}
	if !redacted {
		return u.Path + "?" + u.RawQuery
	}
	return u.Path + "?" + q.Encode()
}

// csrfGuard protects session routes against CSRF. State-changing requests need either an
// Authorization: Bearer header (native app — not an ambient credential, so no CSRF risk) or the
// custom X-Mailin-Client header that only our own client sends. A "simple" cross-site POST (a
// plain form) can't set a custom header without a preflight, and the preflight hits the CORS
// allowlist.
func (s *Server) csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if r.Header.Get("Authorization") == "" && r.Header.Get("X-Mailin-Client") == "" {
				writeErr(w, http.StatusForbidden, "csrf.blocked")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// publicRateLimit limits per-IP the endpoints without login (public Meet, download by token). Blocks
// room/token enumeration and abuse of these anonymous endpoints (audit M).
func (s *Server) publicRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.pubLimiter.allow("pub:"+clientIP(r), time.Now()) {
			writeErr(w, http.StatusTooManyRequests, "rate_limited")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// presentationRateLimit limits the presentation PDF in its own, generous bucket (see presLimiter).
func (s *Server) presentationRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.presLimiter.allow("pres:"+clientIP(r), time.Now()) {
			writeErr(w, http.StatusTooManyRequests, "rate_limited")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requestLogger logs method, URL (with credentials redacted), status and duration.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		defer func() {
			log.Printf("%s %s -> %d (%s)", r.Method, redactSensitiveQuery(r.URL), ww.Status(), time.Since(start))
		}()
		next.ServeHTTP(ww, r)
	})
}

// requirePerm requires the authenticated user (panel session) to hold the permission. The source
// of truth is the stored role, never one coming from the request. Fails closed.
func (s *Server) requirePerm(perm rbac.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := mailUserFrom(r.Context())
			// Effective = role ± the person's exceptions. It's one extra read per request, but only
			// on admin routes (low traffic), and it's what makes the exceptions real: without this
			// they'd be UI decoration, with the backend still deciding by role alone.
			allow, deny := s.memberOverrides(r.Context(), u.TenantID, u.ID)
			if !rbac.HasEffective(rbac.Role(u.Role), allow, deny, perm) {
				writeErr(w, http.StatusForbidden, "rbac.forbidden")
				return
			}
			next.ServeHTTP(w, r.WithContext(r.Context()))
		})
	}
}

// --- responses ---

// orEmpty swaps a nil slice for an empty one: in JSON, nil becomes `null` and the frontend does
// .map/.length on top of that. An empty list is `[]`.
func orEmpty[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr returns an error with a STABLE code (the frontend translates it via i18n — law §4.4)
// plus an English fallback message derived from the code. The code is the source of truth for
// the translation; the message is only a backup and is always English (law §4.6).
func writeErr(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error_code": code, "error": humanizeCode(code)})
}

// humanizeCode turns "auth.invalid_credentials" into "Invalid credentials" (English fallback).
func humanizeCode(code string) string {
	s := code
	if i := strings.LastIndex(code, "."); i >= 0 {
		s = code[i+1:]
	}
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return "Error"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
