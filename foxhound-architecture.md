# FOXHOUND — Architecture Document
### Go Scraping Framework with Native Camoufox Anti-Detection
### Version 0.1 Draft — March 2026

---

## 1. Executive Summary

Foxhound is a Go scraping framework yang dirancang untuk satu tujuan: scrape website
yang dilindungi anti-bot modern, dengan performa Go dan stealth Camoufox, tanpa overhead
percuma.

Differentiator utama:
- Camoufox (Firefox fork) dijalankan NATIVE via playwright-go — bukan WebSocket, bukan
  Python sidecar. Pipeline identik dengan Scrapemate/Colly yang launch Chromium.
- Dual-mode fetching: nethttp + TLS impersonation untuk static pages, Camoufox untuk
  JS-heavy/protected pages.
- Anti-detection di SEMUA layer: TLS, browser fingerprint, behavioral, contextual.
- Human behavior simulation bukan afterthought — itu core feature.

---

## 2. Threat Model: Bagaimana Anti-Bot Mendeteksi Scraper

Sebelum bicara solusi, kita harus paham musuhnya. Anti-bot modern (Cloudflare,
DataDome, Akamai, PerimeterX/HUMAN, GeeTest, Kasada) mendeteksi di 6 layer.

### Layer 1: TLS/Network Fingerprinting
KAPAN: Saat TCP handshake — sebelum HTTP request dikirim.
APA: TLS ClientHello message berisi cipher suites, extensions, elliptic curves, ALPN
values. Ini di-hash jadi JA3/JA4 fingerprint.

BAGAIMANA DETECT:
- Go net/http punya JA3 yang BERBEDA dari browser (13 cipher suites vs Chrome 16,
  12 extensions vs 18, tanpa GREASE values, tanpa post-quantum key exchange).
- Python requests punya JA3 yang sudah ada di blocklist Cloudflare.
- Bahkan sebelum HTTP header dibaca, TLS fingerprint sudah reveal bahwa ini bukan browser.
- JA4 (successor JA3) lebih canggih: include ALPN, SNI, distinguish TCP vs QUIC.
- Akamai extend dengan HTTP/2 SETTINGS frame fingerprinting.
- Deteksi terjadi dalam milidetik pertama koneksi.

REFERENSI: Paper "FP-Inconsistent" (ACM IMC 2025) — across 500K requests dari 20 bot
services, average evasion rate hanya 52.93% vs DataDome dan 44.56% vs BotD. Masalah
utama: fingerprint attributes yang di-alter sering INCONSISTENT satu sama lain.

### Layer 2: Browser Fingerprinting
KAPAN: Saat JavaScript di-execute di page.
APA: Ratusan browser attributes dikumpulkan dan di-combine jadi device fingerprint.

ATTRIBUTES YANG DICEK:
- Canvas rendering output (GPU + driver specific)
- WebGL renderer, vendor, supported extensions, shader precision formats
- AudioContext processing (audio waveform fingerprint)
- Font enumeration dan metrics
- Navigator properties (platform, hardwareConcurrency, deviceMemory, languages)
- Screen properties (width, height, colorDepth, pixelRatio, availWidth)
- Window properties (screenX, screenY, outerWidth, outerHeight)
- Battery API
- Media devices (webcam, microphone count)
- Performance API timing
- Math operation results (minor floating point differences antar engine)
- Plugin list, MIME types
- Timezone vs locale vs geolocation consistency

BAGAIMANA DETECT:
- Headless browser punya fingerprint yang BERBEDA dari normal browser.
- navigator.webdriver = true (Playwright, Puppeteer default).
- Missing browser APIs yang real browser punya.
- Canvas/WebGL output yang identik across sessions = bot fleet.
- Inconsistency: claim Windows tapi GPU renderer = "ANGLE (Apple)" = instant block.

### Layer 3: Automation Detection
KAPAN: Saat page load dan selama interaksi.
APA: Cek apakah browser dikontrol oleh automation tool.

YANG DICEK:
- window.__playwright__binding__ dan similar injected globals
- Playwright/Puppeteer injects JavaScript ke page untuk query elements, evaluate JS,
  run init scripts — semua bisa terdeteksi.
- Stack trace analysis — automation tools punya call stack patterns unik.
- CDP (Chrome DevTools Protocol) artifacts di Chromium.
- Getter hijacking — page override Object.getOwnPropertyDescriptor untuk detect
  kalau property dibaca oleh automation.
- MutationObserver untuk detect DOM manipulation dari automation.
- Chromium-specific: CDP command patterns, debugger interface exposure.

### Layer 4: Behavioral Analysis
KAPAN: Selama user berinteraksi dengan page.
APA: Pattern interaksi dibandingkan dengan statistical model of human behavior.

