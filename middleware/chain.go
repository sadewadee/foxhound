// Package middleware provides composable foxhound.Middleware implementations
// for rate limiting, deduplication, depth limiting, and retry logic.
package middleware

import (
	foxhound "github.com/sadewadee/foxhound"
)

// chainMiddleware applies a slice of middlewares as a single Middleware.
// The first element in the slice is the outermost wrapper (first to execute).
type chainMiddleware struct {
	middlewares []foxhound.Middleware
}

// Chain composes multiple Middleware values into one.
// Middlewares are applied outermost-first: the first argument's Wrap runs
// before subsequent ones, so request processing follows the slice order.
func Chain(middlewares ...foxhound.Middleware) foxhound.Middleware {
	return &chainMiddleware{middlewares: middlewares}
}

// Wrap builds the handler chain by wrapping next with each middleware in
// reverse order so that the outermost middleware executes first.
func (c *chainMiddleware) Wrap(next foxhound.Fetcher) foxhound.Fetcher {
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		next = c.middlewares[i].Wrap(next)
	}
	return next
}
