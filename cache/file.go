package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// FileCache stores cached responses as raw byte files on disk.
//
// File naming:
//   - Each key is hashed with SHA-256 and the hex digest is used as the
//     filename, preventing path-traversal and collisions from arbitrary keys.
//
// TTL strategy:
//   - The file modification time (mtime) is compared against ttl on each Get.
//   - Set always overwrites the file and resets mtime via os.WriteFile.
type FileCache struct {
	dir string
	ttl time.Duration
}

// NewFile creates a FileCache that stores files in dir with the given default
// TTL. dir is created (with all parents) if it does not already exist.
// A ttl of zero means entries never expire.
func NewFile(dir string, ttl time.Duration) (*FileCache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("cache/file: create dir %q: %w", dir, err)
	}
	return &FileCache{dir: dir, ttl: ttl}, nil
}

// Get retrieves the cached bytes for key. Returns (nil, false) on miss or
// when the file is older than the TTL. Expired files are deleted lazily.
func (c *FileCache) Get(_ context.Context, key string) ([]byte, bool) {
	path := c.pathFor(key)

	info, err := os.Stat(path)
	if err != nil {
		// Missing file is a normal cache miss.
		return nil, false
	}

	if c.ttl > 0 && time.Since(info.ModTime()) > c.ttl {
		slog.Debug("cache/file: entry expired", "key", key, "path", path)
		_ = os.Remove(path) // lazy expiry
		return nil, false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		slog.Warn("cache/file: read failed", "key", key, "path", path, "err", err)
		return nil, false
	}

	return data, true
}

// Set writes value to disk, overwriting any existing file for key.
// The TTL parameter is stored implicitly via the file's mtime.
func (c *FileCache) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	path := c.pathFor(key)
	if err := os.WriteFile(path, value, 0o644); err != nil {
		return fmt.Errorf("cache/file: write %q: %w", path, err)
	}
	slog.Debug("cache/file: set", "key", key, "path", path, "bytes", len(value))
	return nil
}

// Delete removes the cached file for key. Returns nil if key does not exist.
func (c *FileCache) Delete(_ context.Context, key string) error {
	path := c.pathFor(key)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cache/file: delete %q: %w", path, err)
	}
	slog.Debug("cache/file: deleted", "key", key)
	return nil
}

// Close is a no-op for the file cache.
func (c *FileCache) Close() error { return nil }

// pathFor returns the absolute file path for a given cache key.
func (c *FileCache) pathFor(key string) string {
	h := sha256.Sum256([]byte(key))
	return filepath.Join(c.dir, hex.EncodeToString(h[:]))
}
