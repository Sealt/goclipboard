package main

import (
	"context"
	"embed"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"goclipboard/internal/handler"
	"goclipboard/internal/middleware"
	"goclipboard/internal/store"
)

//go:embed static/*
var staticFiles embed.FS

func main() {
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

	uploadPassword := os.Getenv("UPLOAD_PASSWORD")
	fileDir := envOrDefault("FILE_DIR", store.DefaultFileDir)
	if err := os.MkdirAll(fileDir, 0o755); err != nil {
		logger.Error("create file dir", "dir", fileDir, "error", err)
		os.Exit(1)
	}
	// Multipart uploads spill to TMPDIR; ensure a writable path (e.g. scratch containers).
	if os.Getenv("TMPDIR") == "" {
		tmpDir := filepath.Join(filepath.Dir(fileDir), "tmp")
		if err := os.MkdirAll(tmpDir, 0o755); err == nil {
			_ = os.Setenv("TMPDIR", tmpDir)
		}
	}

	sto := store.New(store.WithLimits(maxRooms, maxTotalBytes))
	fileSto := store.NewFileStore(store.WithFileRoot(fileDir))
	h := handler.New(sto, staticFiles, logger, handler.Options{
		Files:          fileSto,
		UploadPassword: uploadPassword,
	})
	sto.SetOnExpire(h.PingExpired)
	fileSto.SetOnExpire(h.PingFilesExpired)

	addr := ":" + envOrDefault("PORT", "8080")

	var httpHandler http.Handler = h.Routes()
	httpHandler = middleware.SecurityHeaders(httpHandler)
	httpHandler = middleware.RateLimiter(10, 20)(httpHandler)
	httpHandler = middleware.Blocklist(logger, middleware.DefaultBlocklistConfig())(httpHandler)
	httpHandler = middleware.RequestLogger(logger)(httpHandler)

	srv := &http.Server{
		Addr:    addr,
		Handler: httpHandler,
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
