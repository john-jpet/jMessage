package api

import (
	"net"
	"net/http"
	"sync"
	"time"

	"jmessage/internal/auth"
)

// rateLimiter is a fixed-window rate limiter keyed by an arbitrary
// string (client IP or user ID). It is intentionally simple: exact
// request timestamps per key, filtered against a sliding cutoff.
type rateLimiter struct {
	limit  int
	window time.Duration

	mu    sync.Mutex
	hits  map[string][]time.Time
	calls int // opportunistic cleanup trigger
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, hits: make(map[string][]time.Time)}
}

func (l *rateLimiter) allow(key string) bool {
	now := time.Now()
	cutoff := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)

	// Every so often, drop keys with no recent activity so the map
	// doesn't grow unbounded across the process lifetime.
	l.calls++
	if l.calls%1000 == 0 {
		for k, v := range l.hits {
			if len(v) == 0 {
				delete(l.hits, k)
			}
		}
	}
	return true
}

// byIP rate-limits by client IP — the only option before authentication
// runs (login, register, and any other pre-auth or unauthenticated
// route).
func (l *rateLimiter) byIP(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(clientIP(r)) {
			writeErr(w, http.StatusTooManyRequests, "too many requests, try again later")
			return
		}
		next(w, r)
	}
}

// byIPHandler is byIP for a plain http.Handler (chi middleware shape),
// used for the router-wide backstop that also covers routes with their
// own non-chi auth (e.g. attachment downloads).
func (l *rateLimiter) byIPHandler(next http.Handler) http.Handler {
	return l.byIP(next.ServeHTTP)
}

// byUser rate-limits by authenticated user ID. Must run after
// auth.Tokens.Middleware so UserID is already in the request context;
// unauthenticated requests (no context value) fall back to per-IP so
// they can't bypass the limiter entirely.
func (l *rateLimiter) byUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := auth.UserID(r.Context())
		if key == "" {
			key = clientIP(r)
		}
		if !l.allow(key) {
			writeErr(w, http.StatusTooManyRequests, "too many requests, try again later")
			return
		}
		next(w, r)
	}
}

// byUserHandler is byUser in chi middleware shape (func(http.Handler)
// http.Handler), for use with r.Use on a route group.
func (l *rateLimiter) byUserHandler(next http.Handler) http.Handler {
	return l.byUser(next.ServeHTTP)
}

// clientIP strips the port from RemoteAddr. Deployments behind a
// reverse proxy should terminate at a proxy that sets RemoteAddr
// correctly (or add X-Forwarded-For handling here) — trusting a
// client-supplied header unconditionally would let the limiter be
// bypassed by forging it.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
