package pipeline_test

import (
	"context"
	"testing"

	foxhound "github.com/foxhound-scraper/foxhound"
	"github.com/foxhound-scraper/foxhound/pipeline"
)

func TestValidate_AllRequiredPresent(t *testing.T) {
	v := &pipeline.Validate{Required: []string{"title", "url"}}
	item := foxhound.NewItem()
	item.Set("title", "Fox Hunt")
	item.Set("url", "http://example.com")

	ctx := context.Background()
	result, err := v.Process(ctx, item)
	if err != nil {
		t.Fatalf("Validate.Process error: %v", err)
	}
	if result == nil {
		t.Error("Validate.Process: expected item returned, got nil (item should not be dropped)")
	}
}

func TestValidate_MissingRequiredField(t *testing.T) {
	v := &pipeline.Validate{Required: []string{"title", "url"}}
	item := foxhound.NewItem()
	item.Set("title", "Fox Hunt")
	// "url" is missing

	ctx := context.Background()
	result, err := v.Process(ctx, item)
	if err != nil {
		t.Fatalf("Validate.Process error: %v", err)
	}
	if result != nil {
		t.Error("Validate.Process: expected nil (item dropped), got non-nil item")
	}
}

func TestValidate_EmptyStringField_DropsItem(t *testing.T) {
	v := &pipeline.Validate{Required: []string{"title"}}
	item := foxhound.NewItem()
	item.Set("title", "") // empty string should count as missing

	ctx := context.Background()
	result, err := v.Process(ctx, item)
	if err != nil {
		t.Fatalf("Validate.Process error: %v", err)
	}
	if result != nil {
		t.Error("Validate.Process: expected nil for empty string field, got non-nil")
	}
}

func TestValidate_NoRequiredFields(t *testing.T) {
	v := &pipeline.Validate{Required: []string{}}
	item := foxhound.NewItem()

	ctx := context.Background()
	result, err := v.Process(ctx, item)
	if err != nil {
		t.Fatalf("Validate.Process error: %v", err)
	}
	if result == nil {
		t.Error("Validate.Process with no required fields: expected item returned, got nil")
	}
}

func TestValidate_NonStringNonEmptyValue_Passes(t *testing.T) {
	v := &pipeline.Validate{Required: []string{"count"}}
	item := foxhound.NewItem()
	item.Set("count", 42) // non-string non-empty value

	ctx := context.Background()
	result, err := v.Process(ctx, item)
	if err != nil {
		t.Fatalf("Validate.Process error: %v", err)
	}
	if result == nil {
		t.Error("Validate.Process: non-string non-nil value should pass validation")
	}
}
