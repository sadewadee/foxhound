package pipeline_test

import (
	"context"
	"errors"
	"testing"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/pipeline"
)

func TestTransform_FnIsApplied(t *testing.T) {
	tr := &pipeline.Transform{
		Fn: func(item *foxhound.Item) (*foxhound.Item, error) {
			item.Set("processed", true)
			return item, nil
		},
	}
	item := foxhound.NewItem()
	item.Set("name", "fox")

	result, err := tr.Process(context.Background(), item)
	if err != nil {
		t.Fatalf("Transform.Process error: %v", err)
	}
	if result == nil {
		t.Fatal("Transform.Process: expected item, got nil")
	}
	processed, ok := result.Get("processed")
	if !ok || processed != true {
		t.Errorf("Transform: expected processed=true, got %v", processed)
	}
}

func TestTransform_FnCanDropItem(t *testing.T) {
	tr := &pipeline.Transform{
		Fn: func(item *foxhound.Item) (*foxhound.Item, error) {
			return nil, nil // drop
		},
	}
	item := foxhound.NewItem()

	result, err := tr.Process(context.Background(), item)
	if err != nil {
		t.Fatalf("Transform.Process drop error: %v", err)
	}
	if result != nil {
		t.Error("Transform: Fn returning nil should drop item")
	}
}

func TestTransform_FnErrorPropagates(t *testing.T) {
	sentinel := errors.New("transform failed")
	tr := &pipeline.Transform{
		Fn: func(item *foxhound.Item) (*foxhound.Item, error) {
			return nil, sentinel
		},
	}
	item := foxhound.NewItem()

	_, err := tr.Process(context.Background(), item)
	if !errors.Is(err, sentinel) {
		t.Errorf("Transform: expected sentinel error, got %v", err)
	}
}

func TestTransform_FnCanModifyFields(t *testing.T) {
	tr := &pipeline.Transform{
		Fn: func(item *foxhound.Item) (*foxhound.Item, error) {
			if v, ok := item.Get("price"); ok {
				item.Set("price_cents", int(v.(float64)*100))
			}
			return item, nil
		},
	}
	item := foxhound.NewItem()
	item.Set("price", float64(9.99))

	result, err := tr.Process(context.Background(), item)
	if err != nil {
		t.Fatalf("Transform.Process modify error: %v", err)
	}
	cents, _ := result.Get("price_cents")
	if cents != 999 {
		t.Errorf("Transform modify: got %v, want 999", cents)
	}
}

func TestTransform_NilFnReturnsItemUnchanged(t *testing.T) {
	tr := &pipeline.Transform{Fn: nil}
	item := foxhound.NewItem()
	item.Set("key", "value")

	result, err := tr.Process(context.Background(), item)
	if err != nil {
		t.Fatalf("Transform nil fn error: %v", err)
	}
	if result == nil {
		t.Fatal("Transform nil fn: expected item returned, got nil")
	}
	v, _ := result.Get("key")
	if v != "value" {
		t.Errorf("Transform nil fn: item modified unexpectedly")
	}
}
