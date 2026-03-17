package pipeline_test

import (
	"context"
	"errors"
	"testing"

	foxhound "github.com/foxhound-scraper/foxhound"
	"github.com/foxhound-scraper/foxhound/pipeline"
)

// appendStage appends a marker to "trace" field to verify order.
type appendStage struct {
	marker string
}

func (a *appendStage) Process(_ context.Context, item *foxhound.Item) (*foxhound.Item, error) {
	existing, _ := item.Get("trace")
	if existing == nil {
		item.Set("trace", a.marker)
	} else {
		item.Set("trace", existing.(string)+","+a.marker)
	}
	return item, nil
}

// dropStage always drops the item.
type dropStage struct{}

func (d *dropStage) Process(_ context.Context, _ *foxhound.Item) (*foxhound.Item, error) {
	return nil, nil
}

// errorStage always returns an error.
type errorStage struct{}

func (e *errorStage) Process(_ context.Context, _ *foxhound.Item) (*foxhound.Item, error) {
	return nil, errors.New("stage error")
}

func TestChain_ProcessesInOrder(t *testing.T) {
	chain := pipeline.NewChain(
		&appendStage{"A"},
		&appendStage{"B"},
		&appendStage{"C"},
	)
	item := foxhound.NewItem()
	ctx := context.Background()

	result, err := chain.Process(ctx, item)
	if err != nil {
		t.Fatalf("Chain.Process error: %v", err)
	}
	if result == nil {
		t.Fatal("Chain.Process: expected item, got nil")
	}
	trace, _ := result.Get("trace")
	if trace != "A,B,C" {
		t.Errorf("Chain order: got %q, want %q", trace, "A,B,C")
	}
}

func TestChain_DropStageStopsProcessing(t *testing.T) {
	chain := pipeline.NewChain(
		&appendStage{"A"},
		&dropStage{},
		&appendStage{"C"}, // should not run
	)
	item := foxhound.NewItem()
	ctx := context.Background()

	result, err := chain.Process(ctx, item)
	if err != nil {
		t.Fatalf("Chain.Process with drop stage error: %v", err)
	}
	if result != nil {
		t.Error("Chain.Process: expected nil after drop stage, got non-nil")
	}
}

func TestChain_ErrorPropagates(t *testing.T) {
	chain := pipeline.NewChain(
		&appendStage{"A"},
		&errorStage{},
	)
	item := foxhound.NewItem()
	ctx := context.Background()

	_, err := chain.Process(ctx, item)
	if err == nil {
		t.Error("Chain.Process with error stage: expected error, got nil")
	}
}

func TestChain_EmptyChain_PassesThrough(t *testing.T) {
	chain := pipeline.NewChain()
	item := foxhound.NewItem()
	item.Set("key", "value")
	ctx := context.Background()

	result, err := chain.Process(ctx, item)
	if err != nil {
		t.Fatalf("Chain.Process empty chain error: %v", err)
	}
	if result == nil {
		t.Fatal("Chain.Process empty chain: expected item returned, got nil")
	}
	val, _ := result.Get("key")
	if val != "value" {
		t.Errorf("Empty chain: item modified unexpectedly, got %v", val)
	}
}

func TestChain_SingleStage(t *testing.T) {
	chain := pipeline.NewChain(&appendStage{"only"})
	item := foxhound.NewItem()
	ctx := context.Background()

	result, err := chain.Process(ctx, item)
	if err != nil {
		t.Fatalf("Chain single stage error: %v", err)
	}
	if result == nil {
		t.Fatal("Chain single stage: expected item, got nil")
	}
	trace, _ := result.Get("trace")
	if trace != "only" {
		t.Errorf("Single stage trace: got %q, want only", trace)
	}
}
