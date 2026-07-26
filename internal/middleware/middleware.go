package middleware

import (
	"bufio"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wr := &responseWriter{ResponseWriter: w}
			next.ServeHTTP(wr, r)
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"remote", r.RemoteAddr,
				"status", wr.status,
				"duration", time.Since(start).String(),
			)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if rw.status == 0 {
		rw.status = http.StatusOK
	}
	return rw.ResponseWriter.Write(b)
}

func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack forwards to the underlying writer so WebSocket upgrades work through
// the logging middleware.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijacker not supported")
	}
	return h.Hijack()
}

func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

func RateLimiter(rate float64, burst int) func(http.Handler) http.Handler {
	rl := &rateLimiter{
		entries: make(map[string]*rateLimitEntry),
		rate:    rate,
		burst:   float64(burst),
	}
	go rl.runCleanup()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Presence / streaming traffic is high-frequency and must not starve saves.
			if skipRateLimit(r) {
				next.ServeHTTP(w, r)
				return
			}
			if !rl.allow(clientIP(r)) {
				w.Header().Set("Retry-After", "1")
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":"rate limit exceeded"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func skipRateLimit(r *http.Request) bool {
	path := r.URL.Path
	if path == "/healthz" || strings.HasPrefix(path, "/static/") {
		return true
	}
	// Realtime channels are chatty by design during multi-user sessions.
	if strings.HasSuffix(path, "/ws") || strings.HasSuffix(path, "/cursor") || strings.HasSuffix(path, "/events") {
		return true
	}
	return false
}

type rateLimiter struct {
	mu      sync.Mutex
	entries map[string]*rateLimitEntry
	rate    float64
	burst   float64
}

type rateLimitEntry struct {
	tokens   float64
	lastTime time.Time
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, ok := rl.entries[ip]
	if !ok {
		entry = &rateLimitEntry{tokens: rl.burst, lastTime: now}
		rl.entries[ip] = entry
	} else {
		elapsed := now.Sub(entry.lastTime).Seconds()
		entry.tokens += elapsed * rl.rate
		if entry.tokens > rl.burst {
			entry.tokens = rl.burst
		}
		entry.lastTime = now
	}

	if entry.tokens >= 1 {
		entry.tokens--
		return true
	}
	return false
}

func (rl *rateLimiter) runCleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		cutoff := time.Now().Add(-5 * time.Minute)
		for ip, entry := range rl.entries {
			if entry.lastTime.Before(cutoff) {
				delete(rl.entries, ip)
			}
		}
		rl.mu.Unlock()
	}
}

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		if idx := strings.Index(ip, ","); idx != -1 {
			ip = ip[:idx]
		}
		return strings.TrimSpace(ip)
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}
