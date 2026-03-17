package cache_test

// redis_test.go tests the RedisCache backend.
//
// Tests are skipped automatically when Redis is not reachable on localhost:6379
// so the CI pipeline does not require a running Redis instance.

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/foxhound-scraper/foxhound/cache"
	"github.com/redis/go-redis/v9"
)

// newRedisCache attempts to connect to a local Redis instance.
// If the connection fails (Redis not available) the test is skipped.
func newRedisCache(t *testing.T, prefix string) *cache.RedisCache {
	t.Helper()
	c, err := cache.NewRedis("localhost:6379", "", 15, prefix)
	if err != nil {
		t.Skipf("redis not available: %v", err)
	}
	// Verify connectivity with a PING.
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379", DB: 15})
	defer client.Close()
	if pingErr := client.Ping(context.Background()).Err(); pingErr != nil {
		c.Close()
		t.Skipf("redis not available (PING failed): %v", pingErr)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// uniquePrefix returns a unique key prefix per test to avoid cross-test collisions.
func uniquePrefix(t *testing.T) string {
	return fmt.Sprintf("foxhound:test:%s:%d", t.Name(), time.Now().UnixNano())
}

func TestRedisCache_GetMiss(t *testing.T) {
	c := newRedisCache(t, uniquePrefix(t))
	_, ok := c.Get(context.Background(), "nonexistent")
	if ok {
		t.Fatal("expected cache miss for nonexistent key")
	}
}

func TestRedisCache_SetAndGet(t *testing.T) {
	c := newRedisCache(t, uniquePrefix(t))
	ctx := context.Background()
	want := []byte("hello from redis")

	if err := c.Set(ctx, "k1", want, time.Minute); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	got, ok := c.Get(ctx, "k1")
	if !ok {
		t.Fatal("expected cache hit after Set")
	}
	if !bytes.Equal(got, want) {
		t.Errorf("Get returned %q, want %q", got, want)
	}
}

func TestRedisCache_TTLExpiry(t *testing.T) {
	c := newRedisCache(t, uniquePrefix(t))
	ctx := context.Background()

	if err := c.Set(ctx, "expiring", []byte("v"), 50*time.Millisecond); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Entry must be present immediately after Set.
	if _, ok := c.Get(ctx, "expiring"); !ok {
		t.Fatal("expected cache hit before TTL expiry")
	}

	time.Sleep(100 * time.Millisecond)

	// Entry must be gone after the TTL has elapsed.
	if _, ok := c.Get(ctx, "expiring"); ok {
		t.Fatal("expected cache miss after TTL expiry")
	}
}

func TestRedisCache_Delete(t *testing.T) {
	c := newRedisCache(t, uniquePrefix(t))
	ctx := context.Background()

	_ = c.Set(ctx, "del-key", []byte("value"), time.Minute)

	if err := c.Delete(ctx, "del-key"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if _, ok := c.Get(ctx, "del-key"); ok {
		t.Fatal("expected cache miss after Delete")
	}
}

func TestRedisCache_DeleteMissingKey(t *testing.T) {
	c := newRedisCache(t, uniquePrefix(t))
	// Deleting a non-existent key must not return an error.
	if err := c.Delete(context.Background(), "ghost"); err != nil {
		t.Fatalf("Delete of missing key should not error: %v", err)
	}
}

func TestRedisCache_KeyPrefixing(t *testing.T) {
	// Two caches with different prefixes must not share entries.
	prefixA := uniquePrefix(t) + ":A"
	prefixB := uniquePrefix(t) + ":B"

	cA := newRedisCache(t, prefixA)
	cB := newRedisCache(t, prefixB)
	ctx := context.Background()

	if err := cA.Set(ctx, "shared-key", []byte("from-A"), time.Minute); err != nil {
		t.Fatalf("Set on cA failed: %v", err)
	}

	// cB must not see the key written by cA.
	if _, ok := cB.Get(ctx, "shared-key"); ok {
		t.Error("expected miss on cB for key written by cA (prefix isolation broken)")
	}

	// cA must still see its own key.
	if _, ok := cA.Get(ctx, "shared-key"); !ok {
		t.Error("expected hit on cA for its own key")
	}
}

func TestRedisCache_OverwriteUpdatesValue(t *testing.T) {
	c := newRedisCache(t, uniquePrefix(t))
	ctx := context.Background()

	_ = c.Set(ctx, "k", []byte("old"), time.Minute)
	_ = c.Set(ctx, "k", []byte("new"), time.Minute)

	got, ok := c.Get(ctx, "k")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if !bytes.Equal(got, []byte("new")) {
		t.Errorf("expected 'new', got %q", got)
	}
}

func TestRedisCache_NewRedisFromClient(t *testing.T) {
	// Verify that NewRedisFromClient wires up the provided client correctly.
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   15,
	})
	defer client.Close()

	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis not available: %v", err)
	}

	prefix := uniquePrefix(t)
	c := cache.NewRedisFromClient(client, prefix)
	t.Cleanup(func() { c.Close() })

	ctx := context.Background()
	want := []byte("from-client")
	if err := c.Set(ctx, "k", want, time.Minute); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	got, ok := c.Get(ctx, "k")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if !bytes.Equal(got, want) {
		t.Errorf("Get returned %q, want %q", got, want)
	}
}
