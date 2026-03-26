package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache wraps a Redis client for driver location caching.
//
// Interview talking points:
//   - Cache-Aside pattern: the application manages the cache explicitly.
//     On READ:  check cache → miss → query DB → populate cache.
//     On WRITE: write DB → update cache (write-through).
//   - TTL (Time-To-Live): cached locations expire after 30 seconds.
//     Stale location data is acceptable — a driver's GPS position doesn't
//     change dramatically in 30 seconds, and the TTL bounds staleness.
//   - Redis is single-threaded: all operations are atomic. No need for
//     distributed locks for simple GET/SET.
type Cache struct {
	client *redis.Client
	ttl    time.Duration
}

// NewCache creates a Redis client and verifies connectivity.
func NewCache(addr string, ttl time.Duration) (*Cache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		PoolSize: 10, // Max connections in the pool
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return &Cache{client: client, ttl: ttl}, nil
}

// cacheKey generates a namespaced Redis key.
// Namespace prevents collision with other data stored in the same Redis instance.
func (c *Cache) cacheKey(driverID string) string {
	return fmt.Sprintf("driver_loc:%s", driverID)
}

// Get retrieves a driver's location from cache.
// Returns nil (not an error) on cache miss — the caller should fall back to DB.
func (c *Cache) Get(ctx context.Context, driverID string) (*LocationResponse, error) {
	data, err := c.client.Get(ctx, c.cacheKey(driverID)).Bytes()
	if err == redis.Nil {
		return nil, nil // cache miss — not an error
	}
	if err != nil {
		return nil, fmt.Errorf("cache get: %w", err)
	}

	var loc LocationResponse
	if err := json.Unmarshal(data, &loc); err != nil {
		return nil, fmt.Errorf("cache unmarshal: %w", err)
	}
	return &loc, nil
}

// Set stores a driver's location in cache with TTL.
func (c *Cache) Set(ctx context.Context, loc *LocationResponse) error {
	data, err := json.Marshal(loc)
	if err != nil {
		return fmt.Errorf("cache marshal: %w", err)
	}
	return c.client.Set(ctx, c.cacheKey(loc.DriverID), data, c.ttl).Err()
}

// Delete removes a driver's location from cache (cache invalidation).
func (c *Cache) Delete(ctx context.Context, driverID string) error {
	return c.client.Del(ctx, c.cacheKey(driverID)).Err()
}

// Ping checks if Redis is reachable (used by readiness probe).
func (c *Cache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// Close releases the Redis connection.
func (c *Cache) Close() error {
	return c.client.Close()
}
