package middleware

import (
	"context"
	"sync"

	foxhound "github.com/sadewadee/foxhound"
)

// NewConcurrency returns a Middleware that limits the number of concurrent
// in-flight requests per target domain. A separate semaphore (buffered channel
// of size perDomain) is created lazily for each unique domain.
//
// The middleware is intended to sit as the outermost layer in the chain so it
// caps parallelism before any rate-limit or dedup checks are performed.
func NewConcurrency(perDomain int) foxhound.Middleware {
	if perDomain <= 0 {
		perDomain = 2
	}

	type domainSem struct {
		ch chan struct{}
	}
	var mu sync.Mutex
	domains := make(map[string]*domainSem)

	getSem := func(domain string) chan struct{} {
		mu.Lock()
		defer mu.Unlock()
		if s, ok := domains[domain]; ok {
			return s.ch
		}
		s := &domainSem{ch: make(chan struct{}, perDomain)}
		domains[domain] = s
		return s.ch
	}

	return foxhound.MiddlewareFunc(func(next foxhound.Fetcher) foxhound.Fetcher {
		return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
			domain := job.Domain
			if domain == "" {
				domain = "__default__"
			}
			sem := getSem(domain)
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return next.Fetch(ctx, job)
		})
	})
}
