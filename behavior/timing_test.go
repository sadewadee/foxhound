package behavior

import (
	"testing"
	"time"
)

func TestNewTimingDefaultsAreUsable(t *testing.T) {
	cfg := TimingConfig{
		Mu:    1.0,
		Sigma: 0.8,
		Min:   100 * time.Millisecond,
		Max:   30 * time.Second,
	}
	timing := NewTiming(cfg)
	if timing == nil {
		t.Fatal("NewTiming returned nil")
	}
}

func TestTimingDelayIsWithinBounds(t *testing.T) {
	cfg := TimingConfig{
		Mu:    1.0,
		Sigma: 0.8,
		Min:   500 * time.Millisecond,
		Max:   20 * time.Second,
	}
	timing := NewTiming(cfg)

	for i := 0; i < 200; i++ {
		d := timing.Delay()
		if d < cfg.Min {
			t.Errorf("iteration %d: delay %v below min %v", i, d, cfg.Min)
		}
		if d > cfg.Max {
			t.Errorf("iteration %d: delay %v above max %v", i, d, cfg.Max)
		}
	}
}

// TestTimingDelayIsLogNormalDistributed checks the distribution is not
// degenerate (all values equal) and has a positive median within sensible
// range for the default parameters (mu=1.0, sigma=0.8).
func TestTimingDelayIsLogNormalDistributed(t *testing.T) {
	cfg := TimingConfig{
		Mu:    1.0,
		Sigma: 0.8,
		Min:   10 * time.Millisecond,
		Max:   60 * time.Second,
	}
	timing := NewTiming(cfg)

	samples := make([]time.Duration, 500)
	for i := range samples {
		samples[i] = timing.Delay()
	}

	// Measure variance: all equal → degenerate distribution.
	first := samples[0]
	allEqual := true
	for _, s := range samples[1:] {
		if s != first {
			allEqual = false
			break
		}
	}
	if allEqual {
		t.Error("all 500 delay samples are identical — distribution is degenerate")
	}

	// Median of log-normal(mu=1.0) = exp(1.0) ≈ 2.718s.
	// Accept median in [1s, 6s] to avoid flakiness.
	sorted := make([]time.Duration, len(samples))
	copy(sorted, samples)
	sortDurations(sorted)
	median := sorted[len(sorted)/2]
	if median < 1*time.Second || median > 6*time.Second {
		t.Errorf("median delay %v outside expected window [1s, 6s] for mu=1.0", median)
	}
}

func TestTimingClampsToMin(t *testing.T) {
	// Very tight min/max to force clamping.
	cfg := TimingConfig{
		Mu:    -5.0, // exp(-5) ≈ 6ms — almost always below Min
		Sigma: 0.1,
		Min:   5 * time.Second,
		Max:   10 * time.Second,
	}
	timing := NewTiming(cfg)
	for i := 0; i < 50; i++ {
		d := timing.Delay()
		if d < cfg.Min {
			t.Errorf("delay %v below enforced min %v", d, cfg.Min)
		}
	}
}

func TestTimingClampsToMax(t *testing.T) {
	cfg := TimingConfig{
		Mu:    10.0, // exp(10) ≈ 22026s — almost always above Max
		Sigma: 0.1,
		Min:   1 * time.Millisecond,
		Max:   2 * time.Second,
	}
	timing := NewTiming(cfg)
	for i := 0; i < 50; i++ {
		d := timing.Delay()
		if d > cfg.Max {
			t.Errorf("delay %v above enforced max %v", d, cfg.Max)
		}
	}
}

func TestPageReadDelayIsInRange(t *testing.T) {
	timing := NewTiming(TimingConfig{Mu: 1.0, Sigma: 0.8, Min: 100 * time.Millisecond, Max: 60 * time.Second})
	for i := 0; i < 100; i++ {
		d := timing.PageReadDelay()
		if d < 3*time.Second || d > 15*time.Second {
			t.Errorf("PageReadDelay %v outside [3s, 15s]", d)
		}
	}
}

func TestPaginationDelayIsInRange(t *testing.T) {
	timing := NewTiming(TimingConfig{Mu: 1.0, Sigma: 0.8, Min: 100 * time.Millisecond, Max: 60 * time.Second})
	for i := 0; i < 100; i++ {
		d := timing.PaginationDelay()
		if d < 500*time.Millisecond || d > 2*time.Second {
			t.Errorf("PaginationDelay %v outside [500ms, 2s]", d)
		}
	}
}

func TestSearchDelayIsInRange(t *testing.T) {
	timing := NewTiming(TimingConfig{Mu: 1.0, Sigma: 0.8, Min: 100 * time.Millisecond, Max: 60 * time.Second})
	for i := 0; i < 100; i++ {
		d := timing.SearchDelay()
		if d < 5*time.Second || d > 20*time.Second {
			t.Errorf("SearchDelay %v outside [5s, 20s]", d)
		}
	}
}

func TestTypingDelayIsInRange(t *testing.T) {
	timing := NewTiming(TimingConfig{Mu: 1.0, Sigma: 0.8, Min: 100 * time.Millisecond, Max: 60 * time.Second})
	for i := 0; i < 100; i++ {
		d := timing.TypingDelay()
		if d < 50*time.Millisecond || d > 200*time.Millisecond {
			t.Errorf("TypingDelay %v outside [50ms, 200ms]", d)
		}
	}
}

// sortDurations is a simple insertion sort for test use only.
func sortDurations(s []time.Duration) {
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j] > key {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
}
