package middleware

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	foxhound "github.com/sadewadee/foxhound"
)

// BlockPattern defines a pattern that indicates a blocked response.
type BlockPattern struct {
	// Name is a human-readable label used in log output.
	Name string
	// StatusCode triggers the pattern when the response matches this code.
	// A value of 0 means any status code is eligible.
	StatusCode int
	// BodyContains lists substrings; if any are found in the lowercased body
	// the response is considered blocked.
	BodyContains []string
	// MinBodySize marks a response as suspicious when the body is smaller than
	// this threshold (bytes). 0 disables this check.
	MinBodySize int
	// MaxBodySize is unused in the current heuristic but reserved for future
	// content-length-without-payload detection.
	MaxBodySize int
}

// DefaultBlockPatterns returns the standard set of block detection patterns
// covering the most common anti-bot vendors and generic block signals.
func DefaultBlockPatterns() []BlockPattern {
	return []BlockPattern{
		{
			Name:         "cloudflare",
			BodyContains: []string{"checking your browser", "just a moment", "challenge-platform"},
		},
		{
			Name:         "rate-limit",
			StatusCode:   429,
			BodyContains: []string{"rate limit", "too many requests"},
		},
		{
			Name:         "access-denied",
			StatusCode:   403,
			BodyContains: []string{"access denied", "forbidden", "blocked"},
		},
		{
			Name:         "bot-detection",
			BodyContains: []string{"bot detected", "automated access", "unusual traffic"},
		},
		{
			Name:        "empty-trap",
			StatusCode:  200,
			MinBodySize: 500,
		},
		{
			Name:         "akamai",
			BodyContains: []string{"akamai", "security challenge", "reference #"},
		},
		{
			Name:         "datadome",
			BodyContains: []string{"datadome", "dd.js"},
		},
		{
			Name:         "perimeterx",
			BodyContains: []string{"perimeterx", "px-captcha"},
		},
		{
			Name:         "login-wall",
			BodyContains: []string{"login", "sign in"},
		},
	}
}

// blockDetector is the middleware implementation.
type blockDetector struct {
	maxRetries int
	baseDelay  time.Duration
	patterns   []BlockPattern
}

// NewBlockDetector creates a Middleware that detects soft blocks in responses
// based on HTTP status codes and body content heuristics.  When a block is
// detected it retries with exponential backoff up to maxRetries additional
// times.
//
// If no patterns are provided DefaultBlockPatterns() is used.
func NewBlockDetector(maxRetries int, baseDelay time.Duration, patterns ...BlockPattern) foxhound.Middleware {
	if len(patterns) == 0 {
		patterns = DefaultBlockPatterns()
	}
	return &blockDetector{
		maxRetries: maxRetries,
		baseDelay:  baseDelay,
		patterns:   patterns,
	}
}

// Wrap returns a Fetcher that detects blocks and retries with backoff.
func (bd *blockDetector) Wrap(next foxhound.Fetcher) foxhound.Fetcher {
	return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		for attempt := 0; attempt <= bd.maxRetries; attempt++ {
			resp, err := next.Fetch(ctx, job)
			if err != nil {
				return resp, err
			}

			if pattern := bd.detectBlock(resp); pattern != nil {
				slog.Warn("block detected",
					"url", job.URL,
					"pattern", pattern.Name,
					"attempt", attempt,
					"status", resp.StatusCode,
				)
				if attempt < bd.maxRetries {
					delay := bd.backoff(attempt)
					select {
					case <-time.After(delay):
					case <-ctx.Done():
						return resp, ctx.Err()
					}
					continue
				}
			}
			return resp, nil
		}
		// Unreachable but satisfies the compiler.
		return next.Fetch(ctx, job)
	})
}

// detectBlock returns the first matching BlockPattern, or nil when the
// response looks legitimate.
func (bd *blockDetector) detectBlock(resp *foxhound.Response) *BlockPattern {
	lower := strings.ToLower(string(resp.Body))

	for i := range bd.patterns {
		p := &bd.patterns[i]

		// Status code gate: pattern requires a specific code and this isn't it.
		if p.StatusCode != 0 && resp.StatusCode != p.StatusCode {
			// Body-content checks still apply when no specific code was required.
			// But if a code IS required and doesn't match, skip body check too.
			// Exception: patterns like "access-denied" list a code but also body
			// keywords — we want to match either.  Treat StatusCode as an OR
			// condition with the body check below.
			if len(p.BodyContains) == 0 && p.MinBodySize == 0 {
				continue
			}
		}

		// Status code match (alone is sufficient for patterns without body checks).
		if p.StatusCode != 0 && resp.StatusCode == p.StatusCode &&
			len(p.BodyContains) == 0 && p.MinBodySize == 0 {
			return p
		}

		// Body content check.
		for _, phrase := range p.BodyContains {
			if strings.Contains(lower, strings.ToLower(phrase)) {
				return p
			}
		}

		// Minimum body size check (empty-trap heuristic).
		// A 200 OK with a body smaller than MinBodySize that also lacks <html
		// is suspicious.
		if p.MinBodySize > 0 && resp.StatusCode == 200 &&
			len(resp.Body) < p.MinBodySize &&
			!strings.Contains(lower, "<html") {
			return p
		}
	}
	return nil
}

// backoff computes the delay for a given attempt (0-indexed).
// delay = baseDelay * 2^attempt with ±25 % uniform jitter.
func (bd *blockDetector) backoff(attempt int) time.Duration {
	exp := time.Duration(1) << uint(attempt)
	base := bd.baseDelay * exp

	jitter := float64(base) * 0.25 * (rand.Float64()*2 - 1)
	return base + time.Duration(jitter)
}
