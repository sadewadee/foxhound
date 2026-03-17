package pipeline

import (
	"context"

	foxhound "github.com/foxhound-scraper/foxhound"
)

// Transform applies a user-defined function to each item.
// The function may return a modified item, nil to drop the item, or an error.
// If Fn is nil, the item is returned unchanged.
type Transform struct {
	// Fn is the user-provided transformation function.
	Fn func(item *foxhound.Item) (*foxhound.Item, error)
}

// Process calls t.Fn with the item and returns its result.
// If Fn is nil, the item is returned as-is.
func (t *Transform) Process(_ context.Context, item *foxhound.Item) (*foxhound.Item, error) {
	if t.Fn == nil {
		return item, nil
	}
	return t.Fn(item)
}
