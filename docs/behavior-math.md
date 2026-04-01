# Behavior Mathematics

Foxhound uses statistical models from human factors research to generate realistic browsing behavior. Every timing distribution, mouse trajectory, and keyboard pattern is designed to be indistinguishable from a real human under ML-based anti-bot analysis.

All behavior code lives in the `behavior/` package. Pure Go, zero external dependencies, no goroutines -- every function is deterministic given a PRNG state, so tests are reproducible and the library is safe to call from any context.

## Distribution Library

All distributions are in `behavior/distributions.go`.

### Weibull Distribution

Used for: burst delays, scroll pauses, inter-burst pauses, burst length selection.

```
X = lambda * (-ln(U))^(1/k)    where U ~ Uniform(0,1)
```

**Parameters:**
- `k` (shape): controls skewness. k < 1 gives a heavy right tail (exponential-like). k = 1 is exactly exponential. k > 1 concentrates mass around a mode with a lighter tail. Foxhound uses k in [1.5, 2.2] for most timing, which produces a clear mode with occasional long outliers -- matching observed human pause distributions.
- `lambda` (scale): stretches or compresses the distribution horizontally. Sets the characteristic timescale.

**Why Weibull over Gamma for timing?** Weibull's shape parameter gives direct control over the tail weight independent of the mode location. For human pauses, you want a tunable right skew (most pauses are short, some are long) without shifting the peak. Gamma ties mode and variance together through a single alpha parameter, making it harder to independently control "where most values land" vs "how often outliers appear." Weibull separates these concerns cleanly.

Foxhound provides a clamped variant that uses rejection sampling with a 1000-iteration safety cap, falling back to the midpoint of [lo, hi] if no sample is accepted:

```go
// Used in rhythm.go for burst delays:
ms := WeibullClamped(1.8, 700.0, 150.0, 2000.0) // mode ~550ms, right-skewed
```

### Gamma Distribution (Marsaglia-Tsang)

Used for: scroll distances.

```
For alpha >= 1:
    d = alpha - 1/3,  c = 1/sqrt(9*d)
    loop: x ~ N(0,1),  v = (1 + c*x)^3
    accept if v > 0 and ln(U) < 0.5*x^2 + d - d*v + d*ln(v)
    return d*v / rate

For alpha < 1:
    return Gamma(alpha+1, rate) * U^(1/alpha)
```

**Parameters:**
- `alpha` (shape): controls the distribution shape. Alpha < 1 gives a mode at zero (exponential-like). Alpha > 1 gives a clear mode away from zero. Foxhound uses alpha=2.5 for scan scrolls (mode ~1250px) and alpha=3.0 for reading scrolls (mode ~333px).
- `rate`: inverse scale. Higher rate compresses the distribution leftward.

The Marsaglia-Tsang method is used because it is efficient for alpha >= 1 (the common case), requiring only one or two normal samples per accepted value. For alpha < 1, the Ahrens-Dieter transformation `Gamma(alpha+1) * U^(1/alpha)` reduces it to the alpha >= 1 case.

Scroll distances use Gamma because scroll gestures have a natural minimum (you cannot scroll zero pixels) and the distribution of real scroll distances has a clear mode with a long right tail -- exactly what Gamma produces with alpha > 1.

```go
// Reading mode: Gamma(3.0, 0.006) clamped to [300, 800] px
distance := GammaClamped(3.0, 0.006, float64(cfg.ReadMinPx), float64(cfg.ReadMaxPx))

// Scan mode: Gamma(2.5, 0.0012) clamped to [1000, 3000] px
distance := GammaClamped(2.5, 0.0012, float64(cfg.ScanMinPx), float64(cfg.ScanMaxPx))
```

### Gaussian Noise (Rejection Sampling)

Used for: mouse jitter, click offsets, idle drift.

```go
func GaussianClamped(sigma, bound float64) float64
```

Returns a Gaussian sample within [-bound, +bound]. The sigma is set to `bound / 2.5` so that ~99% of raw samples from N(0, sigma) fall within bounds and are accepted on the first draw.

