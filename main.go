package main

import (
	"context"
	"embed"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"goclipboard/internal/handler"
	"goclipboard/internal/middleware"
	"goclipboard/internal/store"
)

//go:embed static/*
var staticFiles embed.FS

func main() {
	// Container healthcheck mode: the Docker image is scratch-based (no
	// curl/wget), so the healthcheck invokes the binary itself to probe the
	// running server's /healthz endpoint.
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		os.Exit(runHealthCheck())
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	maxRooms := envInt("MAX_ROOMS", store.DefaultMaxRooms)
	maxMemoryMB := envInt("MAX_MEMORY_MB", int(store.DefaultMaxTotalBytes>>20))
	if maxRooms < 1 {
		maxRooms = store.DefaultMaxRooms
	}
	if maxMemoryMB < 1 {
		maxMemoryMB = int(store.DefaultMaxTotalBytes >> 20)
	}
	maxTotalBytes := int64(maxMemoryMB) << 20

	maxWSConns := envInt("MAX_WS_CONNS", handler.DefaultMaxWSConns)
	maxWSConnsPerIP := envInt("MAX_WS_CONNS_PER_IP", handler.DefaultMaxWSConnsPerIP)
	if maxWSConns < 1 {
		maxWSConns = handler.DefaultMaxWSConns
	}
	if maxWSConnsPerIP < 1 {
		maxWSConnsPerIP = handler.DefaultMaxWSConnsPerIP
	}
	wsMsgRate := envFloat("WS_MSG_RATE", handler.DefaultWSMsgRate)
	wsMsgBurst := envInt("WS_MSG_BURST", handler.DefaultWSMsgBurst)
	if wsMsgRate <= 0 {
		wsMsgRate = handler.DefaultWSMsgRate
	}
	if wsMsgBurst < 1 {
		wsMsgBurst = handler.DefaultWSMsgBurst
	}

	// Trusted proxy CIDRs for forwarded client IPs. Empty (default) trusts no
	// proxies, so spoofed X-Forwarded-For headers cannot bypass rate limits
	// or ban arbitrary IPs.
	ipResolver, err := middleware.NewIPResolver(os.Getenv("TRUSTED_PROXIES"))
	if err != nil {
		logger.Error("invalid TRUSTED_PROXIES", "error", err)
		os.Exit(1)
	}

	uploadPassword := os.Getenv("UPLOAD_PASSWORD")
	fileDir := envOrDefault("FILE_DIR", store.DefaultFileDir)
	if err := os.MkdirAll(fileDir, 0o755); err != nil {
		logger.Error("create file dir", "dir", fileDir, "error", err)
		os.Exit(1)
	}
	// Multipart uploads spill to TMPDIR; ensure a writable path (e.g. scratch containers).
	if tmpDir := os.Getenv("TMPDIR"); tmpDir == "" {
		tmpDir = filepath.Join(filepath.Dir(fileDir), "tmp")
		if err := os.MkdirAll(tmpDir, 0o755); err == nil {
			_ = os.Setenv("TMPDIR", tmpDir)
		}
	} else if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		// TMPDIR was explicitly configured but is unusable — fail fast rather
		// than silently breaking multipart uploads at runtime.
		logger.Error("TMPDIR not usable", "dir", tmpDir, "error", err)
		os.Exit(1)
	}

	sto := store.New(store.WithLimits(maxRooms, maxTotalBytes))
	fileSto := store.NewFileStore(store.WithFileRoot(fileDir))
	h := handler.New(sto, staticFiles, logger, handler.Options{
		Files:           fileSto,
		UploadPassword:  uploadPassword,
		IPResolver:      ipResolver,
		MaxWSConns:      maxWSConns,
		MaxWSConnsPerIP: maxWSConnsPerIP,
		WSMsgRate:       wsMsgRate,
		WSMsgBurst:      wsMsgBurst,
	})
	sto.SetOnExpire(h.PingExpired)
	fileSto.SetOnExpire(h.PingFilesExpired)

	addr := ":" + envOrDefault("PORT", "8080")

	var httpHandler http.Handler = h.Routes()
	httpHandler = middleware.SecurityHeaders(httpHandler)
	httpHandler = middleware.RateLimiter(10, 20, ipResolver)(httpHandler)
	httpHandler = middleware.Blocklist(logger, middleware.DefaultBlocklistConfig(), ipResolver)(httpHandler)
	httpHandler = middleware.RequestLogger(logger)(httpHandler)

	srv := &http.Server{
		Addr:              addr,
		Handler:           httpHandler,
		ReadHeaderTimeout: 10 * time.Second, // slowloris guard
		IdleTimeout:       60 * time.Second, // safe: hijacked WS conns bypass this
	}

	stopCleanup := make(chan struct{})
	defer close(stopCleanup)
	go sto.StartCleanup(stopCleanup, time.Minute)
	go fileSto.StartCleanup(stopCleanup, time.Minute)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info("server starting",
			"addr", addr,
			"max_rooms", maxRooms,
			"max_memory_mb", maxMemoryMB,
			"file_dir", fileSto.Root(),
			"admin_password_set", uploadPassword != "",
			"trusted_proxies", strings.TrimSpace(os.Getenv("TRUSTED_PROXIES")),
			"ws_max_conns", maxWSConns,
			"ws_max_conns_per_ip", maxWSConnsPerIP,
			"ws_msg_rate", wsMsgRate,
			"ws_msg_burst", wsMsgBurst,
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-quit
	logger.Info("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", "error", err)
	}
	logger.Info("server stopped")
}

// runHealthCheck probes the local health endpoint and returns a process exit
// code suitable for a container healthcheck.
func runHealthCheck() int {
	port := envOrDefault("PORT", "8080")
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func envOrDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

func envFloat(key string, fallback float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return n
}
