# Cloudflare Bypass Patterns

Proven patterns for bypassing Cloudflare protection with Foxhound.

## Pattern 1: Session Establishment (Homepage Warm-up)

Visit a lightweight page on the target domain first to obtain `cf_clearance` cookie, then navigate to the actual target. This is the most reliable pattern.

**Foxhound v0.0.6+**: This happens automatically. Browser-mode trails prepend a homepage visit by default via the Trail warm-up feature.

### Manual approach:
```go
trail := engine.NewTrail("cf-bypass").
    Navigate("https://target.com/").          // Homepage sets cf_clearance
    Wait("body", 3*time.Second).              // Wait for JS challenge to resolve
    Navigate("https://target.com/api/data").  // Now access target with cookie
    Wait("body", 5*time.Second)
```

### With persistent session (best for multi-page scraping):
```go
cf, _ := fetch.NewCamoufox(
    fetch.WithBrowserIdentity(profile),
    fetch.WithPersistSession(true),  // Keep cookies across fetches
)

// First fetch: establish session
cf.Fetch(ctx, &foxhound.Job{URL: "https://target.com/", FetchMode: foxhound.FetchBrowser})

// Subsequent fetches carry cf_clearance cookie automatically
cf.Fetch(ctx, &foxhound.Job{URL: "https://target.com/products", FetchMode: foxhound.FetchBrowser})
```

### Disable warm-up when not needed:
```go
trail := engine.NewTrail("fast").
    NoWarmup().
    Navigate("https://target.com/page")
```

Or use `--fast` CLI flag.

## Pattern 2: Cookie Injection via JS Evaluate

For sites requiring pre-authentication (NextAuth.js, custom auth), inject cookies before navigation:

```go
job := &foxhound.Job{
    URL:       "https://target.com/dashboard",
    FetchMode: foxhound.FetchBrowser,
    Steps: []foxhound.JobStep{
        {
            Action: foxhound.JobStepEvaluate,
            Script: `() => {
                document.cookie = "session=abc123; path=/; domain=target.com";
                document.cookie = "auth_token=xyz; path=/; domain=target.com";
            }`,
        },
    },
}
```

## Pattern 3: JS Evaluate for Data Extraction

When CSS selectors break due to obfuscated class names (common on Cloudflare-protected sites), use JavaScript evaluation:

```go
trail := engine.NewTrail("extract").
    Navigate("https://target.com/search?q=test").
    Wait("body", 10*time.Second).
    Evaluate(`() => {
        return [...document.querySelectorAll('h3')].map(h => ({
            title: h.textContent,
            link: h.closest('a')?.href || '',
        }));
    }`)
```

Results available in `resp.StepResults["step_N"]`.

## Pattern 4: Identity Consistency

Cloudflare checks that all identity attributes are consistent. Always use the identity system:

```go
profile := identity.Generate(
    identity.WithBrowser(identity.BrowserFirefox),
    identity.WithOS(identity.OSWindows),
    identity.WithCountry("US"),  // Geo must match proxy location
)

cf, _ := fetch.NewCamoufox(
    fetch.WithBrowserIdentity(profile),
)

stealth := fetch.NewStealth(
    fetch.WithIdentity(profile),  // Same identity for both fetchers
)
```

**Critical**: Proxy geo MUST match identity locale/timezone.

## Anti-Patterns (Don't Do This)

- **Don't navigate directly to deep URLs** without visiting the homepage first
- **Don't use different identities** for browser and stealth fetchers on the same domain
- **Don't set uniform random delays** — use log-normal distribution (Foxhound does this by default)
- **Don't retry immediately on 403** — wait with exponential backoff (use `BehaviorProfile: "careful"`)
- **Don't disable NopeCHA** unless you have a specific reason

## Cloudflare Detection Signals

Foxhound detects Cloudflare challenges via:
- Response body: "checking your browser", "just a moment", "challenge-platform"
- Status codes: 403 (blocked), 503 (challenge page)
- `cf-ray` response header presence

When detected, Foxhound's smart fetcher auto-escalates from static to browser mode.
