package middleware

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing X-Content-Type-Options")
	}
	if res.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("missing Referrer-Policy")
	}
	if res.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("missing Content-Security-Policy")
	}
}

func TestResponseWriterFlush(t *testing.T) {
	inner := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: inner}

	rw.Write([]byte("hello"))
	if rw.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", rw.status, http.StatusOK)
	}

	rw.WriteHeader(http.StatusCreated)
	if rw.status != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rw.status, http.StatusCreated)
	}
}

func TestResponseWriterFlushDelegation(t *testing.T) {
	inner := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: inner}

	if _, ok := interface{}(rw).(http.Flusher); !ok {
		t.Fatal("responseWriter should implement http.Flusher")
	}

	rw.Flush()
	if !inner.Flushed {
		t.Fatal("inner writer should be flushed")
	}
}

func TestResponseWriterReadFrom(t *testing.T) {
	inner := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: inner}

	if _, ok := interface{}(rw).(io.ReaderFrom); !ok {
		t.Fatal("responseWriter should implement io.ReaderFrom")
	}

	n, err := rw.ReadFrom(strings.NewReader("hello world"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 11 || inner.Body.String() != "hello world" {
		t.Fatalf("ReadFrom n=%d body=%q", n, inner.Body.String())
	}
	if rw.status != http.StatusOK {
		t.Fatalf("status should default to 200, got %d", rw.status)
	}

	// Status defaulting must not clobber an explicit WriteHeader.
	rw2 := &responseWriter{ResponseWriter: httptest.NewRecorder()}
	rw2.WriteHeader(http.StatusCreated)
	_, _ = rw2.ReadFrom(strings.NewReader("x"))
	if rw2.status != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rw2.status, http.StatusCreated)
	}
}

func TestRateLimiterAllowed(t *testing.T) {
	handler := RateLimiter(10, 5, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i, res.Code, http.StatusOK)
		}
	}
}

func TestRateLimiterBlocked(t *testing.T) {
	handler := RateLimiter(0.001, 2, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:5678"

	for i := 0; i < 3; i++ {
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", res.Code)
	}
}

func TestRateLimiterSkipsCursorAndEvents(t *testing.T) {
	// Burst=1 so a second limited request would 429; skipped paths must still pass.
	handler := RateLimiter(0.001, 1, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust tokens with a normal path.
	limited := httptest.NewRequest(http.MethodGet, "/api/clipboard/x", nil)
	limited.RemoteAddr = "10.0.0.9:1"
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, limited)
	if res.Code != http.StatusOK {
		t.Fatalf("seed status = %d", res.Code)
	}
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, limited)
	if res.Code != http.StatusTooManyRequests {
		t.Fatalf("expected seed path to be limited, got %d", res.Code)
	}

	for _, path := range []string{
		"/api/clipboard/x/ws",
		"/api/clipboard/x/cursor",
		"/api/clipboard/x/events",
		"/healthz",
		"/static/app.js",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "10.0.0.9:1"
		res = httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("path %s should skip rate limit, got %d", path, res.Code)
		}
	}
}

func TestClientIP(t *testing.T) {
	trustLocal, err := NewIPResolver("127.0.0.1/32,::1/128")
	if err != nil {
		t.Fatal(err)
	}
	trustAll, err := NewIPResolver("0.0.0.0/0,::/0")
	if err != nil {
		t.Fatal(err)
	}
	trustNone, err := NewIPResolver("")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		resolver *IPResolver
		remote   string
		xff      string
		xri      string
		want     string
	}{
		{"no headers", trustNone, "192.168.1.1:1234", "", "", "192.168.1.1"},
		{"xff ignored when peer untrusted", trustNone, "10.0.0.1:9999", "203.0.113.1", "", "10.0.0.1"},
		{"xri ignored when peer untrusted", trustNone, "10.0.0.1:9999", "", "198.51.100.1", "10.0.0.1"},
		{"nil resolver never trusts headers", nil, "10.0.0.1:9999", "203.0.113.1", "", "10.0.0.1"},
		{"xff honored from trusted proxy", trustLocal, "127.0.0.1:9999", "203.0.113.1", "", "203.0.113.1"},
		{"xff first entry wins", trustAll, "10.0.0.1:9999", "203.0.113.1, 198.51.100.1", "", "203.0.113.1"},
		{"xri fallback", trustAll, "10.0.0.1:9999", "", "198.51.100.1", "198.51.100.1"},
		{"garbage xff falls back to remote", trustAll, "10.0.0.1:9999", "not-an-ip", "", "10.0.0.1"},
		{"ipv6 loopback remote", trustLocal, "[::1]:8080", "203.0.113.1", "", "203.0.113.1"},
		{"ipv6 remote without headers", trustNone, "[::1]:8080", "", "", "::1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remote
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.xri != "" {
				req.Header.Set("X-Real-IP", tc.xri)
			}
			if got := tc.resolver.ClientIP(req); got != tc.want {
				t.Fatalf("ClientIP = %q, want %q (remote=%q xff=%q xri=%q)", got, tc.want, tc.remote, tc.xff, tc.xri)
			}
		})
	}
}

func TestNewIPResolverInvalidCIDR(t *testing.T) {
	if _, err := NewIPResolver("10.0.0.0/8,bogus"); err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
	if _, err := NewIPResolver("  "); err != nil {
		t.Fatalf("blank input should parse cleanly, got %v", err)
	}
}

func TestSpoofedXFFCannotBypassRateLimit(t *testing.T) {
	// A client that is NOT a trusted proxy must not be able to rotate
	// X-Forwarded-For to dodge the limiter.
	handler := RateLimiter(0.001, 1, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/clipboard/x", nil)
		req.RemoteAddr = "198.51.100.7:1234"
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.0.%d.%d", i, i))
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/clipboard/x", nil)
	req.RemoteAddr = "198.51.100.7:1234"
	req.Header.Set("X-Forwarded-For", "10.99.99.99")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 despite rotated XFF, got %d", res.Code)
	}
}
