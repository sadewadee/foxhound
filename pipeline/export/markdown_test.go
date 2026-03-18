package export_test

import (
	"context"
	"os"
	"strings"
	"testing"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/pipeline/export"
)

// ---------------------------------------------------------------------------
// MarkdownTable
// ---------------------------------------------------------------------------

func TestMarkdownWriter_Table_WritesHeaderAndRows(t *testing.T) {
	path := tempFile(t, ".md")
	w, err := export.NewMarkdown(path, export.MarkdownTable)
	if err != nil {
		t.Fatalf("NewMarkdown error: %v", err)
	}
	ctx := context.Background()

	items := []*foxhound.Item{
		makeItem(map[string]any{"title": "Widget Pro", "price": "$29.99"}),
		makeItem(map[string]any{"title": "Gadget X", "price": "$49.99"}),
	}
	for _, it := range items {
		if err := w.Write(ctx, it); err != nil {
			t.Fatalf("Write error: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	content := string(data)

	// Must contain the pipe-delimited header.
	if !strings.Contains(content, "|") {
		t.Error("MarkdownTable: output should contain pipe characters")
	}
	// Must contain a separator row with dashes (format: "| --- | --- |").
	if !strings.Contains(content, "---") {
		t.Errorf("MarkdownTable: output should contain separator row with dashes. Got:\n%s", content)
	}
	// Field values should appear.
	if !strings.Contains(content, "Widget Pro") {
		t.Errorf("MarkdownTable: 'Widget Pro' not found in output:\n%s", content)
	}
	if !strings.Contains(content, "Gadget X") {
		t.Errorf("MarkdownTable: 'Gadget X' not found in output:\n%s", content)
	}
}

func TestMarkdownWriter_Table_HeaderRowOnce(t *testing.T) {
	path := tempFile(t, ".md")
	w, err := export.NewMarkdown(path, export.MarkdownTable)
	if err != nil {
		t.Fatalf("NewMarkdown error: %v", err)
	}
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		it := makeItem(map[string]any{"name": "item", "val": i})
		if err := w.Write(ctx, it); err != nil {
			t.Fatalf("Write error: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	// header + separator + 3 rows = 5 lines
	if len(lines) != 5 {
		t.Errorf("MarkdownTable: want 5 lines (header+sep+3rows), got %d:\n%s", len(lines), string(data))
	}
}

func TestMarkdownWriter_Table_SeparatorRowFormat(t *testing.T) {
	path := tempFile(t, ".md")
	w, err := export.NewMarkdown(path, export.MarkdownTable)
	if err != nil {
		t.Fatalf("NewMarkdown error: %v", err)
	}
	ctx := context.Background()

	if err := w.Write(ctx, makeItem(map[string]any{"a": "1", "b": "2"})); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		t.Fatalf("Expected at least 2 lines, got %d", len(lines))
	}
	sep := lines[1]
	if !strings.HasPrefix(sep, "|") || !strings.Contains(sep, "---") {
		t.Errorf("Separator line format wrong: %q", sep)
	}
}

func TestMarkdownWriter_Table_EmptyFile_NoItems(t *testing.T) {
	path := tempFile(t, ".md")
	w, err := export.NewMarkdown(path, export.MarkdownTable)
	if err != nil {
		t.Fatalf("NewMarkdown error: %v", err)
	}
	ctx := context.Background()
	_ = w.Flush(ctx)
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	if strings.TrimSpace(string(data)) != "" {
		t.Errorf("Empty table: expected empty file, got:\n%s", string(data))
	}
}

// ---------------------------------------------------------------------------
// MarkdownList
// ---------------------------------------------------------------------------

func TestMarkdownWriter_List_WritesBulletLines(t *testing.T) {
	path := tempFile(t, ".md")
	w, err := export.NewMarkdown(path, export.MarkdownList)
	if err != nil {
		t.Fatalf("NewMarkdown error: %v", err)
	}
	ctx := context.Background()

	items := []*foxhound.Item{
		makeItem(map[string]any{"title": "Widget Pro", "price": "$29.99"}),
		makeItem(map[string]any{"title": "Gadget X", "price": "$49.99"}),
	}
	for _, it := range items {
		if err := w.Write(ctx, it); err != nil {
			t.Fatalf("Write error: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	lines := strings.Split(strings.TrimSpace(content), "\n")

	// Each item is one line starting with "- "
	if len(lines) != 2 {
		t.Errorf("MarkdownList: want 2 lines, got %d:\n%s", len(lines), content)
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "- ") {
			t.Errorf("MarkdownList: line should start with '- ', got: %q", line)
		}
	}
}

func TestMarkdownWriter_List_FirstFieldBold(t *testing.T) {
	path := tempFile(t, ".md")
	w, err := export.NewMarkdown(path, export.MarkdownList)
	if err != nil {
		t.Fatalf("NewMarkdown error: %v", err)
	}
	ctx := context.Background()

	if err := w.Write(ctx, makeItem(map[string]any{"title": "Alpha", "price": "$9"})); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	// The first field value should appear in bold (**value**)
	if !strings.Contains(content, "**") {
		t.Errorf("MarkdownList: first field should be bold (**...**). Got:\n%s", content)
	}
}

// ---------------------------------------------------------------------------
// MarkdownCards
// ---------------------------------------------------------------------------

func TestMarkdownWriter_Cards_WritesH2Heading(t *testing.T) {
	path := tempFile(t, ".md")
	w, err := export.NewMarkdown(path, export.MarkdownCards)
	if err != nil {
		t.Fatalf("NewMarkdown error: %v", err)
	}
	ctx := context.Background()

	// Use a single-field item so the heading is unambiguous.
	if err := w.Write(ctx, makeItem(map[string]any{"name": "Widget Pro"})); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	// First (sorted) field value becomes the ## heading.
	if !strings.Contains(content, "## Widget Pro") {
		t.Errorf("MarkdownCards: expected '## Widget Pro' heading. Got:\n%s", content)
	}
}

func TestMarkdownWriter_Cards_WritesKeyValueBullets(t *testing.T) {
	path := tempFile(t, ".md")
	w, err := export.NewMarkdown(path, export.MarkdownCards)
	if err != nil {
		t.Fatalf("NewMarkdown error: %v", err)
	}
	ctx := context.Background()

	// Sorted keys: [name, price, url] → "name" is heading, "price" & "url" are bullets.
	if err := w.Write(ctx, makeItem(map[string]any{"name": "Widget Pro", "price": "$29.99", "url": "https://example.com"})); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	// price and url should appear as bold key bullets.
	if !strings.Contains(content, "**price**") {
		t.Errorf("MarkdownCards: expected bold key '**price**'. Got:\n%s", content)
	}
	if !strings.Contains(content, "$29.99") {
		t.Errorf("MarkdownCards: expected value '$29.99'. Got:\n%s", content)
	}
}

func TestMarkdownWriter_Cards_MultipleItems_BlankLineSeparated(t *testing.T) {
	path := tempFile(t, ".md")
	w, err := export.NewMarkdown(path, export.MarkdownCards)
	if err != nil {
		t.Fatalf("NewMarkdown error: %v", err)
	}
	ctx := context.Background()

	// Sorted keys: [name, price] → "name" is first, used as heading.
	items := []*foxhound.Item{
		makeItem(map[string]any{"name": "Alpha", "price": "$1"}),
		makeItem(map[string]any{"name": "Beta", "price": "$2"}),
	}
	for _, it := range items {
		if err := w.Write(ctx, it); err != nil {
			t.Fatalf("Write error: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "## Alpha") {
		t.Errorf("MarkdownCards: '## Alpha' not found:\n%s", content)
	}
	if !strings.Contains(content, "## Beta") {
		t.Errorf("MarkdownCards: '## Beta' not found:\n%s", content)
	}
}

// ---------------------------------------------------------------------------
// Common: NonExistentDir, Flush
// ---------------------------------------------------------------------------

func TestMarkdownWriter_NonExistentDir_ReturnsError(t *testing.T) {
	path := "/tmp/foxhound-nonexistent-dir-xyz/out.md"
	_, err := export.NewMarkdown(path, export.MarkdownTable)
	if err == nil {
		t.Error("NewMarkdown on non-existent directory: expected error, got nil")
	}
}

func TestMarkdownWriter_Flush_DoesNotError(t *testing.T) {
	path := tempFile(t, ".md")
	w, err := export.NewMarkdown(path, export.MarkdownTable)
	if err != nil {
		t.Fatalf("NewMarkdown error: %v", err)
	}
	ctx := context.Background()
	_ = w.Write(ctx, makeItem(map[string]any{"k": "v"}))
	if err := w.Flush(ctx); err != nil {
		t.Errorf("Flush error: %v", err)
	}
	_ = w.Close()
}
