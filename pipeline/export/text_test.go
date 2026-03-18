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
// TextLines
// ---------------------------------------------------------------------------

func TestTextWriter_Lines_OneLinePerItem(t *testing.T) {
	path := tempFile(t, ".txt")
	w, err := export.NewText(path, export.TextLines)
	if err != nil {
		t.Fatalf("NewText error: %v", err)
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
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Errorf("TextLines: want 2 lines, got %d:\n%s", len(lines), string(data))
	}
}

func TestTextWriter_Lines_KeyValueFormat(t *testing.T) {
	path := tempFile(t, ".txt")
	w, err := export.NewText(path, export.TextLines)
	if err != nil {
		t.Fatalf("NewText error: %v", err)
	}
	ctx := context.Background()

	if err := w.Write(ctx, makeItem(map[string]any{"title": "Alpha", "price": "$1"})); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	// Each pair should be key=value format
	if !strings.Contains(content, "=") {
		t.Errorf("TextLines: expected key=value format, got:\n%s", content)
	}
	if !strings.Contains(content, "Alpha") {
		t.Errorf("TextLines: value 'Alpha' not found:\n%s", content)
	}
}

func TestTextWriter_Lines_EmptyFile_NoItems(t *testing.T) {
	path := tempFile(t, ".txt")
	w, err := export.NewText(path, export.TextLines)
	if err != nil {
		t.Fatalf("NewText error: %v", err)
	}
	ctx := context.Background()
	_ = w.Flush(ctx)
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	if strings.TrimSpace(string(data)) != "" {
		t.Errorf("TextLines: expected empty file, got:\n%s", string(data))
	}
}

// ---------------------------------------------------------------------------
// TextPretty
// ---------------------------------------------------------------------------

func TestTextWriter_Pretty_WritesSeparatorLines(t *testing.T) {
	path := tempFile(t, ".txt")
	w, err := export.NewText(path, export.TextPretty)
	if err != nil {
		t.Fatalf("NewText error: %v", err)
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

	// Should contain separator characters (dashes or box-drawing)
	hasSep := strings.Contains(content, "----") || strings.Contains(content, "────")
	if !hasSep {
		t.Errorf("TextPretty: expected separator line with dashes or box chars. Got:\n%s", content)
	}
}

func TestTextWriter_Pretty_ContainsFieldValues(t *testing.T) {
	path := tempFile(t, ".txt")
	w, err := export.NewText(path, export.TextPretty)
	if err != nil {
		t.Fatalf("NewText error: %v", err)
	}
	ctx := context.Background()

	if err := w.Write(ctx, makeItem(map[string]any{"title": "Widget Pro", "price": "$29.99"})); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)

	if !strings.Contains(content, "Widget Pro") {
		t.Errorf("TextPretty: 'Widget Pro' not found:\n%s", content)
	}
	if !strings.Contains(content, "$29.99") {
		t.Errorf("TextPretty: '$29.99' not found:\n%s", content)
	}
}

func TestTextWriter_Pretty_MultipleItemsSeparated(t *testing.T) {
	path := tempFile(t, ".txt")
	w, err := export.NewText(path, export.TextPretty)
	if err != nil {
		t.Fatalf("NewText error: %v", err)
	}
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := w.Write(ctx, makeItem(map[string]any{"n": i})); err != nil {
			t.Fatalf("Write error: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	// 3 items = 3 separator blocks (before each block or after each)
	sepCount := strings.Count(content, "────")
	if sepCount == 0 {
		sepCount = strings.Count(content, "----")
	}
	if sepCount < 3 {
		t.Errorf("TextPretty: expected at least 3 separators for 3 items, got %d:\n%s", sepCount, content)
	}
}

// ---------------------------------------------------------------------------
// Common
// ---------------------------------------------------------------------------

func TestTextWriter_NonExistentDir_ReturnsError(t *testing.T) {
	path := "/tmp/foxhound-nonexistent-dir-xyz/out.txt"
	_, err := export.NewText(path, export.TextLines)
	if err == nil {
		t.Error("NewText on non-existent directory: expected error, got nil")
	}
}

func TestTextWriter_Flush_DoesNotError(t *testing.T) {
	path := tempFile(t, ".txt")
	w, err := export.NewText(path, export.TextLines)
	if err != nil {
		t.Fatalf("NewText error: %v", err)
	}
	ctx := context.Background()
	_ = w.Write(ctx, makeItem(map[string]any{"k": "v"}))
	if err := w.Flush(ctx); err != nil {
		t.Errorf("Flush error: %v", err)
	}
	_ = w.Close()
}
