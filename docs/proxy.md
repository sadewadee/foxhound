# Proxy Management

The `proxy` package manages HTTP/SOCKS proxy pools, health checking, and rotation strategies.

## Proxy Pool

`proxy.NewPool` accepts one or more `Provider` instances:

```go
import "github.com/sadewadee/foxhound/proxy"

pool := proxy.NewPool(
    proxy.Static([]string{
        "http://user:pass@host1:3128",
        "socks5://host2:1080",
    }),
)
```

### Pool methods

```go
px, err := pool.Get(ctx)                      // next proxy per rotation strategy
px, err := pool.GetForSession(ctx, "walker-0") // sticky proxy for a session ID
px, err := pool.GetForDomain(ctx, "example.com") // sticky proxy for a domain

pool.Release(px, success bool)        // report result after use
pool.Ban(px, "example.com")           // put proxy on cooldown for that domain

health := pool.Health(px)             // ProxyHealth snapshot
pool.SetRotation(proxy.PerRequest)    // configure rotation strategy
pool.SetCooldown(10 * time.Minute)    // configure cooldown duration
pool.SetMaxRequests(200)              // max requests before auto-cooldown (0 = unlimited)

n := pool.Len()                       // total pool size (including proxies on cooldown)
```

## Rotation Strategies

| Strategy | Constant | Behaviour |
|----------|----------|-----------|
| Per request | `proxy.PerRequest` | New proxy on every request, round-robin through available proxies |
| Per session | `proxy.PerSession` | Same proxy for a walker's entire session; replaced when it goes on cooldown |
| Per domain | `proxy.PerDomain` | Sticky proxy per target domain |
| On block | `proxy.OnBlock` | Keep current proxy; rotate only when a block is detected. Selects the highest-score proxy |

```go
pool.SetRotation(proxy.PerSession)
```

```yaml
proxy:
  rotation: per_session  # per_request | per_session | per_domain | on_block
```

## Health Checking

Each proxy has a `ProxyHealth` snapshot:

```go
type ProxyHealth struct {
    Alive         bool
    Latency       time.Duration
    SuccessRate   float64    // 0.0-1.0
    BlockRate     float64    // 0.0-1.0
    BanCount      int
    CooldownUntil time.Time  // zero if not on cooldown
    Score         float64    // 0.0 (dead) to 1.0 (perfect)
}
```

### Score decay

- Successful request: `score = score * 0.9 + 0.1` (nudge toward 1.0)
- Failed request: `score = score * 0.7` (penalty)
- Ban: `score = score * 0.5` + full cooldown applied

The `OnBlock` strategy always selects the proxy with the highest current score.

### Cooldown

When a proxy is banned or reaches `max_requests_per_proxy`, it is placed on cooldown for the configured duration:

```yaml
proxy:
  cooldown: 30m
  max_requests_per_proxy: 100
  health_check_interval: 60s
```

## Providers

### Static

A hardcoded list of proxy URL strings. Foxhound parses 6 formats:

```go
provider := proxy.Static([]string{
    "http://user:pass@1.2.3.4:3128",      // HTTP with auth
    "https://proxy.example.com:443",       // HTTPS without auth
    "socks5://user:pass@5.6.7.8:1080",    // SOCKS5 with auth
    "socks5://5.6.7.8:1080",              // SOCKS5 without auth
    "1.2.3.4:3128",                       // host:port (assumed HTTP, no auth)
    "user:pass@1.2.3.4:3128",             // user:pass@host:port (assumed HTTP)
})
```

```yaml
proxy:
  providers:
    - type: static
      list:
        - http://user:pass@1.2.3.4:3128
        - socks5://5.6.7.8:1080
```

### BrightData

```go
import proxyproviders "github.com/sadewadee/foxhound/proxy/providers"

provider := proxyproviders.NewBrightData(apiKey, product, country)
```

```yaml
proxy:
  providers:
    - type: brightdata
      api_key: ${BRIGHTDATA_API_KEY}
      product: residential
      country: US
```

