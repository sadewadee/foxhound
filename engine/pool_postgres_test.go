package engine

import (
	"context"
	"os"
	"testing"
)

func TestPostgresPool_AddAndDrain(t *testing.T) {
	dsn := os.Getenv("FOXHOUND_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("FOXHOUND_TEST_POSTGRES not set")
	}

	p, err := NewPostgresPool(dsn, "test_pool_"+t.Name())
	if err != nil {
		t.Fatalf("NewPostgresPool: %v", err)
	}
	defer p.Close()

	ctx := context.Background()
	p.Add(ctx, "https://example.com/1")
	p.Add(ctx, "https://example.com/2")
	p.Add(ctx, "https://example.com/1") // dup

	if got := p.Len(); got != 2 {
		t.Fatalf("Len = %d, want 2", got)
	}

	urls, err := p.Drain(ctx)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(urls) != 2 {
		t.Fatalf("Drain len = %d, want 2", len(urls))
	}
	if got := p.Len(); got != 0 {
		t.Fatalf("Len after drain = %d, want 0", got)
	}
}

func TestPostgresPool_AddBatch(t *testing.T) {
	dsn := os.Getenv("FOXHOUND_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("FOXHOUND_TEST_POSTGRES not set")
	}

	p, err := NewPostgresPool(dsn, "test_pool_batch_"+t.Name())
	if err != nil {
		t.Fatalf("NewPostgresPool: %v", err)
	}
	defer p.Close()

	urls := []string{"https://a.com", "https://b.com", "https://a.com", "https://c.com"}
	p.AddBatch(context.Background(), urls)
	if got := p.Len(); got != 3 {
		t.Fatalf("Len = %d, want 3", got)
	}
}
