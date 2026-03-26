# Custom Dockerfile for Codespaces devcontainer
# Fixes: stale Yarn APT repo key (NO_PUBKEY 62D54FD4003F6525)
# that causes Docker-in-Docker feature install to fail.
FROM mcr.microsoft.com/devcontainers/go:1.22

# Remove the broken Yarn apt source BEFORE any apt-get update runs.
# The base Go devcontainer image ships with a Yarn repo that has an
# expired/missing GPG key, causing all subsequent apt operations to fail.
RUN rm -f /etc/apt/sources.list.d/yarn.list 2>/dev/null; \
    rm -f /etc/apt/keyrings/yarn.gpg 2>/dev/null; \
    apt-get update && apt-get install -y --no-install-recommends \
        jq \
    && rm -rf /var/lib/apt/lists/*
