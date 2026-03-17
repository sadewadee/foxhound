// Package cache_test tests the cache package implementations.
package cache_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/foxhound-scraper/foxhound/cache"
)

// --- MemoryCache tests ---

func TestMemoryCacheGetMiss(t *testing.T) {
	c := cache.NewMemory(10)
	_, ok := c.Get(context.Background(), "missing")
	if ok {
		t.Fatal("expected cache miss for unknown key")
	}
}

func TestMemoryCacheSetAndGet(t *testing.T) {
	c := cache.NewMemory(10)
	want := []byte("hello world")

	if err := c.Set(context.Background(), "k1", want, time.Minute); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	got, ok := c.Get(context.Background(), "k1")
	if !ok {
		t.Fatal("expected cache hit after Set")
	}
	if !bytes.Equal(got, want) {
		t.Errorf("Get returned %q, want %q", got, want)
	}
}

func TestMemoryCacheTTLExpiry(t *testing.T) {
	c := cache.NewMemory(10)

	if err := c.Set(context.Background(), "k", []byte("v"), 20*time.Millisecond); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Should be present immediately.
	if _, ok := c.Get(context.Background(), "k"); !ok {
		t.Fatal("expected cache hit before TTL expiry")
	}

	time.Sleep(40 * time.Millisecond)

	// Should be expired now.
	if _, ok := c.Get(context.Background(), "k"); ok {
		t.Fatal("expected cache miss after TTL expiry")
	}
}

func TestMemoryCacheLRUEviction(t *testing.T) {
	// maxSize=3: inserting a 4th entry should evict the LRU item.
	c := cache.NewMemory(3)
	ctx := context.Background()

	if err := c.Set(ctx, "a", []byte("1"), time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := c.Set(ctx, "b", []byte("2"), time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := c.Set(ctx, "c", []byte("3"), time.Hour); err != nil {
		t.Fatal(err)
	}

	// Access "a" so that "b" becomes the LRU.
	if _, ok := c.Get(ctx, "a"); !ok {
		t.Fatal("expected hit for a")
	}

	// Insert "d" — should evict "b" (LRU).
	if err := c.Set(ctx, "d", []byte("4"), time.Hour); err != nil {
		t.Fatal(err)
	}

	if _, ok := c.Get(ctx, "b"); ok {
		t.Error("expected 'b' to be evicted (LRU)")
	}
	if _, ok := c.Get(ctx, "a"); !ok {
		t.Error("expected 'a' to still be present")
	}
	if _, ok := c.Get(ctx, "c"); !ok {
		t.Error("expected 'c' to still be present")
	}
	if _, ok := c.Get(ctx, "d"); !ok {
		t.Error("expected 'd' to be present")
	}
}

func TestMemoryCacheDelete(t *testing.T) {
	c := cache.NewMemory(10)
	ctx := context.Background()

	_ = c.Set(ctx, "k", []byte("v"), time.Hour)

	if err := c.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if _, ok := c.Get(ctx, "k"); ok {
		t.Fatal("expected cache miss after Delete")
	}
}

func TestMemoryCacheDeleteMissingKey(t *testing.T) {
	c := cache.NewMemory(10)
	// Deleting a non-existent key must not error.
	if err := c.Delete(context.Background(), "nope"); err != nil {
		t.Fatalf("Delete of missing key should not error: %v", err)
	}
}

func TestMemoryCacheConcurrentSafety(t *testing.T) {
	c := cache.NewMemory(100)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", i%10)
			_ = c.Set(ctx, key, []byte("value"), time.Hour)
			_, _ = c.Get(ctx, key)
			_ = c.Delete(ctx, key)
		}()
	}
	wg.Wait()
}

func TestMemoryCacheClose(t *testing.T) {
	c := cache.NewMemory(10)
	if err := c.Close(); err != nil {
		t.Fatalf("Close returned unexpected error: %v", err)
	}
}

