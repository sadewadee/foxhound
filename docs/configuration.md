# Configuration Reference

Foxhound is configured via a YAML file. Environment variables are expanded anywhere in the file using `${VAR}` syntax. The default config path is `config.yaml`.

Load config in Go:

```go
cfg, err := foxhound.LoadConfig("config.yaml")
```

## Full Schema

```yaml
hunt:
  domain: example.com    # primary target domain (used in logs and metrics)
  walkers: 3             # concurrent virtual-user goroutines (default: 3)

identity:
  browser: firefox                         # "firefox" | "chrome" (default: firefox)
  os: [windows, macos, linux]              # OS pool; one is chosen per walker
  fingerprint_db: embedded                 # "embedded" uses bundled profiles

proxy:
  providers:
    - type: static
      list:
        - http://user:pass@host:3128
        - socks5://user:pass@host:1080
    - type: brightdata
      api_key: ${BRIGHTDATA_API_KEY}
      product: residential
      country: US
    - type: oxylabs
      username: ${OXYLABS_USERNAME}
      password: ${OXYLABS_PASSWORD}
      product: residential_proxies
      country: US
    - type: smartproxy
      username: ${SMARTPROXY_USERNAME}
      password: ${SMARTPROXY_PASSWORD}
      country: US
  rotation: per_session              # per_request | per_session | per_domain | on_block
  cooldown: 30m                      # cooldown after ban (default: 30m)
  max_requests_per_proxy: 100        # auto-rotate after N requests (default: 100)
  health_check_interval: 60s         # health check frequency (default: 60s)

fetch:
  static:
    timeout: 30s                     # per-request timeout (default: 30s)
    max_idle_conns: 100              # HTTP connection pool size (default: 100)
    tls_impersonate: true            # enable TLS impersonation (-tags tls)
  browser:
    timeout: 60s                     # per-navigation timeout (default: 60s)
    headless: "virtual"              # "virtual" | "true" | "false"
    instances: 2                     # concurrent browser instances (0 = static-only)
    block_images: true               # block image/media/font resources
    block_webrtc: true               # block WebRTC to prevent IP leaks

middleware:
  ratelimit:
    enabled: true
    requests_per_sec: 2.0            # tokens per second per domain
    burst_size: 5                    # burst allowance
  autothrottle:
    enabled: true
    target_concurrency: 2.0          # desired parallel requests per domain
    initial_delay: 1s                # starting inter-request delay
    min_delay: 500ms                 # floor (default: 500ms)
    max_delay: 30s                   # ceiling; spike to max on 429/503
  dedup:
    strategy: url_canonical          # URL normalisation strategy
    store: memory                    # "memory" | "redis" | "sqlite"
  deltafetch:
    enabled: false                   # skip URLs seen in previous runs
    strategy: skip_seen              # "skip_seen" | "skip_recent"
    ttl: 24h                         # TTL for skip_recent strategy
    store: memory                    # "memory" | "redis" | "sqlite"
  robots_txt:
    enabled: false                   # respect robots.txt
  depth_limit:
    max: 5                           # 0 = unlimited

pipeline:
  - validate:
      required: [title, url]         # drop items missing these fields
  - clean:
      trim_whitespace: true          # trim all string field values
      normalize_price: false         # parse price strings to floats
  - export:
      - type: jsonl
        path: output/${FOXHOUND_RUN_ID}.jsonl
      - type: csv
        path: output/${FOXHOUND_RUN_ID}.csv
      - type: webhook
        path: https://api.example.com/items
        batch_size: 50
      - type: postgres
        table: scraped_items
        upsert_key: url
        batch_size: 100

queue:
  backend: memory                    # "memory" | "redis" | "sqlite"

cache:
  backend: ""                        # "" (disabled) | "memory" | "file" | "sqlite" | "redis"
  ttl: 1h                            # cache entry TTL (default: 1h)
  max_size: 1000                     # max entries for memory cache (default: 1000)

monitor:
  metrics:
    enabled: false
    port: 9090                       # Prometheus /metrics endpoint port
  alerting:
    webhook_url: ""                  # POST alerts here
    error_rate_threshold: 0.10       # alert when error rate exceeds 10%
    block_rate_threshold: 0.20       # alert when block rate exceeds 20%
    cooldown: 5m                     # minimum interval between repeated alerts

captcha:
  enabled: false
  provider: capsolver                # "capsolver" | "twocaptcha"
  api_key: ${CAPSOLVER_API_KEY}

behavior:
  profile: moderate                  # "careful" | "moderate" | "aggressive"

logging:
  level: info                        # "debug" | "info" | "warn" | "error"
  format: json                       # "json" | "text"
  output: stderr                     # "stderr" | "stdout" | "/path/to/file"
```

