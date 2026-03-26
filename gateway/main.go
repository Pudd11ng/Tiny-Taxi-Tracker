package main

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// ─────────────────────────────────────────────────────────
// API Gateway — Entry Point
//
// Single entry point for all external requests.
// Routes requests to the appropriate microservice.
//
// Responsibilities:
//   1. Request routing (/api/users → user-service, /api/orders → order-service)
//   2. Authentication (mock — checks for X-API-Key header)
//   3. Request logging (structured JSON logs)
//   4. Health probes for Kubernetes
//
// Interview talking points:
//   - API Gateway pattern: single entry point decouples clients from
//     internal service topology. Clients don't know about user-service
//     or order-service — they only talk to the gateway.
//   - httputil.ReverseProxy: Go's stdlib reverse proxy handles
//     connection pooling, hop-by-hop header stripping, and request
//     forwarding. No need for external libraries.
//   - In production: use Kong, Envoy, or NGINX as gateway for
//     rate limiting, circuit breaking, and observability.
// ─────────────────────────────────────────────────────────

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// ── Service URLs (resolved by K8s DNS) ───────────────
	userServiceURL := getEnv("USER_SERVICE_URL", "http://user-service:8081")
	orderServiceURL := getEnv("ORDER_SERVICE_URL", "http://order-service:8082")

	userProxy := newReverseProxy(userServiceURL)
	orderProxy := newReverseProxy(orderServiceURL)

	// ── Route registration ───────────────────────────────
	mux := http.NewServeMux()

	// Route /api/users/* → user-service
	mux.Handle("/api/users/", authMiddleware(
		loggingMiddleware(
			http.StripPrefix("/api", userProxy),
		),
	))
	mux.Handle("/api/users", authMiddleware(
		loggingMiddleware(
			http.StripPrefix("/api", userProxy),
		),
	))

	// Route /api/orders/* → order-service
	mux.Handle("/api/orders/", authMiddleware(
		loggingMiddleware(
			http.StripPrefix("/api", orderProxy),
		),
	))
	mux.Handle("/api/orders", authMiddleware(
		loggingMiddleware(
			http.StripPrefix("/api", orderProxy),
		),
	))

	// Gateway health probes (not proxied)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	})

	// Root — service info
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
  "service": "api-gateway",
  "version": "1.0.0",
  "routes": {
    "/api/users":  "user-service",
    "/api/orders": "order-service"
  }
}`))
	})

	// ── Start server ─────────────────────────────────────
	port := getEnv("HTTP_PORT", ":8080")
	srv := &http.Server{
		Addr:         port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		slog.Info("🚪 api-gateway starting", "addr", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	// ── Graceful shutdown ────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	slog.Info("🛑 api-gateway shutdown", "signal", sig.String())

	srv.Close()
	slog.Info("✅ api-gateway stopped cleanly")
}

// ── Reverse Proxy ────────────────────────────────────────

// newReverseProxy creates an httputil.ReverseProxy for a backend service.
//
// Interview talking points:
//   - ReverseProxy handles connection pooling to backend
//   - Director function rewrites the request URL to the backend
//   - ErrorHandler logs proxy failures (backend unreachable)
func newReverseProxy(target string) *httputil.ReverseProxy {
	targetURL, err := url.Parse(target)
	if err != nil {
		slog.Error("invalid proxy target", "url", target, "error", err)
		os.Exit(1)
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// Custom director: preserve the path from StripPrefix
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = targetURL.Host
	}

	// Log proxy errors
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		slog.Error("proxy error",
			"target", target,
			"path", r.URL.Path,
			"error", err,
		)
		http.Error(w, `{"error":"service unavailable"}`, http.StatusBadGateway)
	}

	return proxy
}

// ── Middleware ────────────────────────────────────────────

// authMiddleware checks for a valid API key in the request header.
// This is a MOCK implementation for learning purposes.
//
// Interview talking points:
//   - In production: use JWT tokens verified against an auth service
//   - API Gateway is the single point for auth — downstream services
//     trust that requests from the gateway are already authenticated
//   - mTLS: in a service mesh (Istio), inter-service auth uses
//     mutual TLS certificates instead of API keys
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("X-API-Key")

		// Mock auth: any non-empty key is "valid"
		// In production, validate against an auth service or JWT
		if apiKey == "" {
			// For learning/dev: allow requests without API key but log warning
			slog.Warn("request without API key (allowed in dev mode)",
				"path", r.URL.Path,
				"method", r.Method,
			)
		}

		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs every request with method, path, status, and latency.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap ResponseWriter to capture status code
		wrapped := &statusWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)

		slog.Info("gateway request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
			"latency_ms", time.Since(start).Milliseconds(),
			"remote", stripPort(r.RemoteAddr),
		)
	})
}

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// stripPort removes the port from an address string.
func stripPort(addr string) string {
	if i := strings.LastIndex(addr, ":"); i != -1 {
		return addr[:i]
	}
	return addr
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
