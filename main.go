package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// ── Structured logging (JSON) ────────────────────────────
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// ── Connect to PostgreSQL ────────────────────────────────
	dbURL := getEnv("DATABASE_URL", "postgres://taxi:taxi@postgres:5432/taxi_tracker?sslmode=disable")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store, err := NewStore(ctx, dbURL)
	if err != nil {
		slog.Error("failed to connect to PostgreSQL", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	slog.Info("✅ connected to PostgreSQL")

	// ── Connect to Redis ─────────────────────────────────────
	redisAddr := getEnv("REDIS_ADDR", "redis:6379")
	cache, err := NewCache(redisAddr, 30*time.Second) // 30s TTL for cached locations
	if err != nil {
		slog.Error("failed to connect to Redis", "error", err)
		os.Exit(1)
	}
	defer cache.Close()
	slog.Info("✅ connected to Redis")

	// ── Route registration (Go 1.22+ method routing) ─────────
	h := NewHandler(store, cache)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /location", h.UpdateLocation)
	mux.HandleFunc("GET /location/{driver_id}", h.GetLocation)
	mux.HandleFunc("GET /drivers", h.ListDrivers)
	mux.HandleFunc("GET /healthz", h.Healthz)
	mux.HandleFunc("GET /readyz", h.Readyz)

	// ── Server with timeouts ─────────────────────────────────
	srv := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// ── Start server in a goroutine ──────────────────────────
	go func() {
		slog.Info("🚕 Tiny Taxi Tracker starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed to start", "error", err)
			os.Exit(1)
		}
	}()

	// ── Graceful shutdown ────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	slog.Info("🛑 shutdown signal received", "signal", sig.String())

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("forced shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("✅ server stopped cleanly")
}

// getEnv reads an environment variable with a fallback default.
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
