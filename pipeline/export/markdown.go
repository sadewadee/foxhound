package export

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	foxhound "github.com/sadewadee/foxhound"
)

// MarkdownFormat selects the output style for MarkdownWriter.
type MarkdownFormat int

const (
	// MarkdownTable writes all items as rows in a single pipe-delimited table.
	// Headers are collected from the first item and written once.
	MarkdownTable MarkdownFormat = iota
	// MarkdownList writes each item as a single bullet line.
	// The first (alphabetically sorted) field is bolded; the rest are
	// dash-separated.
	MarkdownList
	// MarkdownCards writes each item as a level-2 heading (first field)
	// followed by bold-key bullet points for remaining fields.
	MarkdownCards
)

// MarkdownWriter exports scraped items to a Markdown file.
// It implements the foxhound.Writer interface.
type MarkdownWriter struct {
	file    *os.File
	format  MarkdownFormat
	headers []string // set from first item (Table format)
	count   int      // items written so far
}

// NewMarkdown opens path for writing and returns a MarkdownWriter.
// Returns an error if the file cannot be created.
func NewMarkdown(path string, format MarkdownFormat) (*MarkdownWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("export: opening Markdown output file %q: %w", path, err)
	}
	return &MarkdownWriter{file: f, format: format}, nil
}

// Write outputs item to the Markdown file according to the configured format.
func (w *MarkdownWriter) Write(_ context.Context, item *foxhound.Item) error {
	switch w.format {
	case MarkdownTable:
		return w.writeTable(item)
	case MarkdownList:
		return w.writeList(item)
	case MarkdownCards:
		return w.writeCard(item)
	default:
		return fmt.Errorf("export: unknown MarkdownFormat %d", w.format)
	}
}

// writeTable emits a GFM pipe table. The header and separator rows are
// written exactly once — from the first item's sorted field keys.
func (w *MarkdownWriter) writeTable(item *foxhound.Item) error {
	keys := sortedKeys(item)

	if w.count == 0 {
		// Capture header order so subsequent rows match.
		w.headers = keys

		// Header row: | key1 | key2 | ...
		if _, err := fmt.Fprintf(w.file, "| %s |\n", strings.Join(keys, " | ")); err != nil {
			return fmt.Errorf("export: writing Markdown table header: %w", err)
		}
		// Separator row: |---|---|...
		seps := make([]string, len(keys))
		for i := range keys {
			seps[i] = "---"
		}
		if _, err := fmt.Fprintf(w.file, "| %s |\n", strings.Join(seps, " | ")); err != nil {
			return fmt.Errorf("export: writing Markdown table separator: %w", err)
		}
	}

	// Data row — use captured header order; missing fields become empty.
	cols := make([]string, len(w.headers))
	for i, h := range w.headers {
		val, ok := item.Get(h)
		if !ok || val == nil {
			cols[i] = ""
		} else {
			// Escape pipe characters inside cell values.
			cols[i] = strings.ReplaceAll(fmt.Sprintf("%v", val), "|", "\\|")
		}
	}
	if _, err := fmt.Fprintf(w.file, "| %s |\n", strings.Join(cols, " | ")); err != nil {
		return fmt.Errorf("export: writing Markdown table row: %w", err)
	}

	w.count++
	return nil
}

// writeList emits "- **firstField** — field2 — field3 — ..."
func (w *MarkdownWriter) writeList(item *foxhound.Item) error {
	keys := sortedKeys(item)
	if len(keys) == 0 {
		if _, err := w.file.WriteString("- \n"); err != nil {
			return fmt.Errorf("export: writing Markdown list item: %w", err)
		}
		w.count++
		return nil
	}

	// First field: bold title.
	firstVal := fieldStr(item, keys[0])
	parts := []string{fmt.Sprintf("**%s**", firstVal)}

	// Remaining fields appended as plain values.
	for _, k := range keys[1:] {
		parts = append(parts, fieldStr(item, k))
	}

	line := "- " + strings.Join(parts, " — ") + "\n"
	if _, err := w.file.WriteString(line); err != nil {
		return fmt.Errorf("export: writing Markdown list item: %w", err)
	}

	w.count++
	return nil
}

// writeCard emits:
//
//	## firstFieldValue
//	- **key**: value
//	- **key**: value
//	(blank line)
func (w *MarkdownWriter) writeCard(item *foxhound.Item) error {
	keys := sortedKeys(item)

	// Blank line between cards (after the first).
	if w.count > 0 {
		if _, err := w.file.WriteString("\n"); err != nil {
			return fmt.Errorf("export: writing Markdown card separator: %w", err)
		}
	}

	// Heading from first field.
	heading := ""
	bodyKeys := keys
	if len(keys) > 0 {
		heading = fieldStr(item, keys[0])
		bodyKeys = keys[1:]
	}
	if _, err := fmt.Fprintf(w.file, "## %s\n", heading); err != nil {
		return fmt.Errorf("export: writing Markdown card heading: %w", err)
	}

	// Remaining fields as bullet points.
	for _, k := range bodyKeys {
		val := fieldStr(item, k)
		if _, err := fmt.Fprintf(w.file, "- **%s**: %s\n", k, val); err != nil {
			return fmt.Errorf("export: writing Markdown card field: %w", err)
		}
	}

	w.count++
	return nil
}

// Flush syncs buffered data to disk.
func (w *MarkdownWriter) Flush(_ context.Context) error {
	return w.file.Sync()
}

// Close closes the underlying file.
func (w *MarkdownWriter) Close() error {
	return w.file.Close()
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// sortedKeys returns item.Fields keys in ascending order.
func sortedKeys(item *foxhound.Item) []string {
	keys := make([]string, 0, len(item.Fields))
	for k := range item.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// fieldStr returns the field value as a string, or "" if absent.
func fieldStr(item *foxhound.Item, key string) string {
	val, ok := item.Get(key)
	if !ok || val == nil {
		return ""
	}
	return fmt.Sprintf("%v", val)
}