func TestMemoryCacheOverwriteUpdatesValue(t *testing.T) {
	c := cache.NewMemory(10)
	ctx := context.Background()

	_ = c.Set(ctx, "k", []byte("old"), time.Hour)
	_ = c.Set(ctx, "k", []byte("new"), time.Hour)

	got, ok := c.Get(ctx, "k")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if !bytes.Equal(got, []byte("new")) {
		t.Errorf("expected 'new', got %q", got)
	}
}

// --- FileCache tests ---

func TestFileCacheGetMiss(t *testing.T) {
	dir := t.TempDir()
	c, err := cache.NewFile(dir, time.Hour)
	if err != nil {
		t.Fatalf("NewFile failed: %v", err)
	}
	defer c.Close()

	_, ok := c.Get(context.Background(), "nonexistent")
	if ok {
		t.Fatal("expected cache miss for unknown key")
	}
}

func TestFileCacheSetAndGet(t *testing.T) {
	dir := t.TempDir()
	c, err := cache.NewFile(dir, time.Hour)
	if err != nil {
		t.Fatalf("NewFile failed: %v", err)
	}
	defer c.Close()

	want := []byte("file cache data")
	if err := c.Set(context.Background(), "page1", want, time.Hour); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	got, ok := c.Get(context.Background(), "page1")
	if !ok {
		t.Fatal("expected cache hit after Set")
	}
	if !bytes.Equal(got, want) {
		t.Errorf("Get returned %q, want %q", got, want)
	}
}

func TestFileCacheTTLExpiry(t *testing.T) {
	dir := t.TempDir()
	c, err := cache.NewFile(dir, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("NewFile failed: %v", err)
	}
	defer c.Close()

	_ = c.Set(context.Background(), "k", []byte("v"), 20*time.Millisecond)

	if _, ok := c.Get(context.Background(), "k"); !ok {
		t.Fatal("expected hit before TTL expiry")
	}

	time.Sleep(40 * time.Millisecond)

	if _, ok := c.Get(context.Background(), "k"); ok {
		t.Fatal("expected miss after TTL expiry")
	}
}

func TestFileCacheDelete(t *testing.T) {
	dir := t.TempDir()
	c, err := cache.NewFile(dir, time.Hour)
	if err != nil {
		t.Fatalf("NewFile failed: %v", err)
	}
	defer c.Close()
	ctx := context.Background()

	_ = c.Set(ctx, "k", []byte("v"), time.Hour)

	if err := c.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if _, ok := c.Get(ctx, "k"); ok {
		t.Fatal("expected cache miss after Delete")
	}
}

func TestFileCacheDeleteMissingKey(t *testing.T) {
	dir := t.TempDir()
	c, err := cache.NewFile(dir, time.Hour)
	if err != nil {
		t.Fatalf("NewFile failed: %v", err)
	}
	defer c.Close()

	if err := c.Delete(context.Background(), "ghost"); err != nil {
		t.Fatalf("Delete of missing key should not error: %v", err)
	}
}

func TestFileCacheCreatesDirectoryIfMissing(t *testing.T) {
	dir := t.TempDir()
	subdir := dir + "/sub/cache"

	c, err := cache.NewFile(subdir, time.Hour)
	if err != nil {
		t.Fatalf("NewFile with non-existent subdir failed: %v", err)
	}
	defer c.Close()

	if _, err := os.Stat(subdir); err != nil {
		t.Fatalf("expected directory to be created: %v", err)
	}
}

func TestFileCachePersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	want := []byte("persistent data")

	c1, err := cache.NewFile(dir, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_ = c1.Set(context.Background(), "persistent", want, time.Hour)
	_ = c1.Close()

	// New instance reads data written by previous instance.
	c2, err := cache.NewFile(dir, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()

	got, ok := c2.Get(context.Background(), "persistent")
	if !ok {
		t.Fatal("expected hit from persisted data")
	}
	if !bytes.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
