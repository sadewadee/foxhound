package cache

// sqlite.go implements the Cache interface using SQLite for persistent,
// disk-backed caching across Foxhound runs.
//
// The schema is a single table:
//
//	cache(key TEXT PRIMARY KEY, value BLOB, expires_at INTEGER)
//
// expires_at is stored as a Unix timestamp (seconds). A value of 0 means the
// entry never expires. Get filters out entries whose expires_at is in the past.

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

const sqliteDriver = "sqlite"

// createTableSQL is the DDL executed on every NewSQLite call to ensure the
// schema exists before any reads or writes.
// expires_at is stored as a Unix timestamp in milliseconds for sub-second TTL
// precision. A value of 0 means the entry never expires.
const createTableSQL = `
CREATE TABLE IF NOT EXISTS cache (
	key        TEXT    PRIMARY KEY,
	value      BLOB    NOT NULL,
	expires_at INTEGER NOT NULL DEFAULT 0
);
`

// SQLiteCache implements Cache using SQLite for persistent caching.
// It is safe for concurrent use; SQLite serialises writes internally.
type SQLiteCache struct {
	db *sql.DB
}

// NewSQLite opens (or creates) a SQLite database at dbPath and ensures the
// cache table exists. Returns an error if the file cannot be opened or the
// schema cannot be applied.
func NewSQLite(dbPath string) (*SQLiteCache, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("cache/sqlite: dbPath must not be empty")
	}

	db, err := sql.Open(sqliteDriver, dbPath)
	if err != nil {
		return nil, fmt.Errorf("cache/sqlite: opening %q: %w", dbPath, err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("cache/sqlite: enabling WAL mode: %w", err)
	}

	if _, err := db.Exec(createTableSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("cache/sqlite: creating cache table: %w", err)
	}

	return &SQLiteCache{db: db}, nil
}

// Get retrieves a cached value by key. Returns (value, true) on hit and
// (nil, false) on miss, TTL expiry, or any internal error.
//
// Expired entries are treated as misses; they are not deleted eagerly here
// to avoid write amplification — they are overwritten on the next Set call.
func (c *SQLiteCache) Get(ctx context.Context, key string) ([]byte, bool) {
	const query = `
		SELECT value FROM cache
		WHERE key = ? AND (expires_at = 0 OR expires_at > ?)
	`
	// Use milliseconds so that sub-second TTLs work correctly.
	now := time.Now().UnixMilli()
	var value []byte
	err := c.db.QueryRowContext(ctx, query, key, now).Scan(&value)
	if err != nil {
		// sql.ErrNoRows covers both genuine misses and TTL-expired entries.
		return nil, false
	}
	return value, true
}

// Set stores value under key with the given TTL.
// A TTL of zero means the entry never expires (expires_at stored as 0).
// An existing entry with the same key is replaced (INSERT OR REPLACE).
func (c *SQLiteCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	var expiresAt int64
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl).UnixMilli()
	}

	const upsert = `
		INSERT OR REPLACE INTO cache (key, value, expires_at)
		VALUES (?, ?, ?)
	`
	if _, err := c.db.ExecContext(ctx, upsert, key, value, expiresAt); err != nil {
		return fmt.Errorf("cache/sqlite: Set %q: %w", key, err)
	}
	return nil
}

// Delete removes a cached entry. Returns nil if the key does not exist.
func (c *SQLiteCache) Delete(ctx context.Context, key string) error {
	const del = `DELETE FROM cache WHERE key = ?`
	if _, err := c.db.ExecContext(ctx, del, key); err != nil {
		return fmt.Errorf("cache/sqlite: Delete %q: %w", key, err)
	}
	return nil
}

// Close releases the underlying database connection pool.
func (c *SQLiteCache) Close() error {
	if err := c.db.Close(); err != nil {
		return fmt.Errorf("cache/sqlite: Close: %w", err)
	}
	return nil
}
