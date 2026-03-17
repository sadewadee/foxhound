// Package pipeline provides composable data processing stages and export
// writers for the Foxhound scraping framework.
package pipeline

import (
	"context"

	foxhound "github.com/foxhound-scraper/foxhound"
)

// Validate is a pipeline stage that drops items missing required fields.
// A field is considered missing if it is absent from item.Fields or if its
// value is an empty string.
type Validate struct {
	// Required is the list of field names that must be present and non-empty.
	Required []string
}

// Process returns nil (dropping the item) if any required field is absent or
// has an empty string value. Otherwise it returns the item unchanged.
func (v *Validate) Process(_ context.Context, item *foxhound.Item) (*foxhound.Item, error) {
	for _, field := range v.Required {
		val, ok := item.Get(field)
		if !ok {
			return nil, nil
		}
		if s, isStr := val.(string); isStr && s == "" {
			return nil, nil
		}
	}
	return item, nil
}
