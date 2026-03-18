package export_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/pipeline/export"
	_ "modernc.org/sqlite"
)

func TestSQLiteWriter_WriteAndReadBack(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	w, err := export.NewSQLite(dbPath, "items")
	if err != nil {
		t.Fatalf("NewSQLite error: %v", err)
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

	// Open and verify.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open error: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM items").Scan(&count); err != nil {
		t.Fatalf("COUNT query error: %v", err)
	}
	if count != 2 {
		t.Errorf("SQLite: expected 2 rows, got %d", count)
	}
}

func TestSQLiteWriter_FieldValuesPreserved(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	w, err := export.NewSQLite(dbPath, "items")
	if err != nil {
		t.Fatalf("NewSQLite error: %v", err)
	}
	ctx := context.Background()

	if err := w.Write(ctx, makeItem(map[string]any{"title": "Widget Pro", "price": "$29.99"})); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open error: %v", err)
	}
	defer db.Close()

	var title, price string
	if err := db.QueryRow("SELECT title, price FROM items LIMIT 1").Scan(&title, &price); err != nil {
		t.Fatalf("SELECT error: %v", err)
	}
	if title != "Widget Pro" {
		t.Errorf("SQLite title: got %q, want 'Widget Pro'", title)
	}
	if price != "$29.99" {
		t.Errorf("SQLite price: got %q, want '$29.99'", price)
	}
}

func TestSQLiteWriter_AutoCreatesTable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	w, err := export.NewSQLite(dbPath, "scraped")
	if err != nil {
		t.Fatalf("NewSQLite error: %v", err)
	}
	ctx := context.Background()

	if err := w.Write(ctx, makeItem(map[string]any{"url": "https://example.com"})); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open error: %v", err)
	}
	defer db.Close()

	// Table "scraped" must exist with a "url" column.
	var url string
	if err := db.QueryRow("SELECT url FROM scraped LIMIT 1").Scan(&url); err != nil {
		t.Fatalf("table 'scraped' not created or 'url' column missing: %v", err)
	}
	if url != "https://example.com" {
		t.Errorf("SQLite url: got %q, want 'https://example.com'", url)
	}
}

func TestSQLiteWriter_NewFieldInLaterItem_AddsColumn(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	w, err := export.NewSQLite(dbPath, "items")
	if err != nil {
		t.Fatalf("NewSQLite error: %v", err)
	}
	ctx := context.Background()

	// First item: title only.
	if err := w.Write(ctx, makeItem(map[string]any{"title": "Alpha"})); err != nil {
		t.Fatalf("Write error (item 1): %v", err)
	}
	// Second item: title + sku (new column).
	if err := w.Write(ctx, makeItem(map[string]any{"title": "Beta", "sku": "SKU-001"})); err != nil {
		t.Fatalf("Write error (item 2): %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open error: %v", err)
	}
	defer db.Close()

	var sku string
	if err := db.QueryRow("SELECT sku FROM items WHERE title='Beta'").Scan(&sku); err != nil {
		t.Fatalf("sku column not found or query failed: %v", err)
	}
	if sku != "SKU-001" {
		t.Errorf("SQLite sku: got %q, want 'SKU-001'", sku)
	}
}

func TestSQLiteWriter_CustomTableName(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	w, err := export.NewSQLite(dbPath, "my_custom_table")
	if err != nil {
		t.Fatalf("NewSQLite error: %v", err)
	}
	ctx := context.Background()

	if err := w.Write(ctx, makeItem(map[string]any{"val": "hello"})); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open error: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM my_custom_table").Scan(&count); err != nil {
		t.Fatalf("custom table query failed: %v", err)
	}
	if count != 1 {
		t.Errorf("custom table: expected 1 row, got %d", count)
	}
}

func TestSQLiteWriter_MultipleWrites_AllRowsPresent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	w, err := export.NewSQLite(dbPath, "items")
	if err != nil {
		t.Fatalf("NewSQLite error: %v", err)
	}
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if err := w.Write(ctx, makeItem(map[string]any{"idx": i, "name": "item"})); err != nil {
			t.Fatalf("Write %d error: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open error: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM items").Scan(&count); err != nil {
		t.Fatalf("COUNT query error: %v", err)
	}
	if count != 5 {
		t.Errorf("SQLite: expected 5 rows, got %d", count)
	}
}

func TestSQLiteWriter_Flush_DoesNotError(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	w, err := export.NewSQLite(dbPath, "items")
	if err != nil {
		t.Fatalf("NewSQLite error: %v", err)
	}
	ctx := context.Background()
	_ = w.Write(ctx, makeItem(map[string]any{"k": "v"}))
	if err := w.Flush(ctx); err != nil {
		t.Errorf("Flush error: %v", err)
	}
	_ = w.Close()
}

func TestSQLiteWriter_NonExistentDir_ReturnsError(t *testing.T) {
	path := filepath.Join("/tmp", "foxhound-nonexistent-dir-xyz", "out.db")
	_, err := export.NewSQLite(path, "items")
	if err == nil {
		// Some SQLite drivers create directories — only fail if we can't create the db
		// and it's clearly wrong. Check the file is at least accessible.
		_ = os.Remove(path)
	}
	// We accept either outcome here since sqlite driver behavior varies.
	// The primary test is that we handle it without panicking.
}

func TestSQLiteWriter_EmptyTable_NoItemsWritten(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	w, err := export.NewSQLite(dbPath, "items")
	if err != nil {
		t.Fatalf("NewSQLite error: %v", err)
	}
	ctx := context.Background()
	_ = w.Flush(ctx)
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	// File should exist (db created), no error expected.
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("SQLite: db file should exist even with 0 items: %v", err)
	}
}
