# Middleware

Middleware wraps the fetcher to add cross-cutting behaviour. Each middleware implements:

```go
type Middleware interface {
    Wrap(next foxhound.Fetcher) foxhound.Fetcher
}
```

## The Chain

When building a hunt from code, middlewares are passed as a slice in `HuntConfig.Middlewares`. The first element in the slice is outermost (executes first on the way in, last on the way out):

```
Request flow:
Concurrency → Metrics → RateLimit → RobotsTxt → DeltaFetch → Dedup
  → AutoThrottle → Cookies → Referer → BlockDetector → Redirect → DepthLimit → Retry → Fetcher

Response flow:
Fetcher → Retry → DepthLimit → Redirect → BlockDetector → Referer → Cookies
  → AutoThrottle → Dedup → DeltaFetch → RobotsTxt → RateLimit → Metrics → Concurrency
```

### Composing the chain manually

```go
import "github.com/sadewadee/foxhound/middleware"

mws := []foxhound.Middleware{
    middleware.NewConcurrency(2),                    // 1. outermost — per-domain semaphore
    middleware.NewMetrics("foxhound"),               // 2.
    middleware.NewRateLimit(2.0, 5),                 // 3.
    middleware.NewDedup(),                           // 4.
    middleware.NewCookies(),                         // 5.
    middleware.NewReferer(),                         // 6.
    middleware.NewBlockDetector(3, time.Second),     // 7.
    middleware.NewRetry(3, 500*time.Millisecond),    // 8. innermost
}

h := engine.NewHunt(engine.HuntConfig{
    Middlewares: mws,
    // ...
})
```

Use `middleware.Chain(...)` to compose multiple middlewares into a single value:

```go
chain := middleware.Chain(
    middleware.NewRateLimit(2.0, 5),
    middleware.NewDedup(),
)
// chain is a single foxhound.Middleware
```

## Full 13-Middleware Chain (CLI default)

The CLI `run` command assembles this chain from the config (in order, outermost first):

| # | Middleware | Always On | Config Key |
|---|-----------|-----------|------------|
| 1 | Concurrency | no | `middleware.concurrency.per_domain > 0` |
| 2 | Metrics | no | `monitor.metrics.enabled: true` |
| 3 | RateLimit | no | `middleware.ratelimit.enabled: true` |
| 4 | RobotsTxt | no | `middleware.robots_txt.enabled: true` |
| 5 | DeltaFetch | no | `middleware.deltafetch.enabled: true` |
| 6 | Dedup | yes | always added |
| 7 | AutoThrottle | no | `middleware.autothrottle.enabled: true` |
| 8 | Cookies | yes | always added |
| 9 | Referer | yes | always added |
| 10 | BlockDetector | yes | always added with default patterns |
| 11 | Redirect | yes | always added (max 10 hops) |
| 12 | DepthLimit | no | `middleware.depth_limit.max > 0` |
| 13 | Retry | yes | always added (3 retries, 500ms base) |

## Middleware Reference

### NewConcurrency

Per-domain semaphore. Limits the number of concurrent in-flight requests for a single domain. A separate semaphore is created lazily per unique domain.

```go
mw := middleware.NewConcurrency(perDomain int)
```

Intended as the outermost middleware so it caps parallelism before rate-limit or dedup checks.

```yaml
middleware:
  concurrency:
    per_domain: 2   # default: 2
```

### NewMetrics

Records Prometheus counters and histograms for every request/response.

```go
mw := middleware.NewMetrics("foxhound")
```

Registers under `namespace`:

- `<ns>_requests_total{domain, status}` — request counter
- `<ns>_request_duration_seconds{domain}` — latency histogram
- `<ns>_errors_total{domain, error_type}` — error counter

```yaml
monitor:
  metrics:
    enabled: true
    port: 9090
```

### NewRateLimit

Per-domain token bucket rate limiter.

```go
mw := middleware.NewRateLimit(requestsPerSec float64, burstSize int)
```

A separate `rate.Limiter` is created lazily per unique domain. Blocks until a token is available or the context is cancelled.

```yaml
middleware:
  ratelimit:
    enabled: true
    requests_per_sec: 2.0
    burst_size: 5
```

### NewRobotsTxt

Checks `robots.txt` before each request and skips disallowed URLs.

```go
mw := middleware.NewRobotsTxt(userAgent string)
```

The `userAgent` string is matched against robots.txt `User-agent:` directives. Pass the browser name (e.g. `"firefox"`) or `"*"` for the catch-all rule.

```yaml
middleware:
  robots_txt:
    enabled: true
```

### NewDeltaFetch

Skips URLs that were already scraped in a previous run.

```go
mw := middleware.NewDeltaFetch(strategy DeltaStrategy, store DeltaStore, ttl time.Duration)
```

Two strategies:

- `DeltaSkipSeen` — skip any URL ever fetched
- `DeltaSkipRecent` — skip only if fetched within `ttl`

Three store backends:

```go
store := middleware.NewMemoryDeltaStore()
store, err := middleware.NewSQLiteDeltaStore("foxhound_delta.db")
store, err := middleware.NewRedisDeltaStore("localhost:6379", "", 0, "foxhound:delta")
```

```yaml
middleware:
  deltafetch:
    enabled: true
    strategy: skip_seen
    ttl: 24h
    store: sqlite
```

