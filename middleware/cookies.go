package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"

	foxhound "github.com/foxhound-scraper/foxhound"
)

// cookiesMiddleware maintains a per-session cookie jar that persists cookies
// across requests. This is critical for sites that set a cookie on the first
// page and verify it on subsequent pages (e.g. session, CSRF tokens).
type cookiesMiddleware struct {
	mu  sync.Mutex
	jar http.CookieJar
}

// NewCookies returns a Middleware that automatically persists and replays
// HTTP cookies across requests within the same session.
func NewCookies() foxhound.Middleware {
	jar, _ := cookiejar.New(nil) // cookiejar.New never returns an error with nil options
	return &cookiesMiddleware{jar: jar}
}

// Wrap returns a Fetcher that injects stored cookies into each outgoing request
// and stores Set-Cookie headers from each response.
func (c *cookiesMiddleware) Wrap(next foxhound.Fetcher) foxhound.Fetcher {
	return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		// Ensure job headers are initialised.
		if job.Headers == nil {
			job.Headers = make(http.Header)
		}

		u, err := url.Parse(job.URL)
		if err != nil {
			slog.Warn("cookies: could not parse job URL", "url", job.URL, "err", err)
			return next.Fetch(ctx, job)
		}

		// Inject stored cookies into the outgoing request.
		c.mu.Lock()
		stored := c.jar.Cookies(u)
		c.mu.Unlock()

		if len(stored) > 0 {
			// Merge with any cookies already set on the job.
			req := &http.Request{Header: job.Headers}
			for _, ck := range stored {
				req.AddCookie(ck)
			}
			job.Headers.Set("Cookie", req.Header.Get("Cookie"))
			slog.Debug("cookies: injected", "url", job.URL, "count", len(stored))
		}

		resp, err := next.Fetch(ctx, job)
		if err != nil {
			return resp, err
		}

		// Store Set-Cookie headers from the response.
		if resp != nil && len(resp.Headers) > 0 {
			respURL := resp.URL
			if respURL == "" {
				respURL = job.URL
			}
			respU, parseErr := url.Parse(respURL)
			if parseErr == nil {
				// Build synthetic http.Response to use SetCookies.
				httpResp := &http.Response{
					Header: resp.Headers,
					Request: &http.Request{
						URL: respU,
					},
				}
				c.mu.Lock()
				c.jar.SetCookies(respU, httpResp.Cookies())
				c.mu.Unlock()
				slog.Debug("cookies: stored from response", "url", respURL,
					"count", len(httpResp.Cookies()))
			}
		}

		return resp, nil
	})
}
