// Package cache provides caching backends for Foxhound responses.
//
// Two implementations are provided:
//   - MemoryCache: in-process LRU cache with TTL expiry.
//   - FileCache: disk-based cache using SHA256-hashed filenames.
package cache

import (
	"context"
	"time"
)

// Cache stores and retrieves cached responses.
type Cache interface {
	// Get retrieves a cached value by key. Returns (value, true) on hit,
	// (nil, false) on miss or TTL expiry.
	Get(ctx context.Context, key string) ([]byte, bool)

	// Set stores value under key with the given TTL.
	// A TTL of zero means the entry never expires.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// Delete removes a cached entry. Returns nil if the key does not exist.
	Delete(ctx context.Context, key string) error

	// Close releases any resources held by the cache.
	Close() error
}
