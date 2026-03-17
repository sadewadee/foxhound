package cache

import (
	"container/list"
	"context"
	"log/slog"
	"sync"
	"time"
)

// cacheEntry is a single cached item stored both in the map and the LRU list.
type cacheEntry struct {
	key       string
	value     []byte
	expiresAt time.Time     // zero means no expiry
	element   *list.Element // position in the LRU list
}

// MemoryCache is a thread-safe, in-memory LRU cache with per-entry TTL.
//
// Eviction policy:
//   - When the cache is at maxSize capacity and a new key is inserted, the
//     least-recently-used item is removed.
//   - Expired entries are lazily removed on Get.
type MemoryCache struct {
	maxSize int
	items   map[string]*cacheEntry
	order   *list.List // front = most-recently used, back = LRU
	mu      sync.RWMutex
}

// NewMemory returns a MemoryCache that holds at most maxSize items.
// maxSize must be > 0; if <= 0 it defaults to 128.
func NewMemory(maxSize int) *MemoryCache {
	if maxSize <= 0 {
		maxSize = 128
	}
	return &MemoryCache{
		maxSize: maxSize,
		items:   make(map[string]*cacheEntry),
		order:   list.New(),
	}
}

// Get retrieves a cached value. A miss or an expired entry both return
// (nil, false). On expiry the entry is deleted.
func (c *MemoryCache) Get(_ context.Context, key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.items[key]
	if !ok {
		return nil, false
	}

	// Lazy TTL expiry.
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		c.removeEntry(entry)
		slog.Debug("cache/memory: entry expired", "key", key)
		return nil, false
	}

	// Move to front (most-recently used).
	c.order.MoveToFront(entry.element)
	return entry.value, true
}

// Set stores value under key with the given TTL. If maxSize is reached the
// least-recently-used entry is evicted first. Overwriting an existing key
// updates the value and moves it to the front of the LRU list.
func (c *MemoryCache) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}

	// Overwrite existing entry.
	if entry, ok := c.items[key]; ok {
		entry.value = value
		entry.expiresAt = expiresAt
		c.order.MoveToFront(entry.element)
		return nil
	}

	// Evict LRU if at capacity.
	if len(c.items) >= c.maxSize {
		c.evictLRU()
	}

	entry := &cacheEntry{
		key:       key,
		value:     value,
		expiresAt: expiresAt,
	}
	entry.element = c.order.PushFront(entry)
	c.items[key] = entry
	slog.Debug("cache/memory: set", "key", key, "ttl", ttl)
	return nil
}

// Delete removes the entry for key. Returns nil if key does not exist.
func (c *MemoryCache) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.items[key]; ok {
		c.removeEntry(entry)
		slog.Debug("cache/memory: deleted", "key", key)
	}
	return nil
}

// Close is a no-op for the in-memory cache; it satisfies the Cache interface.
func (c *MemoryCache) Close() error {
	return nil
}

// evictLRU removes the back element (least-recently used) from the cache.
// Caller must hold c.mu.
func (c *MemoryCache) evictLRU() {
	back := c.order.Back()
	if back == nil {
		return
	}
	entry := back.Value.(*cacheEntry)
	slog.Debug("cache/memory: evicting LRU", "key", entry.key)
	c.removeEntry(entry)
}

// removeEntry removes entry from both the map and the list.
// Caller must hold c.mu.
func (c *MemoryCache) removeEntry(entry *cacheEntry) {
	c.order.Remove(entry.element)
	delete(c.items, entry.key)
}
