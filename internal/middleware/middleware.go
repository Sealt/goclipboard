package middleware

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// IPResolver decides which client IP is used for rate limiting and banning.
//
// Forwarded headers (X-Forwarded-For, X-Real-IP) are only honored when the
// direct peer (RemoteAddr) is inside one of the configured trusted proxy
// CIDRs. With no trusted proxies configured (the default), the client IP is
// always the direct peer, so spoofed headers can neither bypass limits nor
// get arbitrary victim IPs banned.
type IPResolver struct {
	trusted []*net.IPNet
}

// NewIPResolver parses a comma-separated list of trusted proxy CIDRs
// (e.g. "127.0.0.1/32,10.0.0.0/8"). Empty input trusts no proxies.
func NewIPResolver(trustedProxyCIDRs string) (*IPResolver, error) {
	r := &IPResolver{}
	for _, part := range strings.Split(trustedProxyCIDRs, ",") {
		cidr := strings.TrimSpace(part)
		if cidr == "" {
			continue
		}
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy CIDR %q: %w", cidr, err)
		}
		r.trusted = append(r.trusted, ipNet)
	}
	return r, nil
}

// ClientIP returns the effective client IP for r. A nil resolver (or one with
// no trusted proxies) always reports the direct peer address.
func (r *IPResolver) ClientIP(req *http.Request) string {
	host := remoteHost(req.RemoteAddr)
	if r == nil {
		return host
	}
	ip := net.ParseIP(host)
	if ip == nil || !r.trusts(ip) {
		return host
	}
	if forwarded := forwardedClientIP(req); forwarded != "" {
		return forwarded
	}
	return host
}

func (r *IPResolver) trusts(ip net.IP) bool {
	for _, n := range r.trusted {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// forwardedClientIP extracts the first X-Forwarded-For entry, falling back to
// X-Real-IP. Callers must only invoke this after verifying the direct peer is
// a trusted proxy; values that do not parse as IPs are ignored.
func forwardedClientIP(req *http.Request) string {
	if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			xff = xff[:idx]
		}
		xff = strings.TrimSpace(xff)
		if ip := net.ParseIP(xff); ip != nil {
			return ip.String()
		}
	}
	if xri := strings.TrimSpace(req.Header.Get("X-Real-IP")); xri != "" {
		if ip := net.ParseIP(xri); ip != nil {
			return ip.String()
		}
	}
	return ""
}

func remoteHost(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	// No port (unusual): strip a trailing ":port" only for bare IPv4/hostnames;
	// bare IPv6 addresses are returned untouched.
	if idx := strings.LastIndex(remoteAddr, ":"); idx != -1 && !strings.Contains(remoteAddr[idx+1:], ":") {
		return remoteAddr[:idx]
	}
	return remoteAddr
}

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

func RateLimiter(rate float64, burst int, resolver *IPResolver) func(http.Handler) http.Handler {
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
			if !rl.allow(resolver.ClientIP(r)) {
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