SIGNALS YANG DICOLLECT:
Client-side:
- Mouse movements: trajectory, speed, acceleration, curvature, micro-movements
- Mouse clicks: timing between clicks, click precision, double-click speed
- Scroll behavior: scroll speed, scroll distance, scroll direction changes
- Keystroke dynamics: inter-key delay, hold time, typing rhythm
- Touch events & swipe patterns (mobile)
- Page interaction sequence: order of elements interacted
- Time on page, time between actions
- Tab visibility changes (did user switch tabs?)
- Focus/blur events

Server-side:
- Request timing patterns: uniform intervals = bot, random intervals = human
- Session duration dan depth
- Navigation pattern: realistic browsing path vs direct deep links
- API call sequences dan velocity
- Pages per session distribution

BAGAIMANA DETECT:
- Bots: linear mouse movement, constant scroll speed, precise clicks exactly on
  element center, zero idle time, uniform request intervals.
- Humans: erratic mouse movement with micro-corrections, variable scroll speed with
  pauses, clicks slightly off-center, idle time between actions, non-uniform intervals.
- ML models trained on millions of real human sessions vs bot sessions.
- Even "humanized" bots seringkali terdeteksi karena randomization distribution-nya
  TERLALU uniform (real randomness itu clumpy/bursty, bukan smooth uniform).

### Layer 5: Reputation & Contextual
KAPAN: Sebelum dan selama request.
APA: Cross-reference external signals.

SIGNALS:
- IP reputation: datacenter IP vs residential, ASN blacklists, VPN/proxy detection
- Geolocation consistency: IP location vs timezone vs locale vs Accept-Language
- Time context: e-commerce traffic at 3 AM local time = suspicious
- Request patterns: same fingerprint hitting 1000 pages = suspicious
- Historical behavior: fingerprint/IP previously associated with abuse
- Cookie/session anomalies: fresh session every request vs realistic session persistence

### Layer 6: Challenge-Response (CAPTCHA, PoW)
KAPAN: Ketika score dari layer 1-5 mencapai threshold.
APA: Challenge yang harus diselesaikan untuk buktikan human.

JENIS:
- Cloudflare Turnstile: invisible challenge, behavioral scoring
- reCAPTCHA v3: invisible scoring based on interaction patterns
- hCaptcha: image classification
- GeeTest: interactive puzzle + behavioral analysis
- Kasada: Proof-of-Work yang butuh real compute (bikin bot mahal)
- Custom JavaScript challenges

IMPLIKASI: Goal bukan solve CAPTCHA — goal adalah JANGAN PERNAH trigger CAPTCHA.
Kalau sampai kena CAPTCHA, kita sudah gagal di layer sebelumnya.

---

## 3. Defense Strategy: Layer-by-Layer

### 3.1 Layer 1 Defense: TLS Fingerprinting

MASALAH: Go net/http punya TLS fingerprint yang langsung ketahuan bukan browser.

SOLUSI DUAL-MODE:

A) Static pages (NeedJS=false): pakai Go HTTP client dengan TLS impersonation.
   Library yang sudah ada dan proven:
   - enetx/surf — Go HTTP client yang impersonate Chrome v145 dan Firefox v148,
     full JA3/JA4 customization, HTTP/2 + HTTP/3 over QUIC fingerprinting,
     automatic header ordering.
   - Danny-Dasilva/CycleTLS — spoof JA3/JA4 fingerprint di Go dan JS, support
     HTTP/2 fingerprinting juga.
   - Noooste/azuretls-client — auto-mimic Chrome TLS + HTTP/2 fingerprint.

   Ini menggantikan nethttp fetcher standar. Bukan cuma set User-Agent header,
   tapi literally replica TLS handshake + HTTP/2 settings + header ordering dari
   browser tertentu. Deteksi di level TLS = solved.

B) JS-rendered pages (NeedJS=true): Camoufox (Firefox).
   TLS handshake dilakukan oleh Firefox binary itu sendiri — bukan Go.
   Artinya TLS fingerprint = real Firefox fingerprint. Automatic match.
   Nggak perlu impersonation karena ini IS the browser.

CONSISTENCY RULE:
- User-Agent HARUS match dengan TLS fingerprint.
- Claim Firefox 148 di UA tapi JA3 = Chrome? Instant block.
- surf/CycleTLS sudah handle ini — pilih browser profile, semua match.
- Camoufox: automatic — UA, TLS, everything = real Firefox.

### 3.2 Layer 2 Defense: Browser Fingerprinting

MASALAH: Automation tools punya fingerprint berbeda dari real browser. Kalau semua
instance punya fingerprint identik, itu fleet pattern = block.

