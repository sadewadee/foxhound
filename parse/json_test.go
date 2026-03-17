package parse_test

import (
	"net/http"
	"testing"

	foxhound "github.com/foxhound-scraper/foxhound"
	"github.com/foxhound-scraper/foxhound/parse"
)

func newJSONResponse(body string) *foxhound.Response {
	return &foxhound.Response{
		StatusCode: 200,
		Headers:    http.Header{"Content-Type": {"application/json"}},
		Body:       []byte(body),
		URL:        "http://api.example.com/data",
	}
}

func TestJSON_ValidTarget(t *testing.T) {
	resp := newJSONResponse(`{"name":"foxhound","version":1}`)

	var result map[string]any
	if err := parse.JSON(resp, &result); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if result["name"] != "foxhound" {
		t.Errorf("JSON name: got %v, want foxhound", result["name"])
	}
}

func TestJSON_InvalidBody(t *testing.T) {
	resp := newJSONResponse(`not json`)

	var result map[string]any
	if err := parse.JSON(resp, &result); err == nil {
		t.Error("JSON on invalid body: expected error, got nil")
	}
}

func TestJSON_IntoStruct(t *testing.T) {
	resp := newJSONResponse(`{"name":"foxhound","count":42}`)

	type Target struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	var target Target
	if err := parse.JSON(resp, &target); err != nil {
		t.Fatalf("JSON parse into struct error: %v", err)
	}
	if target.Name != "foxhound" {
		t.Errorf("struct Name: got %q, want foxhound", target.Name)
	}
	if target.Count != 42 {
		t.Errorf("struct Count: got %d, want 42", target.Count)
	}
}

func TestJSONPath_SimpleKey(t *testing.T) {
	resp := newJSONResponse(`{"name":"foxhound"}`)

	val, err := parse.JSONPath(resp, "name")
	if err != nil {
		t.Fatalf("JSONPath error: %v", err)
	}
	if val != "foxhound" {
		t.Errorf("JSONPath name: got %v, want foxhound", val)
	}
}

func TestJSONPath_NestedKey(t *testing.T) {
	resp := newJSONResponse(`{"data":{"items":[1,2,3],"count":3}}`)

	val, err := parse.JSONPath(resp, "data.count")
	if err != nil {
		t.Fatalf("JSONPath nested error: %v", err)
	}
	// JSON numbers decode as float64
	if val.(float64) != 3 {
		t.Errorf("JSONPath data.count: got %v, want 3", val)
	}
}

func TestJSONPath_DeeplyNested(t *testing.T) {
	resp := newJSONResponse(`{"a":{"b":{"c":"deep"}}}`)

	val, err := parse.JSONPath(resp, "a.b.c")
	if err != nil {
		t.Fatalf("JSONPath deep nested error: %v", err)
	}
	if val != "deep" {
		t.Errorf("JSONPath a.b.c: got %v, want deep", val)
	}
}

func TestJSONPath_MissingKey(t *testing.T) {
	resp := newJSONResponse(`{"name":"foxhound"}`)

	_, err := parse.JSONPath(resp, "missing")
	if err == nil {
		t.Error("JSONPath on missing key: expected error, got nil")
	}
}

func TestJSONPath_NonObjectAtPath(t *testing.T) {
	resp := newJSONResponse(`{"name":"foxhound"}`)

	_, err := parse.JSONPath(resp, "name.nested")
	if err == nil {
		t.Error("JSONPath traversing into non-object: expected error, got nil")
	}
}

func TestJSONPath_InvalidJSON(t *testing.T) {
	resp := newJSONResponse(`not json`)

	_, err := parse.JSONPath(resp, "key")
	if err == nil {
		t.Error("JSONPath on invalid JSON: expected error, got nil")
	}
}

func TestJSONPath_ArrayAccess(t *testing.T) {
	resp := newJSONResponse(`{"items":["a","b","c"]}`)

	val, err := parse.JSONPath(resp, "items")
	if err != nil {
		t.Fatalf("JSONPath array: error %v", err)
	}
	arr, ok := val.([]any)
	if !ok {
		t.Fatalf("JSONPath items: expected []any, got %T", val)
	}
	if len(arr) != 3 {
		t.Errorf("JSONPath items length: got %d, want 3", len(arr))
	}
}
