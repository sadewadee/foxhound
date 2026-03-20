package engine_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/engine"
)

func makeTestItem(title, price string) *foxhound.Item {
	item := foxhound.NewItem()
	item.Set("title", title)
	item.Set("price", price)
	return item
}

func TestItemList_AppendAndLen(t *testing.T) {
	il := engine.NewItemList()
	if il.Len() != 0 {
		t.Errorf("empty ItemList.Len() = %d, want 0", il.Len())
	}

	il.Append(makeTestItem("A", "10"))
	il.Append(makeTestItem("B", "20"))

	if il.Len() != 2 {
		t.Errorf("ItemList.Len() = %d, want 2", il.Len())
	}
}

func TestItemList_Items_ReturnsCopy(t *testing.T) {
	il := engine.NewItemList()
	il.Append(makeTestItem("A", "10"))
	il.Append(makeTestItem("B", "20"))

	items := il.Items()
	if len(items) != 2 {
		t.Fatalf("Items() returned %d, want 2", len(items))
	}

	items[0] = nil
	if il.Items()[0] == nil {
		t.Error("Items() should return a copy, not a reference")
	}
}

func TestItemList_Clear(t *testing.T) {
	il := engine.NewItemList()
	il.Append(makeTestItem("A", "10"))
	il.Clear()
	if il.Len() != 0 {
		t.Errorf("after Clear(), Len() = %d, want 0", il.Len())
	}
}

func TestItemList_ToJSON(t *testing.T) {
	il := engine.NewItemList()
	il.Append(makeTestItem("Widget", "9.99"))
	il.Append(makeTestItem("Gadget", "19.99"))

	dir := t.TempDir()
	path := filepath.Join(dir, "items.json")

	if err := il.ToJSON(path, false); err != nil {
		t.Fatalf("ToJSON error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	var records []map[string]any
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(records) != 2 {
		t.Errorf("expected 2 records, got %d", len(records))
	}
	if records[0]["title"] != "Widget" {
		t.Errorf("records[0][title] = %v, want Widget", records[0]["title"])
	}
}

func TestItemList_ToJSON_Indented(t *testing.T) {
	il := engine.NewItemList()
	il.Append(makeTestItem("Test", "1.00"))

	dir := t.TempDir()
	path := filepath.Join(dir, "pretty.json")

	if err := il.ToJSON(path, true); err != nil {
		t.Fatalf("ToJSON(indent) error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "\n") {
		t.Error("indented JSON should contain newlines")
	}
}

func TestItemList_ToJSONL(t *testing.T) {
	il := engine.NewItemList()
	il.Append(makeTestItem("A", "1"))
	il.Append(makeTestItem("B", "2"))
	il.Append(makeTestItem("C", "3"))

	dir := t.TempDir()
	path := filepath.Join(dir, "items.jsonl")

	if err := il.ToJSONL(path); err != nil {
		t.Fatalf("ToJSONL error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Errorf("JSONL has %d lines, want 3", len(lines))
	}

	for i, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i, err)
		}
	}
}

func TestItemList_ToCSV(t *testing.T) {
	il := engine.NewItemList()
	il.Append(makeTestItem("Widget", "9.99"))
	il.Append(makeTestItem("Gadget", "19.99"))

	dir := t.TempDir()
	path := filepath.Join(dir, "items.csv")
	columns := []string{"title", "price"}

	if err := il.ToCSV(path, columns); err != nil {
		t.Fatalf("ToCSV error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Errorf("CSV has %d lines, want 3 (header + 2 rows)", len(lines))
	}
	if lines[0] != "title,price" {
		t.Errorf("CSV header = %q, want 'title,price'", lines[0])
	}
}

// ---------------------------------------------------------------------------
// HuntMetrics tests
// ---------------------------------------------------------------------------

func TestHuntMetrics_ElapsedSeconds(t *testing.T) {
	hm := engine.NewHuntMetrics()
	time.Sleep(50 * time.Millisecond)

	elapsed := hm.ElapsedSeconds()
	if elapsed < 0.04 || elapsed > 1.0 {
		t.Errorf("ElapsedSeconds() = %f, expected ~0.05", elapsed)
	}
}

func TestHuntMetrics_RequestsPerSecond(t *testing.T) {
	hm := engine.NewHuntMetrics()
	hm.RequestsCount = 100
	hm.EndTime = hm.StartTime.Add(10 * time.Second)

	rps := hm.RequestsPerSecond()
	if rps < 9 || rps > 11 {
		t.Errorf("RequestsPerSecond() = %f, want ~10", rps)
	}
}

func TestHuntMetrics_IncrementStatus(t *testing.T) {
	hm := engine.NewHuntMetrics()
	hm.IncrementStatus(200)
	hm.IncrementStatus(200)
	hm.IncrementStatus(404)

	if hm.StatusCounts[200] != 2 {
		t.Errorf("StatusCounts[200] = %d, want 2", hm.StatusCounts[200])
	}
	if hm.StatusCounts[404] != 1 {
		t.Errorf("StatusCounts[404] = %d, want 1", hm.StatusCounts[404])
	}
}

func TestHuntMetrics_IncrementResponseBytes(t *testing.T) {
	hm := engine.NewHuntMetrics()
	hm.IncrementResponseBytes("example.com", 1000)
	hm.IncrementResponseBytes("example.com", 500)
	hm.IncrementResponseBytes("other.com", 200)

	if hm.ResponseBytes != 1700 {
		t.Errorf("ResponseBytes = %d, want 1700", hm.ResponseBytes)
	}
	if hm.DomainBytes["example.com"] != 1500 {
		t.Errorf("DomainBytes[example.com] = %d, want 1500", hm.DomainBytes["example.com"])
	}
}

func TestHuntMetrics_ToMap(t *testing.T) {
	hm := engine.NewHuntMetrics()
	hm.RequestsCount = 50
	hm.ItemsScraped = 30

	m := hm.ToMap()
	if m["requests_count"] != int64(50) {
		t.Errorf("ToMap()[requests_count] = %v, want 50", m["requests_count"])
	}
	if m["items_scraped"] != int64(30) {
		t.Errorf("ToMap()[items_scraped] = %v, want 30", m["items_scraped"])
	}
}

// ---------------------------------------------------------------------------
// HuntResult tests
// ---------------------------------------------------------------------------

func TestHuntResult_Completed(t *testing.T) {
	hr := &engine.HuntResult{
		Metrics: engine.NewHuntMetrics(),
		Items:   engine.NewItemList(),
		Paused:  false,
	}
	if !hr.Completed() {
		t.Error("Completed() should be true when not paused")
	}

	hr.Paused = true
	if hr.Completed() {
		t.Error("Completed() should be false when paused")
	}
}

func TestHuntResult_Len(t *testing.T) {
	hr := &engine.HuntResult{
		Metrics: engine.NewHuntMetrics(),
		Items:   engine.NewItemList(),
	}
	if hr.Len() != 0 {
		t.Errorf("empty HuntResult.Len() = %d, want 0", hr.Len())
	}

	hr.Items.Append(makeTestItem("A", "1"))
	if hr.Len() != 1 {
		t.Errorf("HuntResult.Len() = %d, want 1", hr.Len())
	}
}

func TestHuntResult_NilItems(t *testing.T) {
	hr := &engine.HuntResult{Metrics: engine.NewHuntMetrics()}
	if hr.Len() != 0 {
		t.Errorf("HuntResult with nil items: Len() = %d, want 0", hr.Len())
	}
}