SOLUSI:

Camoufox handle ini di level C++ Firefox source code:
- SEMUA navigator properties di-spoof SEBELUM JavaScript bisa baca.
- Canvas, WebGL, AudioContext — output di-modify di rendering pipeline, bukan
  via JS injection (yang bisa dideteksi).
- Font list di-spoof berdasarkan target OS.
- Screen/viewport properties consistent dengan fingerprint.
- WebRTC IP spoofing di protocol level (bukan JS override).
- BrowserForge generate fingerprints yang mimic distribusi statistik real devices.

YANG KITA HANDLE DI GO (fingerprint generator):

```
FingerprintProfile:
  OS:           "windows" | "macos" | "linux"  
  Browser:      "firefox"
  Version:      "148.0"
  
  Screen:
    Width:      1920         # dari database common resolutions
    Height:     1080         # match OS (Mac retina = higher)
    ColorDepth: 24
    PixelRatio: 1.0          # Mac = 2.0

  Hardware:
    Cores:      8            # realistic for claimed OS
    Memory:     8            # GB, realistic
    GPU:        "Intel(R) UHD Graphics 630"  # match OS
    Platform:   "Win32"      # match OS

  Locale:
    Languages:  ["en-US", "en"]
    Timezone:   "America/New_York"
    Locale:     "en-US"
    
  Geo (derived from proxy IP via GeoIP):
    Latitude:   40.7128
    Longitude:  -74.0060

CONSISTENCY RULES:
  - OS = Windows → Platform = "Win32", GPU = Intel/NVIDIA/AMD (not Apple)
  - OS = macOS → Platform = "MacIntel", GPU bisa Apple atau AMD
  - Timezone HARUS match Geo location
  - Languages HARUS plausible untuk Geo location
  - Screen resolution HARUS common (nggak 1337x420)
```

DATABASE: Maintain database 500+ real device profiles yang dicollect dari traffic
analytics. Random pick, tapi SATU profile dipakai SATU session. Jangan mix
attributes dari profile berbeda — itu yang bikin inconsistency.

ROTATION STRATEGY:
- Satu fingerprint per Camoufox browser instance.
- Setelah N requests (configurable, default 200-500), rotate: kill instance,
  spawn baru dengan fingerprint baru.
- Nggak rotate per-page — itu suspicious (real user nggak ganti device tiap page).
- Rotate bersamaan dengan rotate proxy, biar nggak ada "device" yang tiba-tiba
  pindah lokasi.

### 3.3 Layer 3 Defense: Automation Detection

MASALAH: Playwright inject JavaScript ke page. CDP (Chromium) super detectable.

SOLUSI:

Camoufox solve ini secara arsitektural:
- Pakai Juggler protocol (bukan CDP). Juggler itu custom protocol untuk Firefox,
  beroperasi di level lebih rendah, dan JAUH less known oleh anti-bot.
- Playwright's internal page agent code di-sandbox dan isolasi. Page script
  TIDAK BISA detect presence of Playwright via JavaScript inspection.
- Actions (click, type, evaluate JS) di-handle lewat isolated scope — page
  lihat sebagai native user input, bukan automation command.
- Firefox headless mode di-patch supaya identical dengan normal windowed mode.
- navigator.webdriver di-set false (dan bukan via JS override yang bisa dideteksi,
  tapi di C++ level).

CHROMIUM COMPARISON (kenapa kita nggak pakai):
- CDP adalah protocol yang TERKENAL — semua anti-bot tau cara detect.
- Chrome punya artifacts yang Chromium nggak punya — detectable.
- Chrome source code closed — patching sulit.
- Puppeteer stealth plugin? JS-level patches yang bisa di-reverse detect.
- Playwright Chromium: navigator.webdriver masih bisa di-detect via getter trap.

FIREFOX ADVANTAGE (via Camoufox):
- Juggler lebih low-level dan less targeted oleh anti-bot.
- Firefox open source — full C++ patching possible.
- Spidermonkey engine behavior berbeda dari V8 — beberapa WAF check engine
  behavior, dan Camoufox retain real Spidermonkey behavior.
- Less market share in automation = less targeted by detection.

### 3.4 Layer 4 Defense: Human Behavior Simulation

Ini LAYER PALING PENTING dan PALING SERING di-underestimate. Fingerprint sempurna
tapi behavior robotic? Still blocked.

#### 3.4.1 Mouse Movement

MASALAH: Linear movement, instant teleportation, perfectly centered clicks.

SOLUSI:
Camoufox punya C++ level human mouse movement (ported dari riflosnake/HumanCursor).
Tapi ini bisa di-augment dari Go side via BrowserActions.