## Section Details

### hunt

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `domain` | string | — | Target domain. Used to seed the first URL and group metrics. |
| `walkers` | int | 3 | Number of concurrent virtual users. Each walker has its own fetcher session. |

### identity

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `browser` | string | `firefox` | Browser to impersonate: `firefox` or `chrome`. |
| `os` | []string | `[windows, macos, linux]` | OS pool. If more than one, a random OS is picked per walker. |
| `fingerprint_db` | string | `embedded` | Profile database. Only `embedded` is supported in v0.0.1. |

### proxy.rotation strategies

| Value | Behaviour |
|-------|-----------|
| `per_request` | New proxy on every request (round-robin). |
| `per_session` | Same proxy for a walker's entire session. Replaced if it goes on cooldown. |
| `per_domain` | Sticky proxy per target domain. |
| `on_block` | Rotate only when a block is detected. Picks highest-score proxy. |

### fetch.browser.headless modes

| Value | Behaviour |
|-------|-----------|
| `virtual` | Xvfb virtual display. Best for headless servers. Requires Xvfb. |
| `true` | Native headless mode. |
| `false` | Full visible browser window. Use for local debugging. |

Set `instances: 0` to run in static-only mode with no browser dependency.

### middleware.autothrottle

Autothrottle adapts the inter-request delay for each domain using an exponential moving average of response latency:

```
delay = EMA(latency) / target_concurrency
```

On 429 or 503 responses the delay spikes immediately to `max_delay`.

### pipeline.export types

| Type | Description |
|------|-------------|
| `json` | JSON array file. |
| `jsonl` | JSON Lines (one object per line). Recommended for large datasets. |
| `csv` | CSV file. Column order follows the first item's field keys (sorted) unless headers are set in code. |
| `webhook` | HTTP POST to a URL. Supports `batch_size` for batched requests. |
| `postgres` | PostgreSQL upsert. Requires `FOXHOUND_EXPORT_DB` env var or `path` field with a DSN. |

### behavior profiles

| Profile | Timing median | Use case |
|---------|--------------|----------|
| `careful` | ~4.5 s | Cloudflare Enterprise, Akamai Bot Manager. Low throughput, maximum stealth. |
| `moderate` | ~2.7 s | Default. Balanced stealth and throughput. |
| `aggressive` | ~1.6 s | Lightly protected sites. Higher throughput, higher block risk. |

## Environment Variable Expansion

All string values in the config file support `${VAR}` expansion using `os.ExpandEnv`. Values are expanded at load time. Example:

```yaml
captcha:
  api_key: ${CAPSOLVER_API_KEY}

proxy:
  providers:
    - type: brightdata
      api_key: ${BRIGHTDATA_API_KEY}
```

## Defaults

When a field is omitted, `LoadConfig` applies these defaults:

| Field | Default |
|-------|---------|
| `hunt.walkers` | 3 |
| `identity.browser` | `firefox` |
| `identity.os` | `[windows, macos, linux]` |
| `proxy.rotation` | `per_session` |
| `proxy.cooldown` | `30m` |
| `proxy.max_requests_per_proxy` | 100 |
| `proxy.health_check_interval` | `60s` |
| `fetch.static.timeout` | `30s` |
| `fetch.static.max_idle_conns` | 100 |
| `fetch.browser.timeout` | `60s` |
| `fetch.browser.instances` | 2 |
| `queue.backend` | `memory` |
| `cache.ttl` | `1h` |
| `cache.max_size` | 1000 |
| `monitor.metrics.port` | 9090 |
| `monitor.alerting.cooldown` | `5m` |
| `behavior.profile` | `moderate` |
| `logging.level` | `info` |
| `logging.format` | `json` |
| `logging.output` | `stderr` |
| `middleware.autothrottle.target_concurrency` | 2.0 |
| `middleware.autothrottle.initial_delay` | `1s` |
| `middleware.autothrottle.min_delay` | `500ms` |
| `middleware.autothrottle.max_delay` | `30s` |
| `middleware.deltafetch.strategy` | `skip_seen` |
| `middleware.deltafetch.ttl` | `24h` |
| `middleware.deltafetch.store` | `memory` |
