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

## NopeCHA Extension (Auto-Download)

Foxhound auto-downloads the [NopeCHA](https://github.com/NopeCHALLC/nopecha-extension) CAPTCHA-solving extension on first launch. The extension is cached locally and loaded into every Camoufox instance.

- **Source**: GitHub `NopeCHALLC/nopecha-extension` releases
- **Cache location**: `~/.cache/foxhound/extensions/nopecha/`
- **Disable**: set `extension_path: "none"` in config or `WithExtensionPath("none")` in code
- **Custom extension**: set `extension_path` to a directory or `.xpi` file path

The auto-download runs once per machine. Subsequent launches use the cached copy.

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
| `WithExtensionPath(path)` | auto-download NopeCHA | Path to Firefox extension dir/xpi. Set `"none"` to disable. |
| `WithCaptureXHR(patterns)` | nil | Regexp patterns for XHR/fetch response capture (see below) |

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

## JobStep Types

Jobs can carry an ordered list of `Steps` that the browser executes after page load. Each step has an `Action` field identifying its type.

| Action | Const | Description |
|--------|-------|-------------|
| 0 | `JobStepNavigate` | Navigate to a URL (usually the first step, implicit) |
| 1 | `JobStepClick` | Click an element matching `Selector` |
| 2 | `JobStepWait` | Wait for `Selector` to reach `WaitState` (`"attached"`, `"detached"`, `"visible"`, `"hidden"`) |
| 3 | `JobStepExtract` | Extract content from elements matching `Selector` |
| 4 | `JobStepScroll` | Scroll by `ScrollExtent` pixels (`ScrollAxis` 0=vertical, 1=horizontal) |
| 5 | `JobStepInfiniteScroll` | Scroll to bottom repeatedly until no new content loads (max `MaxScrolls`, default 50) |
| 6 | `JobStepLoadMore` | Click a "load more" button (`Selector`) repeatedly (max `MaxClicks`, default 20) |
| 7 | `JobStepPaginate` | Follow pagination links (`Selector`), accumulate HTML from all pages in `StepResults` (max `MaxPages`, default 10) |
| 8 | `JobStepEvaluate` | Execute JavaScript (`Script`). Return value stored in `Response.StepResults["step_N"]` |
| 9 | `JobStepFill` | Type text (`Value`) into an input (`Selector`) with human-like keystrokes via `behavior.Keyboard` |

### InfiniteScroll

By default, InfiniteScroll scrolls the document body. Set `Selector` to scroll inside a custom container (e.g. `"div.results-panel"`).

Use `StopSelector` and `StopCount` to stop early when enough items exist:

```go
foxhound.JobStep{
    Action:       foxhound.JobStepInfiniteScroll,
    Selector:     "div.results-panel",  // custom scrollable container (optional)
    StopSelector: "div.result",         // stop when this selector matches...
    StopCount:    20,                   // ...at least 20 elements
    MaxScrolls:   100,
}
```

### Optional Steps

Set `Optional: true` on any step to make it non-fatal. If the step fails (e.g. a cookie banner dismiss button not present), execution continues to the next step instead of aborting the fetch.

```go
foxhound.JobStep{
    Action:   foxhound.JobStepClick,
    Selector: "#cookie-accept",
    Optional: true,  // skip if not present
}
```

### Evaluate Step

Executes arbitrary JavaScript on the page. The return value is stored in `Response.StepResults` keyed by step index (`"step_0"`, `"step_2"`, etc.):

```go
step := foxhound.JobStep{
    Action: foxhound.JobStepEvaluate,
    Script: "document.querySelectorAll('.item').length",
}
// After fetch:
// resp.StepResults["step_0"] == 42
```

### Fill Step

Types text into an input field using human-like keystrokes (via `behavior.Keyboard`), including natural inter-key delays and occasional corrections:

```go
foxhound.JobStep{
    Action:   foxhound.JobStepFill,
    Selector: "input[name=search]",
    Value:    "foxhound scraper",
}
```

### Paginate

The Paginate step follows pagination links matching `Selector` across multiple pages. HTML from all visited pages is accumulated into `Response.StepResults["step_N"]`, so you can extract items from the combined content after the fetch completes.

## XHR/Fetch Capture

Use `WithCaptureXHR(patterns)` to intercept API responses made by the page. Captured exchanges are available in `Response.CapturedXHR` after the fetch.

```go
import "regexp"

cf, _ := fetch.NewCamoufox(
    fetch.WithCaptureXHR(
        regexp.MustCompile(`/api/products`),
        regexp.MustCompile(`/graphql`),
    ),
)

// After fetch:
for _, xhr := range resp.CapturedXHR {
    fmt.Println(xhr["request_url"], xhr["status"])
    // xhr["body"] contains the response body as []byte
}
```

Each captured entry is a map with keys: `request_url`, `request_method`, `status`, `headers`, `body`.

## Config

```yaml
fetch:
  browser:
    timeout: 60s
    headless: "virtual"   # "virtual" | "true" | "false"
    instances: 2          # concurrent browser instances (0 = static-only)
    block_images: true
    block_webrtc: true
    extension_path: ""    # auto-download NopeCHA (default), "none" to disable, or path to extension
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
