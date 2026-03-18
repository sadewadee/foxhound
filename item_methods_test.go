package foxhound_test

import (
	"encoding/json"
	"strings"
	"testing"

	foxhound "github.com/sadewadee/foxhound"
)

func newTestItem(fields map[string]any) *foxhound.Item {
	it := foxhound.NewItem()
	for k, v := range fields {
		it.Set(k, v)
	}
	return it
}

// ---------------------------------------------------------------------------
// ToJSON / ToJSONPretty
// ---------------------------------------------------------------------------

func TestItem_ToJSON_ProducesValidJSON(t *testing.T) {
	it := newTestItem(map[string]any{"title": "Widget", "price": 9.99})
	data, err := it.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON error: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("ToJSON: not valid JSON: %v\n%s", err, string(data))
	}
	if m["title"] != "Widget" {
		t.Errorf("ToJSON: title=%v, want Widget", m["title"])
	}
}

func TestItem_ToJSONPretty_IsIndented(t *testing.T) {
	it := newTestItem(map[string]any{"title": "Widget"})
	data, err := it.ToJSONPretty()
	if err != nil {
		t.Fatalf("ToJSONPretty error: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "\n") {
		t.Errorf("ToJSONPretty: expected indented JSON with newlines, got:\n%s", content)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("ToJSONPretty: not valid JSON: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ToMap
// ---------------------------------------------------------------------------

func TestItem_ToMap_ReturnsCopy(t *testing.T) {
	it := newTestItem(map[string]any{"a": 1, "b": "hello"})
	m := it.ToMap()
	if len(m) != 2 {
		t.Errorf("ToMap: want 2 keys, got %d", len(m))
	}
	// Mutating the copy must not affect the item.
	m["a"] = 999
	v, _ := it.Get("a")
	if v == 999 {
		t.Error("ToMap: modifying returned map should not affect Item.Fields")
	}
}

// ---------------------------------------------------------------------------
// ToCSVRow
// ---------------------------------------------------------------------------

func TestItem_ToCSVRow_InColumnOrder(t *testing.T) {
	it := newTestItem(map[string]any{"title": "Widget", "price": "$9.99", "sku": "W-001"})
	cols := []string{"sku", "title", "price"}
	row := it.ToCSVRow(cols)
	if len(row) != 3 {
		t.Fatalf("ToCSVRow: want 3 values, got %d", len(row))
	}
	if row[0] != "W-001" {
		t.Errorf("ToCSVRow[0]: got %q, want W-001", row[0])
	}
	if row[1] != "Widget" {
		t.Errorf("ToCSVRow[1]: got %q, want Widget", row[1])
	}
	if row[2] != "$9.99" {
		t.Errorf("ToCSVRow[2]: got %q, want $9.99", row[2])
	}
}

func TestItem_ToCSVRow_MissingFieldReturnsEmpty(t *testing.T) {
	it := newTestItem(map[string]any{"title": "Widget"})
	row := it.ToCSVRow([]string{"title", "missing_field"})
	if row[1] != "" {
		t.Errorf("ToCSVRow missing field: got %q, want empty string", row[1])
	}
}

// ---------------------------------------------------------------------------
// ToMarkdown
// ---------------------------------------------------------------------------

func TestItem_ToMarkdown_ContainsFields(t *testing.T) {
	it := newTestItem(map[string]any{"title": "Widget", "price": "$9.99"})
	md := it.ToMarkdown()
	if !strings.Contains(md, "Widget") {
		t.Errorf("ToMarkdown: 'Widget' not found in:\n%s", md)
	}
	if !strings.Contains(md, "$9.99") {
		t.Errorf("ToMarkdown: '$9.99' not found in:\n%s", md)
	}
}

func TestItem_ToMarkdown_NonEmpty(t *testing.T) {
	it := newTestItem(map[string]any{"k": "v"})
	if it.ToMarkdown() == "" {
		t.Error("ToMarkdown: should return non-empty string for item with fields")
	}
}

// ---------------------------------------------------------------------------
// ToText
// ---------------------------------------------------------------------------

func TestItem_ToText_ContainsKeyValuePairs(t *testing.T) {
	it := newTestItem(map[string]any{"title": "Widget", "price": "$9.99"})
	txt := it.ToText()
	if !strings.Contains(txt, "title") {
		t.Errorf("ToText: 'title' key not found in:\n%s", txt)
	}
	if !strings.Contains(txt, "Widget") {
		t.Errorf("ToText: 'Widget' value not found in:\n%s", txt)
	}
}

// ---------------------------------------------------------------------------
// String (fmt.Stringer)
// ---------------------------------------------------------------------------

func TestItem_String_NonEmpty(t *testing.T) {
	it := newTestItem(map[string]any{"title": "Widget"})
	s := it.String()
	if s == "" {
		t.Error("String: should return non-empty string")
	}
}

// ---------------------------------------------------------------------------
// Keys
// ---------------------------------------------------------------------------

func TestItem_Keys_ReturnsSorted(t *testing.T) {
	it := newTestItem(map[string]any{"z": 1, "a": 2, "m": 3})
	keys := it.Keys()
	if len(keys) != 3 {
		t.Fatalf("Keys: want 3, got %d", len(keys))
	}
	if keys[0] != "a" || keys[1] != "m" || keys[2] != "z" {
		t.Errorf("Keys: want [a m z], got %v", keys)
	}
}

func TestItem_Keys_EmptyItem(t *testing.T) {
	it := foxhound.NewItem()
	keys := it.Keys()
	if len(keys) != 0 {
		t.Errorf("Keys on empty item: want 0, got %d", len(keys))
	}
}

// ---------------------------------------------------------------------------
// Has
// ---------------------------------------------------------------------------

func TestItem_Has_ExistingNonEmptyField(t *testing.T) {
	it := newTestItem(map[string]any{"title": "Widget"})
	if !it.Has("title") {
		t.Error("Has('title'): expected true")
	}
}

func TestItem_Has_MissingField(t *testing.T) {
	it := newTestItem(map[string]any{"title": "Widget"})
	if it.Has("missing") {
		t.Error("Has('missing'): expected false")
	}
}

func TestItem_Has_EmptyStringField(t *testing.T) {
	it := newTestItem(map[string]any{"empty": ""})
	if it.Has("empty") {
		t.Error("Has on empty string field: expected false (empty = not present)")
	}
}

// ---------------------------------------------------------------------------
// GetString
// ---------------------------------------------------------------------------

func TestItem_GetString_ReturnsValue(t *testing.T) {
	it := newTestItem(map[string]any{"title": "Widget"})
	if got := it.GetString("title"); got != "Widget" {
		t.Errorf("GetString: got %q, want Widget", got)
	}
}

func TestItem_GetString_MissingKey_ReturnsEmpty(t *testing.T) {
	it := foxhound.NewItem()
	if got := it.GetString("missing"); got != "" {
		t.Errorf("GetString missing: got %q, want empty", got)
	}
}

func TestItem_GetString_NonStringValue_ReturnsEmpty(t *testing.T) {
	it := newTestItem(map[string]any{"count": 42})
	// Non-string value: returns empty string per spec.
	if got := it.GetString("count"); got != "" {
		t.Errorf("GetString non-string: got %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// GetFloat
// ---------------------------------------------------------------------------

func TestItem_GetFloat_Float64Value(t *testing.T) {
	it := newTestItem(map[string]any{"score": 3.14})
	if got := it.GetFloat("score"); got != 3.14 {
		t.Errorf("GetFloat: got %v, want 3.14", got)
	}
}

func TestItem_GetFloat_IntValue(t *testing.T) {
	it := newTestItem(map[string]any{"count": 42})
	if got := it.GetFloat("count"); got != 42.0 {
		t.Errorf("GetFloat int: got %v, want 42.0", got)
	}
}

func TestItem_GetFloat_MissingKey_ReturnsZero(t *testing.T) {
	it := foxhound.NewItem()
	if got := it.GetFloat("missing"); got != 0 {
		t.Errorf("GetFloat missing: got %v, want 0", got)
	}
}

func TestItem_GetFloat_StringValue_ReturnsZero(t *testing.T) {
	it := newTestItem(map[string]any{"name": "hello"})
	if got := it.GetFloat("name"); got != 0 {
		t.Errorf("GetFloat string: got %v, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// GetInt
// ---------------------------------------------------------------------------

func TestItem_GetInt_IntValue(t *testing.T) {
	it := newTestItem(map[string]any{"count": 7})
	if got := it.GetInt("count"); got != 7 {
		t.Errorf("GetInt: got %v, want 7", got)
	}
}

func TestItem_GetInt_Float64Value(t *testing.T) {
	it := newTestItem(map[string]any{"score": 4.9})
	if got := it.GetInt("score"); got != 4 {
		t.Errorf("GetInt float64: got %v, want 4 (truncated)", got)
	}
}

func TestItem_GetInt_MissingKey_ReturnsZero(t *testing.T) {
	it := foxhound.NewItem()
	if got := it.GetInt("missing"); got != 0 {
		t.Errorf("GetInt missing: got %v, want 0", got)
	}
}

func TestItem_GetInt_StringValue_ReturnsZero(t *testing.T) {
	it := newTestItem(map[string]any{"name": "hello"})
	if got := it.GetInt("name"); got != 0 {
		t.Errorf("GetInt string: got %v, want 0", got)
	}
}
