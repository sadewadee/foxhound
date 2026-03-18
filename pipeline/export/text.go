package export

import (
	"context"
	"fmt"
	"os"
	"strings"

	foxhound "github.com/sadewadee/foxhound"
)

// TextFormat selects the output style for TextWriter.
type TextFormat int

const (
	// TextLines writes one item per line as space-separated key=value pairs.
	TextLines TextFormat = iota
	// TextPretty writes each item as a labelled block surrounded by separators.
	TextPretty
)

const textSeparator = "────────────────────────────"

// TextWriter exports scraped items to a plain-text file.
// It implements the foxhound.Writer interface.
type TextWriter struct {
	file   *os.File
	format TextFormat
	count  int
}

// NewText opens path for writing and returns a TextWriter.
// Returns an error if the file cannot be created.
func NewText(path string, format TextFormat) (*TextWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("export: opening text output file %q: %w", path, err)
	}
	return &TextWriter{file: f, format: format}, nil
}

// Write outputs item to the text file according to the configured format.
func (w *TextWriter) Write(_ context.Context, item *foxhound.Item) error {
	switch w.format {
	case TextLines:
		return w.writeLine(item)
	case TextPretty:
		return w.writePretty(item)
	default:
		return fmt.Errorf("export: unknown TextFormat %d", w.format)
	}
}

// writeLine writes one line: key=value key=value ...
// Keys are emitted in sorted order for determinism.
func (w *TextWriter) writeLine(item *foxhound.Item) error {
	keys := sortedKeys(item)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, fieldStr(item, k)))
	}
	line := strings.Join(parts, " ") + "\n"
	if _, err := w.file.WriteString(line); err != nil {
		return fmt.Errorf("export: writing text line: %w", err)
	}
	w.count++
	return nil
}

// writePretty writes:
//
//	────────────────────────────
//	Key:  value
//	Key2: value2
//	────────────────────────────
func (w *TextWriter) writePretty(item *foxhound.Item) error {
	keys := sortedKeys(item)

	if _, err := fmt.Fprintf(w.file, "%s\n", textSeparator); err != nil {
		return fmt.Errorf("export: writing text pretty separator: %w", err)
	}
	for _, k := range keys {
		// Capitalise the first letter of the key as a label.
		label := strings.ToUpper(k[:1]) + k[1:]
		if _, err := fmt.Fprintf(w.file, "%-8s %s\n", label+":", fieldStr(item, k)); err != nil {
			return fmt.Errorf("export: writing text pretty field: %w", err)
		}
	}
	if _, err := fmt.Fprintf(w.file, "%s\n", textSeparator); err != nil {
		return fmt.Errorf("export: writing text pretty closing separator: %w", err)
	}

	w.count++
	return nil
}

// Flush syncs buffered data to disk.
func (w *TextWriter) Flush(_ context.Context) error {
	return w.file.Sync()
}

// Close closes the underlying file.
func (w *TextWriter) Close() error {
	return w.file.Close()
}
