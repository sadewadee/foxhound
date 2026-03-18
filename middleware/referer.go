package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	foxhound "github.com/sadewadee/foxhound"
)

// refererMiddleware tracks the last fetched URL per domain and uses it to
// synthesise a realistic Referer header on each subsequent request.
type refererMiddleware struct {
	mu      sync.Mutex
	lastURL map[string]string // domain → last successfully requested URL
}

// NewReferer returns a Middleware that automatically sets the Referer header.
//
// Behaviour:
//   - If a Referer header is already present on the job, it is left unchanged.
//   - On the first request to a domain, Referer is set to a Google search URL
//     for that domain, mimicking organic search traffic.
//   - On subsequent requests to the same domain, Referer is set to the URL of
//     the previous request to that domain.
func NewReferer() foxhound.Middleware {
	return &refererMiddleware{lastURL: make(map[string]string)}
}

// Wrap returns a Fetcher that populates the Referer header.
func (r *refererMiddleware) Wrap(next foxhound.Fetcher) foxhound.Fetcher {
	return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		// Clone the job to avoid mutating shared state. Retry middleware may
		// pass the same Job pointer on multiple attempts; mutating headers
		// in-place causes a data race.
		clonedJob := *job
		clonedJob.Headers = job.Headers.Clone()
		if clonedJob.Headers == nil {
			clonedJob.Headers = make(http.Header)
		}
		job = &clonedJob

		// Do not override a manually set Referer.
		if job.Headers.Get("Referer") == "" {
			domain := job.Domain
			if domain == "" {
				domain = domainFrom(job.URL)
			}

			r.mu.Lock()
			prev, exists := r.lastURL[domain]
			r.mu.Unlock()

			var referer string
			if exists {
				referer = prev
			} else {
				referer = fmt.Sprintf("https://www.google.com/search?q=%s", domain)
			}

			job.Headers.Set("Referer", referer)
			slog.Debug("referer: set", "url", job.URL, "referer", referer)
		}

		resp, err := next.Fetch(ctx, job)

		// Record the URL that was just fetched so the next request can use it.
		if err == nil {
			domain := job.Domain
			if domain == "" {
				domain = domainFrom(job.URL)
			}
			r.mu.Lock()
			r.lastURL[domain] = job.URL
			r.mu.Unlock()
		}

		return resp, err
	})
}

// domainFrom extracts the host from a URL string. Returns the raw URL on
// parse failure (graceful degradation).
func domainFrom(rawURL string) string {
	// Avoid importing net/url at package level; use simple parse here.
	// We only need the host for the referer key.
	import_url_parse := func(s string) string {
		// minimal host extraction without importing net/url to avoid cycle
		// — net/url is safe to import; this local closure is just for clarity.
		for i := 0; i < len(s)-3; i++ {
			if s[i] == '/' && s[i+1] == '/' {
				rest := s[i+2:]
				for j, c := range rest {
					if c == '/' || c == ':' || c == '?' {
						return rest[:j]
					}
				}
				return rest
			}
		}
		return s
	}
	return import_url_parse(rawURL)
}