```
Movement Model:
1. Bezier Curve Trajectory
   - Control points random offset dari straight line
   - 3-5 control points per movement
   - Speed varies: slow at start, fast middle, slow at end (bell curve)

2. Micro-movements
   - 1-3px random jitter SELAMA movement
   - Occasional pause mid-movement (50-200ms)
   - Overshoot target by 2-5px, lalu correct back (20% probability)

3. Idle Drift
   - Mouse idle tapi ada micro-drift 1-2px setiap 2-5 detik
   - Real humans nggak bisa hold mouse perfectly still

4. Click Behavior
   - Click slightly off-center: random offset 0-5px dari element center
   - Click delay after movement: 100-300ms (human reaction time)
   - Occasional double-click pada single-click target (2% probability)
   - Mouse-down to mouse-up duration: 50-150ms (bukan instant 0ms)
```

#### 3.4.2 Scroll Behavior

```
Scroll Model:
1. Reading Scroll
   - Scroll 300-800px per gesture
   - Pause 1-5 seconds between scrolls (reading time)
   - Speed bervariasi setiap scroll
   - Occasional scroll UP (re-read section)

2. Quick Scan Scroll
   - Scroll 1000-3000px per gesture
   - Short pause 0.3-1s between scrolls
   - Dipakai untuk scan halaman yang nggak menarik

3. Scroll Deceleration
   - Scroll speed menurun menjelang target element
   - Bukan stop instant — ada momentum decay

4. Mobile Touch Scroll (kalau emulate mobile)
   - Touch-and-drag gesture
   - Fling with deceleration
   - Occasional overscroll + bounce
```

#### 3.4.3 Timing Patterns

INI YANG PALING KRITIKAL. Anti-bot ML models trained untuk detect timing patterns.

```
WRONG (Detectable):
  uniform random: delay = random(1, 3) seconds
  → distribusi uniform itu BUKAN how humans behave
  → real randomness itu bursty/clumpy

RIGHT (Human-like):
  1. Log-normal distribution untuk inter-action delays
     - Banyak short delays (quick clicks)
     - Occasional long delays (reading, thinking, distracted)
     - Formula: delay = exp(mu + sigma * normal_random())
     - mu = 1.0, sigma = 0.8 → median ~2.7s, mean ~4.1s, 95th ~13s
  
  2. Session rhythm
     - Burst: 5-15 rapid actions (navigating to content)
     - Pause: 10-60 seconds (reading/analyzing)
     - Burst: 3-8 actions (next set of pages)
     - Long pause: 1-5 minutes (break/distracted)
  
  3. Context-dependent delay
     - After page load: 3-15 seconds (reading)
     - Between pagination clicks: 0.5-2 seconds (fast)
     - After search: 5-20 seconds (analyzing results)
     - Form filling: 50-200ms per character (typing speed)
  
  4. Circadian pattern
     - Scraping speed bervariasi sepanjang hari
     - Slow di pagi hari, peak di siang, slow lagi malam
     - Occasional gaps (lunch, meetings)
```

#### 3.4.4 Navigation Patterns

```
WRONG:
  - Direct deep links ke semua product pages
  - Sequential: /product/1, /product/2, /product/3, ...
  - No homepage visit, no search interaction

RIGHT:
  1. Entry Points
     - Start dari homepage atau search engine (referer = google.com)
     - Navigate through category → subcategory → product
     - Use search feature occasionally

  2. Browsing Pattern
     - Visit 3-8 pages per "session"
     - Mix of deep pages dan shallow pages
     - Occasionally visit "useless" pages (about, contact, FAQ)
     - Back button usage (30% of navigations)

  3. Session Lifecycle
     - New session: unique fingerprint + proxy
     - Browse 10-30 minutes
     - End session (close browser context)
     - Gap: 5-30 minutes
     - New session: different fingerprint + different proxy

  4. Request Patterns
     - Load main HTML → load CSS/JS/images (normal browser behavior)
     - Camoufox handles ini otomatis karena dia real browser
     - Static fetcher: perlu emulate Accept, Accept-Encoding, Accept-Language
       headers yang consistent
```

#### 3.4.5 Cookie & Session Management

```
1. Accept Cookies
   - Kalau ada cookie consent banner → click Accept (via BrowserActions)
   - Store cookies across pages dalam satu session
   - Ini critical: site yang set cookies lalu check di page berikutnya,
     kalau nggak ada cookies = instant flag

2. Session Persistence
   - Satu BrowserContext = satu session (shared cookies, storage)
   - Jangan bikin new context per page
   - Session duration realistic: 10-60 minutes
   - After session rotate: new context, new fingerprint, new proxy

3. LocalStorage/SessionStorage
   - Real browser populate localStorage dengan site preferences
   - Camoufox handles ini automatically (real browser)
   - Static fetcher: nggak bisa simulate ini (limitation)
```

