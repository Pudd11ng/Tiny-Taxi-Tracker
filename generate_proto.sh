#!/bin/bash
# ══════════════════════════════════════════════════════════
# Proto Code Generation Script
# Run this in Codespaces / Linux to generate Go code from .proto files.
#
# Prerequisites:
#   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
#   go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
#   apt install -y protobuf-compiler  (or brew install protobuf)
# ══════════════════════════════════════════════════════════

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROTO_DIR="${SCRIPT_DIR}/proto"

echo "🔧 Generating Go code from proto files..."

# Generate user service
protoc \
  --go_out="${PROTO_DIR}" --go_opt=paths=source_relative \
  --go-grpc_out="${PROTO_DIR}" --go-grpc_opt=paths=source_relative \
  -I "${PROTO_DIR}" \
  "${PROTO_DIR}/user.proto"

# Generate order service
protoc \
  --go_out="${PROTO_DIR}" --go_opt=paths=source_relative \
  --go-grpc_out="${PROTO_DIR}" --go-grpc_opt=paths=source_relative \
  -I "${PROTO_DIR}" \
  "${PROTO_DIR}/order.proto"

echo "✅ Proto generation complete!"
echo "   Generated files:"
find "${PROTO_DIR}" -name "*.pb.go" -o -name "*_grpc.pb.go" | sort
