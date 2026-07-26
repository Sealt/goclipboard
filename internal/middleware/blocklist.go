package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

type BlocklistConfig struct {
	HardThreshold int
	ScanThreshold int
	Window        time.Duration
	BanSeconds    int
}

func DefaultBlocklistConfig() BlocklistConfig {
	return BlocklistConfig{
		HardThreshold: 5,
		ScanThreshold: 10,
		Window:        30 * time.Second,
		BanSeconds:    1800,
	}
}

type blocklist struct {
	mu    sync.Mutex
	bans  map[string]time.Time
	state map[string]*ipState
	cfg   BlocklistConfig
	log   *slog.Logger
}

type ipState struct {
	hardCount  int
	scanCount  int
	scanPaths  map[string]struct{}
	windowTime time.Time
}

func Blocklist(logger *slog.Logger, cfg BlocklistConfig) func(http.Handler) http.Handler {
	bl := &blocklist{
		bans:  make(map[string]time.Time),
		state: make(map[string]*ipState),
		cfg:   cfg,
		log:   logger,
	}
	go bl.runCleanup()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			now := time.Now()

			if bl.isBanned(ip, now) {
				bl.log.Warn("blocked banned IP",
					"ip", ip,
					"method", r.Method,
					"path", r.URL.Path,
				)
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"error":"access denied"}`))
				return
			}

			wr := &responseWriter{ResponseWriter: w}
			next.ServeHTTP(wr, r)

			bl.record(ip, now, r.URL.Path, wr.status)
		})
	}
}

func (bl *blocklist) isBanned(ip string, now time.Time) bool {
	bl.mu.Lock()
	defer bl.mu.Unlock()

	expiry, ok := bl.bans[ip]
	if !ok {
		return false
	}
	if now.After(expiry) {
		delete(bl.bans, ip)
		return false
	}
	return true
}

func (bl *blocklist) record(ip string, now time.Time, path string, status int) {
	switch classify(status, path) {
	case "hard":
		bl.recordHard(ip, now, path)
	case "scan":
		bl.recordScan(ip, now, path)
	}
}

func (bl *blocklist) recordHard(ip string, now time.Time, path string) {
	bl.mu.Lock()
	defer bl.mu.Unlock()

	st := bl.getOrResetState(ip, now)
	st.hardCount++

	if st.hardCount >= bl.cfg.HardThreshold {
		bl.ban(ip, now, "hard", st.hardCount, path)
	}
}

func (bl *blocklist) recordScan(ip string, now time.Time, path string) {
	bl.mu.Lock()
	defer bl.mu.Unlock()

	st := bl.getOrResetState(ip, now)
	st.scanCount++
	st.scanPaths[path] = struct{}{}

	if len(st.scanPaths) >= bl.cfg.ScanThreshold || st.scanCount >= bl.cfg.ScanThreshold*3 {
		reason := "scan"
		count := len(st.scanPaths)
		if st.scanCount >= bl.cfg.ScanThreshold*3 {
			reason = "scan_burst"
			count = st.scanCount
		}
		bl.ban(ip, now, reason, count, path)
	}
}

func (bl *blocklist) getOrResetState(ip string, now time.Time) *ipState {
	st, ok := bl.state[ip]
	if !ok || now.Sub(st.windowTime) > bl.cfg.Window {
		st = &ipState{
			scanPaths:  make(map[string]struct{}),
			windowTime: now,
		}
		bl.state[ip] = st
	}
	return st
}

func (bl *blocklist) ban(ip string, now time.Time, reason string, count int, lastPath string) {
	bl.bans[ip] = now.Add(time.Duration(bl.cfg.BanSeconds) * time.Second)
	delete(bl.state, ip)

	bl.log.Warn("IP banned",
		"reason", reason,
		"ip", ip,
		"count", count,
		"threshold", map[string]int{"hard": bl.cfg.HardThreshold, "scan": bl.cfg.ScanThreshold}[reason],
		"window", bl.cfg.Window.String(),
		"ban_seconds", bl.cfg.BanSeconds,
		"last_path", lastPath,
	)
}

func (bl *blocklist) runCleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		bl.mu.Lock()
		now := time.Now()

		for ip, expiry := range bl.bans {
			if now.After(expiry) {
				delete(bl.bans, ip)
				bl.log.Info("IP ban expired", "ip", ip)
			}
		}

		for ip, st := range bl.state {
			if now.Sub(st.windowTime) > 2*bl.cfg.Window {
				delete(bl.state, ip)
			}
		}

		bl.mu.Unlock()
	}
}

func classify(status int, path string) string {
	if status == 400 || status == 405 || status == 413 || status == 414 {
		return "hard"
	}
	if status == 404 && isSuspiciousPath(path) {
		return "scan"
	}
	return "none"
}

func isSuspiciousPath(path string) bool {
	if path == "/" {
		return false
	}

	key := strings.TrimPrefix(path, "/")
	if key == "" {
		return true
	}
	if strings.Contains(key, "/") {
		return true
	}

	lower := strings.ToLower(key)
	if lower == "api" || lower == "static" || lower == "healthz" {
		return true
	}
	if len(key) > 64 {
		return true
	}

	if !isAlnum(key[0]) {
		return true
	}
	for i := 1; i < len(key); i++ {
		c := key[i]
		if !isAlnum(c) && c != '.' && c != '_' && c != '-' {
			return true
		}
	}
	return false
}

func isAlnum(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}
