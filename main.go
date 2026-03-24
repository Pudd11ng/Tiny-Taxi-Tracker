package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// LocationResponse represents the JSON payload returned by the /location endpoint.
type LocationResponse struct {
	Driver string  `json:"driver"`
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
}

func locationHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	resp := LocationResponse{
		Driver: "Ali",
		Lat:    1.29,
		Lng:    103.85,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode response",
			"error", err,
			"path", r.URL.Path,
		)
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}

	slog.Info("request handled",
		"method", r.Method,
		"path", r.URL.Path,
		"driver", resp.Driver,
		"latency_ms", time.Since(start).Milliseconds(),
	)
}

// healthzHandler responds to liveness probes.
// Question answered: "Is the process alive and not deadlocked?"
// Failure action:    Orchestrator kills and restarts the container.
func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// readyzHandler responds to readiness probes.
// Question answered: "Is the process ready to accept traffic?"
// Failure action:    Load balancer stops sending traffic to this instance.
// In the future, this should check DB connection, Redis connection, etc.
func readyzHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: check downstream dependencies (DB, cache, message queue)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ready"))
}

func main() {
	// ── Structured logging (JSON) ────────────────────────────
	// Production logs must be structured for querying in ELK/Loki/CloudWatch.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// ── Route registration ───────────────────────────────────
	mux := http.NewServeMux()
	mux.HandleFunc("/location", locationHandler)
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/readyz", readyzHandler)

	// ── Server with timeouts ─────────────────────────────────
	// Without timeouts, a slow client can hold a connection open forever,
	// exhausting server resources (Slowloris attack vector).
	srv := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  5 * time.Second,  // Max time to read the entire request
		WriteTimeout: 10 * time.Second, // Max time to write the response
		IdleTimeout:  120 * time.Second, // Max time for keep-alive connections
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
	// Wait for SIGINT (Ctrl+C) or SIGTERM (docker stop / k8s pod termination).
	//
	// Shutdown sequence:
	//   1. Stop accepting NEW connections
	//   2. Wait for IN-FLIGHT requests to complete (up to 30s)
	//   3. Close idle connections immediately
	//   4. Return — allowing deferred cleanup to run
	//
	// Why 30 seconds? Kubernetes default terminationGracePeriodSeconds is 30.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	slog.Info("🛑 shutdown signal received", "signal", sig.String())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("forced shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("✅ server stopped cleanly")
}