### 3.5 Layer 5 Defense: Reputation & Context

```
Proxy Strategy:
1. Residential Proxies (primary)
   - IP dari ISP residential, bukan datacenter
   - Rotate per-session (bukan per-request)
   - Match proxy location dengan fingerprint locale/timezone
   
2. Quality over Quantity
   - Jangan pakai 1 proxy untuk 1000 requests
   - Max 50-100 requests per proxy per session
   - Cooldown period: proxy yang sudah dipakai, istirahat 30-60 menit
   
3. Geo-consistency
   - Proxy di New York → timezone America/New_York → locale en-US
   - Proxy di London → timezone Europe/London → locale en-GB
   - Camoufox GeoIP feature auto-set timezone/locale dari proxy IP

Header Consistency:
   - Accept-Language HARUS match locale
   - DNT header consistent across session
   - Sec-Fetch-* headers correct untuk navigation type
   - Referer chain realistic (bukan empty referer ke deep page)
```

### 3.6 Layer 6 Defense: Challenge Avoidance

STRATEGY: Jangan pernah trigger challenge. Kalau semua layer di atas executed
dengan benar, CAPTCHA rate harus < 5%.

FALLBACK (kalau tetap kena):
- Cloudflare Turnstile: solvable karena ini behavioral — kalau behavior kita
  sudah human-like, challenge auto-pass.
- reCAPTCHA v3: score-based, invisible. Good behavior = high score = pass.
- hCaptcha/reCAPTCHA v2 visual: butuh solver service (2captcha, CapSolver).
  Ini mahal dan lambat — avoid as last resort.
- Proof-of-Work: biarkan browser solve (Camoufox = real browser with real compute).

---

## 4. System Architecture

### 4.1 Component Diagram

```
┌─────────────────────────────────────────────────────────┐
│                     FOXHOUND ENGINE                     │
│                                                         │
│  User Code:                                             │
│  ┌─────────────────────────────────────────────────┐    │
│  │ type MyJob struct { foxhound.Job }               │    │
│  │ func (j *MyJob) Process(ctx, resp) (data, jobs)  │    │
│  └────────────────────┬────────────────────────────┘    │
│                       │                                 │
│  Core:                ▼                                 │
│  ┌──────────┐  ┌──────────┐  ┌────────────────────┐    │
│  │ Scheduler │  │ Retry    │  │ Middleware Chain    │    │
│  │ (goroutine│  │ Engine   │  │ - Rate Limit       │    │
│  │  pool)    │  │          │  │ - Dedup            │    │
│  └────┬──────┘  └────┬─────┘  │ - Metrics          │    │
│       │              │        │ - Header Enrichment│    │
│       ▼              │        │ - Behavior Timing  │    │
│  ┌────────────────────▼───────┴────────────────────┐    │
│  │            Smart Fetcher (Router)                │    │
│  │                                                  │    │
│  │  Decide:                                         │    │
│  │  NeedJS=false → TLS Impersonation Client         │    │
│  │  NeedJS=true  → Camoufox Browser                 │    │
│  │  Blocked?     → Escalate to Camoufox             │    │
│  │                                                  │    │
│  │  ┌─────────────────┐  ┌──────────────────────┐   │    │
│  │  │ TLS Client      │  │ Camoufox Fetcher     │   │    │
│  │  │ (surf/azuretls) │  │ (playwright-go →     │   │    │
│  │  │                 │  │  Firefox native)      │   │    │
│  │  │ JA3/JA4 match   │  │                      │   │    │
│  │  │ HTTP/2 FP match │  │ C++ fingerprint spoof│   │    │
│  │  │ Header order    │  │ Juggler protocol     │   │    │
│  │  │ ~5-50ms/req     │  │ Human sim engine     │   │    │
│  │  │                 │  │ ~500ms-5s/req        │   │    │
│  │  └─────────────────┘  └──────────────────────┘   │    │
│  └──────────────────────────────────────────────────┘    │
│                       │                                 │
│  Output:              ▼                                 │
│  ┌──────────────────────────────────────────────────┐    │
│  │ Result Pipeline                                   │    │
│  │ CSV | JSON | PostgreSQL | Webhook | Custom        │    │
│  └──────────────────────────────────────────────────┘    │
│                                                         │
│  Support:                                               │
│  ┌────────────┐ ┌───────────┐ ┌───────────────────┐    │
│  │ Fingerprint│ │ Behavior  │ │ Proxy Manager     │    │
│  │ Database   │ │ Simulator │ │ (rotate + geo)    │    │
│  │ (500+ real │ │ (timing,  │ │                   │    │
│  │  profiles) │ │  mouse,   │ │ Residential pool  │    │
│  └────────────┘ │  scroll)  │ │ + cooldown        │    │
│                 └───────────┘ └───────────────────┘    │
└─────────────────────────────────────────────────────────┘
```

