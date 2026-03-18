package middleware

import (
	"context"
	"log/slog"
	"net/url"
	"sort"
	"sync"

	foxhound "github.com/sadewadee/foxhound"
)

// DedupOption configures the dedup middleware.
type DedupOption func(*dedupMiddleware)

// WithFingerprintFunc sets a custom function for generating dedup keys.
// When set, it replaces the default URL-based canonicalization. The function
// receives the job and returns a string key; identical keys are treated as
// duplicates.
//
// Example: include HTTP method in the fingerprint to allow GET and POST
// to the same URL:
//
//	middleware.NewDedup(middleware.WithFingerprintFunc(func(job *foxhound.Job) string {
//	    return job.Method + ":" + job.URL
//	}))
func WithFingerprintFunc(fn func(job *foxhound.Job) string) DedupOption {
	return func(d *dedupMiddleware) {
		d.fingerprintFunc = fn
	}
}

// WithKeepFragments preserves URL fragments in the dedup fingerprint.
// By default, fragments (#section) are stripped before comparison.
func WithKeepFragments(keep bool) DedupOption {
	return func(d *dedupMiddleware) {
		d.keepFragments = keep
	}
}

// WithIncludeHeaders includes request headers (from Job.Headers) in the
// dedup fingerprint. This makes requests with different headers to the same
// URL be treated as distinct.
func WithIncludeHeaders(include bool) DedupOption {
	return func(d *dedupMiddleware) {
		d.includeHeaders = include
	}
}

// dedupMiddleware tracks seen canonical URLs and short-circuits duplicates.
type dedupMiddleware struct {
	mu              sync.Mutex
	seen            map[string]struct{}
	fingerprintFunc func(job *foxhound.Job) string
	keepFragments   bool
	includeHeaders  bool
}

// NewDedup creates a Middleware that skips URLs that have already been fetched.
//
// Canonicalisation rules applied before storing/checking:
//   - Only scheme + host + path + query are compared (fragment dropped).
//   - Query parameters are sorted alphabetically.
//
// Duplicate requests return a zero-value Response (StatusCode 0, empty body)
// without calling the underlying Fetcher.
func NewDedup(opts ...DedupOption) foxhound.Middleware {
	d := &dedupMiddleware{seen: make(map[string]struct{})}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Wrap returns a Fetcher that checks each job URL against the seen set.
// Jobs with DontFilter=true bypass deduplication entirely.
func (d *dedupMiddleware) Wrap(next foxhound.Fetcher) foxhound.Fetcher {
	return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		// Skip dedup when job has DontFilter set.
		if job.DontFilter {
			slog.Debug("dedup: DontFilter=true, skipping dedup", "url", job.URL)
			return next.Fetch(ctx, job)
		}

		key := d.fingerprint(job)

		d.mu.Lock()
		_, seen := d.seen[key]
		if !seen {
			d.seen[key] = struct{}{}
		}
		d.mu.Unlock()

		if seen {
			slog.Debug("dedup: skipping duplicate URL", "url", job.URL, "key", key)
			return &foxhound.Response{StatusCode: 0, Job: job}, nil
		}
		return next.Fetch(ctx, job)
	})
}

// fingerprint generates the dedup key for a job using either the custom
// function or the default URL canonicalization.
func (d *dedupMiddleware) fingerprint(job *foxhound.Job) string {
	if d.fingerprintFunc != nil {
		return d.fingerprintFunc(job)
	}

	key := canonicalURL(job.URL)

	if d.keepFragments {
		// Re-parse to include fragment.
		if u, err := url.Parse(job.URL); err == nil {
			key = key + "#" + u.Fragment
		}
	}

	if d.includeHeaders && job.Headers != nil {
		// Sort and append header keys for deterministic fingerprint.
		keys := make([]string, 0, len(job.Headers))
		for k := range job.Headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			for _, v := range job.Headers[k] {
				key += "|" + k + "=" + v
			}
		}
	}

	return key
}

// canonicalURL normalises a URL for deduplication purposes:
// scheme + host + path with query params sorted.
// Malformed URLs are returned as-is.
func canonicalURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	// Sort query parameters.
	q := u.Query()
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	vals := url.Values{}
	for _, k := range keys {
		vals[k] = q[k]
	}
	u.RawQuery = vals.Encode()
	u.Fragment = ""
	return u.String()
}
