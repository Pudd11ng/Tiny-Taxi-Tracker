# 🚕 Tiny Taxi Tracker

A minimal ride-hailing location service built in **Go**, designed to demonstrate core **system design** and **infrastructure** patterns used by companies like Grab.

---

## Table of Contents

- [Architecture Overview](#architecture-overview)
- [Request Flow](#request-flow)
- [Component Deep Dive](#component-deep-dive)
  - [1. Nginx — Reverse Proxy](#1-nginx--reverse-proxy)
  - [2. Traefik — API Gateway](#2-traefik--api-gateway)
  - [3. Go Service — Application Layer](#3-go-service--application-layer)
- [Containerization Strategy](#containerization-strategy)
- [Networking](#networking)
- [System Design Concepts Demonstrated](#system-design-concepts-demonstrated)
- [How to Run](#how-to-run)
- [API Reference](#api-reference)
- [What's Next — Scaling for Production](#whats-next--scaling-for-production)

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────┐
│                    Docker Network (taxi-net)             │
│                                                         │
│  ┌──────────┐    ┌───────────────┐    ┌──────────────┐  │
│  │          │    │               │    │              │  │
│  │  Nginx   │───▶│   Traefik     │───▶│  Go Service  │  │
│  │ (Proxy)  │    │ (API Gateway) │    │  (taxi-svc)  │  │
│  │  :80     │    │  Rate Limit   │    │   :8080      │  │
│  │          │    │  Routing      │    │              │  │
│  └──────────┘    └───────────────┘    └──────────────┘  │
│       ▲                                                 │
└───────│─────────────────────────────────────────────────┘
        │
   Client Request
   (port 80)
```

**Key design principle:** Only Nginx is exposed to the outside world. All other services communicate internally over the Docker bridge network. This follows the **Defense in Depth** security pattern.

---

## Request Flow

Here is the exact lifecycle of a single request:

```
1. Client sends:  GET http://localhost/location
                        │
2. Nginx (port 80)      │  Nginx is the ONLY externally exposed port.
   receives request      │  It acts as a reverse proxy, forwarding all
                        │  traffic to the Traefik upstream.
                        ▼
3. Traefik (internal)    Traefik receives the proxied request.
   checks routing rules  It matches the path /location against its
                        router rules (configured via Docker labels).
                        │
   Rate Limit Check ──── Traefik applies the rate-limit middleware:
                        • Token bucket algorithm
                        • 5 requests/minute average
                        • Burst of 5 allowed
                        • Exceeding = HTTP 429 Too Many Requests
                        │
4. Go Service (:8080)    If rate limit passes, Traefik forwards
   handles request       to the Go service's /location handler.
                        │
5. Response              The Go handler returns a JSON response:
                        {
                          "driver": "Ali",
                          "lat": 1.29,
                          "lng": 103.85
                        }
                        │
6. Response flows back   Go → Traefik → Nginx → Client
```

---

## Component Deep Dive

### 1. Nginx — Reverse Proxy

**File:** `nginx.conf`

**Role:** Edge server — the single point of entry for all external traffic.

**What it does:**
| Feature | Implementation | Why it matters |
|---|---|---|
| Reverse proxying | `proxy_pass http://traefik_backend` | Hides internal services from the outside world |
| Header forwarding | `X-Real-IP`, `X-Forwarded-For`, `X-Forwarded-Proto` | Preserves original client information for logging, auth, and geo-routing |
| Upstream definition | `upstream traefik_backend { server traefik:80; }` | Uses Docker DNS — `traefik` resolves to the container's internal IP |
| Connection limit | `worker_connections 1024` | Controls max concurrent connections per worker process |

**System design concepts:**
- **L7 (Application Layer) Proxy** — Nginx operates at the HTTP level, inspecting headers and URIs
- **Single Ingress Point** — reduces attack surface; in production, this is where you'd terminate TLS
- **Header enrichment** — `X-Real-IP` and `X-Forwarded-For` are critical because without them, the backend only sees the proxy's IP, not the actual client's IP

**In a Grab context:** Grab uses an edge proxy layer to terminate TLS, enforce geo-based routing (route to nearest datacenter), and apply WAF (Web Application Firewall) rules before traffic hits internal services.

---

### 2. Traefik — API Gateway

**File:** `docker-compose.yml` (labels section)

**Role:** Internal API gateway that provides dynamic routing and middleware (rate limiting).

**How Traefik discovers services:**
```
Traefik reads Docker labels at runtime
        │
        ▼
Label: traefik.http.routers.taxi.rule=PathPrefix(`/location`)
  → "Any request path starting with /location should go to taxi-service"

Label: traefik.http.services.taxi.loadbalancer.server.port=8080
  → "The taxi-service container is listening on port 8080"

Label: traefik.http.routers.taxi.middlewares=taxi-ratelimit
  → "Apply the rate-limit middleware before forwarding"
```

**Rate Limiting Configuration:**
```yaml
average=5     # Allow 5 requests per period on average
period=1m     # The period is 1 minute
burst=5       # Allow up to 5 requests in a quick burst
```

**How Token Bucket works:**
```
Bucket capacity: 5 tokens
Refill rate: 5 tokens per minute (~1 token every 12 seconds)

Request arrives:
  └─ Token available? → YES → Consume token, forward request
  └─ Token available? → NO  → Reject with HTTP 429

A burst of 5 rapid requests is allowed (empties the bucket),
then the client must wait for tokens to refill.
```

**System design concepts:**
- **Service Discovery** — Traefik auto-discovers backends by reading Docker labels (no config file changes needed when services scale)
- **Dynamic Configuration** — labels are evaluated at runtime; add a new container and Traefik auto-routes to it
- **Rate Limiting** — protects the backend from DDoS, bursty clients, and accidental infinite loops
- **Middleware Chain** — Traefik supports stacking middleware (auth → rate-limit → retry → forward)

**In a Grab context:** Grab's API gateway handles authentication, rate limiting per API key, request routing to microservices, circuit breaking, and A/B testing (routing a percentage of traffic to canary deployments).

---

### 3. Go Service — Application Layer

**File:** `main.go`

**Role:** The core business logic — currently a simple location endpoint.

**Code walkthrough:**

```go
// Data model — represents a driver's GPS location
type LocationResponse struct {
    Driver string  `json:"driver"`  // JSON struct tags control serialisation
    Lat    float64 `json:"lat"`
    Lng    float64 `json:"lng"`
}
```
- **Struct tags** (`json:"driver"`) tell Go's JSON encoder to use lowercase keys.
- In production, this struct would include `timestamp`, `speed`, `heading`, `accuracy`, and `status` (available/busy/offline).

```go
func locationHandler(w http.ResponseWriter, r *http.Request) {
    // 1. Build response — in production, this would query a database
    resp := LocationResponse{Driver: "Ali", Lat: 1.29, Lng: 103.85}

    // 2. Set Content-Type BEFORE writing body
    w.Header().Set("Content-Type", "application/json")

    // 3. Encode directly to the ResponseWriter (no intermediate buffer)
    if err := json.NewEncoder(w).Encode(resp); err != nil {
        http.Error(w, "failed to encode response", http.StatusInternalServerError)
    }
}
```
- `json.NewEncoder(w).Encode()` streams JSON directly to the response writer — more memory-efficient than `json.Marshal()` + `w.Write()`.
- Error handling returns a 500 status if encoding fails.

```go
func main() {
    http.HandleFunc("/location", locationHandler)  // Register route
    log.Println("🚕 Tiny Taxi Tracker running on :8080")
    if err := http.ListenAndServe(":8080", nil); err != nil {
        log.Fatalf("server failed to start: %v", err)  // Fatal = log + os.Exit(1)
    }
}
```
- Uses Go's **DefaultServeMux** — a built-in HTTP request multiplexer
- `ListenAndServe` blocks the main goroutine; each incoming request is handled in a new goroutine automatically

**System design concepts:**
- **Stateless service** — no in-memory state, every request is independent. This is critical for horizontal scaling.
- **JSON over HTTP** — the standard REST pattern; in production, Grab uses a mix of REST and gRPC
- **Goroutine-per-request** — Go's concurrency model: each request gets a lightweight goroutine (~2KB stack), allowing thousands of concurrent connections without thread exhaustion

---

## Containerization Strategy

**File:** `Dockerfile`

### Multi-Stage Build

```dockerfile
# Stage 1: BUILD — uses full Go toolchain (~300MB)
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download          # Cache dependencies
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o taxi-tracker .

# Stage 2: RUN — uses minimal Alpine (~5MB)
FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/taxi-tracker .
CMD ["./taxi-tracker"]
```

**Why this matters:**

| Aspect | Single-stage | Multi-stage (ours) |
|---|---|---|
| Final image size | ~300MB+ | ~10-15MB |
| Attack surface | Go compiler, tools | Only binary + certs |
| Build time | Same | Same (cached layers) |
| Deploy speed | Slower pull | Much faster |

**Key flags explained:**
- `CGO_ENABLED=0` — disables C bindings, produces a fully static binary. No dependency on `libc` = runs on `scratch` or `alpine`.
- `GOOS=linux` — cross-compile target. Ensures the binary runs on Linux containers regardless of the build host OS.
- `ca-certificates` — needed for HTTPS calls to external services (e.g., database TLS, API calls).

**System design concept:**
- **Immutable infrastructure** — the built image never changes once pushed. Same image runs in dev, staging, and production.
- **Layer caching** — `COPY go.mod` before `COPY .` ensures dependency download is cached unless `go.mod` changes.

---

## Networking

**File:** `docker-compose.yml`

```yaml
networks:
  taxi-net:
    driver: bridge
```

### How Docker Bridge Networking Works

```
┌─────────────────────────────────────────────┐
│              Host Machine                    │
│                                             │
│  ┌─ Docker Bridge Network (taxi-net) ────┐  │
│  │                                       │  │
│  │  nginx ◄──────► traefik ◄──► taxi-svc │  │
│  │  172.18.0.2     172.18.0.3  172.18.0.4│  │
│  │                                       │  │
│  └───────────────────────────────────────┘  │
│       ▲                                     │
│       │ port mapping: host:80 → nginx:80    │
│       │                                     │
└───────┼─────────────────────────────────────┘
        │
   External client
```

- Containers on the same bridge network resolve each other by **service name** (Docker's built-in DNS)
- `nginx.conf` references `server traefik:80` — Docker DNS resolves `traefik` to `172.18.0.3`
- Only `nginx` has a port mapping (`80:80`); Traefik and the Go service are **not reachable from outside**

**System design concepts:**
- **Network isolation** — services can only be reached through defined paths
- **Service mesh (simplified)** — in production, tools like Istio or Linkerd manage service-to-service communication with mutual TLS, retries, and observability

---

## System Design Concepts Demonstrated

| Concept | Where in this project | Production equivalent |
|---|---|---|
| **Reverse Proxy** | Nginx forwards to Traefik | Nginx/Envoy at the edge |
| **API Gateway** | Traefik with routing + rate limiting | Kong, Envoy, AWS API Gateway |
| **Rate Limiting** | Token bucket via Traefik labels | Redis-backed distributed rate limiter |
| **Service Discovery** | Traefik reads Docker labels | Consul, etcd, Kubernetes DNS |
| **Container Orchestration** | Docker Compose | Kubernetes |
| **Multi-Stage Builds** | Dockerfile builder pattern | CI/CD pipeline artifact |
| **Defense in Depth** | Only Nginx exposed | WAF → Edge Proxy → Gateway → Service |
| **Stateless Services** | Go handler has no in-memory state | Enables horizontal scaling |
| **Bridge Networking** | Docker bridge + DNS resolution | VPC + Service Mesh |
| **Header Propagation** | X-Real-IP, X-Forwarded-For | Distributed tracing context |

---

## How to Run

### Prerequisites
- Docker & Docker Compose installed

### Start the stack
```bash
docker compose up --build
```

### Test the endpoint
```bash
# Normal request
curl http://localhost/location

# Expected response:
# {"driver":"Ali","lat":1.29,"lng":103.85}
```

### Test rate limiting
```bash
# Rapid-fire 10 requests — last few should get HTTP 429
for i in $(seq 1 10); do
  echo "Request $i: $(curl -s -o /dev/null -w '%{http_code}' http://localhost/location)"
done
```

### Stop the stack
```bash
docker compose down
```

---

## API Reference

### `GET /location`

Returns the current location of a driver.

**Response:**
```json
{
  "driver": "Ali",
  "lat": 1.29,
  "lng": 103.85
}
```

| Field | Type | Description |
|---|---|---|
| `driver` | string | Driver's name |
| `lat` | float64 | Latitude (WGS84) |
| `lng` | float64 | Longitude (WGS84) |

**Status Codes:**
| Code | Meaning |
|---|---|
| 200 | Success |
| 429 | Rate limit exceeded (Traefik) |
| 500 | Internal server error |
| 502 | Go service is down (Traefik can't reach backend) |

---

## What's Next — Scaling for Production

This project is intentionally minimal. Here's the roadmap to evolve it into a production-grade system:

| Phase | What to Add | Key Concept |
|---|---|---|
| **Graceful Shutdown** | `os.Signal` handling + `Server.Shutdown()` | Zero-downtime deploys |
| **Health Checks** | `/health` endpoint + Docker `HEALTHCHECK` | Container orchestration readiness |
| **Database** | PostgreSQL with PostGIS + Redis cache | Persistent storage + read caching |
| **Horizontal Scaling** | Multiple Go service replicas | Stateless scaling |
| **Message Queue** | Kafka/NATS for event streaming | Async processing, decoupling |
| **Geospatial Search** | Geohash / S2 geometry indexing | "Find nearest driver" in O(1) |
| **Real-Time Streaming** | WebSocket / gRPC streaming | Push driver locations to riders |
| **Observability** | Prometheus + Grafana + OpenTelemetry | Metrics, tracing, alerting |
| **CI/CD** | GitHub Actions → build → test → deploy | Automated pipeline |
| **Kubernetes** | Replace Compose with K8s manifests | Production orchestration |
