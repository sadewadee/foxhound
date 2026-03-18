package export_test

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/pipeline/export"
)

func TestCSVWriter_ExplicitHeaders(t *testing.T) {
	path := tempFile(t, ".csv")
	w, err := export.NewCSV(path, "name", "price")
	if err != nil {
		t.Fatalf("NewCSV error: %v", err)
	}
	ctx := context.Background()

	items := []*foxhound.Item{
		makeItem(map[string]any{"name": "Widget", "price": "9.99"}),
		makeItem(map[string]any{"name": "Gadget", "price": "19.99"}),
	}
	for _, it := range items {
		if err := w.Write(ctx, it); err != nil {
			t.Fatalf("Write error: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	defer f.Close()

	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("CSV parse error: %v", err)
	}

	// First row = headers
	if len(records) != 3 {
		t.Fatalf("CSV records: got %d rows, want 3 (1 header + 2 data)", len(records))
	}
	if records[0][0] != "name" || records[0][1] != "price" {
		t.Errorf("CSV headers: got %v, want [name price]", records[0])
	}
	if records[1][0] != "Widget" {
		t.Errorf("CSV row1 name: got %q, want Widget", records[1][0])
	}
	if records[2][0] != "Gadget" {
		t.Errorf("CSV row2 name: got %q, want Gadget", records[2][0])
	}
}

func TestCSVWriter_InferHeaders_FromFirstItem(t *testing.T) {
	path := tempFile(t, ".csv")
	w, err := export.NewCSV(path) // no headers specified
	if err != nil {
		t.Fatalf("NewCSV error: %v", err)
	}
	ctx := context.Background()

	item := makeItem(map[string]any{"title": "Article", "author": "Alice"})
	if err := w.Write(ctx, item); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	f, _ := os.Open(path)
	defer f.Close()
	records, _ := csv.NewReader(f).ReadAll()

	if len(records) < 2 {
		t.Fatalf("CSV with inferred headers: got %d rows, want at least 2", len(records))
	}
	// Headers should be the sorted field keys
	headers := records[0]
	if len(headers) != 2 {
		t.Errorf("Inferred headers count: got %d, want 2", len(headers))
	}
	// Sorted: author, title
	if headers[0] != "author" || headers[1] != "title" {
		t.Errorf("Inferred headers (sorted): got %v, want [author title]", headers)
	}
}

func TestCSVWriter_MissingField_WritesEmpty(t *testing.T) {
	path := tempFile(t, ".csv")
	w, err := export.NewCSV(path, "name", "price", "sku")
	if err != nil {
		t.Fatalf("NewCSV error: %v", err)
	}
	ctx := context.Background()

	item := makeItem(map[string]any{"name": "Widget"}) // price and sku missing
	if err := w.Write(ctx, item); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	f, _ := os.Open(path)
	defer f.Close()
	records, _ := csv.NewReader(f).ReadAll()

	if len(records) != 2 {
		t.Fatalf("CSV rows: got %d, want 2", len(records))
	}
	row := records[1]
	if row[0] != "Widget" {
		t.Errorf("row name: got %q, want Widget", row[0])
	}
	if row[1] != "" {
		t.Errorf("missing price: got %q, want empty string", row[1])
	}
	if row[2] != "" {
		t.Errorf("missing sku: got %q, want empty string", row[2])
	}
}

func TestCSVWriter_IntFieldConvertedToString(t *testing.T) {
	path := tempFile(t, ".csv")
	w, err := export.NewCSV(path, "count")
	if err != nil {
		t.Fatalf("NewCSV error: %v", err)
	}
	ctx := context.Background()

	item := makeItem(map[string]any{"count": 42})
	if err := w.Write(ctx, item); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	f, _ := os.Open(path)
	defer f.Close()
	records, _ := csv.NewReader(f).ReadAll()

	if len(records) != 2 {
		t.Fatalf("CSV rows: got %d, want 2", len(records))
	}
	if records[1][0] != "42" {
		t.Errorf("Int field: got %q, want 42", records[1][0])
	}
}

func TestCSVWriter_NonExistentDir_ReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "out.csv")
	_, err := export.NewCSV(path, "name")
	if err == nil {
		t.Error("NewCSV on non-existent directory: expected error, got nil")
	}
}

func TestCSVWriter_Flush_DoesNotError(t *testing.T) {
	path := tempFile(t, ".csv")
	w, err := export.NewCSV(path, "name")
	if err != nil {
		t.Fatalf("NewCSV error: %v", err)
	}
	ctx := context.Background()
	_ = w.Write(ctx, makeItem(map[string]any{"name": "test"}))

	if err := w.Flush(ctx); err != nil {
		t.Errorf("Flush error: %v", err)
	}
	_ = w.Close()
}

func TestCSVWriter_WriteMultipleItems_HeaderOnce(t *testing.T) {
	path := tempFile(t, ".csv")
	w, err := export.NewCSV(path, "id", "val")
	if err != nil {
		t.Fatalf("NewCSV error: %v", err)
	}
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		it := makeItem(map[string]any{"id": i, "val": "x"})
		if err := w.Write(ctx, it); err != nil {
			t.Fatalf("Write error: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	f, _ := os.Open(path)
	defer f.Close()
	records, _ := csv.NewReader(f).ReadAll()

	// 1 header + 5 data rows = 6 total
	if len(records) != 6 {
		t.Errorf("CSV rows: got %d, want 6 (1 header + 5 data)", len(records))
	}
}
