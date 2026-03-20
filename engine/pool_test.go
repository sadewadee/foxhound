package engine

import (
	"context"
	"testing"
)

func TestMemoryPool_AddAndDrain(t *testing.T) {
	p := NewMemoryPool()
	ctx := context.Background()

	if err := p.Add(ctx, "https://example.com/1"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := p.Add(ctx, "https://example.com/2"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := p.Add(ctx, "https://example.com/1"); err != nil {
		t.Fatalf("Add dup: %v", err)
	}

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

func TestMemoryPool_AddBatch(t *testing.T) {
	p := NewMemoryPool()
	ctx := context.Background()

	urls := []string{
		"https://example.com/a",
		"https://example.com/b",
		"https://example.com/a",
		"https://example.com/c",
	}
	if err := p.AddBatch(ctx, urls); err != nil {
		t.Fatalf("AddBatch: %v", err)
	}
	if got := p.Len(); got != 3 {
		t.Fatalf("Len = %d, want 3", got)
	}
}

func TestMemoryPool_DrainEmpty(t *testing.T) {
	p := NewMemoryPool()
	urls, err := p.Drain(context.Background())
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(urls) != 0 {
		t.Fatalf("Drain empty = %d, want 0", len(urls))
	}
}

func TestMemoryPool_Close(t *testing.T) {
	p := NewMemoryPool()
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
