# Dev Squad Gotchas

## 2026-05-14 coordinator -- Go module replace directive fails when both module paths coexist as independents
- Root cause: `replace github.com/Noooste/utls => github.com/refraction-networking/utls v1.8.2` is rejected by Go module system when both paths already exist independently in the dep graph (Noooste/utls via azuretls-client, refraction-networking/utls via gaukas/clienthellod). Error: "used for two different module paths". Even after removing the explicit refraction-networking/utls require entry to eliminate the conflict, the build then fails with "use of internal package not allowed" because Go's internal package visibility is scoped to module path prefix — a consumer registered under `Noooste/utls/...` cannot access `refraction-networking/utls/internal/...` even via replace.
- Fix: Instead of replacing the module, used `session.ModifyConfig` (a public hook on azuretls.Session exposed in connection.go) to apply `utls.RenegotiateNever` directly to the `tls.Config` before every handshake. This fixes the panic without touching the module graph. Only change to go.mod is promoting `Noooste/utls` from indirect to direct (since fetch/stealth_tls.go now imports it for the constant).
- Prevention: Before attempting a `replace` directive to substitute a fork with upstream, check whether both module paths already exist in the transitive dep graph. If yes, the replace will fail. Also check whether the target module uses `internal/` subpackages — if it does, any cross-prefix replace will fail at build time regardless of dep graph state. The right fix in this situation is to work through the existing API surface of the wrapping library (azuretls Session.ModifyConfig in this case), not at the module graph level.

