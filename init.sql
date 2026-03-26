-- ══════════════════════════════════════════════════════════
-- Distributed System — Shared Database Schema
-- ══════════════════════════════════════════════════════════
-- Shared DB for MVP. In production, each service would own its DB.
-- This file is auto-executed by PostgreSQL on first boot.

-- ── Users Table ─────────────────────────────────────────
CREATE TABLE IF NOT EXISTS users (
    user_id    VARCHAR(255) PRIMARY KEY,
    name       VARCHAR(255) NOT NULL,
    email      VARCHAR(255) UNIQUE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users (email);

-- ── Orders Table ────────────────────────────────────────
CREATE TABLE IF NOT EXISTS orders (
    order_id   VARCHAR(255) PRIMARY KEY,
    user_id    VARCHAR(255) NOT NULL REFERENCES users(user_id),
    item       VARCHAR(255) NOT NULL,
    quantity   INT NOT NULL DEFAULT 1,
    price      DOUBLE PRECISION NOT NULL DEFAULT 0,
    status     VARCHAR(50) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_orders_user_id ON orders (user_id);
CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders (created_at DESC);

-- ── Seed Data ───────────────────────────────────────────
INSERT INTO users (user_id, name, email) VALUES
    ('user-ali',   'Ali Ahmad',    'ali@example.com'),
    ('user-budi',  'Budi Santoso', 'budi@example.com'),
    ('user-casey', 'Casey Lim',    'casey@example.com')
ON CONFLICT (user_id) DO NOTHING;

INSERT INTO orders (order_id, user_id, item, quantity, price, status) VALUES
    ('order-001', 'user-ali',  'Nasi Lemak',   2, 12.50, 'completed'),
    ('order-002', 'user-budi', 'Chicken Rice',  1,  8.00, 'pending'),
    ('order-003', 'user-ali',  'Teh Tarik',     3,  4.50, 'completed')
ON CONFLICT (order_id) DO NOTHING;
