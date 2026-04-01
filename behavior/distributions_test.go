package behavior

import (
	"math"
	"testing"
)

func TestWeibullSamplePositive(t *testing.T) {
	for i := 0; i < 1000; i++ {
		s := WeibullSample(1.8, 0.7)
		if s <= 0 {
			t.Fatalf("WeibullSample returned non-positive: %f", s)
		}
	}
}

func TestWeibullClampedInBounds(t *testing.T) {
	for i := 0; i < 1000; i++ {
		s := WeibullClamped(1.8, 0.7, 0.15, 2.0)
		if s < 0.15 || s > 2.0 {
			t.Fatalf("WeibullClamped out of bounds: %f", s)
		}
	}
}

func TestWeibullShapeIsRightSkewed(t *testing.T) {
	// With k=1.8, lambda=0.7: mode < mean (right skewed)
	// Count samples below vs above the mean
	n := 10000
	sum := 0.0
	belowMean := 0
	for i := 0; i < n; i++ {
		s := WeibullSample(1.8, 0.7)
		sum += s
	}
	mean := sum / float64(n)

	for i := 0; i < n; i++ {
		s := WeibullSample(1.8, 0.7)
		if s < mean {
			belowMean++
		}
	}
	// For right-skewed distribution, more than 50% of samples should be below mean
	ratio := float64(belowMean) / float64(n)
	if ratio < 0.5 {
		t.Fatalf("Expected right-skewed distribution (>50%% below mean), got %.1f%%", ratio*100)
	}
}

func TestGammaSamplePositive(t *testing.T) {
	// Test both alpha >= 1 and alpha < 1 paths
	for _, alpha := range []float64{0.5, 1.0, 2.5, 3.0} {
		for i := 0; i < 1000; i++ {
			s := GammaSample(alpha, 1.0)
			if s <= 0 {
				t.Fatalf("GammaSample(%.1f, 1.0) returned non-positive: %f", alpha, s)
			}
		}
	}
}

func TestGammaClampedInBounds(t *testing.T) {
	for i := 0; i < 1000; i++ {
		s := GammaClamped(3.0, 0.006, 300, 800)
		if s < 300 || s > 800 {
			t.Fatalf("GammaClamped out of bounds: %f", s)
		}
	}
}

func TestGammaMeanApproximate(t *testing.T) {
	// Gamma(alpha, rate) has mean = alpha / rate
	alpha, rate := 3.0, 0.006
	expected := alpha / rate // 500
	n := 50000
	sum := 0.0
	for i := 0; i < n; i++ {
		sum += GammaSample(alpha, rate)
	}
	mean := sum / float64(n)
	// Allow 10% tolerance
	if math.Abs(mean-expected)/expected > 0.1 {
		t.Fatalf("GammaSample mean: expected ~%.0f, got %.0f", expected, mean)
	}
}

func TestGaussianClampedInBounds(t *testing.T) {
	for i := 0; i < 10000; i++ {
		s := GaussianClamped(0.8, 2.0)
		if s < -2.0 || s > 2.0 {
			t.Fatalf("GaussianClamped out of bounds: %f", s)
		}
	}
}

func TestGaussianClampedCenterHeavy(t *testing.T) {
	// Gaussian should have more samples near center than edges
	n := 10000
	center := 0 // count in [-0.5, 0.5]
	for i := 0; i < n; i++ {
		s := GaussianClamped(0.8, 2.0)
		if s >= -0.5 && s <= 0.5 {
			center++
		}
	}
	// For Gaussian with sigma=0.8, ~47% should be in [-0.5, 0.5]
	// For uniform, it would be ~25%. Assert > 35% to distinguish.
	ratio := float64(center) / float64(n)
	if ratio < 0.35 {
		t.Fatalf("Expected Gaussian center-heavy (>35%% in [-0.5, 0.5]), got %.1f%%", ratio*100)
	}
}

func TestLogNormalSamplePositive(t *testing.T) {
	for i := 0; i < 1000; i++ {
		s := LogNormalSample(0.0, 0.15)
		if s <= 0 {
			t.Fatalf("LogNormalSample returned non-positive: %f", s)
		}
	}
}
