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

	"github.com/google/uuid"
)

// ─────────────────────────────────────────────────────────
// User Service — Entry Point
//
// Serves USER CRUD operations over HTTP.
// Other services (order-service) call this via internal HTTP.
// In production (Phase 2), this would also expose a gRPC server
// on a separate port for faster inter-service communication.
//
// Interview talking points:
//   - Service discovery: In K8s, other pods reach this via
//     http://user-service:8081 (ClusterIP DNS)
//   - Health probes: /healthz (liveness) and /readyz (readiness)
//   - Graceful shutdown: drains in-flight requests before exit
// ─────────────────────────────────────────────────────────

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// ── Connect to PostgreSQL ────────────────────────────
	dbURL := getEnv("DATABASE_URL", "postgres://taxi:taxi@postgres:5432/taxi_tracker?sslmode=disable")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store, err := NewStore(ctx, dbURL)
	if err != nil {
		slog.Error("failed to connect to PostgreSQL", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	slog.Info("✅ user-service connected to PostgreSQL")

	// ── Connect to Redis ─────────────────────────────────
	redisAddr := getEnv("REDIS_ADDR", "redis:6379")
	cache, err := NewCache(redisAddr, 60*time.Second)
	if err != nil {
		slog.Error("failed to connect to Redis", "error", err)
		os.Exit(1)
	}
	defer cache.Close()
	slog.Info("✅ user-service connected to Redis")

	// ── HTTP Routes ──────────────────────────────────────
	mux := http.NewServeMux()

	// User CRUD endpoints (called by gateway & other services)
	mux.HandleFunc("POST /users", handleCreateUser(store, cache))
	mux.HandleFunc("GET /users/{user_id}", handleGetUser(store, cache))

	// Kubernetes probes
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		probeCtx, probeCancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer probeCancel()
		if err := store.Ping(probeCtx); err != nil {
			http.Error(w, "db not ready", http.StatusServiceUnavailable)
			return
		}
		if err := cache.Ping(probeCtx); err != nil {
			http.Error(w, "cache not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	})

	// ── Start HTTP server ────────────────────────────────
	port := getEnv("HTTP_PORT", ":8081")
	srv := &http.Server{
		Addr:         port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		slog.Info("👤 user-service starting", "addr", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	// ── Graceful shutdown ────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	slog.Info("🛑 user-service shutdown", "signal", sig.String())

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx)
	slog.Info("✅ user-service stopped cleanly")
}

// ── HTTP Handlers ────────────────────────────────────────

// handleCreateUser creates a new user.
// POST /users  { "name": "...", "email": "..." }
func handleCreateUser(store *Store, cache *Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		var req struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			slog.Warn("invalid request body", "error", err)
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		if req.Name == "" || req.Email == "" {
			http.Error(w, `{"error":"name and email are required"}`, http.StatusBadRequest)
			return
		}

		userID := "user-" + uuid.New().String()[:8]

		user, err := store.CreateUser(r.Context(), userID, req.Name, req.Email)
		if err != nil {
			slog.Error("failed to create user", "error", err)
			http.Error(w, `{"error":"failed to create user"}`, http.StatusInternalServerError)
			return
		}

		// Write-through: populate cache
		if cacheErr := cache.Set(r.Context(), user); cacheErr != nil {
			slog.Warn("cache write failed (non-fatal)", "error", cacheErr)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(user)

		slog.Info("user created",
			"user_id", user.UserID,
			"latency_ms", time.Since(start).Milliseconds(),
		)
	}
}

// handleGetUser retrieves a user with cache-aside pattern.
// GET /users/{user_id}
func handleGetUser(store *Store, cache *Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		userID := r.PathValue("user_id")

		if userID == "" {
			http.Error(w, `{"error":"user_id is required"}`, http.StatusBadRequest)
			return
		}

		// Step 1: Check cache
		cached, err := cache.Get(r.Context(), userID)
		if err != nil {
			slog.Warn("cache read failed, falling back to DB", "error", err)
		}

		cacheHit := cached != nil

		if cached != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(cached)
			slog.Info("user retrieved",
				"user_id", userID, "cache_hit", true,
				"latency_ms", time.Since(start).Milliseconds(),
			)
			return
		}

		// Step 2: Cache miss → query DB
		user, err := store.GetUser(r.Context(), userID)
		if err != nil {
			slog.Error("user not found", "error", err, "user_id", userID)
			http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
			return
		}

		// Step 3: Populate cache
		if cacheErr := cache.Set(r.Context(), user); cacheErr != nil {
			slog.Warn("failed to populate cache", "error", cacheErr)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(user)

		slog.Info("user retrieved",
			"user_id", userID, "cache_hit", cacheHit,
			"latency_ms", time.Since(start).Milliseconds(),
		)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
