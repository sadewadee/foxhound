package spider

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

// ItemList is a collection of scraped items with export methods.
// It is safe for concurrent use.
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
		return fmt.Errorf("spider: create dir for %q: %w", path, err)
	}

	// Convert items to field maps for clean JSON output.
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
		return fmt.Errorf("spider: marshal items: %w", err)
	}

	return os.WriteFile(path, data, 0o644)
}

// ToJSONL exports items as JSON Lines (one JSON object per line).
func (il *ItemList) ToJSONL(path string) error {
	il.mu.RLock()
	defer il.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("spider: create dir for %q: %w", path, err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("spider: create %q: %w", path, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, item := range il.items {
		if err := enc.Encode(item.ToMap()); err != nil {
			return fmt.Errorf("spider: encode item: %w", err)
		}
	}
	return nil
}

// ToCSV exports items as CSV with the given column order.
func (il *ItemList) ToCSV(path string, columns []string) error {
	il.mu.RLock()
	defer il.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("spider: create dir for %q: %w", path, err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("spider: create %q: %w", path, err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	// Write header.
	if err := w.Write(columns); err != nil {
		return fmt.Errorf("spider: write CSV header: %w", err)
	}

	// Write rows.
	for _, item := range il.items {
		if err := w.Write(item.ToCSVRow(columns)); err != nil {
			return fmt.Errorf("spider: write CSV row: %w", err)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// CrawlStats
// ---------------------------------------------------------------------------

// CrawlStats holds statistics for a spider run.
type CrawlStats struct {
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
	DownloadDelay       time.Duration
	ConcurrentRequests  int

	StatusCounts map[int]int64
	DomainBytes  map[string]int64
	LogCounts    map[string]int64
}

// NewCrawlStats creates a CrawlStats initialised with the current time.
func NewCrawlStats() *CrawlStats {
	return &CrawlStats{
		StartTime:    time.Now(),
		StatusCounts: make(map[int]int64),
		DomainBytes:  make(map[string]int64),
		LogCounts:    make(map[string]int64),
	}
}

// ElapsedSeconds returns the duration of the crawl in seconds.
func (cs *CrawlStats) ElapsedSeconds() float64 {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	end := cs.EndTime
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(cs.StartTime).Seconds()
}

// RequestsPerSecond returns the average requests per second.
func (cs *CrawlStats) RequestsPerSecond() float64 {
	elapsed := cs.ElapsedSeconds()
	if elapsed == 0 {
		return 0
	}
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return float64(cs.RequestsCount) / elapsed
}

// IncrementStatus records an HTTP status code occurrence.
func (cs *CrawlStats) IncrementStatus(status int) {
	cs.mu.Lock()
	cs.StatusCounts[status]++
	cs.mu.Unlock()
}

// IncrementResponseBytes adds byte count for a domain.
func (cs *CrawlStats) IncrementResponseBytes(domain string, count int64) {
	cs.mu.Lock()
	cs.ResponseBytes += count
	cs.DomainBytes[domain] += count
	cs.mu.Unlock()
}

// ToMap returns the stats as a map for structured logging or JSON export.
func (cs *CrawlStats) ToMap() map[string]any {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	return map[string]any{
		"items_scraped":        cs.ItemsScraped,
		"items_dropped":        cs.ItemsDropped,
		"elapsed_seconds":      cs.ElapsedSeconds(),
		"concurrent_requests":  cs.ConcurrentRequests,
		"requests_count":       cs.RequestsCount,
		"requests_per_second":  cs.RequestsPerSecond(),
		"failed_requests":      cs.FailedRequestsCount,
		"offsite_requests":     cs.OffsiteRequests,
		"blocked_requests":     cs.BlockedRequests,
		"response_bytes":       cs.ResponseBytes,
		"response_status_count": cs.StatusCounts,
		"domains_response_bytes": cs.DomainBytes,
		"log_count":            cs.LogCounts,
	}
}

// ---------------------------------------------------------------------------
// CrawlResult
// ---------------------------------------------------------------------------

// CrawlResult is the complete result from a spider run.
type CrawlResult struct {
	// Stats holds the crawl statistics.
	Stats *CrawlStats
	// Items holds all scraped items.
	Items *ItemList
	// Paused is true if the crawl was paused (not completed).
	Paused bool
}

// Completed returns true if the crawl finished normally (was not paused).
func (cr *CrawlResult) Completed() bool {
	return !cr.Paused
}

// Len returns the number of scraped items.
func (cr *CrawlResult) Len() int {
	if cr.Items == nil {
		return 0
	}
	return cr.Items.Len()
}
