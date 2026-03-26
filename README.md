# 🏗️ Distributed Microservices System

A minimal distributed system built with Go, demonstrating real-world microservice patterns used at companies like Grab.

## Architecture

```
Client
 │
 ▼
NGINX Ingress (L7 routing)
 │
 ▼
API Gateway (:8080)
 │
 ├── /api/users  → User Service (:8081)
 │                    ├── Redis (cache)
 │                    └── PostgreSQL (data)
 │
 └── /api/orders → Order Service (:8082)
                      ├── → User Service (inter-service call)
                      └── PostgreSQL (data)
```

## Components

| Service | Port | Role |
|---------|------|------|
| **NGINX Ingress** | 80 | External entry point, L7 HTTP routing |
| **API Gateway** | 8080 | Auth, routing, logging |
| **User Service** | 8081 | User CRUD + Redis caching |
| **Order Service** | 8082 | Order CRUD + calls user-service |
| **PostgreSQL** | 5432 | Shared database (MVP) |
| **Redis** | 6379 | User data cache (cache-aside pattern) |

## Key Patterns Demonstrated

- **API Gateway**: Single entry point decouples clients from internal services
- **Service Discovery**: K8s DNS resolves `http://user-service:8081` automatically
- **Cache-Aside**: Check Redis → miss → query DB → populate cache
- **Write-Through**: Write to DB → immediately update cache
- **Inter-Service Communication**: order-service → user-service (HTTP/gRPC)
- **Health Probes**: `/healthz` (liveness) + `/readyz` (readiness)
- **Graceful Shutdown**: Drain in-flight requests before exit

## Quick Start

### Option 1: Docker Compose (Local Dev)

```bash
docker compose up --build
```

Test:
```bash
# Create a user
curl -X POST http://localhost/api/users \
  -H 'Content-Type: application/json' \
  -d '{"name":"Ali","email":"ali@test.com"}'

# Get a user (seed data)
curl http://localhost/api/users/user-ali

# Create an order (calls user-service internally)
curl -X POST http://localhost/api/orders \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"user-ali","item":"Nasi Lemak","quantity":2,"price":12.50}'

# List user's orders
curl http://localhost/api/orders/user/user-ali
```

### Option 2: Kubernetes (Codespaces/Linux)

```bash
# Deploy everything (creates kind cluster + deploys)
chmod +x deploy-k8s.sh
./deploy-k8s.sh

# Add host entry
echo '127.0.0.1 api.local' | sudo tee -a /etc/hosts

# Test
curl http://api.local/api/users/user-ali
```

## Project Structure

```
├── gateway/              # API Gateway (Go)
│   ├── main.go           #   Reverse proxy + auth + logging
│   └── Dockerfile
├── user-service/         # User Service (Go)
│   ├── main.go           #   HTTP server + handlers
│   ├── store.go          #   PostgreSQL operations
│   ├── cache.go          #   Redis cache layer
│   └── Dockerfile
├── order-service/        # Order Service (Go)
│   ├── main.go           #   HTTP server + handlers
│   ├── store.go          #   PostgreSQL operations
│   ├── user_client.go    #   Inter-service HTTP client
│   └── Dockerfile
├── proto/                # gRPC Proto Definitions (Phase 2)
│   ├── user.proto
│   └── order.proto
├── k8s/                  # Kubernetes Manifests
│   ├── namespace.yaml
│   ├── postgres-deployment.yaml
│   ├── redis-deployment.yaml
│   ├── gateway-deployment.yaml
│   ├── user-deployment.yaml
│   ├── order-deployment.yaml
│   └── ingress.yaml
├── docker-compose.yml    # Local development
├── nginx.conf            # NGINX config (simulates Ingress)
├── init.sql              # Database schema + seed data
├── deploy-k8s.sh         # K8s deployment script
└── generate_proto.sh     # Proto code generation (Phase 2)
```

## Request Flow Example: Create Order

```
1. Client → POST /api/orders
2. NGINX → forwards to API Gateway
3. Gateway → validates request, proxies to order-service
4. order-service → calls user-service to validate user exists
5. user-service → checks Redis cache → cache miss → queries PostgreSQL
6. user-service → populates cache → returns user to order-service
7. order-service → inserts order into PostgreSQL
8. Response flows back: order-service → gateway → NGINX → client
```

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Language | Go 1.22 |
| Container | Docker |
| Orchestration | Kubernetes (kind) |
| Gateway | Go (httputil.ReverseProxy) |
| Ingress | NGINX Ingress Controller |
| RPC | HTTP (Phase 1) / gRPC (Phase 2) |
| Cache | Redis 7 |
| Database | PostgreSQL 16 |

## Phase 2 Roadmap

- [ ] gRPC for inter-service communication (proto files ready)
- [ ] Kafka for async messaging (order events → notification service)
- [ ] Prometheus metrics
- [ ] Distributed tracing (Jaeger/Zipkin)
- [ ] Circuit breaker (resilience)
- [ ] Service mesh (Istio)
