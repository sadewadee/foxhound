# Anti-Detection

Foxhound's anti-detection system is built around one core principle: **consistency over randomness**. A random User-Agent without a matching TLS fingerprint, header order, and OS is worse than no rotation at all — it creates an incoherent identity that anti-bot systems flag immediately.

## Threat Model

Modern anti-bot systems (Cloudflare, Akamai, DataDome, PerimeterX) operate on multiple detection layers simultaneously:

| Layer | What is checked | Common failure mode |
|-------|----------------|---------------------|
| **TLS** | JA3/JA4 fingerprint, cipher suite order, GREASE values, ALPN | Mismatched TLS profile vs User-Agent |
| **HTTP** | Header order, pseudo-header order (HTTP/2), missing or unexpected headers | Wrong Sec-Fetch-* values, wrong header order for declared browser |
| **Browser** | navigator properties, WebGL vendor/renderer, screen resolution, platform string | Inconsistent hardware vs declared OS, missing navigator fields |
| **Behavioral** | Request timing distribution, inter-page delays, session length | Uniform timing (trivially detected by ML), too-fast navigation |
| **Contextual** | Referer consistency, cookie state, proxy geo vs identity locale | No Referer on first page, missing session cookies, mismatched timezone |
| **CAPTCHA** | Presence of reCAPTCHA, hCaptcha, Cloudflare Turnstile, GeeTest | Challenge triggered by any failure in layers 1-5 |

## Layer 1 — TLS Impersonation

### Default build

The default build (`go build ./...`) uses Go's standard `net/http` with correct header ordering derived from the identity profile. TLS handshakes use Go's default TLS stack. This is sufficient for sites without JA3/JA4 fingerprinting.

### TLS build (`-tags tls`)

```bash
go build -tags tls ./...
```

This selects `fetch/stealth_tls.go`, which uses `github.com/Noooste/azuretls-client` for real TLS impersonation:

- **JA3/JA4 fingerprint matching** for Firefox and Chrome profiles
- **Cipher suite reordering** to match the declared browser version
- **GREASE value injection** (Go's standard TLS omits GREASE)
- **HTTP/2 SETTINGS frame** matching the browser's known frame values

The TLS profile is derived automatically from the identity profile:

```go
profile := identity.Generate(identity.WithBrowser(identity.BrowserFirefox))
// profile.TLSProfile == "firefox_134.0"
// StealthFetcher uses this to select the matching cipher suite + SETTINGS
```

## Layer 2 — HTTP Fingerprinting

### Header ordering

Browsers send headers in a specific, version-dependent order. Go's `net/http` sorts headers alphabetically by default, which is easily detected.

Foxhound applies the canonical header order from `identity.Profile.HeaderOrder` for every request:

```go
// Firefox header order (from identity/profile.go):
[]string{
    "Host", "User-Agent", "Accept", "Accept-Language",
    "Accept-Encoding", "Connection", "Upgrade-Insecure-Requests",
    "Sec-Fetch-Dest", "Sec-Fetch-Mode", "Sec-Fetch-Site",
    "Sec-Fetch-User", "Priority", "Pragma", "Cache-Control",
}

// Chrome header order (from identity/profile.go):
[]string{
    "Host", "Connection", "sec-ch-ua", "sec-ch-ua-mobile",
    "sec-ch-ua-platform", "Upgrade-Insecure-Requests", "User-Agent",
    "Accept", "Sec-Fetch-Site", "Sec-Fetch-Mode",
    "Sec-Fetch-User", "Sec-Fetch-Dest", "Accept-Encoding",
    "Accept-Language", "Priority",
}
```

### Sec-Fetch headers

Foxhound sets all required `Sec-Fetch-*` headers with correct values:

```
Sec-Fetch-Dest: document
Sec-Fetch-Mode: navigate
Sec-Fetch-Site: none         (first page) | same-origin | cross-site
Sec-Fetch-User: ?1
```

The `Referer` middleware updates `Sec-Fetch-Site` context by tracking the previous URL per domain.

## Layer 3 — Identity Consistency

Every generated `identity.Profile` guarantees internal consistency across all attributes:

```go
profile := identity.Generate(
    identity.WithBrowser(identity.BrowserFirefox),
    identity.WithOS(identity.OSWindows),
    identity.WithTimezone("America/New_York"),
)
```

This produces a profile where:

- `profile.UA` contains the correct Firefox version string for Windows
- `profile.TLSProfile` is the matching TLS fingerprint for that Firefox version
- `profile.HeaderOrder` is Firefox's specific header order (not Chrome's)
- `profile.Platform` is `"Win32"` (not `"Linux x86_64"`)
- `profile.ScreenW` / `profile.ScreenH` are a common Windows resolution
- `profile.Timezone` is `"America/New_York"` (matches the declared locale)
- `profile.CamoufoxEnv` contains `CAMOU_CONFIG_*` vars for browser mode

