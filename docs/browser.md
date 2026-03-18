# Browser Mode

Foxhound uses Camoufox — a patched Firefox fork — as its browser backend. The browser communicates via the Juggler protocol (Firefox's native DevTools protocol) rather than CDP (Chrome DevTools Protocol).

## Why Camoufox over Chromium

- **Juggler protocol** is far less targeted by anti-bot vendors than CDP. Most anti-bot JS checks specifically look for CDP artefacts (e.g. `window.chrome`, `navigator.webdriver` via CDP injection).
- **Firefox source is open for C++ patching**. Camoufox patches `navigator` APIs directly at the browser level rather than injecting JavaScript overrides that can be detected by checking `Function.prototype.toString()`.
- **CAMOU_CONFIG environment variables** control screen resolution, locale, hardware concurrency, device memory, GPU string, platform, and timezone — all surfaced through real `navigator.*` and `screen.*` APIs without any JS override.

## C++ Level Anti-Fingerprinting

Camoufox patches these browser APIs at the source level (not via JS injection):

| API | What is patched |
|-----|----------------|
| `navigator.webdriver` | Removed entirely |
| `canvas` fingerprint | Deterministic per-session noise |
| `WebGL` vendor/renderer | Configurable GPU string |
| `AudioContext` fingerprint | Seeded noise |
| Font enumeration | Controlled available font list |
| `screen.width` / `screen.height` | Set from identity profile |
| `navigator.hardwareConcurrency` | Set from profile |
| `navigator.deviceMemory` | Set from profile |
| `navigator.platform` | Matches OS in profile |

All values are controlled via `CAMOU_CONFIG_*` environment variables derived from `identity.Profile.CamoufoxEnv`.

## Build Tags

Camoufox support requires the `playwright` build tag:

```bash
# Default build — CamoufoxFetcher.Fetch returns an error:
go build ./...

# Real browser support:
go build -tags playwright ./...
go test -tags playwright ./fetch/...
```

After building with the playwright tag, install the Camoufox binary once per environment:

```bash
go run github.com/playwright-community/playwright-go/cmd/playwright install firefox
```

The playwright install mechanism downloads Camoufox from the official release channel. The default (stub) build keeps the binary small (~40 MB static binary) and dependency-free.

## Initialising CamoufoxFetcher

```go
import "github.com/sadewadee/foxhound/fetch"
import "github.com/sadewadee/foxhound/identity"

profile := identity.Generate(
    identity.WithBrowser(identity.BrowserFirefox),
    identity.WithOS(identity.OSLinux),
)

cf, err := fetch.NewCamoufox(
    fetch.WithBrowserIdentity(profile),       // all CAMOU_CONFIG vars derived from profile
    fetch.WithHeadless("virtual"),            // "virtual" | "true" | "false"
    fetch.WithBlockImages(true),              // block image/media/font resources
    fetch.WithBrowserTimeout(60*time.Second), // per-navigation timeout
    fetch.WithMaxBrowserRequests(300),         // restart browser after 300 requests
    fetch.WithPersistSession(false),          // fresh context per request (default)
)
if err != nil {
    log.Fatal(err)
}
defer cf.Close()
```

## CamoufoxOption Reference

| Option | Default | Description |
|--------|---------|-------------|
| `WithBrowserIdentity(p)` | nil | Sets all `CAMOU_CONFIG_*` env vars from the profile |
| `WithHeadless(mode)` | `"virtual"` | Display mode: `"virtual"` (Xvfb), `"true"` (native headless), `"false"` (visible window) |
| `WithBlockImages(bool)` | false | Block image/media/font requests. Cuts page-load time by 30-70% for content-only scraping. |
| `WithBrowserTimeout(d)` | 60s | Per-navigation timeout |
| `WithMaxBrowserRequests(n)` | 300 | Restart browser instance after N requests (0 = never restart) |
| `WithPersistSession(bool)` | false | Reuse `BrowserContext` across fetches (preserves cookies/localStorage) |
| `WithBrowserProxy(url)` | "" | HTTP or SOCKS5 proxy for all browser requests |

## Headless Modes

| Mode | Description | Use case |
|------|-------------|----------|
| `"virtual"` | Xvfb virtual display | Headless servers without a GPU. Requires `xvfb-run` or equivalent. |
| `"true"` | Native headless | Faster startup, no Xvfb dependency. |
| `"false"` | Full visible window | Local debugging — watch the browser navigate. |

## Human Simulation in Browser Mode

When using Camoufox with a behavior profile, walkers apply human-like simulation between page navigations:

- **Mouse movement**: Bezier curve paths with configurable jitter and overshoot probability. No direct linear paths.
- **Scroll**: Human-like scroll distances and pauses. Occasionally scrolls back up.
- **Cookie consent**: Automatically clicks common cookie consent dialogs ("Accept", "Accept All", "I agree")
- **reCAPTCHA v2 auto-click**: Attempts to click reCAPTCHA checkboxes before triggering a solver
- **Cloudflare JS challenge**: Waits for the challenge to complete and the page to reload
- **Navigation delays**: Log-normal delays between page loads (profile-dependent)
- **Session rhythm**: Burst/pause cadence matching the configured behavior profile

Set the behavior profile via `HuntConfig.BehaviorProfile` or `behavior.profile` in config.yaml.

## Browser Proxy

```go
cf, _ := fetch.NewCamoufox(
    fetch.WithBrowserProxy("http://user:pass@proxy.example.com:3128"),
    // SOCKS5 without auth:
    // fetch.WithBrowserProxy("socks5://proxy.example.com:1080"),
)
```

The proxy is set at the `BrowserContext` level, so all page navigations and sub-resources route through it.

Note: Playwright's Firefox implementation does not support SOCKS5 with authentication. Use HTTP proxies with auth, or SOCKS5 without credentials.

## Session Persistence

By default (`WithPersistSession(false)`), a fresh `BrowserContext` is created for each fetch call. This provides full isolation between requests.

Enable session persistence when scraping sites that require login state across pages:

```go
cf, _ := fetch.NewCamoufox(
    fetch.WithPersistSession(true),
)
```

With persistence enabled, cookies, localStorage, and IndexedDB are preserved across requests until the browser is restarted.

## Browser Lifecycle

The browser instance is restarted after `maxRequests` fetches. This clears accumulated state (memory leaks, cached data) and allows the browser fingerprint to be rotated. The default is 300 requests per instance.

```go
cf, _ := fetch.NewCamoufox(
    fetch.WithMaxBrowserRequests(500),  // restart after 500 requests
)
```

Set `WithMaxBrowserRequests(0)` to disable automatic restarts (not recommended for long-running hunts).

## SmartFetcher — Automatic Escalation

The `SmartFetcher` starts with the static fetcher and automatically escalates to Camoufox when a block is detected:

```go
staticFetcher := fetch.NewStealth(fetch.WithIdentity(profile))
browserFetcher, _ := fetch.NewCamoufox(fetch.WithBrowserIdentity(profile))

smart := fetch.NewSmart(staticFetcher, browserFetcher)
```

Escalation is triggered by `DefaultBlockDetector` for status codes 401, 403, 407, 429, 503.

Per-job control via `FetchMode`:

```go
job.FetchMode = foxhound.FetchStatic  // force static
job.FetchMode = foxhound.FetchBrowser // force Camoufox
job.FetchMode = foxhound.FetchAuto    // auto-route (default)
```

## Config

```yaml
fetch:
  browser:
    timeout: 60s
    headless: "virtual"   # "virtual" | "true" | "false"
    instances: 2          # concurrent browser instances (0 = static-only)
    block_images: true
    block_webrtc: true
```

Setting `instances: 0` disables browser mode entirely.

## Static-Only Mode

To build a static-only binary (no Playwright dependency, ~40 MB):

```bash
go build -o foxhound ./cmd/foxhound/
```

The default (stub) `CamoufoxFetcher.Fetch` returns a clear error:

```
camoufox: playwright-go not configured — rebuild with: go build -tags playwright ./...
  Then install the browser: go run github.com/playwright-community/playwright-go/cmd/playwright install firefox
```
