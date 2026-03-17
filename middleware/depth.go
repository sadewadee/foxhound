package middleware

import (
	"context"
	"fmt"
	"log/slog"

	foxhound "github.com/foxhound-scraper/foxhound"
)

// depthMiddleware aborts requests whose Job.Depth exceeds the configured limit.
type depthMiddleware struct {
	maxDepth int
}

// NewDepthLimit creates a Middleware that returns an error for any job with
// Depth > maxDepth, preventing the underlying Fetcher from being called.
func NewDepthLimit(maxDepth int) foxhound.Middleware {
	return &depthMiddleware{maxDepth: maxDepth}
}

// Wrap returns a Fetcher that checks job depth before forwarding.
func (d *depthMiddleware) Wrap(next foxhound.Fetcher) foxhound.Fetcher {
	return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		if job.Depth > d.maxDepth {
			slog.Debug("depth: limit exceeded, skipping job",
				"url", job.URL, "depth", job.Depth, "max_depth", d.maxDepth)
			return nil, fmt.Errorf("depth: job %q at depth %d exceeds max %d", job.URL, job.Depth, d.maxDepth)
		}
		return next.Fetch(ctx, job)
	})
}
