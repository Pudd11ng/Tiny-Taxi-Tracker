package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
)

// ─────────────────────────────────────────────────────────
// Order Service — Entry Point
//
// Manages orders. Calls user-service via HTTP to validate
// user existence before creating an order.
//
// Interview talking points:
//   - Service-to-service communication: order → user via internal HTTP
//     In K8s, resolved via DNS: http://user-service:8081
//   - In production, this would use gRPC for lower latency (~10x faster
//     than JSON over HTTP due to protobuf binary encoding).
//   - Eventual consistency: if user-service is temporarily down, the
//     order creation fails — this is synchronous coupling (by design).
//     For looser coupling, use async messaging (Kafka, Phase 2).
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
	slog.Info("✅ order-service connected to PostgreSQL")

	// ── User service client (inter-service communication) ─
	userServiceURL := getEnv("USER_SERVICE_URL", "http://user-service:8081")
	userClient := NewUserClient(userServiceURL)

	// ── HTTP Routes ──────────────────────────────────────
	mux := http.NewServeMux()

	mux.HandleFunc("POST /orders", handleCreateOrder(store, userClient))
	mux.HandleFunc("GET /orders/{order_id}", handleGetOrder(store))
	mux.HandleFunc("GET /orders/user/{user_id}", handleListUserOrders(store))

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
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	})

	// ── Start HTTP server ────────────────────────────────
	port := getEnv("HTTP_PORT", ":8082")
	srv := &http.Server{
		Addr:         port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		slog.Info("📦 order-service starting", "addr", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	// ── Graceful shutdown ────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	slog.Info("🛑 order-service shutdown", "signal", sig.String())

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx)
	slog.Info("✅ order-service stopped cleanly")
}

// ── HTTP Handlers ────────────────────────────────────────

// handleCreateOrder creates a new order after validating user exists.
//
// Flow:
//   1. Parse request
//   2. Call user-service to validate user exists (inter-service call)
//   3. Insert order into DB
//   4. Return created order
//
// Interview talking points:
//   - This is synchronous coupling: if user-service is down, order fails
//   - Alternative: async validation via Kafka (Phase 2)
//   - Circuit breaker: in production, wrap the user-service call with
//     a circuit breaker to fail fast instead of hanging
func handleCreateOrder(store *Store, userClient *UserClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		var req struct {
			UserID   string  `json:"user_id"`
			Item     string  `json:"item"`
			Quantity int     `json:"quantity"`
			Price    float64 `json:"price"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		if req.UserID == "" || req.Item == "" {
			http.Error(w, `{"error":"user_id and item are required"}`, http.StatusBadRequest)
			return
		}
		if req.Quantity <= 0 {
			req.Quantity = 1
		}

		// ── Inter-service call: validate user exists ─────
		// This is the KEY distributed system pattern:
		// order-service calls user-service to verify the user.
		user, err := userClient.GetUser(r.Context(), req.UserID)
		if err != nil {
			slog.Error("user validation failed", "error", err, "user_id", req.UserID)
			http.Error(w, fmt.Sprintf(`{"error":"user not found: %s"}`, req.UserID), http.StatusBadRequest)
			return
		}

		slog.Info("user validated via user-service",
			"user_id", user.UserID,
			"user_name", user.Name,
		)

		// ── Create order ─────────────────────────────────
		orderID := "order-" + uuid.New().String()[:8]

		order, err := store.CreateOrder(r.Context(), orderID, req.UserID, req.Item, req.Quantity, req.Price)
		if err != nil {
			slog.Error("failed to create order", "error", err)
			http.Error(w, `{"error":"failed to create order"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(order)

		slog.Info("order created",
			"order_id", order.OrderID,
			"user_id", req.UserID,
			"item", req.Item,
			"latency_ms", time.Since(start).Milliseconds(),
		)
	}
}

// handleGetOrder retrieves a single order by ID.
func handleGetOrder(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderID := r.PathValue("order_id")
		if orderID == "" {
			http.Error(w, `{"error":"order_id is required"}`, http.StatusBadRequest)
			return
		}

		order, err := store.GetOrder(r.Context(), orderID)
		if err != nil {
			http.Error(w, `{"error":"order not found"}`, http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(order)
	}
}

// handleListUserOrders returns all orders for a given user.
func handleListUserOrders(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.PathValue("user_id")
		if userID == "" {
			http.Error(w, `{"error":"user_id is required"}`, http.StatusBadRequest)
			return
		}

		orders, err := store.ListUserOrders(r.Context(), userID)
		if err != nil {
			http.Error(w, `{"error":"failed to list orders"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(orders)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
