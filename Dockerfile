# ── Build stage ──────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o taxi-tracker .

# ── Runtime stage ────────────────────────────────────────────
FROM alpine:3.19

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /app/taxi-tracker .

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
  CMD wget -qO- http://localhost:8080/healthz || exit 1

CMD ["./taxi-tracker"]
