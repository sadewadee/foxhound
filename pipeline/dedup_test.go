package pipeline_test

import (
	"context"
	"testing"

	foxhound "github.com/foxhound-scraper/foxhound"
	"github.com/foxhound-scraper/foxhound/pipeline"
)

func TestItemDedup_FirstItemPasses(t *testing.T) {
	d := pipeline.NewItemDedup("url")
	item := foxhound.NewItem()
	item.Set("url", "http://example.com/page1")

	result, err := d.Process(context.Background(), item)
	if err != nil {
		t.Fatalf("ItemDedup.Process error: %v", err)
	}
	if result == nil {
		t.Error("ItemDedup: first item should pass, got nil")
	}
}

func TestItemDedup_DuplicateIsDropped(t *testing.T) {
	d := pipeline.NewItemDedup("url")
	ctx := context.Background()

	item1 := foxhound.NewItem()
	item1.Set("url", "http://example.com/page1")
	item1.Set("title", "First")

	item2 := foxhound.NewItem()
	item2.Set("url", "http://example.com/page1") // same URL
	item2.Set("title", "Duplicate")

	r1, err := d.Process(ctx, item1)
	if err != nil {
		t.Fatalf("ItemDedup first item error: %v", err)
	}
	if r1 == nil {
		t.Fatal("ItemDedup: first item should pass")
	}

	r2, err := d.Process(ctx, item2)
	if err != nil {
		t.Fatalf("ItemDedup duplicate error: %v", err)
	}
	if r2 != nil {
		t.Error("ItemDedup: duplicate item should be dropped (nil), got non-nil")
	}
}

func TestItemDedup_DifferentKeysAllPass(t *testing.T) {
	d := pipeline.NewItemDedup("id")
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		item := foxhound.NewItem()
		item.Set("id", i)
		result, err := d.Process(ctx, item)
		if err != nil {
			t.Fatalf("ItemDedup item %d error: %v", i, err)
		}
		if result == nil {
			t.Errorf("ItemDedup: item with unique id=%d should pass, got nil", i)
		}
	}
}

func TestItemDedup_MissingKeyFieldDropsItem(t *testing.T) {
	d := pipeline.NewItemDedup("url")
	item := foxhound.NewItem()
	// "url" field not set

	result, err := d.Process(context.Background(), item)
	if err != nil {
		t.Fatalf("ItemDedup missing key error: %v", err)
	}
	if result != nil {
		t.Error("ItemDedup: item missing key field should be dropped")
	}
}

func TestItemDedup_ThreadSafe(t *testing.T) {
	d := pipeline.NewItemDedup("url")
	ctx := context.Background()

	// Run concurrent writes — should not race
	done := make(chan struct{}, 10)
	for i := 0; i < 10; i++ {
		go func(n int) {
			item := foxhound.NewItem()
			item.Set("url", n) // unique per goroutine
			_, _ = d.Process(ctx, item)
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
