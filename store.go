package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps a PostgreSQL connection pool.
//
// Interview talking points:
//   - Connection pooling: pgxpool reuses connections instead of opening/closing
//     per query. This avoids the TCP handshake + TLS negotiation overhead.
//   - MaxConns: limits the max connections to protect the DB. PostgreSQL has
//     a max_connections setting (default 100). If you have 10 service replicas
//     each with MaxConns=10, that's 100 connections — right at the limit.
//   - MinConns: keeps idle connections warm so the first query after idle
//     doesn't pay the connection setup cost.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a connection pool and verifies connectivity.
func NewStore(ctx context.Context, connStr string) (*Store, error) {
	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Tune pool size — in production, this should be configurable via env vars.
	config.MaxConns = 10
	config.MinConns = 2

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	// Verify the connection actually works before proceeding.
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return &Store{pool: pool}, nil
}

// UpsertLocation inserts or updates a driver's location.
//
// Uses PostgreSQL's ON CONFLICT (UPSERT) — atomic insert-or-update.
// This avoids the race condition of SELECT → check → INSERT/UPDATE.
func (s *Store) UpsertLocation(ctx context.Context, loc LocationUpdate) (*LocationResponse, error) {
	var resp LocationResponse

	err := s.pool.QueryRow(ctx,
		`INSERT INTO driver_locations (driver_id, lat, lng, updated_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (driver_id)
		 DO UPDATE SET lat = $2, lng = $3, updated_at = NOW()
		 RETURNING driver_id, lat, lng, updated_at`,
		loc.DriverID, loc.Lat, loc.Lng,
	).Scan(&resp.DriverID, &resp.Lat, &resp.Lng, &resp.UpdatedAt)

	if err != nil {
		return nil, fmt.Errorf("upsert location: %w", err)
	}
	return &resp, nil
}

// GetLocation retrieves a single driver's location from PostgreSQL.
func (s *Store) GetLocation(ctx context.Context, driverID string) (*LocationResponse, error) {
	var resp LocationResponse

	err := s.pool.QueryRow(ctx,
		`SELECT driver_id, lat, lng, updated_at
		 FROM driver_locations
		 WHERE driver_id = $1`,
		driverID,
	).Scan(&resp.DriverID, &resp.Lat, &resp.Lng, &resp.UpdatedAt)

	if err != nil {
		return nil, fmt.Errorf("get location: %w", err)
	}
	return &resp, nil
}

// ListLocations returns all drivers ordered by most recently updated.
func (s *Store) ListLocations(ctx context.Context) ([]LocationResponse, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT driver_id, lat, lng, updated_at
		 FROM driver_locations
		 ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list locations: %w", err)
	}
	defer rows.Close()

	var locations []LocationResponse
	for rows.Next() {
		var loc LocationResponse
		if err := rows.Scan(&loc.DriverID, &loc.Lat, &loc.Lng, &loc.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan location: %w", err)
		}
		locations = append(locations, loc)
	}
	return locations, nil
}

// Ping checks if the database is reachable (used by readiness probe).
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Close releases all pooled connections.
func (s *Store) Close() {
	s.pool.Close()
}
