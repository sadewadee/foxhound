package engine

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	foxhound "github.com/sadewadee/foxhound"
)

// ItemList is a thread-safe collection of scraped items with batch export
// methods (JSON, JSONL, CSV). Use it with Hunt.ItemCallback or
// Hunt.StreamWithStats to accumulate items during a hunt.
type ItemList struct {
	mu    sync.RWMutex
	items []*foxhound.Item
}

// NewItemList creates an empty ItemList.
func NewItemList() *ItemList {
	return &ItemList{}
}

// Append adds an item to the list.
func (il *ItemList) Append(item *foxhound.Item) {
	il.mu.Lock()
	il.items = append(il.items, item)
	il.mu.Unlock()
}

// Len returns the number of items.
func (il *ItemList) Len() int {
	il.mu.RLock()
	defer il.mu.RUnlock()
	return len(il.items)
}

// Items returns a copy of the items slice.
func (il *ItemList) Items() []*foxhound.Item {
	il.mu.RLock()
	defer il.mu.RUnlock()
	result := make([]*foxhound.Item, len(il.items))
	copy(result, il.items)
	return result
}

// Clear removes all items.
func (il *ItemList) Clear() {
	il.mu.Lock()
	il.items = nil
	il.mu.Unlock()
}

// ToJSON exports all items to a JSON file. When indent is true, the output
// is pretty-printed with 2-space indentation.
func (il *ItemList) ToJSON(path string, indent bool) error {
	il.mu.RLock()
	defer il.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("engine: create dir for %q: %w", path, err)
	}

	records := make([]map[string]any, len(il.items))
	for i, item := range il.items {
		records[i] = item.ToMap()
	}

	var data []byte
	var err error
	if indent {
		data, err = json.MarshalIndent(records, "", "  ")
	} else {
		data, err = json.Marshal(records)
	}
	if err != nil {
		return fmt.Errorf("engine: marshal items: %w", err)
	}

	return os.WriteFile(path, data, 0o644)
}

// ToJSONL exports items as JSON Lines (one JSON object per line).
func (il *ItemList) ToJSONL(path string) error {
	il.mu.RLock()
	defer il.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("engine: create dir for %q: %w", path, err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("engine: create %q: %w", path, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, item := range il.items {
		if err := enc.Encode(item.ToMap()); err != nil {
			return fmt.Errorf("engine: encode item: %w", err)
		}
	}
	return nil
}

// ToCSV exports items as CSV with the given column order.
func (il *ItemList) ToCSV(path string, columns []string) error {
	il.mu.RLock()
	defer il.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("engine: create dir for %q: %w", path, err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("engine: create %q: %w", path, err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write(columns); err != nil {
		return fmt.Errorf("engine: write CSV header: %w", err)
	}

	for _, item := range il.items {
		if err := w.Write(item.ToCSVRow(columns)); err != nil {
			return fmt.Errorf("engine: write CSV row: %w", err)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// HuntMetrics — extended hunt-level metrics
// ---------------------------------------------------------------------------

// HuntMetrics holds extended statistics for a hunt beyond what Stats tracks.
// It adds offsite/blocked counters, status code breakdown, and per-domain
// byte tracking.
type HuntMetrics struct {
	mu sync.RWMutex

	RequestsCount       int64
	FailedRequestsCount int64
	OffsiteRequests     int64
	BlockedRequests     int64
	ItemsScraped        int64
	ItemsDropped        int64
	ResponseBytes       int64
	StartTime           time.Time
	EndTime             time.Time
	RequestDelay        time.Duration
	ParallelRequests    int

	StatusCounts map[int]int64
	DomainBytes  map[string]int64
	LogCounts    map[string]int64
}

// NewHuntMetrics creates a HuntMetrics initialised with the current time.
func NewHuntMetrics() *HuntMetrics {
	return &HuntMetrics{
		StartTime:    time.Now(),
		StatusCounts: make(map[int]int64),
		DomainBytes:  make(map[string]int64),
		LogCounts:    make(map[string]int64),
	}
}

// ElapsedSeconds returns the duration of the hunt in seconds.
func (hm *HuntMetrics) ElapsedSeconds() float64 {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	end := hm.EndTime
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(hm.StartTime).Seconds()
}

// RequestsPerSecond returns the average requests per second.
func (hm *HuntMetrics) RequestsPerSecond() float64 {
	elapsed := hm.ElapsedSeconds()
	if elapsed == 0 {
		return 0
	}
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return float64(hm.RequestsCount) / elapsed
}

// IncrementStatus records an HTTP status code occurrence.
func (hm *HuntMetrics) IncrementStatus(status int) {
	hm.mu.Lock()
	hm.StatusCounts[status]++
	hm.mu.Unlock()
}

// IncrementResponseBytes adds byte count for a domain.
func (hm *HuntMetrics) IncrementResponseBytes(domain string, count int64) {
	hm.mu.Lock()
	hm.ResponseBytes += count
	hm.DomainBytes[domain] += count
	hm.mu.Unlock()
}

// ToMap returns the metrics as a map for structured logging or JSON export.
func (hm *HuntMetrics) ToMap() map[string]any {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	end := hm.EndTime
	if end.IsZero() {
		end = time.Now()
	}
	elapsed := end.Sub(hm.StartTime).Seconds()
	rps := float64(0)
	if elapsed > 0 {
		rps = float64(hm.RequestsCount) / elapsed
	}

	return map[string]any{
		"items_scraped":          hm.ItemsScraped,
		"items_dropped":          hm.ItemsDropped,
		"elapsed_seconds":        elapsed,
		"parallel_requests":      hm.ParallelRequests,
		"requests_count":         hm.RequestsCount,
		"requests_per_second":    rps,
		"failed_requests":        hm.FailedRequestsCount,
		"offsite_requests":       hm.OffsiteRequests,
		"blocked_requests":       hm.BlockedRequests,
		"response_bytes":         hm.ResponseBytes,
		"response_status_count":  hm.StatusCounts,
		"domains_response_bytes": hm.DomainBytes,
		"log_count":              hm.LogCounts,
	}
}

// ---------------------------------------------------------------------------
// HuntResult
// ---------------------------------------------------------------------------

// HuntResult is the complete result from a hunt execution.
type HuntResult struct {
	// Metrics holds the hunt metrics.
	Metrics *HuntMetrics
	// Items holds all scraped items.
	Items *ItemList
	// Paused is true if the hunt was paused (not completed).
	Paused bool
}

// Completed returns true if the hunt finished normally (was not paused).
func (hr *HuntResult) Completed() bool {
	return !hr.Paused
}

// Len returns the number of scraped items.
func (hr *HuntResult) Len() int {
	if hr.Items == nil {
		return 0
	}
	return hr.Items.Len()
}
