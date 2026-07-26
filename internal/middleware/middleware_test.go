package middleware

import (
	"net/http"
	"net/http/httptest"
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

func TestRateLimiterAllowed(t *testing.T) {
	handler := RateLimiter(10, 5)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := RateLimiter(0.001, 2)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := RateLimiter(0.001, 1)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	cases := []struct {
		remoteAddr string
		xff        string
		xri        string
		want       string
	}{
		{"192.168.1.1:1234", "", "", "192.168.1.1"},
		{"10.0.0.1:9999", "203.0.113.1", "", "203.0.113.1"},
		{"10.0.0.1:9999", "203.0.113.1, 198.51.100.1", "", "203.0.113.1"},
		{"10.0.0.1:9999", "", "198.51.100.1", "198.51.100.1"},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = tc.remoteAddr
		if tc.xff != "" {
			req.Header.Set("X-Forwarded-For", tc.xff)
		}
		if tc.xri != "" {
			req.Header.Set("X-Real-IP", tc.xri)
		}
		got := clientIP(req)
		if got != tc.want {
			t.Fatalf("clientIP = %q, want %q (remote=%q xff=%q xri=%q)", got, tc.want, tc.remoteAddr, tc.xff, tc.xri)
		}
	}
}
