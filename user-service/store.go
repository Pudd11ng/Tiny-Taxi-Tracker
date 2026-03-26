package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps a PostgreSQL connection pool for user operations.
//
// Interview talking points:
//   - Connection pooling: pgxpool reuses connections instead of opening/closing
//     per query (avoids TCP+TLS overhead).
//   - UPSERT vs INSERT: we use INSERT for users (unique constraint on email).
//   - UUID primary keys: generated server-side with google/uuid for
//     distributed ID generation without coordination.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a connection pool and verifies connectivity.
func NewStore(ctx context.Context, connStr string) (*Store, error) {
	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	config.MaxConns = 10
	config.MinConns = 2

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return &Store{pool: pool}, nil
}

// User represents a user record.
type User struct {
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	CreatedAt string `json:"created_at"`
}

// CreateUser inserts a new user and returns the created record.
func (s *Store) CreateUser(ctx context.Context, userID, name, email string) (*User, error) {
	var user User
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (user_id, name, email, created_at)
		 VALUES ($1, $2, $3, NOW())
		 RETURNING user_id, name, email, created_at::text`,
		userID, name, email,
	).Scan(&user.UserID, &user.Name, &user.Email, &user.CreatedAt)

	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return &user, nil
}

// GetUser retrieves a user by ID.
func (s *Store) GetUser(ctx context.Context, userID string) (*User, error) {
	var user User
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, name, email, created_at::text
		 FROM users WHERE user_id = $1`,
		userID,
	).Scan(&user.UserID, &user.Name, &user.Email, &user.CreatedAt)

	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return &user, nil
}

// Ping checks if the database is reachable.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Close releases all pooled connections.
func (s *Store) Close() {
	s.pool.Close()
}