**Why rejection sampling instead of hard-clamping?** Hard-clamping (capping values at the boundary) creates a probability spike at the edges. Anti-bot ML systems trained on mouse telemetry would see an unnatural density bump at exactly +/-bound pixels. Rejection sampling preserves the Gaussian shape all the way to the boundary, producing a clean bell curve with no artifacts.

```go
// Click offset: sigma = 5.0 / 2.5 = 2.0, bound = 5.0 px
offset := GaussianClamped(2.0, 5.0)

// Idle drift: sigma = 2.0 / 2.5 = 0.8, bound = 2.0 px
drift := GaussianClamped(0.8, 2.0)
```

### LogNormal

Used for: per-keystroke typing variance, click duration, general inter-action timing.

```
X = exp(mu + sigma * N(0,1))
```

LogNormal is the default distribution for human reaction times in the cognitive psychology literature (Luce, 1986). The key property: the log of human response times is approximately normal, so the response times themselves are right-skewed with a long tail. This matches the real pattern -- most keystrokes are fast, but occasional hesitations (thinking, reading ahead) produce much longer gaps.

```go
// Click duration: LogNormal(4.5, 0.3) → median ~90ms, mean ~94ms
ms := LogNormalSample(4.5, 0.3)

// General inter-action delay: LogNormal(1.0, 0.8) → median ~2.7s, mean ~4.1s
delay := LogNormalSample(1.0, 0.8)
```

## Typing Model

### Bigram-Aware Per-Character Speed

The typing model in `behavior/keyboard.go` generates inter-key delays that vary by character pair, simulating the biomechanics of touch-typing. When `BigramModel` is enabled (default on all named profiles), each keystroke delay is computed as:

```
delay(P, C) = base * freqFactor(C) * bigramFactor(P, C) * fatigueFactor(pos)
```

The result is then sampled from `LogNormal(ln(computed), 0.35)` for per-keystroke variance. This two-stage approach (deterministic model + stochastic perturbation) produces realistic timing that has both the structural patterns of real typing and the noise of human motor control.

**base**: midpoint of [MinDelay, MaxDelay]. For the moderate profile, this is 125ms.

**freqFactor(C)**: common letters are typed faster. Based on English letter frequency rank (e=0, t=1, a=2, ... z=25):

```
freqFactor = 0.8 + 0.4 * (rank / 26)
```

The most common letter 'e' gets factor 0.8 (20% faster than base). The rarest letter 'z' gets factor ~1.18 (18% slower). This matches the well-documented frequency-speed correlation in typing research.

**bigramFactor(P, C)**: models the physical hand/finger transition between the previous key P and current key C, using a QWERTY finger assignment map:

| Transition | Factor | Reason |
|-----------|--------|--------|
| Different hand | 0.85 | Parallel motion -- one hand moves while the other strikes |
| Same hand, different finger (distant) | 0.95 | Slight reach, but independent fingers |
| Same hand, adjacent finger | 1.0 | Neutral |
| Same finger | 2.0 | Finger must travel to new row -- slowest transition |

The finger map assigns each letter to [hand, finger] on the QWERTY layout: `a` = left pinky, `s` = left ring, `d` = left middle, `f`/`g` = left index, etc.

**fatigueFactor(pos)**: gradual slowdown over long strings:

```
fatigueFactor = 1.0 + 0.001 * position
```

At position 100 (a ~100-character input), this adds 10% to the delay. Negligible for short inputs, noticeable for long form fills.

**Typo simulation**: with probability `TypoProb` (default 0.02), a random adjacent-key character is inserted, followed by a backspace and the correct character. Adjacent keys are looked up from a QWERTY neighbor map.

```go
profile := behavior.CarefulProfile()
// BigramModel is enabled by default on all named profiles
kb := behavior.NewKeyboard(profile.Keyboard)
actions := kb.TypeString("hello world")
// Each KeyAction has: Char, Delay, IsBackspace
// Total actions > len("hello world") due to occasional typo+backspace pairs
```