### Oxylabs

```go
provider := proxyproviders.NewOxylabs(username, password, product, country)
```

```yaml
proxy:
  providers:
    - type: oxylabs
      username: ${OXYLABS_USERNAME}
      password: ${OXYLABS_PASSWORD}
      product: residential_proxies
      country: US
```

### SmartProxy

```go
provider := proxyproviders.NewSmartproxy(username, password, country)
```

```yaml
proxy:
  providers:
    - type: smartproxy
      username: ${SMARTPROXY_USERNAME}
      password: ${SMARTPROXY_PASSWORD}
      country: US
```

### Custom Provider

Implement the `proxy.Provider` interface:

```go
type Provider interface {
    Proxies(ctx context.Context) ([]*Proxy, error)
}

type MyProvider struct{ APIKey string }

func (p *MyProvider) Proxies(ctx context.Context) ([]*proxy.Proxy, error) {
    raw := []string{"http://user:pass@host:3128"}
    var out []*proxy.Proxy
    for _, r := range raw {
        px, err := proxy.Parse(r)
        if err != nil {
            continue
        }
        px.Country = "US"
        out = append(out, px)
    }
    return out, nil
}

pool := proxy.NewPool(&MyProvider{APIKey: "..."})
```

## GetForGeo

Select a proxy by country code and optional city. Returns the best-scoring proxy matching the requested geography:

```go
proxy, err := pool.GetForGeo("ID", "denpasar")
```

The country code is ISO 3166-1 alpha-2 (e.g. `"ID"` for Indonesia, `"US"` for United States). The city parameter is case-insensitive and optional -- pass an empty string to match any city in the country.

## Proxy Parsing

Parse a raw proxy string into a `Proxy` struct:

```go
px, err := proxy.Parse("http://user:pass@host:3128")
// px.Protocol == "http"
// px.Host     == "host"
// px.Port     == "3128"
// px.Username == "user"
// px.Password == "pass"
```

## Using Proxies with the Fetcher

Pass a proxy URL to the stealth fetcher:

```go
fetcher := fetch.NewStealth(
    fetch.WithIdentity(profile),
    fetch.WithProxy("http://user:pass@host:3128"),
)
```

For browser mode use `fetch.WithBrowserProxy`:

```go
camoufox, _ := fetch.NewCamoufox(
    fetch.WithBrowserIdentity(profile),
    fetch.WithBrowserProxy("http://user:pass@proxy.example.com:3128"),
)
```

**SOCKS5 with authentication**: Firefox/Playwright does not natively support SOCKS5 proxies with credentials. As of v0.0.12, foxhound includes a **transparent local SOCKS5 bridge** that handles this automatically — when you configure `socks5://user:pass@host:port`, foxhound spawns a local unauthenticated relay and passes credentials to the upstream proxy. No extra config needed.

```go
// SOCKS5 with auth — works automatically via bridge (v0.0.12+)
camoufox, _ := fetch.NewCamoufox(
    fetch.WithBrowserProxy("socks5://user:pass@proxy.example.com:1080"),
)
```

## GeoIP Matching

To avoid contextual flags, proxy geo must match the identity's timezone and locale.

### Automatic matching via identity.WithProxy

```go
// Foxhound looks up the proxy IP's country and resolves timezone + locale:
profile := identity.Generate(
    identity.WithProxy("203.0.113.42"),  // proxy's external IP
)
// profile.Timezone == "America/New_York"
// profile.Locale   == "en-US"
```

### Explicit country code

```go
profile := identity.Generate(
    identity.WithCountry("US"),
)
```

### Custom GeoResolver

```go
type MaxMindResolver struct{ db *maxminddb.Reader }

func (r *MaxMindResolver) Resolve(ip string) (identity.GeoInfo, error) {
    // query MaxMind database
}

profile := identity.Generate(
    identity.WithProxy("203.0.113.42"),
    identity.WithGeoResolver(&MaxMindResolver{db: myDB}),
)
```
