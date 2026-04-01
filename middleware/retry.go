package middleware

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	foxhound "github.com/sadewadee/foxhound"
)

// blockedStatusCodes contains HTTP status codes that indicate the server is
// blocking the request. Responses with these codes are retried.
var blockedStatusCodes = map[int]bool{
	403: true,
	429: true,
	503: true,
	407: true,
}

// retryMiddleware wraps a Fetcher with exponential-backoff retry logic.
type retryMiddleware struct {
	maxRetries         int
	baseDelay          time.Duration
	retryNetworkErrors bool // retry on transient network errors (default true)
}

// NewRetry creates a Middleware that retries failed or blocked requests up to
// maxRetries additional times (i.e. 1 + maxRetries total attempts).
//
// Backoff between attempts is baseDelay * 2^attempt, with ±25 % uniform
// jitter to spread retry storms.  Context cancellation stops retries
// immediately.
func NewRetry(maxRetries int, baseDelay time.Duration) foxhound.Middleware {
	return &retryMiddleware{maxRetries: maxRetries, baseDelay: baseDelay, retryNetworkErrors: true}
}

// Wrap returns a Fetcher that retries on error or blocked status code.
func (r *retryMiddleware) Wrap(next foxhound.Fetcher) foxhound.Fetcher {
	return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		var (
			resp *foxhound.Response
			err  error
		)

		for attempt := 0; attempt <= r.maxRetries; attempt++ {
			if attempt > 0 {
				delay := r.backoff(attempt - 1)
				slog.Debug("retry: waiting before retry",
					"url", job.URL, "attempt", attempt, "delay", delay)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(delay):
				}
			}

			resp, err = next.Fetch(ctx, job)
			if err == nil && !blockedStatusCodes[resp.StatusCode] {
				return resp, nil
			}

			// Don't retry non-retryable errors.
			if err != nil && (!r.retryNetworkErrors || !isRetryableNetworkError(err)) {
				return resp, err
			}

			slog.Debug("retry: attempt failed",
				"url", job.URL, "attempt", attempt,
				"err", err,
				"status_code", statusCode(resp))
		}

		return resp, err
	})
}

// backoff computes the delay for a given attempt (0-indexed).
// delay = baseDelay * 2^attempt * (1 ± 0.25 jitter)
func (r *retryMiddleware) backoff(attempt int) time.Duration {
	exp := time.Duration(1) << attempt // 2^attempt
	base := r.baseDelay * exp

	// Apply ±25 % jitter.
	jitter := float64(base) * 0.25 * (rand.Float64()*2 - 1)
	return base + time.Duration(jitter)
}

// statusCode extracts the status code from a possibly-nil Response.
func statusCode(resp *foxhound.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}

// isRetryableNetworkError checks if an error is a transient network error
// that should be retried. These are connection-level failures that typically
// succeed on a second attempt.
func isRetryableNetworkError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	retryable := []string{
		"reset",
		"timeout",
		"refused",
		"eof",
		"broken pipe",
		"connection closed",
		"no such host",
		"network is unreachable",
		"i/o timeout",
		"tls handshake",
		"ns_error_net_reset",
	}
	for _, r := range retryable {
		if strings.Contains(msg, r) {
			return true
		}
	}
	return false
}
