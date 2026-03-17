package parse

import (
	"encoding/json"
	"fmt"
	"strings"

	foxhound "github.com/foxhound-scraper/foxhound"
)

// JSON parses the response body as JSON into target.
// target must be a pointer (e.g. *map[string]any or a pointer to a struct).
func JSON(resp *foxhound.Response, target any) error {
	if err := json.Unmarshal(resp.Body, target); err != nil {
		return fmt.Errorf("parse: decoding JSON response: %w", err)
	}
	return nil
}

// JSONPath extracts a value from the response body using a dot-separated path.
// For example, "data.items" navigates {"data":{"items":[...]}}.
// Only map traversal is supported; the final value may be of any JSON type.
func JSONPath(resp *foxhound.Response, path string) (any, error) {
	var root any
	if err := json.Unmarshal(resp.Body, &root); err != nil {
		return nil, fmt.Errorf("parse: decoding JSON response: %w", err)
	}

	parts := strings.Split(path, ".")
	current := root
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("parse: JSONPath: cannot traverse into non-object at segment %q (type %T)", part, current)
		}
		val, exists := m[part]
		if !exists {
			return nil, fmt.Errorf("parse: JSONPath: key %q not found", part)
		}
		current = val
	}
	return current, nil
}
