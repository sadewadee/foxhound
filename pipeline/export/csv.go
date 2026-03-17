package export

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"sort"

	foxhound "github.com/foxhound-scraper/foxhound"
)

// CSVWriter exports items to a CSV file.
// It implements the foxhound.Writer interface.
//
// If headers are provided at construction they are used as the column order.
// If no headers are provided, they are inferred (sorted alphabetically) from
// the first item written. Missing field values are written as empty strings.
type CSVWriter struct {
	file    *os.File
	writer  *csv.Writer
	headers []string
	written bool // true once the header row has been written
}

// NewCSV opens path for writing and returns a CSVWriter.
// Provide explicit headers to fix the column order; omit them to infer from
// the first item written. Returns an error if the file cannot be created.
func NewCSV(path string, headers ...string) (*CSVWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("export: opening CSV output file %q: %w", path, err)
	}
	w := &CSVWriter{
		file:    f,
		writer:  csv.NewWriter(f),
		headers: headers,
	}
	return w, nil
}

// Write serialises item.Fields as a CSV row. On the first Write the header
// row is emitted. If headers were not provided at construction they are
// inferred from this item's field keys (sorted alphabetically).
func (w *CSVWriter) Write(_ context.Context, item *foxhound.Item) error {
	if !w.written {
		if len(w.headers) == 0 {
			// Infer headers from first item — sorted for determinism.
			for k := range item.Fields {
				w.headers = append(w.headers, k)
			}
			sort.Strings(w.headers)
		}
		if err := w.writer.Write(w.headers); err != nil {
			return fmt.Errorf("export: writing CSV header row: %w", err)
		}
		w.written = true
	}

	row := make([]string, len(w.headers))
	for i, h := range w.headers {
		val, ok := item.Get(h)
		if !ok || val == nil {
			row[i] = ""
		} else {
			row[i] = fmt.Sprintf("%v", val)
		}
	}
	if err := w.writer.Write(row); err != nil {
		return fmt.Errorf("export: writing CSV data row: %w", err)
	}
	return nil
}

// Flush ensures all buffered CSV data is written to the file.
func (w *CSVWriter) Flush(_ context.Context) error {
	w.writer.Flush()
	if err := w.writer.Error(); err != nil {
		return fmt.Errorf("export: flushing CSV writer: %w", err)
	}
	return w.file.Sync()
}

// Close flushes and closes the underlying file.
func (w *CSVWriter) Close() error {
	w.writer.Flush()
	if err := w.writer.Error(); err != nil {
		_ = w.file.Close()
		return fmt.Errorf("export: flushing CSV on close: %w", err)
	}
	return w.file.Close()
}
