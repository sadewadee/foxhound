package middleware

import (
	"context"
	"log/slog"
	"net/url"
	"sort"
	"sync"

	foxhound "github.com/sadewadee/foxhound"
)

// dedupMiddleware tracks seen canonical URLs and short-circuits duplicates.
type dedupMiddleware struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

// NewDedup creates a Middleware that skips URLs that have already been fetched.
//
// Canonicalisation rules applied before storing/checking:
//   - Only scheme + host + path + query are compared (fragment dropped).
//   - Query parameters are sorted alphabetically.
//
// Duplicate requests return a zero-value Response (StatusCode 0, empty body)
// without calling the underlying Fetcher.
func NewDedup() foxhound.Middleware {
	return &dedupMiddleware{seen: make(map[string]struct{})}
}

// Wrap returns a Fetcher that checks each job URL against the seen set.
func (d *dedupMiddleware) Wrap(next foxhound.Fetcher) foxhound.Fetcher {
	return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		key := canonicalURL(job.URL)

		d.mu.Lock()
		_, seen := d.seen[key]
		if !seen {
			d.seen[key] = struct{}{}
		}
		d.mu.Unlock()

		if seen {
			slog.Debug("dedup: skipping duplicate URL", "url", job.URL, "canonical", key)
			return &foxhound.Response{StatusCode: 0, Job: job}, nil
		}
		return next.Fetch(ctx, job)
	})
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