### 4.2 Data Flow

```
Request masuk:

1. Job dari Queue
   ↓
2. Middleware: rate limit check (per-domain)
   ↓
3. Middleware: dedup check (sudah pernah fetch?)
   ↓
4. Middleware: behavior timing (apply human-like delay)
   ↓
5. Middleware: enrich headers (UA, Accept-Language, Referer match fingerprint)
   ↓
6. Smart Fetcher decides:
   │
   ├── Static: TLS Client (surf)
   │   - JA3/JA4 impersonate Firefox/Chrome
   │   - HTTP/2 fingerprint match
   │   - Header ordering match
   │   - Proxy rotation
   │   ↓
   │   Response OK? → Parser → User Process()
   │   Response blocked? → Escalate to Camoufox ──┐
   │                                               │
   └── Browser: Camoufox ◄─────────────────────────┘
       - Launch/reuse Firefox instance (native via playwright-go)
       - Fingerprint already injected via CAMOU_CONFIG env vars
       - Navigate to URL
       - Human behavior simulation:
         · Wait for page load (networkidle)
         · Simulate mouse movement to viewport
         · Random scroll (read simulation)
         · Custom BrowserActions if defined
         · Cookie/consent handling
       - Extract content
       ↓
       Response → Parser → User Process()
                                ↓
                          Result{Data, NextJobs}
                                ↓
                   ┌────────────┴────────────┐
                   │                         │
              Data → Writers             NextJobs → Queue
              (CSV/JSON/DB)           (enqueue for processing)
```

---

## 5. Performance Deep Analysis

### 5.1 Throughput Targets

```
Mode           │ Target/worker │ 10 workers  │ Bottleneck
───────────────┼───────────────┼─────────────┼──────────────────
TLS Client     │ 20-100 req/s  │ 200-1000/s  │ Network I/O + rate limit
  (static)     │ 5-50ms/req    │             │ 
               │               │             │
Camoufox       │ 0.3-2 req/s   │ 3-20/s      │ Browser render + 
  (browser)    │ 500ms-5s/req  │             │ human simulation delay
               │               │             │
Mixed 80/20    │ ~16-80 req/s  │ 160-800/s   │ Effective throughput
               │ avg ~12-60ms  │             │ sangat tinggi karena
               │               │             │ mayoritas static
```

PERBANDINGAN:
- Scrapy (Python): ~50-200 req/s static, ~1-5 req/s browser (Playwright plugin)
- Crawlee (JS): ~30-150 req/s static, ~2-10 req/s browser (Playwright)
- Colly (Go): ~100-500 req/s static, no browser support
- Scrapemate (Go): ~100-400 req/s static, ~1-5 req/s browser (Chromium)

Foxhound TLS Client mode comparable dengan Colly/Scrapemate.
Foxhound Camoufox mode comparable dengan Scrapemate browser, tapi dengan
anti-detection yang jauh lebih baik.

### 5.2 Memory Budget

```
Component                  │ RAM
───────────────────────────┼──────────────
Go runtime + engine        │ 30-60 MB
TLS Client (connection pool)│ 20-50 MB
Fingerprint DB (in-memory) │ 5-10 MB
Playwright driver          │ 50-80 MB
Per Camoufox instance      │ 200-400 MB
  (Firefox process)        │

Configurations:
────────────────────────────────────────────
Static-only (no browser):   │ 50-120 MB total
  → Sangat ringan, bisa run di small VPS

1 Camoufox instance:        │ 330-590 MB total
  → Cukup untuk low-volume protected sites

4 Camoufox instances:       │ 930-1,750 MB total
  → Standard production setup

8 Camoufox instances:       │ 1,730-3,310 MB total
  → Heavy workload, butuh 4GB+ RAM
```

MITIGATION:
- Lazy loading: Camoufox instances HANYA di-spawn kalau ada request yang butuh.
- Instance pooling: reuse instances, jangan spawn-kill per request.
- Tab management: 1 tab per instance, close setelah done. Firefox memory leak
  kalau terlalu banyak tabs.
- Periodic restart: kill dan restart instance setiap 200-500 requests untuk
  prevent memory bloat.

### 5.3 CPU Usage

