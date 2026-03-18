# Proxy Management

The `proxy` package manages HTTP/SOCKS proxy pools, health checking, and rotation strategies.

## Proxy Pool

`proxy.NewPool` accepts one or more `Provider` instances. Providers are queried once during construction.

```go
import "github.com/sadewadee/foxhound/proxy"

pool := proxy.NewPool(
    proxy.Static([]string{
        "http://user:pass@host1:3128",
        "socks5://user:pass@host2:1080",
    }),
)
```

### Pool methods

```go
// Get next proxy according to rotation strategy:
px, err := pool.Get(ctx)

// Sticky proxy for a session ID:
px, err := pool.GetForSession(ctx, "walker-0")

// Sticky proxy for a domain:
px, err := pool.GetForDomain(ctx, "example.com")

// Report result after use:
pool.Release(px, success bool)

// Ban a proxy from a domain (triggers cooldown):
pool.Ban(px, "example.com")

// Get health snapshot:
health := pool.Health(px)

// Configure rotation strategy:
pool.SetRotation(proxy.PerRequest)

// Configure cooldown duration:
pool.SetCooldown(10 * time.Minute)

// Configure max requests before auto-cooldown (0 = unlimited):
pool.SetMaxRequests(200)

// Total pool size (including proxies on cooldown):
n := pool.Len()
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

Config:

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
    SuccessRate   float64    // 0.0–1.0
    BlockRate     float64    // 0.0–1.0
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

When a proxy is banned (`pool.Ban`) or reaches `max_requests_per_proxy`, it is placed on cooldown for the configured duration. The pool waits precisely until the earliest cooldown expiry before retrying, rather than busy-waiting.

```yaml
proxy:
  cooldown: 30m
  max_requests_per_proxy: 100
  health_check_interval: 60s
```

## Providers

### Static

A hardcoded list of proxy URL strings.

```go
provider := proxy.Static([]string{
    "http://user:pass@1.2.3.4:3128",
    "socks5://user:pass@5.6.7.8:1080",
    "https://proxy.example.com:443",
})
```

Supported URL formats:
- `http://user:pass@host:port`
- `https://host:port`
- `socks5://user:pass@host:port`
- `host:port` (assumed HTTP, no auth)

```yaml
proxy:
  providers:
    - type: static
      list:
        - http://user:pass@1.2.3.4:3128
        - socks5://user:pass@5.6.7.8:1080
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
    // fetch from your proxy service API
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

Pass a proxy URL to the stealth fetcher via `fetch.WithProxy`:

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
    fetch.WithBrowserProxy("socks5://user:pass@host:1080"),
)
```
