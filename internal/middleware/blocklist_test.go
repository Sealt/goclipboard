package middleware

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func cfg(hard, scan int) BlocklistConfig {
	return BlocklistConfig{
		HardThreshold: hard,
		ScanThreshold: scan,
		Window:        10 * time.Second,
		BanSeconds:    60,
	}
}

func TestBlocklistPassesNormalRequest(t *testing.T) {
	handler := Blocklist(testLogger(), cfg(5, 10))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/normal-key", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
}

func TestNormal404NotCounted(t *testing.T) {
	handler := Blocklist(testLogger(), cfg(5, 10))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	ip := "10.0.0.8:1111"
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodGet, "/room-123", nil)
		req.RemoteAddr = ip
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusNotFound {
			t.Fatalf("request %d: expected 404, got %d", i, res.Code)
		}
	}
}

func TestRepeatedSameScanPath(t *testing.T) {
	handler := Blocklist(testLogger(), cfg(5, 3))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	ip := "10.0.0.9:2222"
	burst := 3 * 3
	for i := 0; i < burst+2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/.env", nil)
		req.RemoteAddr = ip
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)

		if i < burst {
			if res.Code != http.StatusNotFound {
				t.Fatalf("request %d: expected 404, got %d", i, res.Code)
			}
		} else {
			if res.Code != http.StatusForbidden {
				t.Fatalf("request %d: expected 403 after burst, got %d", i, res.Code)
			}
		}
	}
}

func TestHardViolationBans(t *testing.T) {
	handler := Blocklist(testLogger(), cfg(3, 10))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))

	ip := "10.0.0.10:3333"
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/anything", nil)
		req.RemoteAddr = ip
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.RemoteAddr = ip
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("ban status = %d, want %d", res.Code, http.StatusForbidden)
	}
}

func TestScanDiversityBans(t *testing.T) {
	handler := Blocklist(testLogger(), cfg(10, 4))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	ip := "10.0.0.11:4444"
	paths := []string{"/.env", "/api", "/static", "/healthz"}

	for i, p := range paths {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		req.RemoteAddr = ip
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)

		if i < len(paths)-1 {
			if res.Code != http.StatusNotFound {
				t.Fatalf("path %s: unexpected status %d", p, res.Code)
			}
		} else {
			if res.Code != http.StatusNotFound {
				t.Fatalf("path %s before threshold: unexpected status %d", p, res.Code)
			}
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/.git", nil)
	req.RemoteAddr = ip
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("after 4 unique scan paths, status = %d, want %d; body: %s", res.Code, http.StatusForbidden, res.Body.String())
	}
}

func TestMixedAttacks(t *testing.T) {
	handler := Blocklist(testLogger(), cfg(3, 10))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))

	ip := "10.0.0.12:5555"

	mkReq := func(path string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = ip
		return req
	}

	handler.ServeHTTP(httptest.NewRecorder(), mkReq("/"))
	handler.ServeHTTP(httptest.NewRecorder(), mkReq("/"))
	handler.ServeHTTP(httptest.NewRecorder(), mkReq("/"))

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, mkReq("/"))

	if res.Code != http.StatusForbidden {
		t.Fatalf("mixed ban status = %d, want %d", res.Code, http.StatusForbidden)
	}
}

func TestWindowReset(t *testing.T) {
	cfg := BlocklistConfig{
		HardThreshold: 5,
		ScanThreshold: 5,
		Window:        50 * time.Millisecond,
		BanSeconds:    60,
	}

	handler := Blocklist(testLogger(), cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))

	ip := "10.0.0.13:6666"
	for i := 0; i < 4; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ip
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	time.Sleep(100 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ip
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("after window reset, status = %d, want %d", res.Code, http.StatusBadRequest)
	}
}

func TestBanExpires(t *testing.T) {
	cfg := BlocklistConfig{
		HardThreshold: 2,
		ScanThreshold: 10,
		Window:        10 * time.Second,
		BanSeconds:    1,
	}

	var mu sync.Mutex
	var banned bool
	handler := Blocklist(testLogger(), cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if banned {
			t.Error("handler called while banned")
		}
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
	}))

	ip := "10.0.0.14:7777"
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ip
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	mu.Lock()
	banned = true
	mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ip
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("banned status = %d, want %d", res.Code, http.StatusForbidden)
	}

	time.Sleep(1500 * time.Millisecond)

	mu.Lock()
	banned = false
	mu.Unlock()

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = ip
	res2 := httptest.NewRecorder()
	handler.ServeHTTP(res2, req2)
	if res2.Code != http.StatusBadRequest {
		t.Fatalf("after ban expiry, status = %d, want %d", res2.Code, http.StatusBadRequest)
	}
}

func TestSeparateIPs(t *testing.T) {
	handler := Blocklist(testLogger(), cfg(2, 5))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))

	badIP := "10.0.0.15:1111"
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = badIP
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = badIP
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatal("bad IP should be banned")
	}

	goodIP := "10.0.0.16:2222"
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = goodIP
	res2 := httptest.NewRecorder()
	handler.ServeHTTP(res2, req2)
	if res2.Code != http.StatusBadRequest {
		t.Fatal("different IP should not be affected")
	}
}

func TestClassifyHard(t *testing.T) {
	hardStatuses := []int{
		http.StatusBadRequest,
		http.StatusMethodNotAllowed,
		http.StatusRequestEntityTooLarge,
		http.StatusRequestURITooLong,
	}
	for _, s := range hardStatuses {
		if classify(s, "/any") != "hard" {
			t.Errorf("status %d should be hard violation", s)
		}
	}
}

func TestClassifyScan(t *testing.T) {
	scanPaths := []string{
		"/.env",
		"/api",
		"/static",
		"/healthz",
		"//etc/passwd",
		"/..%2f..%2fetc",
		"/multiple/segments",
		"/@special",
		"/very-long-path-exceeding-64-characters-1234567890123456789012345678901234567890",
	}
	for _, p := range scanPaths {
		if classify(http.StatusNotFound, p) != "scan" {
			t.Errorf("path %q with 404 should be scan violation", p)
		}
	}
}

func TestClassifyNormal404(t *testing.T) {
	normalPaths := []string{
		"/room-1",
		"/a1b2c3d4",
		"/my.clipboard",
		"/test_key",
		"/abc-123",
		"/x",
	}
	for _, p := range normalPaths {
		if classify(http.StatusNotFound, p) != "none" {
			t.Errorf("path %q with 404 should NOT be a violation", p)
		}
	}
}

func TestClassifySuccess(t *testing.T) {
	statuses := []int{http.StatusOK, http.StatusCreated, http.StatusNoContent, http.StatusFound, http.StatusNotModified}
	for _, s := range statuses {
		if classify(s, "/.env") != "none" {
			t.Errorf("status %d should not be a violation", s)
		}
	}
}

func TestClassify5xxNotViolation(t *testing.T) {
	statuses := []int{
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
	}
	for _, s := range statuses {
		if classify(s, "/any") != "none" {
			t.Errorf("status %d should NOT be a violation (server error, not attack)", s)
		}
	}
}
