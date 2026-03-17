package cache

// redis.go implements the Cache interface using Redis as the backing store.
//
// All cache keys are namespaced with a configurable prefix to avoid collisions
// when multiple Foxhound instances share the same Redis database.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache implements Cache using Redis.
// It is safe for concurrent use.
type RedisCache struct {
	client *redis.Client
	prefix string // namespace prefix applied to every key
}

// NewRedis creates a RedisCache that connects to Redis at addr.
// password may be empty. db selects the Redis logical database (0–15).
// prefix namespaces all keys to avoid collisions with other data stored in the
// same Redis instance.
//
// Returns an error if the client cannot be created (e.g., invalid addr format).
func NewRedis(addr, password string, db int, prefix string) (*RedisCache, error) {
	if addr == "" {
		return nil, fmt.Errorf("cache/redis: addr must not be empty")
	}
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	return &RedisCache{client: client, prefix: prefix}, nil
}

// NewRedisFromClient wraps an existing *redis.Client. Use this when you need to
// share a connection pool across multiple components or configure advanced
// options (TLS, sentinel, cluster) on the client yourself.
func NewRedisFromClient(client *redis.Client, prefix string) *RedisCache {
	return &RedisCache{client: client, prefix: prefix}
}

// Get retrieves a cached value by key. Returns (value, true) on hit and
// (nil, false) on miss or TTL expiry. The key is automatically prefixed.
func (c *RedisCache) Get(ctx context.Context, key string) ([]byte, bool) {
	val, err := c.client.Get(ctx, c.prefixed(key)).Bytes()
	if err != nil {
		// redis.Nil means the key does not exist (cache miss); any other error
		// is treated as a miss rather than surfacing internal errors to callers.
		return nil, false
	}
	return val, true
}

// Set stores value under key with the given TTL.
// A TTL of zero stores the entry without an expiration time (it persists until
// explicitly deleted or Redis is flushed). The key is automatically prefixed.
func (c *RedisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	err := c.client.Set(ctx, c.prefixed(key), value, ttl).Err()
	if err != nil {
		return fmt.Errorf("cache/redis: Set %q: %w", key, err)
	}
	return nil
}

// Delete removes a cached entry. Returns nil if the key does not exist.
// The key is automatically prefixed.
func (c *RedisCache) Delete(ctx context.Context, key string) error {
	if err := c.client.Del(ctx, c.prefixed(key)).Err(); err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("cache/redis: Delete %q: %w", key, err)
	}
	return nil
}

// Close releases the underlying Redis connection pool.
func (c *RedisCache) Close() error {
	if err := c.client.Close(); err != nil {
		return fmt.Errorf("cache/redis: Close: %w", err)
	}
	return nil
}

// prefixed returns the cache key with the configured namespace prefix applied.
// The separator is ":" which is the Redis convention for key namespacing.
func (c *RedisCache) prefixed(key string) string {
	if c.prefix == "" {
		return key
	}
	return c.prefix + ":" + key
}
