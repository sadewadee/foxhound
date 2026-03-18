# Deployment

## Docker

### Pull from GHCR

```bash
docker pull ghcr.io/sadewadee/foxhound:v0.0.1
docker pull ghcr.io/sadewadee/foxhound:latest
```

### Build locally

```bash
# Static-only (~40 MB):
docker build -t foxhound:latest .

# With browser support (includes Firefox + Xvfb, larger image):
docker build --build-arg BUILD_TAGS=playwright -t foxhound:playwright .
```

### Single worker

```bash
docker compose up
```

Starts three services:
- `foxhound` — scraping worker
- `redis` — job queue, dedup store, response cache (512 MB limit, LRU eviction)
- `postgres` — persistent queue and export target

### Multi-worker

```bash
docker compose up --scale foxhound=4
```

Scales to 4 concurrent foxhound workers. All workers share the same Redis queue and Postgres database.

### With monitoring (Prometheus + Grafana)

```bash
docker compose --profile monitoring up
```

Adds:
- `prometheus` on port `9091` — scrapes `/metrics` from foxhound workers
- `grafana` on port `3000` — pre-loaded Foxhound dashboard

Default Grafana credentials: `admin` / `admin` (override with `GRAFANA_PASSWORD` env var).

## docker-compose.yml Reference

```yaml
services:
  foxhound:
    image: foxhound:latest
    build:
      context: .
      dockerfile: Dockerfile
    command: run --config /app/config/config.yaml
    environment:
      FOXHOUND_MODE: ${FOXHOUND_MODE:-auto}
      REDIS_URL: redis://redis:6379/0
      DATABASE_URL: postgres://foxhound:${POSTGRES_PASSWORD:-foxhound}@postgres:5432/foxhound
      FOXHOUND_LOG_LEVEL: ${FOXHOUND_LOG_LEVEL:-info}
      FOXHOUND_RUN_ID: ${FOXHOUND_RUN_ID:-default}
    volumes:
      - ./config:/app/config:ro
      - foxhound-output:/app/output
    ports:
      - "9090:9090"   # Prometheus /metrics
    deploy:
      resources:
        limits:
          memory: 2G
          cpus: "2.0"
```

Config files are mounted read-only from `./config/`. Output files are written to the `foxhound-output` named volume.

## Environment Variables

Copy `.env.example` to `.env` and populate:

```bash
cp .env.example .env
```

| Variable | Default | Description |
|----------|---------|-------------|
| `FOXHOUND_MODE` | `auto` | Fetch mode: `auto`, `static`, or `browser` |
| `FOXHOUND_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `FOXHOUND_RUN_ID` | `default` | Namespaces output files: `output/${FOXHOUND_RUN_ID}.jsonl` |
| `REDIS_URL` | *(none)* | Redis connection URL: `redis://host:6379/0` |
| `DATABASE_URL` | *(none)* | PostgreSQL DSN for queue and export |
| `FOXHOUND_EXPORT_DB` | *(none)* | PostgreSQL DSN specifically for the postgres export writer |
| `POSTGRES_PASSWORD` | `foxhound` | Postgres password (docker-compose only) |
| `GRAFANA_PASSWORD` | `admin` | Grafana admin password (docker-compose monitoring profile) |
| `BRIGHTDATA_API_KEY` | *(none)* | BrightData proxy API key |
| `OXYLABS_USERNAME` | *(none)* | Oxylabs proxy username |
| `OXYLABS_PASSWORD` | *(none)* | Oxylabs proxy password |
| `CAPSOLVER_API_KEY` | *(none)* | Capsolver CAPTCHA API key |
| `TWOCAPTCHA_API_KEY` | *(none)* | 2Captcha API key |

## Static-Only Deployment (no browser)

For sites that don't require JavaScript execution:

```bash
# Environment variable:
FOXHOUND_MODE=static foxhound run --config config.yaml

# Config (disable browser instances):
# fetch:
#   browser:
#     instances: 0
```

A static-only binary has no Playwright or browser dependency and produces a ~40 MB Docker image.

## Scaling Patterns

### Horizontal scaling (multiple workers, shared queue)

All workers must share the same Redis queue:

```yaml
queue:
  backend: redis
```

Each worker independently pops jobs, processes them, and pushes discovered URLs back to the shared queue. Deduplication is shared across workers via the Redis dedup store:

```yaml
middleware:
  dedup:
    store: redis
  deltafetch:
    enabled: true
    store: redis
```

### Vertical scaling (more walkers per worker)

```yaml
hunt:
  walkers: 16
  max_concurrency: 32
```

Or override at runtime:

```bash
foxhound run --config config.yaml --workers 16
```

For static-only scraping, 16-32 walkers per process is typical. For browser mode, keep walkers equal to or less than the number of browser instances (default: 2).

### Resuming interrupted runs

Use the Redis or SQLite queue backends for resumability:

```bash
foxhound resume --hunt-id my-hunt-001 --queue redis://localhost:6379/0 --config config.yaml
```

## Health Endpoint

When `monitor.metrics.enabled: true`, the Prometheus metrics endpoint starts on `monitor.metrics.port` (default: 9090).

Docker healthcheck:

```yaml
healthcheck:
  test: ["CMD", "curl", "-f", "http://localhost:9090/metrics"]
  interval: 30s
  timeout: 5s
  retries: 3
```

## v0.0.1 Release

The v0.0.1 tag is the first public release:

```bash
git checkout v0.0.1
go build -o foxhound ./cmd/foxhound/
```

Docker image:

```bash
docker pull ghcr.io/sadewadee/foxhound:v0.0.1
```

## Rollback

Config changes are the primary failure mode. To roll back:

1. Stop workers: `docker compose down`
2. Revert `config.yaml` to the previous version
3. Restart: `docker compose up`

Database migrations (Postgres queue schema) are additive and do not require rollback.

For a clean reset of all state:

```bash
docker compose down -v   # removes all volumes (queue, cache, dedup state)
docker compose up
```
