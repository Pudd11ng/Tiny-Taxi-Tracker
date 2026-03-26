#!/bin/bash
# ══════════════════════════════════════════════════════════
# Deploy to Local Kubernetes (kind)
#
# This script:
#   1. Creates a kind cluster with Ingress support
#   2. Builds Docker images
#   3. Loads images into kind
#   4. Deploys all K8s resources
#   5. Installs NGINX Ingress Controller
#
# Prerequisites:
#   - Docker running
#   - kind installed: go install sigs.k8s.io/kind@latest
#   - kubectl installed
#
# Usage:
#   chmod +x deploy-k8s.sh
#   ./deploy-k8s.sh
# ══════════════════════════════════════════════════════════

set -euo pipefail

CLUSTER_NAME="app-cluster"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "════════════════════════════════════════════════════"
echo "  Deploying Distributed System to Kubernetes (kind)"
echo "════════════════════════════════════════════════════"

# ── Step 1: Create kind cluster ──────────────────────────
echo ""
echo "📦 Step 1: Creating kind cluster..."
if kind get clusters 2>/dev/null | grep -q "$CLUSTER_NAME"; then
  echo "   Cluster '$CLUSTER_NAME' already exists, skipping."
else
  cat <<EOF | kind create cluster --name "$CLUSTER_NAME" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    kubeadmConfigPatches:
      - |
        kind: InitConfiguration
        nodeRegistration:
          kubeletExtraArgs:
            node-labels: "ingress-ready=true"
    extraPortMappings:
      - containerPort: 80
        hostPort: 80
        protocol: TCP
      - containerPort: 443
        hostPort: 443
        protocol: TCP
  - role: worker
  - role: worker
EOF
  echo "   ✅ Cluster created with 1 control-plane + 2 workers"
fi

# ── Step 2: Build Docker images ──────────────────────────
echo ""
echo "🏗️  Step 2: Building Docker images..."
docker build -t api-gateway:latest    "${SCRIPT_DIR}/gateway/"
docker build -t user-service:latest   "${SCRIPT_DIR}/user-service/"
docker build -t order-service:latest  "${SCRIPT_DIR}/order-service/"
echo "   ✅ All images built"

# ── Step 3: Load images into kind ────────────────────────
echo ""
echo "📤 Step 3: Loading images into kind cluster..."
kind load docker-image api-gateway:latest   --name "$CLUSTER_NAME"
kind load docker-image user-service:latest  --name "$CLUSTER_NAME"
kind load docker-image order-service:latest --name "$CLUSTER_NAME"
echo "   ✅ Images loaded"

# ── Step 4: Install NGINX Ingress Controller ─────────────
echo ""
echo "🌐 Step 4: Installing NGINX Ingress Controller..."
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.9.5/deploy/static/provider/kind/deploy.yaml
echo "   Waiting for Ingress Controller to be ready..."
kubectl wait --namespace ingress-nginx \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/component=controller \
  --timeout=120s
echo "   ✅ Ingress Controller ready"

# ── Step 5: Deploy K8s resources ─────────────────────────
echo ""
echo "🚀 Step 5: Deploying application resources..."
kubectl apply -f "${SCRIPT_DIR}/k8s/namespace.yaml"
kubectl apply -f "${SCRIPT_DIR}/k8s/postgres-deployment.yaml"
kubectl apply -f "${SCRIPT_DIR}/k8s/redis-deployment.yaml"

echo "   Waiting for PostgreSQL to be ready..."
kubectl wait --namespace app-system \
  --for=condition=ready pod \
  --selector=app=postgres \
  --timeout=120s

echo "   Waiting for Redis to be ready..."
kubectl wait --namespace app-system \
  --for=condition=ready pod \
  --selector=app=redis \
  --timeout=60s

kubectl apply -f "${SCRIPT_DIR}/k8s/user-deployment.yaml"
kubectl apply -f "${SCRIPT_DIR}/k8s/order-deployment.yaml"
kubectl apply -f "${SCRIPT_DIR}/k8s/gateway-deployment.yaml"
kubectl apply -f "${SCRIPT_DIR}/k8s/ingress.yaml"

echo "   Waiting for all pods to be ready..."
kubectl wait --namespace app-system \
  --for=condition=ready pod \
  --all \
  --timeout=120s

# ── Step 6: Print status ─────────────────────────────────
echo ""
echo "════════════════════════════════════════════════════"
echo "  ✅ Deployment Complete!"
echo "════════════════════════════════════════════════════"
echo ""
echo "📊 Pod Status:"
kubectl get pods -n app-system
echo ""
echo "🌐 Services:"
kubectl get svc -n app-system
echo ""
echo "📡 Ingress:"
kubectl get ingress -n app-system
echo ""
echo "🧪 Test Commands:"
echo "   # Add api.local to /etc/hosts:"
echo "   echo '127.0.0.1 api.local' | sudo tee -a /etc/hosts"
echo ""
echo "   # Create a user:"
echo "   curl -X POST http://api.local/api/users \\"
echo "     -H 'Content-Type: application/json' \\"
echo "     -d '{\"name\":\"Test User\",\"email\":\"test@example.com\"}'"
echo ""
echo "   # Get a user:"
echo "   curl http://api.local/api/users/user-ali"
echo ""
echo "   # Create an order:"
echo "   curl -X POST http://api.local/api/orders \\"
echo "     -H 'Content-Type: application/json' \\"
echo "     -d '{\"user_id\":\"user-ali\",\"item\":\"Nasi Goreng\",\"quantity\":2,\"price\":10.00}'"
echo ""
echo "   # List user orders:"
echo "   curl http://api.local/api/orders/user/user-ali"