```
Component                    │ CPU Pattern
─────────────────────────────┼─────────────────────────
Go engine (scheduling, queue)│ Minimal (<5%)
TLS Client requests          │ Minimal (TLS handshake spike)
HTML parsing (goquery)       │ Moderate per-page (1-5ms)
Camoufox (Firefox rendering) │ HIGH during page load (50-100% core)
                             │ Idle after render complete
Human sim (mouse/scroll)     │ Negligible (<1%)

Recommendation:
- 1 Camoufox instance = 1 CPU core dedicated
- Static-only mode = 1 core handles 500+ req/s
- Mixed mode with 4 instances = 4-6 cores recommended
```

### 5.4 Network Consumption

```
Mode           │ Bandwidth/request │ 1000 requests
───────────────┼───────────────────┼──────────────
TLS Client     │ 50-500 KB         │ 50-500 MB
  (HTML only)  │ (no images/CSS)   │
               │                   │
Camoufox       │ 500 KB - 5 MB     │ 500 MB - 5 GB
  (full page)  │ (all resources)   │
               │                   │
Camoufox       │ 200 KB - 2 MB     │ 200 MB - 2 GB
  (block imgs) │ (block_images=T)  │

PROXY COST IMPACT:
Residential proxy = $5-15/GB.
1000 requests via Camoufox (full) = $2.50-75
1000 requests via Camoufox (block images) = $1-30
1000 requests via TLS Client = $0.25-7.50

→ block_images=true bisa save 50-70% proxy cost
→ TLS Client mode save 80-95% proxy cost vs full browser
→ Dual-mode fetching bukan cuma performance optimization, ini cost optimization
```

### 5.5 Effective Throughput: The Real Metric

Raw throughput nggak berarti apa-apa kalau 40% request kena block.

```
Scenario: 10,000 target pages, protected site (Cloudflare + DataDome)

┌───────────────┬──────────────┬───────────────────┬──────────────┐
│               │ Block Rate   │ Total Attempts    │ Wall Time    │
│               │              │ (incl retries)    │              │
├───────────────┼──────────────┼───────────────────┼──────────────┤
│ Colly         │ 90%+ (no     │ Mostly fails      │ Unusable     │
│ (raw HTTP)    │ anti-detect) │                   │              │
├───────────────┼──────────────┼───────────────────┼──────────────┤
│ Scrapemate    │ 40-60%       │ 25,000-40,000     │ 8-15 hours   │
│ (Chromium PW) │              │ (many retries)    │ (+ backoff)  │
├───────────────┼──────────────┼───────────────────┼──────────────┤
│ Foxhound      │ 5-15%        │ 11,000-13,000     │ 2-5 hours    │
│ (Camoufox)    │              │ (few retries)     │              │
├───────────────┼──────────────┼───────────────────┼──────────────┤
│ Foxhound      │ 1-5%         │ 10,100-10,500     │ 1.5-4 hours  │
│ (Camoufox +   │              │ (minimal retries) │              │
│  full human   │              │                   │              │
│  simulation)  │              │                   │              │
└───────────────┴──────────────┴───────────────────┴──────────────┘

On unprotected sites: semua framework performs similarly.
On protected sites: Foxhound effective throughput 3-5x lebih tinggi.
```

---

## 6. Resource Usage Summary

### 6.1 Minimum Production Setup

```
Target: scrape 5,000 pages/day dari protected sites

Hardware:
  - 2 vCPU, 4 GB RAM, 40 GB SSD
  - VPS: $20-40/month (Hetzner, DigitalOcean)

Software:
  - Go binary: ~30 MB disk
  - Camoufox binary: ~300 MB disk
  - Playwright driver: ~50 MB disk
  - Total disk: ~400 MB

Runtime:
  - 2 Camoufox instances (rotate per session)
  - ~1-1.5 GB RAM used
  - ~1.5 CPU cores average (spike during page render)

Proxy:
  - Residential: 5-10 GB/day @ $8/GB = $40-80/month
  - With block_images: 2-5 GB/day = $16-40/month
  - With dual-mode (80% static): 1-2 GB/day = $8-16/month

Total monthly: $68-136 (infra + proxy)
```

### 6.2 Scaling Comparison

```
                    │ Foxhound    │ Scrapy+PW  │ Crawlee
────────────────────┼─────────────┼────────────┼──────────
10K pages/day       │ 2C/4GB      │ 2C/4GB     │ 2C/4GB
  protected         │ $70-140/mo  │ $150-300   │ $120-250
                    │ (low block) │ (high      │ (medium
                    │             │  retry)    │  retry)
────────────────────┼─────────────┼────────────┼──────────
100K pages/day      │ 4C/8GB      │ 8C/16GB    │ 8C/16GB
  protected         │ $200-400/mo │ $600-1200  │ $500-1000
                    │             │ (proxy     │
                    │             │  burn)     │
────────────────────┼─────────────┼────────────┼──────────
1M pages/day        │ 16C/32GB    │ Not        │ 32C/64GB
  protected         │ + Redis     │ practical  │ + Redis
                    │ $800-1500   │ (too many  │ $3000-5000
                    │             │  blocks)   │
```