## Session Fatigue Model

Real users do not maintain a constant speed throughout a session. They start slow (orienting, reading the first page), speed up as they settle into a rhythm, and gradually slow down as fatigue sets in. The fatigue model in `behavior/fatigue.go` captures this inverted-U curve.

### The Formula

```
speed_factor(t) = warmup(t) * fatigue(t) * noise

warmup(t)  = 1.0 + WarmupAmplitude * exp(-t / WarmupTau)
fatigue(t) = 1.0 + FatigueAmplitude * (1 - exp(-t / FatigueTau))
noise      ~ clamp(1.0 + N(0, 0.05), 0.85, 1.15)
```

Values > 1.0 mean slower. The factor is multiplied into any base delay to produce the actual wait time.

**warmup(t)** is an exponential decay starting at `1 + WarmupAmplitude` and decaying toward 1.0 with time constant `WarmupTau`. At t=0 the user is slow (reading, orienting). By t = 3*WarmupTau the warmup effect is essentially gone.

**fatigue(t)** is an exponential rise starting at 1.0 and approaching `1 + FatigueAmplitude` with time constant `FatigueTau`. It models the gradual slowdown from sustained attention.

**noise** is per-call Gaussian perturbation (±5%, hard-clamped to ±15%) that prevents the speed curve from being a smooth function. Anti-bot systems looking for deterministic timing patterns would otherwise detect the exponential shape.

### Timeline (Default Config)

| Elapsed | warmup | fatigue | Combined factor | Phase |
|---------|--------|---------|-----------------|-------|
| 0s | 1.40 | 1.00 | ~1.40 | Session start -- slow, orienting |
| 60s | 1.24 | 1.01 | ~1.25 | Warming up |
| 120s | 1.15 | 1.01 | ~1.16 | Approaching cruise |
| 300s | 1.07 | 1.02 | ~1.09 | Cruising -- fastest point |
| 1800s | 1.00 | 1.16 | ~1.16 | Fatigue building |
| 3600s | 1.00 | 1.22 | ~1.22 | Noticeable slowdown |

### Default Config Values

```go
WarmupAmplitude:  0.4       // 40% slower at session start
WarmupTau:        2 min     // 63% of warmup gone after 2 minutes
FatigueAmplitude: 0.25      // up to 25% slower at late session
FatigueTau:       30 min    // 63% of max fatigue reached after 30 minutes
```

### Usage

```go
fatigue := behavior.NewSessionFatigue(behavior.DefaultFatigueConfig())
fatigue.Start()
// factor = warmup(t) * fatigue(t) * noise
delay := fatigue.AdjustDelay(2 * time.Second)
// At t=0: delay ≈ 2.8s (2s * 1.4)
// At t=5min: delay ≈ 2.2s (2s * 1.09)
// At t=30min: delay ≈ 2.3s (2s * 1.16)
```

Each Walker owns its own `SessionFatigue` instance. Not safe for concurrent use.

## Per-Session Profile Jitter

A fixed behavior profile is a fingerprint. If every session from the same machine uses `ModerateProfile()` with identical parameters, a clustering attack can group all sessions by their exact timing distribution, mouse jitter amplitude, and typo rate -- even without seeing the same IP or cookies.

`Jitter()` returns a copy of the profile with every numeric parameter independently perturbed by ±15%:

```
perturbed = original * (1 + U(-0.15, +0.15))
```

This applies to all config fields: `TimingMu`, `TimingSigma`, `ScrollMinPx`, `ScrollMaxPx`, `TypoProb`, `OvershootProb`, `WarmupAmplitude`, `FatigueTau`, burst sizes, pause durations -- every tunable number.

The result is that two sessions from the same profile will have detectably different statistical signatures. An observer cannot determine that they came from the same software by comparing distributions.

```go
profile := behavior.CarefulProfile().Jitter()
// Every numeric parameter is perturbed ±15%
// TimingMu, ScrollMinPx, TypoProb, etc. all vary per session
```

For custom jitter fractions:

