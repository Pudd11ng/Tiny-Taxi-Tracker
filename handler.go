package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Handler holds references to the store and cache — dependency injection.
//
// Interview talking point:
//   By injecting dependencies, we can swap PostgreSQL for an in-memory store
//   in tests, or swap Redis for a no-op cache. This is how Grab's services
//   are structured — interfaces + injection for testability.
type Handler struct {
	store *Store
	cache *Cache
}

// NewHandler creates a handler with its dependencies.
func NewHandler(store *Store, cache *Cache) *Handler {
	return &Handler{store: store, cache: cache}
}

// ── POST /location ──────────────────────────────────────────
// Updates a driver's GPS location.
//
// Flow: Validate → Write DB → Update Cache (write-through)
//
// Write-through means we update the cache immediately after writing to DB.
// Alternative: write-behind (async cache update) — higher throughput but
// risks serving stale data if the async update fails.
func (h *Handler) UpdateLocation(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	var update LocationUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		slog.Warn("invalid request body", "error", err)
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if update.DriverID == "" {
		http.Error(w, `{"error":"driver_id is required"}`, http.StatusBadRequest)
		return
	}

	// Write to PostgreSQL (source of truth)
	loc, err := h.store.UpsertLocation(r.Context(), update)
	if err != nil {
		slog.Error("failed to upsert location", "error", err, "driver_id", update.DriverID)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	// Update cache (write-through) — non-fatal if it fails
	if err := h.cache.Set(r.Context(), loc); err != nil {
		slog.Warn("cache write failed (non-fatal)", "error", err, "driver_id", update.DriverID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(loc)

	slog.Info("location updated",
		"driver_id", update.DriverID,
		"lat", update.Lat,
		"lng", update.Lng,
		"latency_ms", time.Since(start).Milliseconds(),
	)
}

// ── GET /location/{driver_id} ───────────────────────────────
// Retrieves a single driver's location using the cache-aside pattern.
//
// Cache-Aside Flow:
//   1. Check Redis cache
//   2. Cache HIT  → return cached data (fast path, ~1ms)
//   3. Cache MISS → query PostgreSQL (slow path, ~5-20ms)
//   4. Populate cache with DB result for next read
//
// Why cache-aside (vs read-through)?
//   - Application controls cache logic explicitly
//   - Can handle cache failures gracefully (fall back to DB)
//   - Most common pattern at Grab for read-heavy workloads
func (h *Handler) GetLocation(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	driverID := r.PathValue("driver_id") // Go 1.22+ path parameter

	if driverID == "" {
		http.Error(w, `{"error":"driver_id is required"}`, http.StatusBadRequest)
		return
	}

	// Step 1: Check cache
	loc, err := h.cache.Get(r.Context(), driverID)
	if err != nil {
		slog.Warn("cache read failed, falling back to DB", "error", err, "driver_id", driverID)
	}

	cacheHit := loc != nil

	// Step 2: Cache miss → query database
	if loc == nil {
		loc, err = h.store.GetLocation(r.Context(), driverID)
		if err != nil {
			slog.Error("driver not found", "error", err, "driver_id", driverID)
			http.Error(w, `{"error":"driver not found"}`, http.StatusNotFound)
			return
		}

		// Step 3: Populate cache for subsequent reads
		if cacheErr := h.cache.Set(r.Context(), loc); cacheErr != nil {
			slog.Warn("failed to populate cache", "error", cacheErr, "driver_id", driverID)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(loc)

	slog.Info("location retrieved",
		"driver_id", driverID,
		"cache_hit", cacheHit,
		"latency_ms", time.Since(start).Milliseconds(),
	)
}

// ── GET /drivers ────────────────────────────────────────────
// Lists all drivers with their latest locations.
func (h *Handler) ListDrivers(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	locations, err := h.store.ListLocations(r.Context())
	if err != nil {
		slog.Error("failed to list drivers", "error", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(locations)

	slog.Info("drivers listed",
		"count", len(locations),
		"latency_ms", time.Since(start).Milliseconds(),
	)
}

// ── GET /healthz ────────────────────────────────────────────
// Liveness probe: "Is the process alive?"
func (h *Handler) Healthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// ── GET /readyz ─────────────────────────────────────────────
// Readiness probe: "Can the process serve traffic?"
// Now checks BOTH PostgreSQL and Redis connectivity.
func (h *Handler) Readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.store.Ping(ctx); err != nil {
		slog.Warn("readiness check failed: db", "error", err)
		http.Error(w, "db not ready", http.StatusServiceUnavailable)
		return
	}

	if err := h.cache.Ping(ctx); err != nil {
		slog.Warn("readiness check failed: cache", "error", err)
		http.Error(w, "cache not ready", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ready"))
}
