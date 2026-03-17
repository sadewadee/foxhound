# syntax=docker/dockerfile:1
# Multi-stage production Dockerfile for Foxhound.
#
# Build stages:
#   builder — compiles the Go binary with all optimisations
#   runtime — minimal Ubuntu image with Camoufox / Firefox dependencies
#
# Usage:
#   docker build -t foxhound:latest .
#   docker run --rm -v $(pwd)/config.yaml:/app/config.yaml foxhound:latest run --config /app/config.yaml

# ---------------------------------------------------------------------------
# Stage 1: builder
# ---------------------------------------------------------------------------
FROM golang:1.23-bookworm AS builder

# Install build dependencies for CGO-dependent packages.
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

# Copy dependency manifests first to leverage layer caching.
COPY go.mod go.sum ./
RUN go mod download

# Copy source tree and build the CLI binary.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -ldflags="-w -s -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" \
    -o /foxhound \
    ./cmd/foxhound

# ---------------------------------------------------------------------------
# Stage 2: runtime
# ---------------------------------------------------------------------------
FROM ubuntu:24.04 AS runtime

# Install Camoufox / Firefox runtime dependencies and security updates.
RUN apt-get update && apt-get upgrade -y && apt-get install -y --no-install-recommends \
    # Firefox / Camoufox shared libraries
    libgtk-3-0 \
    libdbus-glib-1-2 \
    libxt6 \
    libnss3 \
    libxcomposite1 \
    libxdamage1 \
    libxrandr2 \
    libxss1 \
    libxcursor1 \
    libxi6 \
    libxtst6 \
    libdrm2 \
    libgbm1 \
    libasound2t64 \
    # Fonts for realistic rendering
    fonts-liberation \
    fonts-noto \
    # TLS certificates
    ca-certificates \
    # Timezone data (identity locale matching)
    tzdata \
    # curl for health checks
    curl \
    && rm -rf /var/lib/apt/lists/*

# Create a non-root user for security.
RUN groupadd --gid 1001 foxhound && \
    useradd --uid 1001 --gid foxhound --shell /bin/bash --create-home foxhound

WORKDIR /app

# Copy the compiled binary from the builder stage.
COPY --from=builder /foxhound /usr/local/bin/foxhound

# Default directories.
RUN mkdir -p /app/output /app/config && \
    chown -R foxhound:foxhound /app

USER foxhound

# Expose the monitoring/metrics port (used when monitor/prometheus is enabled).
EXPOSE 9090

# Health check — the /health endpoint will be implemented in Phase 3.
HEALTHCHECK --interval=30s --timeout=10s --start-period=15s --retries=3 \
    CMD curl -fsS http://localhost:9090/health || exit 1

ENTRYPOINT ["/usr/local/bin/foxhound"]
CMD ["help"]
