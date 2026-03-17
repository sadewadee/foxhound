package middleware

import (
	"context"
	"log/slog"
	"sync"
	"time"

	foxhound "github.com/foxhound-scraper/foxhound"
)

// DeltaStrategy controls when DeltaFetch skips a URL.
type DeltaStrategy int

const (
	// DeltaSkipSeen skips any URL that has ever been fetched, regardless of
	// how long ago.
	DeltaSkipSeen DeltaStrategy = iota

	// DeltaSkipRecent skips a URL only if it was fetched within the TTL
	// window. After the TTL elapses the URL is re-fetched and the timestamp
	// is refreshed.
	DeltaSkipRecent
)

// DeltaStore persists the set of already-scraped URLs across restarts.
type DeltaStore interface {
	// Seen reports whether key has been marked, and when (zero if not seen).
	Seen(key string) (bool, time.Time)
	// Mark records that key was fetched now.
	Mark(key string) error
	// Close releases any resources held by the store.
	Close() error
}

// deltaFetchMiddleware skips URLs that were already scraped according to its
// strategy and backing store.
type deltaFetchMiddleware struct {
	strategy DeltaStrategy
	store    DeltaStore
	ttl      time.Duration
}

// NewDeltaFetch returns a Middleware that avoids re-fetching known URLs.
//
//   - strategy=DeltaSkipSeen: skip if ever seen.
//   - strategy=DeltaSkipRecent: skip only if seen within ttl.
//
// Skipped requests return a zero-value Response (StatusCode 0) without calling
// the underlying Fetcher, matching the behaviour of NewDedup.
func NewDeltaFetch(strategy DeltaStrategy, store DeltaStore, ttl time.Duration) foxhound.Middleware {
	return &deltaFetchMiddleware{strategy: strategy, store: store, ttl: ttl}
}

// Wrap returns a Fetcher that checks each job URL against the DeltaStore.
func (d *deltaFetchMiddleware) Wrap(next foxhound.Fetcher) foxhound.Fetcher {
	return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		key := job.URL

		seen, seenAt := d.store.Seen(key)

		if seen {
			switch d.strategy {
			case DeltaSkipSeen:
				slog.Debug("deltafetch: skipping seen URL", "url", key)
				return &foxhound.Response{StatusCode: 0, Job: job}, nil

			case DeltaSkipRecent:
				if d.ttl > 0 && time.Since(seenAt) < d.ttl {
					slog.Debug("deltafetch: skipping recent URL",
						"url", key, "seen_at", seenAt, "ttl", d.ttl)
					return &foxhound.Response{StatusCode: 0, Job: job}, nil
				}
				// TTL has elapsed — fall through to re-fetch.
				slog.Debug("deltafetch: TTL expired, re-fetching", "url", key)
			}
		}

		resp, err := next.Fetch(ctx, job)
		if err == nil {
			if markErr := d.store.Mark(key); markErr != nil {
				slog.Warn("deltafetch: failed to mark URL", "url", key, "err", markErr)
			}
		}
		return resp, err
	})
}

// ---------------------------------------------------------------------------
// MemoryDeltaStore — in-memory implementation (lost on restart)
// ---------------------------------------------------------------------------

// seenEntry holds the time a key was first marked.
type seenEntry struct {
	at time.Time
}

// MemoryDeltaStore is a thread-safe, in-memory DeltaStore. State is lost when
// the process exits; use a persistent store (e.g. Redis, SQLite) for
// production cross-run deduplication.
type MemoryDeltaStore struct {
	mu    sync.Mutex
	items map[string]seenEntry
}

// NewMemoryDeltaStore returns an empty in-memory DeltaStore.
func NewMemoryDeltaStore() *MemoryDeltaStore {
	return &MemoryDeltaStore{items: make(map[string]seenEntry)}
}

// Seen implements DeltaStore.
func (m *MemoryDeltaStore) Seen(key string) (bool, time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.items[key]
	if !ok {
		return false, time.Time{}
	}
	return true, entry.at
}

// Mark implements DeltaStore.
func (m *MemoryDeltaStore) Mark(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[key] = seenEntry{at: time.Now()}
	return nil
}

// Close is a no-op for the in-memory store.
func (m *MemoryDeltaStore) Close() error { return nil }