```go
// Tight jitter for testing (±5%)
profile := behavior.ModerateProfile().JitterBy(0.05)

// Wide jitter for maximum session diversity (±30%)
profile := behavior.ModerateProfile().JitterBy(0.30)
```

**Warning**: `JitterBy(0)` returns an exact copy with no perturbation -- equivalent to no jitter. Always call `Jitter()` or `JitterBy(frac)` with frac > 0 in production.

## Mouse Movement

Mouse trajectories are generated in `behavior/mouse.go` and executed in browser mode via playwright-go.

### Bezier Path Traversal

`Mouse.MoveTo(start, end)` produces a sequence of 20-50 screen coordinates along a bezier curve:

1. **Control points**: 3-5 random control points are placed between start and end, offset perpendicular to the straight line by up to ±30% of the total distance. This creates natural curvature -- real mouse paths are never perfectly straight.
2. **Evaluation**: the De Casteljau algorithm evaluates the bezier at evenly-spaced t values.
3. **Speed profile**: t is remapped through `smoothstep(t) = 3t^2 - 2t^3` (ease-in-out cubic). This produces slow movement near the start and end with fast traversal in the middle, matching the acceleration-deceleration pattern of real hand movements.
4. **Per-point jitter**: each sampled point receives independent Gaussian noise (±Jitter px, default 2.0). This simulates hand tremor.
5. **Overshoot**: with probability `OvershootProb` (default 0.2), the cursor overshoots the target by up to `OvershootPx` pixels, then a correction segment brings it back. This models the Fitts's Law overshoot observed in rapid targeting.

### Click Behavior

**Click offset**: clicks do not land at the exact center of an element. `ClickOffset()` returns a Gaussian-distributed offset with sigma=2.0, bound=5.0 px in each axis. Center-heavy, but never pixel-perfect.

**Click duration**: the time between mouse-down and mouse-up follows `LogNormal(4.5, 0.3)`, producing a median of ~90ms and a mean of ~94ms, clamped to [40ms, 250ms]. Real human click durations cluster around 80-100ms with a right tail from deliberate presses.

### Idle Micro-Drift

`IdleDrift()` returns tiny Gaussian displacements (sigma=0.8, bound=2.0 px) for simulating an idle mouse. Real users do not hold the mouse perfectly still -- there is always sub-pixel tremor from hand/arm contact.

## Session Rhythm

The `Rhythm` state machine in `behavior/rhythm.go` models the burst/pause cadence of real browsing sessions:

1. **Burst phase**: 5-15 rapid actions (page loads, clicks, scrolls). Inter-action delays are Weibull(k=1.8, lambda=700ms), producing a mode around 550ms with right skew. Clamped to [150ms, 2s].
2. **Pause phase**: after each burst, a normal pause (Weibull-distributed in [PauseMin, PauseMax]) or a long pause (with probability `LongPauseProb`).
3. **Long pause**: simulates a coffee break or context switch. Duration drawn from [LongPauseMin, LongPauseMax].

Burst length itself is Weibull-distributed: `WeibullClamped(k=2.2, lambda=0.5*range, 0, range)` places the mode in the lower half of the range, producing more frequent short bursts with occasional long ones.

## Adaptive Domain Scoring

The `DomainScorer` in `fetch/domain_score.go` uses a Bayesian Beta distribution model to learn, per domain, whether static fetches are likely to be blocked.

### The Risk Formula

```
risk = (blocked + alpha) / (blocked + success + alpha + beta)
```

This is the posterior mean of a `Beta(alpha + blocked, beta + success)` distribution. The prior `Beta(alpha, beta)` encodes your initial belief about how likely a domain is to block static requests.

**Default prior**: `Beta(1, 3)` -- 75% prior success rate. Assumes static fetches usually work. The risk score starts at `1 / (1 + 3) = 0.25` with no data.

**Social media prior**: `Beta(3, 1)` -- 75% prior block rate. Assumes social media sites almost always block static fetches. Starts at `3 / (3 + 1) = 0.75`, which immediately exceeds the escalation threshold.

