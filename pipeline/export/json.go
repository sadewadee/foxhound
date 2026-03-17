// Package export provides Writer implementations for exporting scraped items
// to various formats and destinations.
package export

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	foxhound "github.com/foxhound-scraper/foxhound"
)

// JSONFormat selects the output format for JSONWriter.
type JSONFormat int

const (
	// JSONArray writes a single JSON array containing all items.
	// The array is opened on creation and closed on Close().
	JSONArray JSONFormat = iota
	// JSONLines writes one JSON object per line (NDJSON / JSON Lines format).
	JSONLines
)

// JSONWriter exports items to a JSON or JSON Lines file.
// It implements the foxhound.Writer interface.
type JSONWriter struct {
	file   *os.File
	format JSONFormat
	enc    *json.Encoder
	count  int
}

// NewJSON opens path for writing and returns a JSONWriter.
// For JSONArray format the opening bracket is written immediately.
// Returns an error if the file cannot be created.
func NewJSON(path string, format JSONFormat) (*JSONWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("export: opening JSON output file %q: %w", path, err)
	}

	w := &JSONWriter{
		file:   f,
		format: format,
		enc:    json.NewEncoder(f),
	}

	if format == JSONArray {
		if _, err := f.WriteString("[\n"); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("export: writing JSON array opening bracket: %w", err)
		}
	}
	return w, nil
}

// Write serialises item.Fields and appends it to the output.
// For JSONArray, commas are inserted between items automatically.
func (w *JSONWriter) Write(_ context.Context, item *foxhound.Item) error {
	switch w.format {
	case JSONLines:
		if err := w.enc.Encode(item.Fields); err != nil {
			return fmt.Errorf("export: encoding JSON line: %w", err)
		}
	case JSONArray:
		if w.count > 0 {
			if _, err := w.file.WriteString(",\n"); err != nil {
				return fmt.Errorf("export: writing JSON array separator: %w", err)
			}
		}
		data, err := json.Marshal(item.Fields)
		if err != nil {
			return fmt.Errorf("export: marshalling JSON array item: %w", err)
		}
		if _, err := w.file.Write(data); err != nil {
			return fmt.Errorf("export: writing JSON array item: %w", err)
		}
	}
	w.count++
	return nil
}

// Flush syncs buffered data to disk. For JSONWriter, the json.Encoder writes
// directly to the underlying file so this is primarily a fsync hint.
func (w *JSONWriter) Flush(_ context.Context) error {
	return w.file.Sync()
}

// Close finalises the output file. For JSONArray it writes the closing bracket.
// Always closes the underlying file handle.
func (w *JSONWriter) Close() error {
	if w.format == JSONArray {
		var closing string
		if w.count > 0 {
			closing = "\n]"
		} else {
			closing = "]"
		}
		if _, err := w.file.WriteString(closing); err != nil {
			_ = w.file.Close()
			return fmt.Errorf("export: writing JSON array closing bracket: %w", err)
		}
	}
	return w.file.Close()
}
