# syntax=docker/dockerfile:1
# Multi-stage production Dockerfile for Foxhound.
#
# Build stages:
#   builder  — compiles the Go binary (playwright build tag for full browser support)
#   browser  — installs Camoufox + playwright-go driver inside a throwaway image
#   runtime  — minimal Ubuntu image: binary + browser caches + Xvfb + non-root user
#
# Usage:
#   docker build -t foxhound:latest .
#   docker run --rm -v $(pwd)/config.yaml:/app/config.yaml foxhound:latest run --config /app/config.yaml
#
# Static-only build (no browser, smaller image — skip browser stage):
#   docker build --target runtime-static -t foxhound:static .
#
# Environment variables recognised at runtime:
#   DISPLAY                   Virtual display (default :99, managed by Xvfb entrypoint)
#   PLAYWRIGHT_BROWSERS_PATH  Override playwright browser cache location
#   FOXHOUND_DATA_DIR         Working directory for output, queues, caches (default /data)

# ---------------------------------------------------------------------------
# Stage 1: builder
# Compile the Go binary with the playwright build tag so the real Camoufox
# fetcher is included. CGO is disabled for a fully static binary.
# ---------------------------------------------------------------------------
FROM golang:1.25-bookworm AS builder

# git is needed for the version tag embedded via ldflags.
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

# Dependency manifests first — maximises Docker layer cache hits.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build with playwright tag so camoufox_playwright.go is compiled in.
# The playwright driver itself is installed in the browser stage.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -tags playwright \
    -ldflags="-w -s -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" \
    -o /foxhound \
    ./cmd/foxhound

# ---------------------------------------------------------------------------
# Stage 2: browser
# Install Camoufox (Python package) and the playwright-go driver in a
# throwaway image. Only the ~/.cache directories are copied to the runtime
# stage, keeping the final image free of Python, pip, and build tools.
# ---------------------------------------------------------------------------
FROM ubuntu:24.04 AS browser

# Full dependency set needed to run Firefox/Camoufox during installation.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    python3 \
    python3-pip \
    xvfb \
    fonts-liberation \
    fonts-noto-cjk \
    libasound2t64 \
    libatk1.0-0t64 \
    libcairo2 \
    libcups2t64 \
    libdbus-glib-1-2 \
    libgdk-pixbuf-2.0-0 \
    libgtk-3-0t64 \
    libnspr4 \
    libnss3 \
    libpango-1.0-0 \
    libx11-xcb1 \
    libxcomposite1 \
    libxdamage1 \
    libxrandr2 \
    && rm -rf /var/lib/apt/lists/*

# Fetch the Camoufox browser binary via the official Python package.
# --break-system-packages is required on Debian/Ubuntu 24.04 (PEP 668).
RUN pip3 install --break-system-packages camoufox \
    && python3 -m camoufox fetch

# Install the playwright-go driver that matches the version pinned in go.mod.
# We copy only go.mod so Docker can cache this layer independently of source.
COPY --from=builder /build/go.mod /tmp/go.mod

# Extract the playwright-go version and install the matching playwright CLI,
# then install the Firefox browser for that version.
RUN apt-get update && apt-get install -y --no-install-recommends golang \
    && PWGO_VER=$(grep -oE 'playwright-go v[0-9]+\.[0-9]+\.[0-9]+' /tmp/go.mod | awk '{print $2}') \
    && go install github.com/playwright-community/playwright-go/cmd/playwright@${PWGO_VER} \
    && /root/go/bin/playwright install --with-deps firefox \
    && apt-get purge -y golang \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/* /root/go/pkg /tmp/go.mod

# ---------------------------------------------------------------------------
# Stage 3: runtime
# Minimal production image. Only the compiled binary, the browser caches,
# and the runtime shared libraries are present. Xvfb provides the virtual
# display that Camoufox needs on headless servers.
# ---------------------------------------------------------------------------
FROM ubuntu:24.04 AS runtime

# Security updates + runtime shared libraries only (no build tools).
RUN apt-get update && apt-get upgrade -y && apt-get install -y --no-install-recommends \
    # Virtual display for headless Camoufox
    xvfb \
    # Firefox / Camoufox shared libraries
    libgtk-3-0t64 \
    libdbus-glib-1-2 \
    libxt6 \
    libnss3 \
    libnspr4 \
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
    libatk1.0-0t64 \
    libcairo2 \
    libcups2t64 \
    libgdk-pixbuf-2.0-0 \
    libpango-1.0-0 \
    libx11-xcb1 \
    # Fonts for realistic page rendering
    fonts-liberation \
    fonts-noto \
    fonts-noto-cjk \
    # TLS certificates
    ca-certificates \
    # Timezone data (IANA tz — identity locale matching)
    tzdata \
    # curl for liveness/readiness health checks
    curl \
    && rm -rf /var/lib/apt/lists/*

# Non-root user — running as root inside a container is a security risk and
# some anti-bot systems flag root-owned browser processes.
RUN groupadd --gid 1001 foxhound && \
    useradd --uid 1001 --gid foxhound --shell /bin/bash --create-home foxhound

# Copy the statically-compiled foxhound binary.
COPY --from=builder /foxhound /usr/local/bin/foxhound

# Copy Camoufox browser binary (fetched by the browser stage).
COPY --from=browser --chown=foxhound:foxhound \
    /root/.cache/camoufox \
    /home/foxhound/.cache/camoufox

# Copy playwright-go driver and Firefox browser (installed by the browser stage).
COPY --from=browser --chown=foxhound:foxhound \
    /root/.cache/ms-playwright \
    /home/foxhound/.cache/ms-playwright

# Working directories for output, queue persistence, and scraping cache.
RUN mkdir -p /data/output /data/queue /data/cache /app/config && \
    chown -R foxhound:foxhound /data /app

# /dev/shm is critical for browser stability under load. Mount as a tmpfs
# with at least 256 MB (docker run --shm-size=256m) or use the volume below.
VOLUME ["/dev/shm", "/data"]

USER foxhound
WORKDIR /home/foxhound

# Runtime environment variables.
# DISPLAY points to the Xvfb virtual display started by the entrypoint.
# PLAYWRIGHT_BROWSERS_PATH tells playwright-go where Firefox lives.
# FOXHOUND_DATA_DIR is the root for all persistent data.
ENV DISPLAY=:99 \
    PLAYWRIGHT_BROWSERS_PATH=/home/foxhound/.cache/ms-playwright \
    FOXHOUND_DATA_DIR=/data

# Expose the Prometheus metrics port.
EXPOSE 9090

# Health check hits the /health liveness endpoint exposed by the monitor package.
HEALTHCHECK --interval=30s --timeout=10s --start-period=20s --retries=3 \
    CMD curl -fsS http://localhost:9090/health || exit 1

# Entrypoint: start a virtual framebuffer then hand off to foxhound.
# Xvfb is backgrounded; the foxhound process replaces the shell so signals
# (SIGTERM, SIGINT) are delivered directly to foxhound for graceful shutdown.
ENTRYPOINT ["sh", "-c", "Xvfb :99 -screen 0 1920x1080x24 -nolisten tcp & exec foxhound \"$@\"", "--"]
CMD ["run"]
