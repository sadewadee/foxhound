package cache_test

// sqlite_test.go tests the SQLiteCache backend.
//
// All tests use a temporary file so they are fully isolated and clean up
// automatically via t.TempDir().

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/foxhound-scraper/foxhound/cache"
)

// newSQLiteCache creates a SQLiteCache backed by a temp file.
func newSQLiteCache(t *testing.T) *cache.SQLiteCache {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cache.db")
	c, err := cache.NewSQLite(path)
	if err != nil {
		t.Fatalf("NewSQLite failed: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestSQLiteCache_GetMiss(t *testing.T) {
	c := newSQLiteCache(t)
	_, ok := c.Get(context.Background(), "nonexistent")
	if ok {
		t.Fatal("expected cache miss for nonexistent key")
	}
}

func TestSQLiteCache_SetAndGet(t *testing.T) {
	c := newSQLiteCache(t)
	ctx := context.Background()
	want := []byte("hello sqlite")

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

func TestSQLiteCache_TTLExpiry(t *testing.T) {
	c := newSQLiteCache(t)
	ctx := context.Background()

	if err := c.Set(ctx, "expiring", []byte("v"), 50*time.Millisecond); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Entry must be present before the TTL elapses.
	if _, ok := c.Get(ctx, "expiring"); !ok {
		t.Fatal("expected cache hit before TTL expiry")
	}

	time.Sleep(100 * time.Millisecond)

	// Entry must be treated as a miss after TTL has elapsed.
	if _, ok := c.Get(ctx, "expiring"); ok {
		t.Fatal("expected cache miss after TTL expiry")
	}
}

func TestSQLiteCache_Delete(t *testing.T) {
	c := newSQLiteCache(t)
	ctx := context.Background()

	_ = c.Set(ctx, "del-key", []byte("value"), time.Minute)

	if err := c.Delete(ctx, "del-key"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if _, ok := c.Get(ctx, "del-key"); ok {
		t.Fatal("expected cache miss after Delete")
	}
}

func TestSQLiteCache_DeleteMissingKey(t *testing.T) {
	c := newSQLiteCache(t)
	// Deleting a non-existent key must not return an error.
	if err := c.Delete(context.Background(), "ghost"); err != nil {
		t.Fatalf("Delete of missing key should not error: %v", err)
	}
}

func TestSQLiteCache_OverwriteUpdatesValue(t *testing.T) {
	c := newSQLiteCache(t)
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

func TestSQLiteCache_Persistence(t *testing.T) {
	// Write data with one instance, close it, reopen from the same file.
	dbPath := filepath.Join(t.TempDir(), "persist.db")
	want := []byte("persistent value")

	c1, err := cache.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite (first instance) failed: %v", err)
	}
	if err := c1.Set(context.Background(), "persist-key", want, time.Hour); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if err := c1.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Reopen from same path — data must survive.
	c2, err := cache.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite (second instance) failed: %v", err)
	}
	defer c2.Close()

	got, ok := c2.Get(context.Background(), "persist-key")
	if !ok {
		t.Fatal("expected cache hit from persisted data")
	}
	if !bytes.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSQLiteCache_Close(t *testing.T) {
	path := filepath.Join(t.TempDir(), "close.db")
	c, err := cache.NewSQLite(path)
	if err != nil {
		t.Fatalf("NewSQLite failed: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close returned unexpected error: %v", err)
	}
}

func TestSQLiteCache_ZeroTTLNeverExpires(t *testing.T) {
	// A TTL of zero should mean the entry never expires.
	c := newSQLiteCache(t)
	ctx := context.Background()

	if err := c.Set(ctx, "forever", []byte("v"), 0); err != nil {
		t.Fatalf("Set (zero TTL) failed: %v", err)
	}

	got, ok := c.Get(ctx, "forever")
	if !ok {
		t.Fatal("expected cache hit for zero-TTL entry")
	}
	if !bytes.Equal(got, []byte("v")) {
		t.Errorf("got %q, want %q", got, "v")
	}
}