### Why random UA rotation is dangerous

| Scenario | Result |
|----------|--------|
| Random UA + default Go TLS | JA3 fingerprint is Go's, not Firefox's — instant flag |
| Firefox UA + Chrome TLS profile | Mismatched fingerprint — instant flag |
| Firefox UA + Windows OS + Tokyo timezone | Contextual mismatch — suspicious |
| Foxhound profile (all attributes consistent) | No mismatch at any layer |

## Layer 4 — Human Behavior Simulation

### Log-normal timing

Real human inter-action times follow a log-normal distribution: most actions are fast, but rare long pauses (reading, distraction) make the mean significantly higher than the median. Uniform random timing is trivially detected by ML-based anti-bot systems.

The `behavior.Timing` type generates delays from a log-normal distribution:

```
delay = exp(Mu + Sigma * N(0,1))
```

Default parameters (`moderate` profile): Mu=1.0, Sigma=0.8

- Median delay: ~2.7 s
- Mean delay: ~4.1 s
- 95th percentile: ~13 s

### Behavior profiles

| Profile | Mu | Sigma | Median | Use case |
|---------|----|-------|--------|----------|
| `careful` | 1.5 | 0.5 | ~4.5 s | Cloudflare Enterprise, Akamai |
| `moderate` | 1.0 | 0.8 | ~2.7 s | Most sites (default) |
| `aggressive` | 0.5 | 0.6 | ~1.6 s | Lightly protected sites |

### Bezier mouse movement

Browser mode uses Bezier curves for mouse movement rather than direct linear paths. The mouse path includes realistic overshoot and jitter.

### Session rhythm

Walkers implement a burst/pause rhythm: a burst of N requests followed by a longer pause, occasionally interrupted by a long break (lunch, distraction). This matches real user session patterns.

Configuration via `BehaviorProfile` in `HuntConfig`:

```go
h := engine.NewHunt(engine.HuntConfig{
    BehaviorProfile: "careful",  // "careful" | "moderate" | "aggressive"
    // ...
})
```

## Layer 5 — Contextual Consistency

### Referer management

The `middleware.NewReferer()` middleware maintains per-domain Referer state:

- First request to a domain: `Referer: https://www.google.com/search?q=<domain>` (mimics organic search)
- Subsequent requests: previous URL in the same domain (realistic navigation flow)
- Manual Referer on a job is never overwritten

### Cookie persistence

The `middleware.NewCookies()` middleware maintains a per-session cookie jar. Cookies set by the server on page 1 are automatically sent on page 2. This is critical for sites that set CSRF tokens or session identifiers on first access.

### Proxy geo matching

The `identity.WithProxy(ip)` and `identity.WithCountry(code)` options resolve the proxy's geographic location and set matching timezone, locale, and languages:

```go
// Proxy in New York → identity uses America/New_York timezone + en-US locale
profile := identity.Generate(
    identity.WithProxy("203.0.113.42"),  // your proxy's external IP
)
```

A New York proxy + Tokyo timezone is a contextual flag. Foxhound prevents this by deriving all locale data from the proxy geo.

## Layer 6 — CAPTCHA Prevention

The goal is to **never trigger a CAPTCHA**. If a CAPTCHA appears, one or more earlier layers have failed.

Detection order:
1. Check response body for CAPTCHA markers (reCAPTCHA, hCaptcha, Turnstile, GeeTest)
2. Check status codes: 403 with challenge page HTML

When a CAPTCHA is detected:
- Log the event at WARN level
- If `captcha.enabled: true` and a solver is configured, attempt automatic solving
- If solving fails, escalate to browser mode with human simulation

### CAPTCHA solver configuration

```yaml
captcha:
  enabled: true
  provider: capsolver   # "capsolver" | "twocaptcha"
  api_key: ${CAPSOLVER_API_KEY}
```

Solvers are used as a last resort. The primary strategy is prevention.

## Block Detection

`SmartFetcher` uses `DefaultBlockDetector` to detect blocks:

```go
// Status codes that trigger escalation to browser:
case 401, 403, 407, 429, 503:
    return true
```

Implement `BlockDetector` to add custom detection logic (e.g. checking response body for "Access Denied" text):

```go
type MyDetector struct{}

func (d *MyDetector) IsBlocked(resp *foxhound.Response) bool {
    if resp.StatusCode == 403 {
        return true
    }
    return bytes.Contains(resp.Body, []byte("Access Denied"))
}

smart := fetch.NewSmart(staticFetcher, browserFetcher,
    fetch.WithBlockDetector(&MyDetector{}),
)
```
