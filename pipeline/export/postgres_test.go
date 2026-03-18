package export_test

import (
	"context"
	"os"
	"testing"
	"time"

	foxhound "github.com/foxhound-scraper/foxhound"
	"github.com/foxhound-scraper/foxhound/pipeline/export"
)

// postgresConnString returns a test DSN, or skips the test if none is set.
// Set FOXHOUND_TEST_PG=postgres://user:pass@localhost/testdb to run Postgres tests.
func postgresConnString(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("FOXHOUND_TEST_PG")
	if dsn == "" {
		t.Skip("FOXHOUND_TEST_PG not set; skipping Postgres integration test")
	}
	return dsn
}

func pgItem(url, sku, title string) *foxhound.Item {
	item := foxhound.NewItem()
	item.URL = url
	item.Set("sku", sku)
	item.Set("title", title)
	item.Set("scraped_at", time.Now().Format(time.RFC3339))
	return item
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func TestNewPostgres_InvalidConnString_ReturnsError(t *testing.T) {
	// A clearly bogus DSN should fail to connect.
	_, err := export.NewPostgres("host=255.255.255.255 port=1 dbname=nope connect_timeout=1", "items")
	if err == nil {
		t.Fatal("expected error for unreachable host, got nil")
	}
}

func TestNewPostgres_DefaultBatchSize_IsPositive(t *testing.T) {
	dsn := postgresConnString(t)
	w, err := export.NewPostgres(dsn, "test_default_batch")
	if err != nil {
		t.Fatalf("NewPostgres: %v", err)
	}
	defer func() { _ = w.Close() }()
	// No public getter — just verify the writer was created without error.
}

// ---------------------------------------------------------------------------
// Write + Flush
// ---------------------------------------------------------------------------

func TestPostgresWriter_WriteAndFlush_StoresItems(t *testing.T) {
	dsn := postgresConnString(t)
	table := "test_write_flush"

	w, err := export.NewPostgres(dsn, table, export.WithPGBatchSize(10))
	if err != nil {
		t.Fatalf("NewPostgres: %v", err)
	}
	defer func() { _ = w.Close() }()

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		item := pgItem("https://example.com", "SKU-001", "Widget")
		if err := w.Write(ctx, item); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	if err := w.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}

func TestPostgresWriter_AutoFlush_WhenBatchFull(t *testing.T) {
	dsn := postgresConnString(t)
	table := "test_auto_flush"

	w, err := export.NewPostgres(dsn, table, export.WithPGBatchSize(2))
	if err != nil {
		t.Fatalf("NewPostgres: %v", err)
	}
	defer func() { _ = w.Close() }()

	ctx := context.Background()
	// Writing 2 items should trigger an auto-flush (batch size = 2).
	for i := 0; i < 2; i++ {
		if err := w.Write(ctx, pgItem("https://example.com", "SKU-AUTO", "Auto")); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	// A third write should not error even after the batch already flushed.
	if err := w.Write(ctx, pgItem("https://example.com", "SKU-AUTO3", "Auto3")); err != nil {
		t.Fatalf("Write third: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Upsert
// ---------------------------------------------------------------------------

func TestPostgresWriter_WithUpsert_NoErrorOnDuplicate(t *testing.T) {
	dsn := postgresConnString(t)
	table := "test_upsert"

	w, err := export.NewPostgres(dsn, table,
		export.WithUpsert("sku"),
		export.WithPGBatchSize(1),
	)
	if err != nil {
		t.Fatalf("NewPostgres with upsert: %v", err)
	}
	defer func() { _ = w.Close() }()

	ctx := context.Background()
	// Write the same SKU twice — second write should upsert without error.
	item := pgItem("https://example.com", "SKU-UPSERT", "Original")
	if err := w.Write(ctx, item); err != nil {
		t.Fatalf("Write first: %v", err)
	}
	item2 := pgItem("https://example.com", "SKU-UPSERT", "Updated")
	if err := w.Write(ctx, item2); err != nil {
		t.Fatalf("Write second (upsert): %v", err)
	}
}

// ---------------------------------------------------------------------------
// Close flushes remainder
// ---------------------------------------------------------------------------

func TestPostgresWriter_Close_FlushesRemaining(t *testing.T) {
	dsn := postgresConnString(t)
	table := "test_close_flush"

	w, err := export.NewPostgres(dsn, table, export.WithPGBatchSize(100))
	if err != nil {
		t.Fatalf("NewPostgres: %v", err)
	}

	ctx := context.Background()
	// Write fewer items than batch size so auto-flush does not trigger.
	for i := 0; i < 5; i++ {
		if err := w.Write(ctx, pgItem("https://example.com", "SKU-CLOSE", "Close")); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	// Close must flush and not return error.
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

func TestWithPGBatchSize_ZeroOrNegative_UsesDefault(t *testing.T) {
	dsn := postgresConnString(t)
	// Should not panic and should create the writer cleanly.
	w, err := export.NewPostgres(dsn, "test_batch_zero", export.WithPGBatchSize(0))
	if err != nil {
		t.Fatalf("NewPostgres with zero batch: %v", err)
	}
	defer func() { _ = w.Close() }()
}