### Asymmetric Decay

Old outcomes decay exponentially, but blocks decay 4x slower than successes:

```
effective_success = success * exp(-age * ln(2) / halflife)
effective_blocked = blocked * exp(-age * ln(2) / (halflife * 4))
```

This asymmetry reflects operational reality: when a site starts blocking, that protection usually persists. A brief success window does not erase evidence of prior blocking.

### Decision Thresholds

| Risk Score | Action | Meaning |
|-----------|--------|---------|
| risk <= 0.3 | `ActionStaticNormal` | Static fetch with normal timeout |
| 0.3 < risk <= 0.6 | `ActionStaticCautious` | Static with shorter timeout (fail-fast) |
| risk > 0.6 | `ActionBrowserDirect` | Skip static, go straight to browser |

### Configuration Examples

```go
// Default: optimistic prior, 1-hour decay half-life
scorer := fetch.NewDomainScorer(fetch.DefaultDomainScoreConfig())
// Prior: Beta(1,3) — starts at 25% risk
// 1 block + 0 success → risk = 2/5 = 0.40 → cautious
// 2 blocks + 0 success → risk = 3/6 = 0.50 → cautious
// 3 blocks + 0 success → risk = 4/7 = 0.57 → cautious
// 4 blocks + 0 success → risk = 5/8 = 0.63 → browser direct

// Social media: pessimistic prior, 24-hour decay
scorer := fetch.NewDomainScorer(fetch.SocialMediaScoreConfig())
// Prior: Beta(3,1) — starts at 75% risk
// Immediately recommends browser direct
// 1 success shifts to: (3)/(3+1+3+1) = 0.375 → cautious static
// Blocks decay 4x slower than successes — protection evidence persists
```

## Circuit Breaker

The circuit breaker in `middleware/circuitbreaker.go` implements a per-domain 3-state FSM that prevents wasting requests on domains that are actively blocking.

### State Machine

```
Closed  ──[failure rate > threshold]──>  Open  ──[timeout expires]──>  Half-Open
  ^                                                                       │
  └──────────────────[probe succeeds]─────────────────────────────────────┘
  Open  <──────────[probe fails, trips++]─────────────────────────────────┘
```

**Closed**: normal operation. A sliding window of the last `WindowSize` (default 20) outcomes tracks the failure rate. The circuit trips when: (1) at least `MinObservations` (default 5) requests have been recorded, and (2) the failure rate exceeds `FailureThreshold` (default 0.5).

**Open**: all requests are rejected immediately with a synthetic 503 response. The circuit stays open for an exponentially increasing duration.

**Half-Open**: one probe request is allowed through. If it succeeds, the circuit closes and the trip counter resets. If it fails, the circuit re-opens with `trips++`.

### Backoff Formula

```
open_duration = min(BaseTimeout * 2^(trips-1), MaxTimeout) * U(0.5, 1.5)
```

The jitter factor `U(0.5, 1.5)` is uniform random, which obscures the exponential structure from any observer correlating retry intervals. Without jitter, exact powers-of-two retry intervals would be a bot signature.

| Trip | Base Duration | With Jitter (range) |
|------|--------------|---------------------|
| 1 | 30s | 15s -- 45s |
| 2 | 60s | 30s -- 90s |
| 3 | 2m | 1m -- 3m |
| 4 | 4m | 2m -- 6m |
| 5 | 8m | 4m -- 10m (capped) |
| 6+ | 10m (max) | 5m -- 10m (capped) |

### Configuration

```go
cb := middleware.NewCircuitBreaker(middleware.CircuitBreakerConfig{
    FailureThreshold: 0.5,           // trip at 50% failure rate
    MinObservations:  5,             // need 5 requests before evaluating
    WindowSize:       20,            // sliding window of last 20 outcomes
    BaseTimeout:      30 * time.Second,
    MaxTimeout:       10 * time.Minute,
    MaxTrips:         8,             // cap at 8 consecutive trips
})
```

Failures are defined as: HTTP 403, 429, or 503 responses, or any error from the underlying fetcher.

