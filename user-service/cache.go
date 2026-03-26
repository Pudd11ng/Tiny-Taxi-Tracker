package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache wraps a Redis client for user data caching.
//
// Interview talking points:
//   - Cache-Aside pattern: app checks cache first, falls back to DB on miss
//   - TTL: cached users expire after 60s (user data changes infrequently)
//   - Namespace keys: "user:<id>" to avoid collision
type Cache struct {
	client *redis.Client
	ttl    time.Duration
}

// NewCache creates a Redis client and verifies connectivity.
func NewCache(addr string, ttl time.Duration) (*Cache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		PoolSize: 10,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return &Cache{client: client, ttl: ttl}, nil
}

func (c *Cache) cacheKey(userID string) string {
	return fmt.Sprintf("user:%s", userID)
}

// Get retrieves a user from cache. Returns nil on miss.
func (c *Cache) Get(ctx context.Context, userID string) (*User, error) {
	data, err := c.client.Get(ctx, c.cacheKey(userID)).Bytes()
	if err == redis.Nil {
		return nil, nil // cache miss
	}
	if err != nil {
		return nil, fmt.Errorf("cache get: %w", err)
	}

	var user User
	if err := json.Unmarshal(data, &user); err != nil {
		return nil, fmt.Errorf("cache unmarshal: %w", err)
	}
	return &user, nil
}

// Set stores a user in cache with TTL.
func (c *Cache) Set(ctx context.Context, user *User) error {
	data, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("cache marshal: %w", err)
	}
	return c.client.Set(ctx, c.cacheKey(user.UserID), data, c.ttl).Err()
}

// Delete removes a user from cache.
func (c *Cache) Delete(ctx context.Context, userID string) error {
	return c.client.Del(ctx, c.cacheKey(userID)).Err()
}

// Ping checks if Redis is reachable.
func (c *Cache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// Close releases the Redis connection.
func (c *Cache) Close() error {
	return c.client.Close()
}