## 2026-05-14 coordinator -- TLS renegotiation panic in Noooste/utls v1.3.x on HelloFirefox_* connections
- Root cause: `Noooste/utls v1.3.20` is a stale fork that never backported upstream fix for TLS renegotiation (refraction-networking/utls issue #284). When a server sends HelloRequest mid-connection (TLS 1.2 renegotiation), `handleRenegotiation` calls `clientHandshake` → `loadSession` → `sessionController.onEnterLoadSessionCheck`. If `sessionController.locked == true` (set during initial handshake), it panics: "tls: LoadSessionCoordinator.onEnterLoadSessionCheck failed: session is set and locked". Triggered on `HelloFirefox_*` ClientHelloIDs which is what foxhound uses by default. Surface rate: 11 panics/hour under captcha-heavy concurrent load.
- Fix: Set `session.ModifyConfig = func(config *utls.Config) error { config.Renegotiation = utls.RenegotiateNever; return nil }` in `NewStealth`. `handleRenegotiation` returns `alertNoRenegotiation` before the panic site. ClientHello fingerprint unaffected (RenegotiationInfoExtension is in ClientHello extension list, not determined by `config.Renegotiation`).
- Prevention: When using a transitive dep that is a stale fork of an upstream library for a security-critical path (TLS), pin the test discipline to concurrent multi-request scenarios — the panic only surfaces under concurrent load with specific server behavior (HelloRequest). Single-shot unit tests will not catch it. If a dep is a named fork (Noooste/* vs refraction-networking/*), audit GitHub for maintenance status before relying on it for production TLS behavior.

## 2026-04-07 coordinator -- Firefox version mismatch: profiles said 148.0 but Camoufox is 135.0
- Root cause: All firefox_*.json profile files had `browser_ver: "148.0"` but installed Camoufox binary is based on Firefox 135.0.1 (verified via application.ini). This caused UA mismatch — browser reported Firefox 135 at C++ level but CAMOU_CONFIG injected Firefox 148 UA, creating a detectable inconsistency.
- Fix: Updated all firefox_*.json profiles, fallback profile, and stealth fetcher defaults from 148.0 to 135.0. Created firefox_135.0.json TLS profile. Updated all tests.
- Prevention: Always check `application.ini` in Camoufox binary to verify Firefox version before updating profile data. Add a startup check that validates profile version matches binary version.

## 2026-04-07 coordinator -- CAMOU_CONFIG over-specified: manually set WebGL/fonts/canvas that BrowserForge handles
- Root cause: Previous fix added `addWebGLConfig()`, `addFontConfig()`, and `canvas:aaOffset/aaCapOffset` to `BuildCamoufoxConfig()`. BrowserForge auto-populates these with realistic statistical distributions from its built-in database. Our static values were fingerprintable as a cluster.
- Fix: Removed addWebGLConfig, addFontConfig, font lists (windowsFonts/macosFonts/linuxFonts), and canvas noise from BuildCamoufoxConfig. BrowserForge now handles all of these automatically.
- Prevention: Read Camoufox docs before overriding properties. Rule of thumb: only set what we want to CONTROL (identity consistency). Let BrowserForge handle everything else.

## 2026-04-07 coordinator -- Missing navigator.language/languages and window.devicePixelRatio in CAMOU_CONFIG
- Root cause: BuildCamoufoxConfig set locale:language but not navigator.language/navigator.languages (official Camoufox property names). Also missing window.devicePixelRatio. Also missing OS-specific taskbar height for screen.availHeight.
- Fix: Added navigator.language, navigator.languages, window.devicePixelRatio. Replaced hardcoded -40 with screenAvailHeight() helper (Windows -40, macOS -25, Linux -28). Added extractLang/extractRegion helpers.
- Prevention: Cross-reference official Camoufox property name list when implementing CAMOU_CONFIG.

## 2026-04-07 coordinator -- Camoufox CAMOU_CONFIG format was completely wrong
- Root cause: `buildCamoufoxEnv()` created individual env vars like `CAMOU_CONFIG_SCREEN_W=1920` but Camoufox expects a JSON blob in `CAMOU_CONFIG_1`, `CAMOU_CONFIG_2`, etc. (chunked at 2000 bytes). Additionally the CamoufoxEnv map was never injected into browser launch options — only addon config used CAMOU_CONFIG_1.
- Fix: Rewrote to produce proper JSON with dot-path keys (`screen.width`, `navigator.userAgent`, etc.), added WebGL, font, and canvas config, merged fingerprint+addon config into single JSON blob, and removed Playwright context overrides that conflicted with Camoufox C++ level spoofing.
- Prevention: Read actual Camoufox documentation/source before implementing config format. The Python camoufox package shows the correct format.

## 2026-04-07 coordinator -- Playwright context overrides conflicted with Camoufox
- Root cause: `buildContextOptions()` set UserAgent, Locale, TimezoneId via Playwright context API even when Camoufox was active. This creates a detectable mismatch between JS-injected values and C++ level values.
- Fix: Added `hasCamoufox` flag — when true, skip UA/Locale/TimezoneId in context options. Keep Viewport (window size != screen size) and Geolocation permissions.
- Prevention: When using a browser fork that patches at C++ level, never also override the same values at the automation layer.

## 2026-04-07 coordinator -- examples/social is stale (pre-existing, not caused by our changes)
- Root cause: `examples/social/main.go` references old API signatures (`fetch.NewStealth` returning 2 values, `middleware.DefaultBlockDetectorConfig`, `export.NewJSONLinesWriter`, etc.)
- Fix: Not fixed — this is a pre-existing issue outside the scope of the Camoufox fingerprint work
- Prevention: Run `go build -tags playwright ./...` periodically to catch stale examples

## 2026-04-07 coordinator -- Google Search 429 was proxy IP reputation, not fingerprint
- Root cause: Proxy exit IP `154.6.48.10` is flagged by Google for `/search` specifically. Google homepage (200) and Maps work fine through the same proxy, but `/search` always returns 429. Direct (no proxy) returns 200 immediately.
- Fix: Need a proxy with clean Google Search reputation. The fingerprint fixes (below) are still valuable for general anti-detection.
- Prevention: Always test with direct connection first to isolate proxy vs fingerprint issues. Check proxy exit IP, not entry IP.

## 2026-04-07 coordinator -- Five fingerprint issues found in stealth fetcher
- Root cause: Multiple deviations from real Firefox fingerprint:
  1. `Windows NT 11.0` in UA — Windows 11 uses `NT 10.0` (Microsoft kept it)
  2. `Connection: keep-alive` header — illegal over HTTP/2, bot signal
  3. `Accept` missing `image/png,image/svg+xml` — Firefox includes these
  4. `Accept-Encoding` missing `zstd` — Firefox 138+ supports zstd
  5. `Accept-Language` quality factors wrong — used `q=0.9,0.8,...` instead of Firefox's `q=0.5` (for 2 langs)
  6. Missing `TE: trailers` header — Firefox sends this
  7. `Cache-Control: max-age=0` not needed for initial navigation — Firefox doesn't send this on first load
- Fix: All six issues fixed in both stealth_tls.go and stealth_default.go, plus identity profile data
- Prevention: Compare headers against real Firefox DevTools network tab before shipping

## 2026-04-06 coordinator -- Agent dispatch failed (Skill tool does not resolve subagent types)
- Root cause: Attempted to dispatch `dev-squad:architect`, `dev-squad:reviewer`, `dev-squad:backend` via Skill tool but these are Agent tool identifiers, not Skills
- Fix: Performed the audit directly as coordinator instead of dispatching
- Prevention: In environments without the Agent tool, coordinator must perform the work directly or use a different dispatch mechanism

## 2026-04-06 coordinator -- Logic audit: initially misdiagnosed P0-1 as deadlock
- Root cause: Failed to trace `defer p.mu.Unlock()` through the full fallback path in `GetForGeo`, initially concluded it was a deadlock when it was actually a fragile-but-functional unlock/relock pattern
- Fix: Re-read the exact code line by line and traced lock state through all paths before finalizing the report
- Prevention: Always trace defer statements to their execution point (function return), not where they appear in source order

## 2026-04-06 coordinator -- Captcha soft-block fix was too aggressive
- Root cause: Removed "blocked" and "forbidden" keywords entirely from soft-block detection, but existing tests expected them to still trigger when no normal page structure is present
- Fix: Kept all keywords but added structural check (no <nav, <footer, <main) as additional signal
- Prevention: Always run tests after each fix, not just at the end

## 2026-04-06 coordinator -- Performance audit: body-copy pattern is pervasive
- Root cause: Multiple packages (captcha/detect, middleware/blocked, parse/metadata) each independently call `string(resp.Body)` and `strings.ToLower()` creating 200KB+ of garbage per request
- Fix: Documented as H1+H2+M7-M9 in performance audit; need unified body scanning or cached lowercase body
- Prevention: When adding response body inspection, check if another layer already does it; consider adding a cached `LowerBody()` method on Response

## 2026-04-06 coordinator -- Performance fixes: DomainStats fields changed from values to atomics
- Root cause: Changing DomainStats fields from plain int64 to atomic.Int64 and AvgLatency/AvgProcessLatency from fields to methods broke tests that accessed them as fields
- Fix: Updated engine_test.go to call AvgProcessLatency() as a method
- Prevention: When changing struct fields to methods, always grep for all usage sites including test files before considering the change complete

## 2026-04-06 coordinator -- Performance fixes: circuitbreaker outcome.at removal
- Root cause: Removed `at time.Time` from outcome struct but forgot to update the struct literal in record()
- Fix: Removed `at: time.Now()` from the struct literal
- Prevention: When removing struct fields, always search for all initialization sites

## 2026-04-06 coordinator -- Xvfb "virtual" mode was a no-op in Go code
- Root cause: `headless: "virtual"` was documented as using Xvfb but Go code treated it identically to `headless: "true"` (native headless). Only the Docker entrypoint managed Xvfb, with no crash recovery, health monitoring, or cleanup.
- Fix: Created `fetch/display.go` — a proper Xvfb lifecycle manager with dynamic display allocation, crash monitoring/restart, stale lock cleanup, and /dev/shm validation. Wired into CamoufoxFetcher.
- Prevention: When adding a feature mode (like "virtual"), implement the actual behavior in Go code rather than relying on external shell scripts

## 2026-04-07 coordinator -- Browser timeout config not wired through in run.go
- Root cause: `cmd/foxhound/run.go` constructed CamoufoxFetcher without passing `WithBrowserTimeout(cfg.Fetch.Browser.Timeout.Duration)`, so user config was silently ignored
- Fix: Added `fetch.WithBrowserTimeout(cfg.Fetch.Browser.Timeout.Duration)` to camoufox options
- Prevention: When adding config fields, always grep for where the config is consumed and verify it's actually passed through

## 2026-04-07 coordinator -- Navigation used networkidle wait event causing slow/timeout on Google SERP
- Root cause: Default `WaitUntilStateNetworkidle` waits for all network activity to stop, which can take 10+ seconds on ad-heavy pages like Google SERP. With extension loaded, `WaitUntilStateLoad` was used which is also slow. Both eat into the navigation timeout budget.
- Fix: Changed default to `WaitUntilStateDomcontentloaded` (sufficient for content scraping). Added retry-with-escalation: if navigation times out, retry with 2x timeout and `domcontentloaded`
- Prevention: For scraping, `domcontentloaded` is almost always sufficient — the DOM is ready even if third-party scripts haven't finished

## 2026-05-02 coordinator -- v0.0.19 single-shot test methodology missed sustained-request pin failures
- Root cause: v0.0.19 end-to-end verification used single-shot requests (one request per target). azuretls DefaultPinManager only fails on the SECOND+ request to the same host within a session — the first request opens an extra handshake to capture the SPKI pin and always succeeds. Multi-edge CDN targets (Bing, Google, Cloudflare) rotate certificates across edges, so the second request often lands on a different edge with a different SPKI, triggering "pin verification failed for <host>".
- Fix: Set `sess.InsecureSkipVerify = true` by default in `NewStealth` (disables PinManager); add `WithStrictTLSVerify()` opt-in. See v0.0.20.
- Prevention: When testing TLS scraping fixes against multi-edge targets (Bing/Google/CF), make at least 2 sequential requests to the same host within the same session before declaring the fix correct. Single-shot testing is insufficient to surface PinManager-class failures. Source: v0.0.20 retrospective.

## 2026-04-29 backend -- azuretls ApplyHTTP2 bypasses browser-aware HTTP/2 defaults (issue #41)
- Root cause: `Session.ApplyHTTP2(fp)` calls `getDefaultHTTP2Transport()` which is NOT browser-aware (unlike `initHTTP2(browser)`). `applyPseudoHeaders` ends with `tr.HeaderPriority = defaultHeaderPriorities("")` — empty browser string. Once `HTTP2Transport != nil`, the lazy `initTransport(browser)` SKIPS `initHTTP2(browser)`, so `Priorities`, `SettingsOrder`, `ConnectionFlow`, and `HeaderPriority` fall to generic defaults. Pairing `WithJA3(firefox)` + `WithHTTP2Fingerprint(firefox)` produces JA3-says-Firefox / HTTP2-is-half-generic mismatch that Akamai-class validators detect and terminate with `tls: illegal parameter` (CDN obfuscation — alert misleading).
- Fix: `WithIdentity` only sets `session.Browser` (e.g. "firefox") and lets azuretls's built-in `GetLastFirefoxVersion` produce the ClientHello at request time — empirically the built-in spec works on Bing/DDG (verified through proxy) while our captured JA3 from `presets.FirefoxLatest()` was rejected with `tls: illegal parameter`. Captured strings drift faster than this repo updates; built-in is more current. The HTTP/2 layer is left to azuretls's browser-aware `initHTTP2(browser)`. Removed v0.0.18 multi-browser bundles (`ChromeLatest`/`SafariLatest`/`All`/`JA3Pool`) — they violated the project's "Camoufox only" stance and let users compose Firefox headers + Chrome TLS by accident. Manual `WithJA3 + WithHTTP2Fingerprint` pairing kept for power users with a `slog.Warn` at NewStealth that links the issue.
- Earlier rejected approach: tried auto-applying `presets.FirefoxLatest().JA3` inside `WithIdentity`. Failed end-to-end smoketest — bare azuretls Firefox passed Bing/DDG, but applying our preset string broke them. Lesson: don't assume curated capture > library default; the library default is updated more often than this repo.
- Prevention: When wrapping a third-party library that exposes setter methods for related state (TLS spec, HTTP/2 fingerprint), do not surface them as independent options if the library does not compose them safely. Equally important: when CLAUDE.md says "X only" and the code ships "X, Y, Z", question the code, not the principle. Don't preserve API surface that contradicts the project's stated design just because it was already there.

## 2026-05-17 backend — LocalePolicy override silently lost when geo-resolver runs before policy check
- Root cause: `applyGeoToConfig()` (called by `WithCountry`/`WithProxy`) mutates `cfg.locale` to the geo-derived value. The LocalePolicy check `if cfg.localePolicy == LocalePolicyEnglishDefault && cfg.locale == ""` ran after geo resolution, so `cfg.locale` was already "ru-RU" and the policy was skipped. `TestLocalePolicy_EnglishDefault_WithCountryRU` failed with Locale=ru-RU, want en-US.
- Fix: Added `localeExplicit bool` to `generateConfig`. Set ONLY by `WithLocale()`. Changed policy guard from `cfg.locale == ""` to `!cfg.localeExplicit`. This correctly distinguishes "user set locale explicitly" from "geo-resolver set locale automatically". `WithLocale("ja-JP")` still wins; `WithCountry("RU")` + policy correctly yields en-US locale with proxy-matched timezone.
- Prevention: When a config struct has both explicit-user fields and auto-resolved fields of the same name, always track explicitness with a separate boolean flag. Never use the presence/absence of the resolved value as a proxy for intent — later pipeline stages can populate it before your gate runs.

## 2026-05-17 backend — examples/social build failures are pre-existing stale code, not caused by v0.0.25
- Root cause: `examples/social/main.go` references old API signatures (wrong NewStealth args, removed export.NewJSONLinesWriter, etc.). This was already documented in 2026-04-07 gotchas entry.
- Fix: Build only the affected packages (`./fetch/... ./captcha/... ./behavior/... ./identity/...`) when verifying new changes, not `./...` with the playwright build tag. The `go test ./...` (without playwright tag) correctly excludes the stale example via build constraints.
- Prevention: Stale examples are not blocked by tests — they only break `go build -tags playwright ./...`. Before any release, build the four core packages explicitly rather than full-tree with playwright tag. Add a note in the release checklist.

## 2026-05-19 coordinator — CAMOU_CONFIG Accept-Language key uses "headers." prefix, not bare key name
- Root cause: The `reference_camoufox.md` memory file listed the Accept-Language config key as just `"Accept-Language"`. The actual Camoufox dot-path schema uses `"headers.Accept-Language"` (with `headers.` prefix). Using the bare form causes `camoufox.exceptions.UnknownProperty: Unknown property Accept-Language in config` at runtime.
- Fix: Changed `config["Accept-Language"]` to `config["headers.Accept-Language"]` in `identity/profile.go` and all 4 test references in `identity/identity_test.go`. Confirmed via `camoufox.utils._load_properties()` which returns `{'headers.User-Agent': 'str', 'headers.Accept-Language': 'str', 'headers.Accept-Encoding': 'str'}`.
- Prevention: When adding any new CAMOU_CONFIG key, verify the exact key name via `camoufox.utils._load_properties()` in Python before writing Go code. The `reference_camoufox.md` memory file is incomplete — treat it as a starting point, not ground truth, for Camoufox config property names.

## 2026-05-19 coordinator — "same signal observed on two targets" does not imply "same fix closes both"
- Root cause: Phase 6 HAR captures showed frankenlocale Accept-Language (`en-DE`, `fr-DE`) on BOTH Google and Walmart. The Accept-Language fix was framed as closing both gaps. v0.0.26 proved this WRONG for Walmart: Accept-Language is now clean (`en-US,en;q=0.9`) at the wire level, but Walmart's PerimeterX block is unchanged at 16 requests. PerimeterX uses multiple independent signals — the Accept-Language fix is necessary but not sufficient.
- Fix: Ship with documented gap. Add known-open-gap note to CHANGELOG. Create future investigation tracker at `.dev-squad/v0.0.27-walmart-perimeterx-investigation.md`.
- Prevention: When the same anomalous signal appears across multiple targets, analyze whether those targets use the SAME anti-bot logic. Google's pre-WAF filter reads Accept-Language in isolation; PerimeterX uses Accept-Language + behavioral signals + browser fingerprint + session warmup. A fix that closes the signal doesn't necessarily close the bot detection. For each target, identify the full signal stack it uses before claiming a fix will close it.

## 2026-05-19 coordinator — Diagnostic tools must be archived once the diagnosis is locked
- Root cause: Used a dual-browser comparison harness as a diagnostic to find Camoufox-side bugs (Phase 6 — identified the Accept-Language frankenlocale). Useful and approved for that one-time purpose. But continued reusing the dual-browser phases (phase6_har.py, audit.py parity run) for v0.0.26 verification AFTER the diagnosis was locked — which violates the "Camoufox only" stance in spirit even though the shipped foxhound code stays Camoufox-only. The diagnostic tool became a habit instead of being archived.
- Fix: Added `README.md` to `.dev-squad/cloakbrowser-audit/` stating the diagnostic is archived and the comparison phases must not be re-run. Created `harness/verify_v26.py` as the Camoufox-only replacement. All future verification uses only `verify_v26.py` or equivalent Camoufox-only scripts.
- Prevention: When a project constraint says "<X> only" (e.g., Camoufox only), the principle applies to ALL active workflows including audit/diagnostic tools, not just the shipped code. Diagnostic exceptions to a constraint must have a defined END condition. Once the diagnosis is locked, archive the diagnostic tool immediately. Don't reuse it just because it's familiar and convenient.

## 2026-05-19 coordinator — v0.0.25 confirmation bias: all 3 audit-predicted fixes failed empirical acceptance
- Root cause: The CloakBrowser-vs-Camoufox audit observed CloakBrowser winning on Google email dorks and PerimeterX-protected sites. The audit's verdict section CONFIDENTLY ATTRIBUTED these wins to three causes: (a) Camoufox leaks local WebRTC IP, (b) Camoufox's `geoip=True` causes non-English `Accept-Language` that Google flags, (c) Camoufox lacks a press-hold solver for PerimeterX. v0.0.25 shipped 3 fixes targeting exactly those hypotheses. Phase 5 then ran each fix against its predicted gap. ALL THREE HYPOTHESES WERE WRONG: (a) Camoufox already obfuscates local IP by default — the screenshot-reading that "Camoufox leaks 192.168.x.x" was wrong; (b) Camoufox with en-US locale STILL gets Google's "About this page" block, so locale wasn't the cause; (c) PerimeterX rejects naive press-and-hold (detection works, mouse trajectory + hold duration work, but PX requires more — likely mouse trajectory shape, microtimings across the full session, or browser-engine-level signals). v0.0.25 ships clean code that doesn't deliver its stated production value.
- Fix: Update `.dev-squad/cloakbrowser-audit/audit-report.md` Phase 5 section with empirical results; mark Fix 1 redundant, Fix 2 + Fix 3 ineffective. v0.0.25 is not reverted (code is correct, options are useful for OTHER cases, no regressions). Plan Phase 6 deep investigation: capture TLS ClientHello bytes + HTTP request waterfalls (HAR) for both browsers on the same target, byte-level diff to find the real discriminator. Until that completes, the Camoufox production gap on Google email dorks and PerimeterX targets remains open.
- Prevention: When an audit explains an observed result, verify the explanation EMPIRICALLY before shipping code that depends on it. The right sequence is: (1) observe gap, (2) hypothesize cause, (3) **test hypothesis in isolation** (e.g., apply just the proposed mechanism to a control browser and confirm the gap closes), (4) THEN implement the fix in production code. v0.0.25 skipped step 3 — the audit verdict became the spec without an intermediate validation. Add to release checklist: "For audit-derived fixes, every Fix must have an isolated test that proves the proposed mechanism closes the gap BEFORE the fix is implemented in production code." If isolated test fails, return to step 2 (re-hypothesize) instead of shipping. This is the same lesson as the gotcha 2026-04-29 (issue #41) — "don't assume curated capture > library default" — generalized to "don't assume diagnosis > raw observation".

## 2026-05-28 coordinator — v0.0.24 renegotiation fix was a NO-OP: ModifyConfig overwritten by ApplyPreset (issue #43 still live through v0.0.26)
- Root cause: v0.0.24 "fixed" the utls v1.3.20 renegotiation panic (issue #43) by setting `sess.ModifyConfig = func(c *utls.Config){ c.Renegotiation = RenegotiateNever }` in `NewStealth`. This NEVER took effect. azuretls connection builders call `ModifyConfig` (connection.go:113) BEFORE `ApplyPreset(spec)` (connection.go:139). `ApplyPreset` → `ApplyConfig` (utls u_conn.go:483) iterates the spec's extensions and `RenegotiationInfoExtension.writeToUConn` (utls u_tls_extensions.go:1687) executes `uc.config.Renegotiation = e.Renegotiation`, where `e.Renegotiation` is the SPEC's value — azuretls's Firefox preset `GetLastFirefoxVersion` hardcodes `RenegotiateOnceAsClient` (profiles.go:467). Net: `config.Renegotiation` ends as `RenegotiateOnceAsClient` regardless of `ModifyConfig`. On the first renegotiation (`handshakes==1`), `handleRenegotiation` (utls conn.go:1287) does NOT bail for `RenegotiateOnceAsClient` (only bails when `handshakes > 1`) → `clientHandshake → loadSession → onEnterLoadSessionCheck` panic. The panic is in fhttp's `persistConn.readLoop` goroutine (library-spawned) → CANNOT be `recover()`'d → process crash. Confirmed in production: a deployment on foxhound v0.0.24 ("fixes #43") kept panicking on concurrent enrich load with the identical stack; v0.0.25/v0.0.26 never touched renegotiation, so v0.0.26 shipped the same no-op. The v0.0.24 regression test (`TestNewStealth_ModifyConfig_SetsRenegotiateNever`) passed because it called `ModifyConfig` on a bare `&utls.Config{}` and never exercised `ApplyPreset`.
- Fix (v0.0.27): wrap `sess.GetClientHelloSpec` in `NewStealth` (installed AFTER JA3 option handling so it chains any `WithJA3`-set spec) to set every `*utls.RenegotiationInfoExtension`'s `.Renegotiation = RenegotiateNever`. This is the value `ApplyPreset` reads via `writeToUConn`, so it survives. Covers default preset + `WithJA3` spec + all three azuretls dial paths (connection.go:121 / pinner.go:132 / proxy.go:502) which all read `GetClientHelloSpec`. Fingerprint unchanged — the `Renegotiation` enum is internal-only and never marshaled: `RenegotiationInfoExtension.Read()` (u_tls_extensions.go:1648) emits the extension unconditionally, and the custom-hello marshaller `MarshalClientHelloNoECH` (u_conn.go:606) builds from the extension list, not from `SecureRenegotiationSupported`. Wire-verified byte-identical (`renegotiation_info` body `ff01000100`). New regression test `TestNewStealth_GetClientHelloSpec_ForcesRenegotiateNever` asserts the spec-level value for default + JA3 paths and was proven to FAIL when the wrapper is neutralized.
- Prevention: (1) When a library applies layered config (a hook PLUS a preset), find the LAST writer of the field. A "set it via the hook" fix is worthless if a later preset-application overwrites it — read the library's apply-order, don't assume the hook wins. Grep the dependency for every assignment to the field (`config.Renegotiation =`) before declaring a config-hook fix correct. (2) A regression test MUST exercise the REAL code path, not the mechanism in isolation. The v0.0.24 test asserted `ModifyConfig`'s effect on a fresh config but never ran `ApplyPreset` — so it green-lit a no-op for three releases. Test the observable end-state (here: the spec the connection actually uses), and prove the test FAILS without the fix. (3) For panics in dependency-spawned goroutines, `recover()` is impossible — the fix MUST prevent reaching the panic. (4) Same family as gotcha 2026-05-20 (isolated test passes, real path fails under concurrency) and 2026-05-19 (verify the mechanism empirically before trusting the fix).
