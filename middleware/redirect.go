package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	foxhound "github.com/foxhound-scraper/foxhound"
)

// redirectStatusCodes contains HTTP status codes that require following a
// Location header to a new URL.
var redirectStatusCodes = map[int]bool{
	301: true,
	302: true,
	303: true,
	307: true,
	308: true,
}

// redirectMiddleware follows Location headers up to a configured maximum.
type redirectMiddleware struct {
	maxRedirects int
}

// NewRedirect returns a Middleware that follows HTTP redirects.
//
// Up to maxRedirects hops are followed. If the chain exceeds maxRedirects an
// error is returned. A value of 0 disables redirect following entirely.
func NewRedirect(maxRedirects int) foxhound.Middleware {
	return &redirectMiddleware{maxRedirects: maxRedirects}
}

// Wrap returns a Fetcher that follows redirect responses.
func (r *redirectMiddleware) Wrap(next foxhound.Fetcher) foxhound.Fetcher {
	return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		currentJob := job
		for hop := 0; ; hop++ {
			resp, err := next.Fetch(ctx, currentJob)
			if err != nil {
				return resp, err
			}

			if !redirectStatusCodes[resp.StatusCode] {
				return resp, nil
			}

			if hop >= r.maxRedirects {
				return nil, fmt.Errorf("redirect: exceeded max redirects (%d) for %q",
					r.maxRedirects, job.URL)
			}

			location := resp.Headers.Get("Location")
			if location == "" {
				// No Location header — treat as a normal response.
				slog.Warn("redirect: redirect response missing Location header",
					"url", currentJob.URL, "status", resp.StatusCode)
				return resp, nil
			}

			slog.Debug("redirect: following",
				"from", currentJob.URL, "to", location,
				"status", resp.StatusCode, "hop", hop+1)

			// Clone the job with the new URL; preserve other fields.
			nextJob := *currentJob
			nextJob.URL = location
			nextJob.Headers = currentJob.Headers.Clone()
			if nextJob.Headers == nil {
				nextJob.Headers = make(http.Header)
			}
			currentJob = &nextJob
		}
	})
}
