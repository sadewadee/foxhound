package engine

import (
	"math"
	"math/rand"
	"time"

	foxhound "github.com/sadewadee/foxhound"
)

// RetryPolicy controls when and how often failed requests are retried.
type RetryPolicy struct {
	// MaxRetries is the maximum number of retry attempts (not counting the
	// original attempt). A value of 3 means up to 4 total attempts.
	MaxRetries int

	// BaseDelay is the initial delay before the first retry.
	BaseDelay time.Duration

	// MaxDelay caps the computed delay regardless of the attempt number.
	MaxDelay time.Duration

	// Backoff is the exponential multiplier applied to each successive delay.
	// A value of 2.0 doubles the delay each attempt.
	Backoff float64
}

// DefaultRetryPolicy returns a sensible retry policy suitable for most scraping
// workloads: 3 retries, starting at 1 second, doubling up to 30 seconds.
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxRetries: 3,
		BaseDelay:  time.Second,
		MaxDelay:   30 * time.Second,
		Backoff:    2.0,
	}
}

// ShouldRetry reports whether the request should be retried.
// attempt is the zero-based number of retries already performed.
// err is the fetch error (may be nil). resp is the response (may be nil on
// network errors).
func (rp *RetryPolicy) ShouldRetry(attempt int, err error, resp *foxhound.Response) bool {
	if attempt >= rp.MaxRetries {
		return false
	}
	if err != nil {
		return true
	}
	if resp != nil && resp.StatusCode >= 500 {
		return true
	}
	return false
}

// Delay returns how long to wait before the given retry attempt. It uses
// exponential backoff with full-jitter so that concurrent walkers do not
// stampede the same target simultaneously.
func (rp *RetryPolicy) Delay(attempt int) time.Duration {
	// ceil = BaseDelay * Backoff^attempt, capped at MaxDelay.
	ceiling := float64(rp.BaseDelay) * math.Pow(rp.Backoff, float64(attempt))
	if ceiling > float64(rp.MaxDelay) {
		ceiling = float64(rp.MaxDelay)
	}
	// Full jitter: uniform random in [0, ceiling).
	jittered := time.Duration(rand.Float64() * ceiling) //nolint:gosec // non-crypto use
	if jittered <= 0 {
		jittered = time.Duration(ceiling / 2)
	}
	return jittered
}
