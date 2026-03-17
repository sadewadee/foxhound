package export_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	foxhound "github.com/foxhound-scraper/foxhound"
	"github.com/foxhound-scraper/foxhound/pipeline/export"
)

func tempFile(t *testing.T, suffix string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "foxhound-*"+suffix)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	f.Close()
	return f.Name()
}

func makeItem(fields map[string]any) *foxhound.Item {
	item := foxhound.NewItem()
	for k, v := range fields {
		item.Set(k, v)
	}
	return item
}

// --- JSONLines tests ---

func TestJSONWriter_JSONLines_WritesOneLinePerItem(t *testing.T) {
	path := tempFile(t, ".jsonl")
	w, err := export.NewJSON(path, export.JSONLines)
	if err != nil {
		t.Fatalf("NewJSON error: %v", err)
	}
	ctx := context.Background()

	items := []*foxhound.Item{
		makeItem(map[string]any{"name": "alpha", "price": 1.0}),
		makeItem(map[string]any{"name": "beta", "price": 2.0}),
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
		t.Fatalf("JSONLines: got %d lines, want 2. Content:\n%s", len(lines), string(data))
	}
	// Each line must be valid JSON
	for i, line := range lines {
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("Line %d is not valid JSON: %v\nLine: %s", i, err, line)
		}
	}
}

func TestJSONWriter_JSONLines_EmptyFile_NoItems(t *testing.T) {
	path := tempFile(t, ".jsonl")
	w, err := export.NewJSON(path, export.JSONLines)
	if err != nil {
		t.Fatalf("NewJSON error: %v", err)
	}
	ctx := context.Background()
	_ = w.Flush(ctx)
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	if strings.TrimSpace(string(data)) != "" {
		t.Errorf("Empty JSONLines file should have no content, got: %q", string(data))
	}
}

// --- JSONArray tests ---

func TestJSONWriter_JSONArray_ProducesValidArray(t *testing.T) {
	path := tempFile(t, ".json")
	w, err := export.NewJSON(path, export.JSONArray)
	if err != nil {
		t.Fatalf("NewJSON error: %v", err)
	}
	ctx := context.Background()

	items := []*foxhound.Item{
		makeItem(map[string]any{"name": "alpha"}),
		makeItem(map[string]any{"name": "beta"}),
		makeItem(map[string]any{"name": "gamma"}),
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

	var arr []map[string]any
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Fatalf("JSONArray output is not valid JSON array: %v\nContent:\n%s", err, string(data))
	}
	if len(arr) != 3 {
		t.Errorf("JSONArray: got %d items, want 3", len(arr))
	}
}

func TestJSONWriter_JSONArray_EmptyArray(t *testing.T) {
	path := tempFile(t, ".json")
	w, err := export.NewJSON(path, export.JSONArray)
	if err != nil {
		t.Fatalf("NewJSON error: %v", err)
	}
	ctx := context.Background()
	_ = w.Flush(ctx)
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	var arr []any
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Fatalf("Empty JSONArray should produce valid empty array: %v\nContent:\n%s", err, string(data))
	}
	if len(arr) != 0 {
		t.Errorf("Empty JSONArray: got %d items, want 0", len(arr))
	}
}

func TestJSONWriter_JSONArray_SingleItem(t *testing.T) {
	path := tempFile(t, ".json")
	w, err := export.NewJSON(path, export.JSONArray)
	if err != nil {
		t.Fatalf("NewJSON error: %v", err)
	}
	ctx := context.Background()

	if err := w.Write(ctx, makeItem(map[string]any{"x": 1})); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	var arr []map[string]any
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Fatalf("Single item JSONArray not valid: %v\nContent:\n%s", err, string(data))
	}
	if len(arr) != 1 {
		t.Errorf("Single item JSONArray: got %d items, want 1", len(arr))
	}
}

func TestJSONWriter_NonExistentDir_ReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent", "out.json")
	_, err := export.NewJSON(path, export.JSONLines)
	if err == nil {
		t.Error("NewJSON on non-existent directory: expected error, got nil")
	}
}

func TestJSONWriter_Flush_DoesNotError(t *testing.T) {
	path := tempFile(t, ".jsonl")
	w, err := export.NewJSON(path, export.JSONLines)
	if err != nil {
		t.Fatalf("NewJSON error: %v", err)
	}
	ctx := context.Background()
	_ = w.Write(ctx, makeItem(map[string]any{"k": "v"}))

	if err := w.Flush(ctx); err != nil {
		t.Errorf("Flush error: %v", err)
	}
	_ = w.Close()
}
