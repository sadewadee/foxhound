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
Metrics → RateLimit → Dedup → Cookies → Referer → Retry → Fetcher

Response flow:
Fetcher → Retry → Referer → Cookies → Dedup → RateLimit → Metrics
```

### Composing the chain manually

```go
import "github.com/sadewadee/foxhound/middleware"

mws := []foxhound.Middleware{
    middleware.NewMetrics("foxhound"),           // 1. outermost
    middleware.NewRateLimit(2.0, 5),             // 2.
    middleware.NewDedup(),                        // 3.
    middleware.NewCookies(),                      // 4.
    middleware.NewReferer(),                      // 5.
    middleware.NewRetry(3, 500*time.Millisecond), // 6. innermost
}

h := engine.NewHunt(engine.HuntConfig{
    Middlewares: mws,
    // ...
})
```

Use `middleware.Chain(...)` to compose multiple middlewares into a single middleware value:

```go
chain := middleware.Chain(
    middleware.NewRateLimit(2.0, 5),
    middleware.NewDedup(),
)
// chain is a single foxhound.Middleware
```

## Full 11-Middleware Chain (CLI default)

The CLI `run` command assembles this chain from the config (in order, outermost first):

| # | Middleware | Always On | Config Key |
|---|-----------|-----------|------------|
| 1 | Metrics | no | `monitor.metrics.enabled: true` |
| 2 | RateLimit | no | `middleware.ratelimit.enabled: true` |
| 3 | RobotsTxt | no | `middleware.robots_txt.enabled: true` |
| 4 | DeltaFetch | no | `middleware.deltafetch.enabled: true` |
| 5 | Dedup | yes | always added |
| 6 | AutoThrottle | no | `middleware.autothrottle.enabled: true` |
| 7 | Cookies | yes | always added |
| 8 | Referer | yes | always added |
| 9 | Redirect | yes | always added (max 10 hops) |
| 10 | DepthLimit | no | `middleware.depth_limit.max > 0` |
| 11 | Retry | yes | always added (3 retries, 500ms base) |

## Middleware Reference

### NewMetrics

Records Prometheus counters and histograms for every request/response.

```go
mw := middleware.NewMetrics("foxhound")
```

Registers three instruments under `namespace`:

- `<ns>_requests_total{domain, status}` — request counter
- `<ns>_request_duration_seconds{domain}` — latency histogram
- `<ns>_errors_total{domain, error_type}` — error counter

Instruments are registered with the default Prometheus registry. Enable the metrics endpoint via config:

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

A separate `rate.Limiter` is created lazily per unique domain. The `Wait` method blocks until a token is available or the context is cancelled.

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

Skips URLs that were already scraped in a previous run. Backed by a persistent store.

```go
mw := middleware.NewDeltaFetch(strategy DeltaStrategy, store DeltaStore, ttl time.Duration)
```

Two strategies:

- `DeltaSkipSeen` — skip any URL ever fetched, regardless of when
- `DeltaSkipRecent` — skip only if fetched within `ttl`; re-fetch after TTL expires

Three store backends:

```go
// In-memory (lost on restart — useful for testing)
store := middleware.NewMemoryDeltaStore()

// SQLite (persistent across restarts)
store, err := middleware.NewSQLiteDeltaStore("foxhound_delta.db")

// Redis (distributed, shared across workers)
store, err := middleware.NewRedisDeltaStore("localhost:6379", "", 0, "foxhound:delta")
```

Skipped requests return a zero-value Response (StatusCode 0) without calling the underlying fetcher.

```yaml
middleware:
  deltafetch:
    enabled: true
    strategy: skip_seen     # "skip_seen" | "skip_recent"
    ttl: 24h
    store: sqlite           # "memory" | "redis" | "sqlite"
```

### NewDedup

In-run URL deduplication. Prevents the same URL from being fetched twice within a single hunt run.

```go
mw := middleware.NewDedup()
```

Canonicalisation before storing/checking:
- Only scheme + host + path + query are compared (fragment dropped)
- Query parameters are sorted alphabetically

Duplicate requests return a zero-value Response (StatusCode 0, empty body) without calling the fetcher.

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

1. After each response, update an exponential moving average (EMA, alpha=0.3) of response latency for the domain
2. Compute: `delay = EMA(latency) / TargetConcurrency`
3. Clamp to `[MinDelay, MaxDelay]`
4. On 429 or 503: spike immediately to `MaxDelay`
5. Sleep the computed delay before the next request to that domain

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

Uses Go's standard `net/http/cookiejar`. On each request:
1. Injects stored cookies from the jar for the target domain
2. After the response, stores any `Set-Cookie` headers

Critical for sites that set session tokens or CSRF tokens on the first page and verify them on subsequent pages. Always active in the CLI default chain.

### NewReferer

Sets a realistic `Referer` header on each request.

```go
mw := middleware.NewReferer()
```

Behaviour:
- If a `Referer` is already set on the job, it is not overwritten
- First request to a domain: `Referer: https://www.google.com/search?q=<domain>` (organic search simulation)
- Subsequent requests: the previous URL fetched for that domain

Always active in the CLI default chain. Critical for anti-bot bypass on sites that verify Referer consistency.

### NewRedirect

Follows HTTP redirects (301, 302, 303, 307, 308).

```go
mw := middleware.NewRedirect(maxRedirects int)
```

Up to `maxRedirects` hops are followed. Returns an error if the chain exceeds the limit. The CLI default uses 10 hops. A value of 0 disables redirect following.

### NewDepthLimit

Aborts requests whose `Job.Depth` exceeds the configured limit.

```go
mw := middleware.NewDepthLimit(maxDepth int)
```

Returns an error for any job with `Depth > maxDepth`, preventing the underlying fetcher from being called. Only active when `max > 0` in config.

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
            "status", statusCode(resp),
        )
        return resp, err
    })
}
```

Important when writing middleware that modifies job headers: always clone the job first to avoid mutating shared state (the retry middleware re-uses the same `*Job` pointer across multiple attempts):

```go
cloned := *job
cloned.Headers = job.Headers.Clone()
if cloned.Headers == nil {
    cloned.Headers = make(http.Header)
}
job = &cloned
```