### NewDedup

In-run URL deduplication. Prevents the same URL from being fetched twice within a single hunt run.

```go
mw := middleware.NewDedup()
```

Canonicalisation before storing:
- Only scheme + host + path + query are compared (fragment dropped)
- Query parameters are sorted alphabetically

### NewAutoThrottle

Adaptive per-domain delay based on observed response latency.

```go
mw := middleware.NewAutoThrottle(middleware.AutoThrottleConfig{
    TargetConcurrency: 2.0,
    InitialDelay:      1 * time.Second,
    MinDelay:          500 * time.Millisecond,
    MaxDelay:          30 * time.Second,
})
```

Algorithm:

1. After each response, update EMA (alpha=0.3) of response latency for the domain
2. Compute: `delay = EMA(latency) / TargetConcurrency`
3. Clamp to `[MinDelay, MaxDelay]`
4. On 429 or 503: spike immediately to `MaxDelay`

```yaml
middleware:
  autothrottle:
    enabled: true
    target_concurrency: 2.0
    initial_delay: 1s
    min_delay: 500ms
    max_delay: 30s
```

### NewCookies

Persists HTTP cookies across requests within the same session.

```go
mw := middleware.NewCookies()
```

Uses Go's standard `net/http/cookiejar`. Injects stored cookies on each request and stores `Set-Cookie` headers from responses. Always active in the CLI default chain.

### NewReferer

Sets a realistic `Referer` header on each request.

```go
mw := middleware.NewReferer()
```

Behaviour:
- If a `Referer` is already set on the job, it is not overwritten
- First request to a domain: `Referer: https://www.google.com/search?q=<domain>` (organic search simulation)
- Subsequent requests: the previous URL fetched for that domain

Always active in the CLI default chain.

### NewBlockDetector

Detects soft blocks based on HTTP status codes and body content heuristics. Retries with exponential backoff when a block is detected.

```go
mw := middleware.NewBlockDetector(maxRetries int, baseDelay time.Duration, patterns ...BlockPattern)
```

If no patterns are provided, `DefaultBlockPatterns()` is used. The 9 default patterns are:

| Name | Trigger |
|------|---------|
| `cloudflare` | Body contains: "checking your browser", "just a moment", "challenge-platform" |
| `rate-limit` | Status 429 + body contains: "rate limit", "too many requests" |
| `access-denied` | Status 403 + body contains: "access denied", "forbidden", "blocked" |
| `bot-detection` | Body contains: "bot detected", "automated access", "unusual traffic" |
| `empty-trap` | Status 200 + body smaller than 500 bytes + no `<html` tag |
| `akamai` | Body contains: "akamai", "security challenge", "reference #" |
| `datadome` | Body contains: "datadome", "dd.js" |
| `perimeterx` | Body contains: "perimeterx", "px-captcha" |
| `login-wall` | Body contains: "login", "sign in" |

Custom patterns:

```go
patterns := []middleware.BlockPattern{
    {
        Name:         "my-site-block",
        StatusCode:   200,
        BodyContains: []string{"access restricted"},
    },
}
mw := middleware.NewBlockDetector(3, time.Second, patterns...)
```

Backoff formula: `baseDelay * 2^attempt * (1 ± 25% jitter)`

### NewRedirect

Follows HTTP redirects (301, 302, 303, 307, 308).

```go
mw := middleware.NewRedirect(maxRedirects int)
```

Up to `maxRedirects` hops are followed. Returns an error if the chain exceeds the limit. The CLI default uses 10 hops.

### NewDepthLimit

Aborts requests whose `Job.Depth` exceeds the configured limit.

```go
mw := middleware.NewDepthLimit(maxDepth int)
```

Returns an error for any job with `Depth > maxDepth`. Only active when `max > 0` in config.

```yaml
middleware:
  depth_limit:
    max: 5
```

### NewRetry

Retries failed or blocked requests with exponential backoff.

```go
mw := middleware.NewRetry(maxRetries int, baseDelay time.Duration)
```

Total attempts: `1 + maxRetries`. Retries on:
- Any fetch error (network failure, timeout)
- Status codes: 403, 407, 429, 503

Backoff formula: `baseDelay * 2^attempt * (1 ± 25% jitter)`

Example delays for `baseDelay=500ms`:
- Attempt 1: ~500ms
- Attempt 2: ~1s
- Attempt 3: ~2s

Context cancellation stops retries immediately. Always active (innermost middleware) in the CLI default chain.

## Custom Middleware

Implement `foxhound.Middleware` or use `foxhound.MiddlewareFunc`:

```go
type TimingMiddleware struct {
    log *slog.Logger
}

func (m *TimingMiddleware) Wrap(next foxhound.Fetcher) foxhound.Fetcher {
    return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
        start := time.Now()
        resp, err := next.Fetch(ctx, job)
        m.log.Info("fetch timing",
            "url", job.URL,
            "duration", time.Since(start),
        )
        return resp, err
    })
}
```

When modifying job headers, always clone the job first to avoid mutating shared state across retry attempts:

```go
cloned := *job
cloned.Headers = job.Headers.Clone()
if cloned.Headers == nil {
    cloned.Headers = make(http.Header)
}
job = &cloned
```
