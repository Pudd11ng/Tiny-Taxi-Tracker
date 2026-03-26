package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ─────────────────────────────────────────────────────────
// UserClient — Inter-Service HTTP Client
//
// order-service uses this to call user-service.
// In Kubernetes, uses DNS service discovery:
//   http://user-service:8081/users/{user_id}
//
// Interview talking points:
//   - Service discovery: K8s DNS resolves "user-service" to ClusterIP
//   - Timeouts: 5s timeout prevents hanging if user-service is slow
//   - In production: add circuit breaker (e.g., sony/gobreaker) to
//     fail fast if user-service is unhealthy
//   - gRPC alternative: in Phase 2, replace HTTP client with gRPC
//     client for ~10x faster serialization
// ─────────────────────────────────────────────────────────

type UserClient struct {
	baseURL    string
	httpClient *http.Client
}

// UserInfo is the response from user-service.
type UserInfo struct {
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	CreatedAt string `json:"created_at"`
}

// NewUserClient creates a client to talk to user-service.
func NewUserClient(baseURL string) *UserClient {
	return &UserClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second, // don't hang forever
		},
	}
}

// GetUser calls user-service to retrieve a user by ID.
// This is the inter-service communication pattern.
func (c *UserClient) GetUser(ctx context.Context, userID string) (*UserInfo, error) {
	url := fmt.Sprintf("%s/users/%s", c.baseURL, userID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call user-service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("user-service returned %d for user %s", resp.StatusCode, userID)
	}

	var user UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decode user response: %w", err)
	}

	return &user, nil
}
