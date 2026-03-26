# ── Build stage ──────────────────────────────────────────
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build -o user-service .

# ── Runtime stage ────────────────────────────────────────
FROM alpine:3.19
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /app/user-service .
EXPOSE 8081
HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
  CMD wget -qO- http://localhost:8081/healthz || exit 1
CMD ["./user-service"]
