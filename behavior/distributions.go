package behavior

import (
	"math"
	"math/rand/v2"
)

// WeibullSample returns a sample from a Weibull distribution with shape k and scale lambda.
// Formula: X = lambda * (-ln(U))^(1/k) where U ~ Uniform(0,1)
// Weibull is used for rhythm delays and scroll pauses because it produces
// right-skewed distributions matching observed human reaction times.
func WeibullSample(k, lambda float64) float64 {
	u := rand.Float64()
	for u == 0 {
		u = rand.Float64()
	}
	return lambda * math.Pow(-math.Log(u), 1.0/k)
}

// WeibullClamped returns a Weibull sample clamped to [lo, hi].
// Uses rejection sampling with a safety cap of 1000 iterations.
// Falls back to the midpoint of [lo, hi] if no sample is accepted.
func WeibullClamped(k, lambda, lo, hi float64) float64 {
	if lo > hi {
		return lo
	}
	for i := 0; i < 1000; i++ {
		s := WeibullSample(k, lambda)
		if s >= lo && s <= hi {
			return s
		}
	}
	return lo + (hi-lo)/2
}

// GammaSample returns a sample from a Gamma(alpha, rate) distribution using
// the Marsaglia-Tsang method. Pure Go, no external dependencies.
//
// For alpha >= 1:
//
//	d = alpha - 1/3, c = 1/sqrt(9*d)
//	loop: x = NormFloat64(), v = (1+c*x)^3
//	accept if v > 0 and log(U) < 0.5*x^2 + d - d*v + d*ln(v)
//	return d*v / rate
//
// For alpha < 1: use Gamma(alpha+1) * U^(1/alpha) transformation.
func GammaSample(alpha, rate float64) float64 {
	if alpha < 1.0 {
		// Ahrens-Dieter method for alpha < 1
		return GammaSample(alpha+1.0, rate) * math.Pow(rand.Float64(), 1.0/alpha)
	}

	d := alpha - 1.0/3.0
	c := 1.0 / math.Sqrt(9.0*d)

	for {
		var x, v float64
		for {
			x = rand.NormFloat64()
			v = 1.0 + c*x
			if v > 0 {
				break
			}
		}
		v = v * v * v

		u := rand.Float64()
		// Squeeze test
		if u < 1.0-0.0331*(x*x)*(x*x) {
			return d * v / rate
		}
		// Full test
		if math.Log(u) < 0.5*x*x+d*(1.0-v+math.Log(v)) {
			return d * v / rate
		}
	}
}

// GammaClamped returns a Gamma sample clamped to [lo, hi] via rejection.
// Uses a safety cap of 1000 iterations. Falls back to the midpoint if
// no sample is accepted (indicates misconfigured parameters).
func GammaClamped(alpha, rate, lo, hi float64) float64 {
	if lo > hi {
		return lo
	}
	for i := 0; i < 1000; i++ {
		s := GammaSample(alpha, rate)
		if s >= lo && s <= hi {
			return s
		}
	}
	return lo + (hi-lo)/2
}

// GaussianClamped returns a Gaussian sample with given sigma, within [-bound, +bound].
// Uses rejection sampling to avoid probability spikes at the boundaries.
// sigma should be bound/2.5 so ~99% of raw samples are accepted.
func GaussianClamped(sigma, bound float64) float64 {
	for i := 0; i < 1000; i++ {
		s := rand.NormFloat64() * sigma
		if s >= -bound && s <= bound {
			return s
		}
	}
	// Fallback: return a sample within the center half
	return rand.NormFloat64() * sigma * 0.5
}

// LogNormalSample returns a sample from LogNormal(mu, sigma).
// Result = exp(mu + sigma * NormFloat64())
func LogNormalSample(mu, sigma float64) float64 {
	return math.Exp(mu + sigma*rand.NormFloat64())
}
