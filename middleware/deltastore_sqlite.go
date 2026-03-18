package middleware

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // SQLite driver (CGO-free)
)

// SQLiteDeltaStore is a persistent DeltaStore backed by a local SQLite file.
// State survives process restarts — unlike MemoryDeltaStore — making it
// suitable for single-node production crawls where Redis is unavailable.
//
// SQLiteDeltaStore is safe for concurrent use; WAL mode is enabled to allow
// concurrent readers while a single writer commits.
type SQLiteDeltaStore struct {
	db *sql.DB
}

// NewSQLiteDeltaStore opens (or creates) a SQLite database at dbPath and
// ensures the deltafetch table exists.
func NewSQLiteDeltaStore(dbPath string) (*SQLiteDeltaStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("sqlite delta store: opening %q: %w", dbPath, err)
	}

	// Enable WAL mode for better write concurrency.
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite delta store: enabling WAL: %w", err)
	}

	const createSQL = `
		CREATE TABLE IF NOT EXISTS deltafetch (
			key     TEXT    PRIMARY KEY,
			seen_at INTEGER NOT NULL
		)`
	if _, err := db.Exec(createSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite delta store: creating table: %w", err)
	}

	return &SQLiteDeltaStore{db: db}, nil
}

// Seen implements DeltaStore.
// Returns (true, time) when key is found, (false, zero) otherwise.
func (s *SQLiteDeltaStore) Seen(key string) (bool, time.Time) {
	var unixSec int64
	err := s.db.QueryRow(
		`SELECT seen_at FROM deltafetch WHERE key = ?`, key,
	).Scan(&unixSec)

	if err == sql.ErrNoRows {
		return false, time.Time{}
	}
	if err != nil {
		// Treat read errors conservatively: report as unseen so the fetch proceeds.
		return false, time.Time{}
	}

	return true, time.Unix(unixSec, 0)
}

// Mark implements DeltaStore.
// Inserts or replaces the key with the current Unix timestamp.
func (s *SQLiteDeltaStore) Mark(key string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO deltafetch (key, seen_at) VALUES (?, ?)`,
		key, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("sqlite delta store: marking %q: %w", key, err)
	}
	return nil
}

// Close closes the underlying SQLite database connection.
func (s *SQLiteDeltaStore) Close() error {
	return s.db.Close()
}
