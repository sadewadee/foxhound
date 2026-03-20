package engine

import (
	"context"
	"sync"
)

// Pool stores discovered URLs between collect and process phases.
// Implementations must be safe for concurrent use.
type Pool interface {
	// Add stores a URL. Duplicates are silently ignored.
	Add(ctx context.Context, url string) error
	// AddBatch stores multiple URLs. Duplicates are silently ignored.
	AddBatch(ctx context.Context, urls []string) error
	// Drain returns all stored URLs and empties the pool.
	Drain(ctx context.Context) ([]string, error)
	// Len returns the number of URLs in the pool.
	Len() int
	// Close releases resources.
	Close() error
}

// MemoryPool is an in-memory Pool backed by a slice + dedup set.
type MemoryPool struct {
	mu   sync.Mutex
	urls []string
	seen map[string]struct{}
}

// NewMemoryPool creates an empty in-memory pool.
func NewMemoryPool() *MemoryPool {
	return &MemoryPool{
		seen: make(map[string]struct{}),
	}
}

func (p *MemoryPool) Add(_ context.Context, url string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.seen[url]; ok {
		return nil
	}
	p.seen[url] = struct{}{}
	p.urls = append(p.urls, url)
	return nil
}

func (p *MemoryPool) AddBatch(_ context.Context, urls []string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, u := range urls {
		if _, ok := p.seen[u]; ok {
			continue
		}
		p.seen[u] = struct{}{}
		p.urls = append(p.urls, u)
	}
	return nil
}

func (p *MemoryPool) Drain(_ context.Context) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.urls
	p.urls = nil
	p.seen = make(map[string]struct{})
	return out, nil
}

func (p *MemoryPool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.urls)
}

func (p *MemoryPool) Close() error {
	return nil
}
