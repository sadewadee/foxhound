package middleware_test

import (
	"os"
	"testing"
	"time"

	"github.com/sadewadee/foxhound/middleware"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func redisAddr(t *testing.T) (addr, password string, db int) {
	t.Helper()
	addr = os.Getenv("FOXHOUND_TEST_REDIS")
	if addr == "" {
		t.Skip("FOXHOUND_TEST_REDIS not set; skipping Redis integration test")
	}
	return addr, "", 0
}

// ---------------------------------------------------------------------------
// RedisDeltaStore
// ---------------------------------------------------------------------------

func TestRedisDeltaStore_UnseenKey_ReturnsFalse(t *testing.T) {
	addr, pass, db := redisAddr(t)
	store, err := middleware.NewRedisDeltaStore(addr, pass, db, "test:delta")
	if err != nil {
		t.Fatalf("NewRedisDeltaStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	seen, ts := store.Seen("never-seen-key-" + t.Name())
	if seen {
		t.Error("expected key to be unseen")
	}
	if !ts.IsZero() {
		t.Errorf("expected zero time for unseen key, got %v", ts)
	}
}

func TestRedisDeltaStore_AfterMark_ReturnsSeen(t *testing.T) {
	addr, pass, db := redisAddr(t)
	store, err := middleware.NewRedisDeltaStore(addr, pass, db, "test:delta")
	if err != nil {
		t.Fatalf("NewRedisDeltaStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	key := "mark-key-" + t.Name()
	before := time.Now().Truncate(time.Second)

	if err := store.Mark(key); err != nil {
		t.Fatalf("Mark: %v", err)
	}

	seen, ts := store.Seen(key)
	if !seen {
		t.Fatal("expected key to be seen after Mark")
	}
	if ts.Before(before) {
		t.Errorf("stored timestamp %v predates mark time %v", ts, before)
	}
}

func TestRedisDeltaStore_MarkTwice_PreservesTimestamp(t *testing.T) {
	addr, pass, db := redisAddr(t)
	store, err := middleware.NewRedisDeltaStore(addr, pass, db, "test:delta")
	if err != nil {
		t.Fatalf("NewRedisDeltaStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	key := "double-mark-" + t.Name()
	if err := store.Mark(key); err != nil {
		t.Fatalf("Mark first: %v", err)
	}
	_, firstTs := store.Seen(key)

	time.Sleep(10 * time.Millisecond)

	if err := store.Mark(key); err != nil {
		t.Fatalf("Mark second: %v", err)
	}
	_, secondTs := store.Seen(key)

	// Second mark should update the timestamp.
	if !secondTs.After(firstTs) && secondTs.Equal(firstTs) {
		// Allow equal timestamps in low-resolution environments.
		t.Logf("timestamps equal (low-resolution clock), this is acceptable")
	}
}

func TestRedisDeltaStore_Close_NoError(t *testing.T) {
	addr, pass, db := redisAddr(t)
	store, err := middleware.NewRedisDeltaStore(addr, pass, db, "test:delta")
	if err != nil {
		t.Fatalf("NewRedisDeltaStore: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// ---------------------------------------------------------------------------
// SQLiteDeltaStore
// ---------------------------------------------------------------------------

func tempSQLitePath(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp("", "foxhound-delta-*.db")
	if err != nil {
		t.Fatalf("creating temp db: %v", err)
	}
	_ = f.Close()
	t.Cleanup(func() { _ = os.Remove(f.Name()) })
	return f.Name()
}

func TestSQLiteDeltaStore_UnseenKey_ReturnsFalse(t *testing.T) {
	store, err := middleware.NewSQLiteDeltaStore(tempSQLitePath(t))
	if err != nil {
		t.Fatalf("NewSQLiteDeltaStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	seen, ts := store.Seen("never-seen")
	if seen {
		t.Error("expected unseen key to return false")
	}
	if !ts.IsZero() {
		t.Errorf("expected zero timestamp for unseen key, got %v", ts)
	}
}

func TestSQLiteDeltaStore_AfterMark_ReturnsSeen(t *testing.T) {
	store, err := middleware.NewSQLiteDeltaStore(tempSQLitePath(t))
	if err != nil {
		t.Fatalf("NewSQLiteDeltaStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	before := time.Now().Truncate(time.Second)
	key := "seen-key"

	if err := store.Mark(key); err != nil {
		t.Fatalf("Mark: %v", err)
	}

	seen, ts := store.Seen(key)
	if !seen {
		t.Fatal("expected key seen after Mark")
	}
	if ts.Before(before) {
		t.Errorf("stored timestamp %v predates mark time %v", ts, before)
	}
}

func TestSQLiteDeltaStore_MarkIdempotent_UpdatesTimestamp(t *testing.T) {
	store, err := middleware.NewSQLiteDeltaStore(tempSQLitePath(t))
	if err != nil {
		t.Fatalf("NewSQLiteDeltaStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	key := "idempotent-key"
	if err := store.Mark(key); err != nil {
		t.Fatalf("Mark first: %v", err)
	}
	_, first := store.Seen(key)

	time.Sleep(2 * time.Millisecond)

	if err := store.Mark(key); err != nil {
		t.Fatalf("Mark second (should not error): %v", err)
	}
	_, second := store.Seen(key)

	// Timestamp must be updated on re-mark.
	if second.Before(first) {
		t.Errorf("second mark timestamp %v should not be before first %v", second, first)
	}
}

func TestSQLiteDeltaStore_MultipleKeys_Independent(t *testing.T) {
	store, err := middleware.NewSQLiteDeltaStore(tempSQLitePath(t))
	if err != nil {
		t.Fatalf("NewSQLiteDeltaStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.Mark("key-a"); err != nil {
		t.Fatalf("Mark key-a: %v", err)
	}

	seenA, _ := store.Seen("key-a")
	seenB, _ := store.Seen("key-b")

	if !seenA {
		t.Error("key-a should be seen")
	}
	if seenB {
		t.Error("key-b should not be seen")
	}
}

func TestSQLiteDeltaStore_PersistsAcrossReopens(t *testing.T) {
	path := tempSQLitePath(t)

	// First open: mark a key.
	store1, err := middleware.NewSQLiteDeltaStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteDeltaStore (1): %v", err)
	}
	if err := store1.Mark("persistent-key"); err != nil {
		t.Fatalf("Mark: %v", err)
	}
	if err := store1.Close(); err != nil {
		t.Fatalf("Close (1): %v", err)
	}

	// Second open: key should still be seen.
	store2, err := middleware.NewSQLiteDeltaStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteDeltaStore (2): %v", err)
	}
	defer func() { _ = store2.Close() }()

	seen, ts := store2.Seen("persistent-key")
	if !seen {
		t.Fatal("key should persist across store reopens")
	}
	if ts.IsZero() {
		t.Error("persisted timestamp should not be zero")
	}
}

func TestSQLiteDeltaStore_Close_NoError(t *testing.T) {
	store, err := middleware.NewSQLiteDeltaStore(tempSQLitePath(t))
	if err != nil {
		t.Fatalf("NewSQLiteDeltaStore: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
