package parse

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	// Pure-Go SQLite driver — no CGO required.
	_ "modernc.org/sqlite"
)

// AdaptiveStore is the interface for persisting element signatures.
// Implementations handle the actual storage mechanism (file, SQLite, etc.).
type AdaptiveStore interface {
	// Save persists an element signature for a domain and selector name.
	Save(domain, name string, sig *ElementSignature) error
	// Load retrieves a previously saved signature.
	Load(domain, name string) (*ElementSignature, error)
	// Close releases storage resources.
	Close() error
}

// SQLiteAdaptiveStore persists adaptive selector signatures in a SQLite
// database, keyed by domain and selector name. This allows element tracking
// across scraping runs and supports concurrent access.
//
// The storage is thread-safe via an RWMutex and SQLite WAL mode for
// concurrent reads.
type SQLiteAdaptiveStore struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewSQLiteAdaptiveStore creates a new SQLite-backed adaptive storage at the
// given database path. The database and table are created if they don't exist.
func NewSQLiteAdaptiveStore(dbPath string) (*SQLiteAdaptiveStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("parse: adaptive sqlite: open %q: %w", dbPath, err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("parse: adaptive sqlite: set WAL: %w", err)
	}

	// Create the storage table.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS adaptive_elements (
			id INTEGER PRIMARY KEY,
			domain TEXT NOT NULL,
			identifier TEXT NOT NULL,
			signature_data TEXT NOT NULL,
			UNIQUE (domain, identifier)
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("parse: adaptive sqlite: create table: %w", err)
	}

	return &SQLiteAdaptiveStore{db: db}, nil
}

// Save persists an element signature. If a signature already exists for
// the given domain and name, it is replaced.
func (s *SQLiteAdaptiveStore) Save(domain, name string, sig *ElementSignature) error {
	if sig == nil {
		return fmt.Errorf("parse: adaptive sqlite: signature must not be nil")
	}

	data, err := json.Marshal(sig)
	if err != nil {
		return fmt.Errorf("parse: adaptive sqlite: marshal signature: %w", err)
	}

	normalizedDomain := normalizeDomain(domain)
	hashedID := hashIdentifier(name)

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err = s.db.Exec(`
		INSERT OR REPLACE INTO adaptive_elements (domain, identifier, signature_data)
		VALUES (?, ?, ?)
	`, normalizedDomain, hashedID, string(data))

	if err != nil {
		return fmt.Errorf("parse: adaptive sqlite: save: %w", err)
	}
	return nil
}

// Load retrieves a previously saved signature. Returns nil, nil when no
// signature is found for the given domain and name.
func (s *SQLiteAdaptiveStore) Load(domain, name string) (*ElementSignature, error) {
	normalizedDomain := normalizeDomain(domain)
	hashedID := hashIdentifier(name)

	s.mu.RLock()
	defer s.mu.RUnlock()

	var data string
	err := s.db.QueryRow(`
		SELECT signature_data FROM adaptive_elements
		WHERE domain = ? AND identifier = ?
	`, normalizedDomain, hashedID).Scan(&data)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("parse: adaptive sqlite: load: %w", err)
	}

	var sig ElementSignature
	if err := json.Unmarshal([]byte(data), &sig); err != nil {
		return nil, fmt.Errorf("parse: adaptive sqlite: unmarshal: %w", err)
	}

	return &sig, nil
}

// Close releases the database connection.
func (s *SQLiteAdaptiveStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}

// normalizeDomain lowercases the domain for consistent keying.
func normalizeDomain(domain string) string {
	return strings.ToLower(strings.TrimSpace(domain))
}

// hashIdentifier creates a SHA-256 hash of the identifier with length suffix
// to reduce collision probability.
func hashIdentifier(id string) string {
	cleaned := strings.ToLower(strings.TrimSpace(id))
	h := sha256.Sum256([]byte(cleaned))
	return fmt.Sprintf("%x_%d", h, len(cleaned))
}

// FileAdaptiveStore wraps the existing JSON file-based storage to implement
// the AdaptiveStore interface.
type FileAdaptiveStore struct {
	extractor *AdaptiveExtractor
}

// NewFileAdaptiveStore creates a file-based adaptive store using the existing
// JSON persistence mechanism.
func NewFileAdaptiveStore(path string) *FileAdaptiveStore {
	return &FileAdaptiveStore{
		extractor: NewAdaptiveExtractor(path),
	}
}

// Save persists a signature using the file-based JSON storage.
func (fs *FileAdaptiveStore) Save(domain, name string, sig *ElementSignature) error {
	key := domain + ":" + name
	fs.extractor.mu.Lock()
	if _, ok := fs.extractor.selectors[key]; !ok {
		fs.extractor.selectors[key] = &AdaptiveSelector{
			Name:     key,
			MinScore: 0.4,
		}
	}
	fs.extractor.selectors[key].Signature = sig
	fs.extractor.mu.Unlock()
	return fs.extractor.Save()
}

// Load retrieves a signature from the file-based JSON storage.
func (fs *FileAdaptiveStore) Load(domain, name string) (*ElementSignature, error) {
	key := domain + ":" + name
	fs.extractor.mu.RLock()
	defer fs.extractor.mu.RUnlock()
	if s, ok := fs.extractor.selectors[key]; ok && s.Signature != nil {
		return s.Signature, nil
	}
	return nil, nil
}

// Close saves the file and releases resources.
func (fs *FileAdaptiveStore) Close() error {
	return fs.extractor.Save()
}
