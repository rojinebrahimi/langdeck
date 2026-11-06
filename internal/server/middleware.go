// Package server holds HTTP plumbing that is independent of LangDeck's API:
// security headers, rate limiting, request ids, logging, metrics and recovery.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/time/rate"

	"github.com/rojinebrahimi/langdeck/internal/util"
)

type ctxKey int

const (
	requestIDKey ctxKey = iota
	clientIPKey
)

// RequestID returns the request id stored by the RequestID middleware.
func RequestID(ctx context.Context) string { s, _ := ctx.Value(requestIDKey).(string); return s }

// ClientIP returns the caller's IP as resolved by the RealIP middleware.
func ClientIP(ctx context.Context) string { s, _ := ctx.Value(clientIPKey).(string); return s }

// WithRequestID assigns a request id, reusing a sane incoming X-Request-ID.
func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" || len(id) > 64 || strings.ContainsAny(id, " \r\n\t") {
			id = util.RandomToken(12)
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

// RealIP resolves the client IP. Forwarded headers are honoured only when the
// deployment says a trusted proxy sets them; otherwise they are spoofable.
func RealIP(trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			if trustProxy {
				if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
					ip = strings.TrimSpace(strings.Split(xff, ",")[0])
				} else if xr := r.Header.Get("X-Real-IP"); xr != "" {
					ip = strings.TrimSpace(xr)
				}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey, ip)))
		})
	}
}

// SecurityHeaders sets a strict baseline. The UI is a same-origin SPA with no
// inline scripts, which lets the CSP stay tight.
func SecurityHeaders(hsts bool) func(http.Handler) http.Handler {
	const csp = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
		"font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'"
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", csp)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Cross-Origin-Resource-Policy", "same-origin")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CORS allows an explicit list of origins. With an empty list no CORS headers
// are sent, which is the right default for a same-origin UI.
func CORS(origins []string) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, o := range origins {
		allowed[o] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && allowed[origin] {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Add("Vary", "Origin")
				if r.Method == http.MethodOptions {
					h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
					h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-CSRF-Token, X-Request-ID")
					h.Set("Access-Control-Max-Age", "600")
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// LimitBody caps request bodies.
func LimitBody(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Recover turns panics into 500s and logs the stack.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
						panic(v)
					}
					log.Error("panic", "err", v, "request_id", RequestID(r.Context()), "stack", string(debug.Stack()))
					http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// statusWriter records the status code. It forwards Flush so SSE keeps working.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }
func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "langdeck_http_requests_total", Help: "HTTP requests by route, method and status.",
	}, []string{"route", "method", "status"})
	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "langdeck_http_request_duration_seconds", Help: "HTTP request latency by route.",
		Buckets: prometheus.DefBuckets,
	}, []string{"route", "method"})
)

// Observe logs each request and records Prometheus metrics. The chi route
// pattern is used as the label so metric cardinality stays bounded.
func Observe(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			route := "unmatched"
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				route = rc.RoutePattern()
			}
			d := time.Since(start)
			httpRequests.WithLabelValues(route, r.Method, strconv.Itoa(sw.status)).Inc()
			httpDuration.WithLabelValues(route, r.Method).Observe(d.Seconds())
			if route == "/healthz" || route == "/readyz" {
				return
			}
			log.Info("http", "method", r.Method, "route", route, "status", sw.status,
				"duration_ms", d.Milliseconds(), "ip", ClientIP(r.Context()), "request_id", RequestID(r.Context()))
		})
	}
}

// RateLimiter is a keyed token-bucket limiter with idle eviction.
type RateLimiter struct {
	mu      sync.Mutex
	rps     rate.Limit
	burst   int
	entries map[string]*limiterEntry
}

type limiterEntry struct {
	lim  *rate.Limiter
	seen time.Time
}

// NewRateLimiter creates a limiter allowing rps per key with the given burst.
func NewRateLimiter(rps float64, burst int) *RateLimiter {
	return &RateLimiter{rps: rate.Limit(rps), burst: burst, entries: map[string]*limiterEntry{}}
}

// Allow reports whether key may proceed now.
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	e, ok := l.entries[key]
	if !ok {
		if len(l.entries) > 50_000 { // bound memory under a flood of unique keys
			l.evict(now, time.Minute)
		}
		e = &limiterEntry{lim: rate.NewLimiter(l.rps, l.burst)}
		l.entries[key] = e
	}
	e.seen = now
	return e.lim.Allow()
}

func (l *RateLimiter) evict(now time.Time, idle time.Duration) {
	for k, e := range l.entries {
		if now.Sub(e.seen) > idle {
			delete(l.entries, k)
		}
	}
}

// Janitor evicts idle keys until ctx ends.
func (l *RateLimiter) Janitor(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			l.mu.Lock()
			l.evict(now, 10*time.Minute)
			l.mu.Unlock()
		}
	}
}

// LimitByIP rejects callers that exceed the limiter, keyed by client IP.
func LimitByIP(l *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !l.Allow(ClientIP(r.Context())) {
				w.Header().Set("Retry-After", "1")
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
