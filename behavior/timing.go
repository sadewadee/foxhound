// Package behavior provides human-like behavioral simulation for the Foxhound
// scraping engine.  Every exported type is pure computation — no I/O, no
// goroutines — so callers can use the helpers from any context.
package behavior

import (
	"math"
	"math/rand/v2"
	"time"
)

// TimingConfig configures the log-normal delay generator.
type TimingConfig struct {
	// Mu is the log-normal location parameter (default 1.0).
	// Controls the median: median = exp(Mu).
	Mu float64
	// Sigma is the log-normal scale parameter (default 0.8).
	// Controls spread; higher = more variance / heavier tail.
	Sigma float64
	// Min is the hard lower bound for any generated delay.
	Min time.Duration
	// Max is the hard upper bound for any generated delay.
	Max time.Duration
}

// Timing generates human-like delays using log-normal distribution.
//
// Why log-normal?  Real human inter-action times are right-skewed: most
// actions happen quickly but rare long pauses (reading, distraction) pull the
// mean well above the median.  Uniform random delay is trivially detectable
// by ML-based anti-bot systems because its distribution is not bursty.
type Timing struct {
	config TimingConfig
}

// NewTiming creates a Timing instance with the supplied configuration.
func NewTiming(cfg TimingConfig) *Timing {
	return &Timing{config: cfg}
}

// lognormal samples from a log-normal distribution with the receiver's Mu and
// Sigma, clamping the result to [Min, Max].
//
// Formula: delay = exp(Mu + Sigma * N(0,1))
func (t *Timing) lognormal() time.Duration {
	// rand.NormFloat64 returns a sample from standard normal N(0,1).
	z := rand.NormFloat64()
	seconds := math.Exp(t.config.Mu + t.config.Sigma*z)
	d := time.Duration(seconds * float64(time.Second))

	if d < t.config.Min {
		return t.config.Min
	}
	if d > t.config.Max {
		return t.config.Max
	}
	return d
}

// uniformSeconds returns a uniformly-distributed duration in [minS, maxS].
func uniformSeconds(minS, maxS float64) time.Duration {
	r := minS + rand.Float64()*(maxS-minS)
	return time.Duration(r * float64(time.Second))
}

// Delay returns a human-like general-purpose delay drawn from the configured
// log-normal distribution, clamped to [Min, Max].
//
// With default parameters (Mu=1.0, Sigma=0.8):
//   - median ≈ 2.7 s
//   - mean   ≈ 4.1 s
//   - 95th   ≈ 13 s
func (t *Timing) Delay() time.Duration {
	return t.lognormal()
}

// PageReadDelay returns a delay appropriate for page-reading behaviour.
// Range: [3 s, 15 s].
func (t *Timing) PageReadDelay() time.Duration {
	return uniformSeconds(3, 15)
}

// PaginationDelay returns a delay for clicking pagination controls.
// Range: [0.5 s, 2 s].
func (t *Timing) PaginationDelay() time.Duration {
	return uniformSeconds(0.5, 2)
}

// SearchDelay returns a delay for analysing search-result pages.
// Range: [5 s, 20 s].
func (t *Timing) SearchDelay() time.Duration {
	return uniformSeconds(5, 20)
}

// TypingDelay returns a per-character typing delay.
// Range: [50 ms, 200 ms].
func (t *Timing) TypingDelay() time.Duration {
	ms := 50 + rand.Float64()*(200-50)
	return time.Duration(ms * float64(time.Millisecond))
}