## Tuning Guide

### Profile Comparison

| Parameter | Careful | Moderate | Aggressive |
|-----------|---------|----------|------------|
| Timing Mu (median delay) | 1.5 (~4.5s) | 1.0 (~2.7s) | 0.5 (~1.6s) |
| Timing Sigma | 0.5 | 0.8 | 0.6 |
| Mouse Jitter (px) | 3.0 | 2.0 | 1.0 |
| Overshoot Prob | 0.30 | 0.20 | 0.10 |
| Keyboard Min/Max (ms) | 80/250 | 50/200 | 30/100 |
| Typo Probability | 0.04 | 0.02 | 0.01 |
| Scroll-Up Probability | 0.25 | 0.15 | 0.08 |
| Burst Size | 5-10 | 5-15 | 8-20 |
| Long Pause Prob | 0.25 | 0.15 | 0.08 |
| Warmup Amplitude | 0.50 | 0.40 | 0.20 |
| Fatigue Amplitude | 0.30 | 0.25 | 0.15 |

### When to Use Each Profile

**Careful**: Cloudflare Enterprise, Akamai Bot Manager, DataDome, PerimeterX. Any site where a single anomalous request triggers a CAPTCHA for the entire session. Throughput: ~10-20 pages/hour per walker.

**Moderate** (default): most sites with standard Cloudflare, light rate limiting, or basic bot detection. Good balance of stealth and throughput. Throughput: ~30-60 pages/hour per walker.

**Aggressive**: sites with no behavioral analysis -- public APIs, government data portals, sites behind only a WAF with rate limiting. Throughput: ~100+ pages/hour per walker.

### Platform-Specific Adjustments

**Social media** (Instagram, Twitter/X, LinkedIn): use `SocialMediaScoreConfig()` for the domain scorer, `CarefulProfile()` for behavior, and always start in browser mode. Social media sites have the most sophisticated behavioral analysis and will fingerprint timing distributions across sessions.

**E-commerce** (Amazon, Shopify stores): `ModerateProfile()` is usually sufficient. Increase `ScrollUpProb` to 0.20+ since real shoppers scroll back to compare products. Use the `Paginate` step for product listings.

**Search engines** (Google, Bing): `CarefulProfile()` with `SearchDelay` between result clicks. Never use `AggressiveProfile` -- search engines correlate timing across queries.

### Key Tunable Parameters

| Parameter | Location | Default | Safe Range | Effect of Increase |
|-----------|----------|---------|------------|-------------------|
| `TimingMu` | `TimingConfig` | 1.0 | 0.3 -- 2.0 | Higher median delay |
| `TimingSigma` | `TimingConfig` | 0.8 | 0.3 -- 1.5 | More variance, heavier tail |
| `TypoProb` | `KeyboardConfig` | 0.02 | 0.0 -- 0.08 | More typo+backspace pairs |
| `OvershootProb` | `MouseConfig` | 0.2 | 0.05 -- 0.40 | More mouse overshoot corrections |
| `ScrollUpProb` | `ScrollConfig` | 0.15 | 0.05 -- 0.30 | More re-reading scroll-ups |
| `WarmupAmplitude` | `FatigueConfig` | 0.4 | 0.1 -- 0.8 | Slower session start |
| `FatigueAmplitude` | `FatigueConfig` | 0.25 | 0.1 -- 0.5 | Stronger late-session slowdown |
| `LongPauseProb` | `RhythmConfig` | 0.15 | 0.05 -- 0.30 | More frequent long breaks |
| `Jitter (profile)` | `Jitter()` / `JitterBy()` | 0.15 | 0.05 -- 0.30 | Wider per-session parameter spread |

**Warning**: setting `TimingSigma` above 1.5 produces extremely heavy tails -- occasional delays of 60+ seconds. This can trigger server-side session timeouts. Setting `TypoProb` above 0.08 makes the typing unrealistically error-prone. Keep parameters within the documented safe ranges unless you have measured the specific target's detection thresholds.
