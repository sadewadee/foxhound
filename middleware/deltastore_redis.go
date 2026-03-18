package middleware

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisDeltaStore is a persistent DeltaStore backed by Redis.
// Keys are stored as "<prefix>:<key>" with the Unix timestamp (seconds) as
// their value, allowing TTL-aware re-crawl strategies.
//
// RedisDeltaStore is safe for concurrent use.
type RedisDeltaStore struct {
	client *redis.Client
	prefix string
}

// NewRedisDeltaStore connects to Redis at addr and returns a RedisDeltaStore.
//
//   - addr: host:port, e.g. "localhost:6379"
//   - password: empty string for no auth
//   - db: Redis database index (0-15)
//   - prefix: key namespace, e.g. "foxhound:delta"
func NewRedisDeltaStore(addr, password string, db int, prefix string) (*RedisDeltaStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis delta store: ping failed: %w", err)
	}

	return &RedisDeltaStore{client: client, prefix: prefix}, nil
}

// redisKey returns the namespaced Redis key for key.
func (r *RedisDeltaStore) redisKey(key string) string {
	return r.prefix + ":" + key
}

// Seen implements DeltaStore.
// Returns (true, timestamp) if the key exists, otherwise (false, zero time).
func (r *RedisDeltaStore) Seen(key string) (bool, time.Time) {
	ctx := context.Background()
	val, err := r.client.Get(ctx, r.redisKey(key)).Result()
	if err == redis.Nil {
		return false, time.Time{}
	}
	if err != nil {
		// Treat read errors as "not seen" so the fetch is retried.
		return false, time.Time{}
	}

	unixSec, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		// Unparseable value — treat as not seen.
		return false, time.Time{}
	}

	return true, time.Unix(unixSec, 0)
}

// Mark implements DeltaStore.
// Stores the current Unix timestamp under the namespaced key.
func (r *RedisDeltaStore) Mark(key string) error {
	ctx := context.Background()
	val := strconv.FormatInt(time.Now().Unix(), 10)
	if err := r.client.Set(ctx, r.redisKey(key), val, 0).Err(); err != nil {
		return fmt.Errorf("redis delta store: SET %q: %w", key, err)
	}
	return nil
}

// Close closes the underlying Redis client connection.
func (r *RedisDeltaStore) Close() error {
	return r.client.Close()
}
