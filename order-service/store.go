package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps a PostgreSQL connection pool for order operations.
type Store struct {
	pool *pgxpool.Pool
}

// Order represents an order record.
type Order struct {
	OrderID   string  `json:"order_id"`
	UserID    string  `json:"user_id"`
	Item      string  `json:"item"`
	Quantity  int     `json:"quantity"`
	Price     float64 `json:"price"`
	Status    string  `json:"status"`
	CreatedAt string  `json:"created_at"`
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

// CreateOrder inserts a new order.
func (s *Store) CreateOrder(ctx context.Context, orderID, userID, item string, quantity int, price float64) (*Order, error) {
	var order Order
	err := s.pool.QueryRow(ctx,
		`INSERT INTO orders (order_id, user_id, item, quantity, price, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, 'pending', NOW())
		 RETURNING order_id, user_id, item, quantity, price, status, created_at::text`,
		orderID, userID, item, quantity, price,
	).Scan(&order.OrderID, &order.UserID, &order.Item, &order.Quantity, &order.Price, &order.Status, &order.CreatedAt)

	if err != nil {
		return nil, fmt.Errorf("create order: %w", err)
	}
	return &order, nil
}

// GetOrder retrieves an order by ID.
func (s *Store) GetOrder(ctx context.Context, orderID string) (*Order, error) {
	var order Order
	err := s.pool.QueryRow(ctx,
		`SELECT order_id, user_id, item, quantity, price, status, created_at::text
		 FROM orders WHERE order_id = $1`,
		orderID,
	).Scan(&order.OrderID, &order.UserID, &order.Item, &order.Quantity, &order.Price, &order.Status, &order.CreatedAt)

	if err != nil {
		return nil, fmt.Errorf("get order: %w", err)
	}
	return &order, nil
}

// ListUserOrders returns all orders for a given user.
func (s *Store) ListUserOrders(ctx context.Context, userID string) ([]Order, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT order_id, user_id, item, quantity, price, status, created_at::text
		 FROM orders WHERE user_id = $1
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list user orders: %w", err)
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.OrderID, &o.UserID, &o.Item, &o.Quantity, &o.Price, &o.Status, &o.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan order: %w", err)
		}
		orders = append(orders, o)
	}
	return orders, nil
}

// Ping checks if the database is reachable.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Close releases all pooled connections.
func (s *Store) Close() {
	s.pool.Close()
}
