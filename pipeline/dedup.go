package pipeline

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	foxhound "github.com/sadewadee/foxhound"
)

// ItemDedup drops duplicate items based on a key field.
// The first item with a given key value passes through; subsequent items with
// the same key are dropped (Process returns nil, nil).
// Items that are missing the key field entirely are also dropped.
//
// ItemDedup is safe for concurrent use.
type ItemDedup struct {
	// KeyField is the item field used as the deduplication key.
	KeyField string
	seen     map[string]struct{}
	mu       sync.Mutex
}

// NewItemDedup returns an ItemDedup that deduplicates on keyField.
func NewItemDedup(keyField string) *ItemDedup {
	return &ItemDedup{
		KeyField: keyField,
		seen:     make(map[string]struct{}),
	}
}

// Process returns nil if the item's key field value has been seen before, or
// if the key field is absent. Otherwise it marks the value as seen and returns
// the item unchanged.
// valToString converts a value to string efficiently for dedup key generation.
func valToString(val any) string {
	switch v := val.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (d *ItemDedup) Process(_ context.Context, item *foxhound.Item) (*foxhound.Item, error) {
	val, ok := item.Get(d.KeyField)
	if !ok {
		return nil, nil
	}
	key := valToString(val)

	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.seen[key]; exists {
		return nil, nil
	}
	d.seen[key] = struct{}{}
	return item, nil
}