Foxhound advantage grows LEBIH BESAR di scale lebih tinggi, karena:
- Block rate tetap low → nggak perlu over-provision untuk retries
- Proxy burn rate rendah → cost/page menurun
- Go efficiency → less hardware per throughput unit

---

## 7. Tradeoffs & Honest Limitations

### What Foxhound CANNOT solve:

1. CAPTCHA farms
   Kalau site always-on CAPTCHA (setiap visitor), kita perlu solver service.
   Foxhound minimize CAPTCHA encounters, bukan solve semua CAPTCHA.

2. Login-gated content
   Foxhound bisa handle login flows via BrowserActions, tapi account management
   (creation, rotation, ban recovery) di luar scope.

3. 100% undetectable
   Anti-bot dan scraper itu arms race. Foxhound hari ini undetectable oleh
   most anti-bots. 6 bulan dari sekarang? Perlu maintenance. Camoufox juga
   perlu update untuk address new fingerprint inconsistencies.

4. Real-time data (< 1 second)
   Human simulation intentionally slow. Kalau butuh real-time price monitoring
   per-second, pakai API/webhook approach, bukan scraping.

5. Firefox-only
   Beberapa sites serve content berbeda ke Firefox vs Chrome. Rare, tapi possible.
   Mitigation: UA bisa di-set ke Chrome, tapi ini introduce inconsistency risk.

### What Foxhound IS great at:

1. Protected e-commerce (product data, pricing, availability)
2. Travel sites (flights, hotels — heavily protected)
3. Real estate listings
4. Job boards
5. Any site with Cloudflare/Akamai/DataDome yang block Chromium bots

---

## 8. Implementation Priorities

### Phase 1: Core (Week 1-3)
- [ ] Go project setup, interfaces
- [ ] TLS Client fetcher (integrate surf/azuretls)
- [ ] Camoufox launcher (playwright-go + CAMOU_CONFIG env vars)
- [ ] Fingerprint generator (basic: 50 profiles, OS/screen/hardware consistent)
- [ ] Job queue (in-memory), scheduler, result writers (CSV, JSON)
- [ ] Basic middleware (rate limit, dedup)

### Phase 2: Human Simulation (Week 3-5)
- [ ] Behavior timing engine (log-normal delays, session rhythm)
- [ ] Navigation pattern generator (entry points, browsing paths)
- [ ] Cookie/consent handler
- [ ] Smart Fetcher with auto-escalation (static → browser)

### Phase 3: Production (Week 5-8)
- [ ] Redis queue adapter
- [ ] PostgreSQL result writer
- [ ] Proxy manager with rotation + geo-matching + cooldown
- [ ] Fingerprint database expansion (500+ profiles)
- [ ] Metrics (Prometheus) + dashboard
- [ ] Docker Compose deployment
- [ ] Integration tests against real anti-bot sites

### Phase 4: Advanced (Week 8+)
- [ ] CAPTCHA solver integration (CapSolver/2captcha)
- [ ] Session persistence (resume interrupted scrapes)
- [ ] Distributed mode (multiple workers, shared Redis queue)
- [ ] SaaS API layer (FastAPI or Go HTTP server)
- [ ] Dashboard UI

---

## Appendix A: Key Dependencies

```
Go:
  github.com/playwright-community/playwright-go  — browser automation
  github.com/enetx/surf                          — TLS impersonation HTTP client
  github.com/PuerkitoBio/goquery                 — HTML parsing
  github.com/redis/go-redis/v9                   — Redis client
  github.com/prometheus/client_golang             — metrics

External binary:
  Camoufox Firefox build                          — anti-detect browser
  (~300MB, downloaded once)

Optional:
  github.com/Danny-Dasilva/CycleTLS              — alternative TLS client
  github.com/Noooste/azuretls-client             — alternative TLS client
```

## Appendix B: Test Sites for Validation

```
Anti-bot testing:
  https://nowsecure.nl/                  — Cloudflare bot management
  https://bot.sannysoft.com/             — Automation detection tests
  https://browserleaks.com/              — Comprehensive fingerprint check
  https://abrahamjuliot.github.io/creepjs/  — CreepJS fingerprint analysis
  https://tls.peet.ws/api/all            — TLS/JA3/JA4 fingerprint check
  https://check.ja3.zone/               — JA3 verification

Behavioral testing:
  Manual observation — record real human sessions, compare with Foxhound sessions.
  A/B test: same site, same pages — measure block rate with and without human sim.
```
