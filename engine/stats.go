// Package engine implements the core orchestration layer for the Foxhound
// scraping framework: Hunt (campaign coordinator), Walker (virtual user),
// Trail (navigation path), Scheduler (goroutine pool), RetryPolicy, and Stats.
package engine

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DomainStats holds per-domain request counters and latency tracking.
type DomainStats struct {
	Requests   int64
	Errors     int64
	Blocked    int64
	totalNanos int64 // accumulated fetch latency in nanoseconds
	AvgLatency time.Duration

	processNanos      int64 // accumulated end-to-end job latency in nanoseconds
	processCount      int64
	AvgProcessLatency time.Duration
}

// Stats holds runtime metrics for a Hunt. All top-level counters use
// atomic.Int64 so callers can read them without holding any lock.
type Stats struct {
	StartedAt      time.Time
	RequestCount   atomic.Int64
	SuccessCount   atomic.Int64
	ErrorCount     atomic.Int64
	BlockedCount   atomic.Int64
	ItemCount      atomic.Int64
	EscalatedCount atomic.Int64
	BytesReceived  atomic.Int64

	mu          sync.RWMutex
	domainStats map[string]*DomainStats
}

// NewStats creates a Stats instance ready for use.
func NewStats() *Stats {
	return &Stats{
		StartedAt:   time.Now(),
		domainStats: make(map[string]*DomainStats),
	}
}

// RecordRequest records a completed fetch attempt for the given domain.
// Pass err=nil and blocked=false for a clean success.
func (s *Stats) RecordRequest(domain string, duration time.Duration, err error, blocked bool) {
	s.RequestCount.Add(1)
	if err != nil {
		s.ErrorCount.Add(1)
	} else {
		s.SuccessCount.Add(1)
	}
	if blocked {
		s.BlockedCount.Add(1)
	}

	s.mu.Lock()
	ds, ok := s.domainStats[domain]
	if !ok {
		ds = &DomainStats{}
		s.domainStats[domain] = ds
	}
	ds.Requests++
	if err != nil {
		ds.Errors++
	}
	if blocked {
		ds.Blocked++
	}
	ds.totalNanos += duration.Nanoseconds()
	if ds.Requests > 0 {
		ds.AvgLatency = time.Duration(ds.totalNanos / ds.Requests)
	}
	s.mu.Unlock()
}

// RecordItems increments the scraped item counter by count.
func (s *Stats) RecordItems(count int) {
	s.ItemCount.Add(int64(count))
}

// RecordEscalation increments the count of requests that were escalated from
// the static fetcher to the browser fetcher.
func (s *Stats) RecordEscalation() {
	s.EscalatedCount.Add(1)
}

// RecordBytes adds n to the total bytes-received counter.
func (s *Stats) RecordBytes(n int64) {
	s.BytesReceived.Add(n)
}

// RecordProcessDuration records the end-to-end processing time for a job
// (fetch + process + pipeline + write) for the given domain.
func (s *Stats) RecordProcessDuration(domain string, duration time.Duration) {
	s.mu.Lock()
	ds, ok := s.domainStats[domain]
	if !ok {
		ds = &DomainStats{}
		s.domainStats[domain] = ds
	}
	ds.processNanos += duration.Nanoseconds()
	ds.processCount++
	ds.AvgProcessLatency = time.Duration(ds.processNanos / ds.processCount)
	s.mu.Unlock()
}

// DomainStatsFor returns the DomainStats for the given domain, or nil if no
// requests have been recorded for it.
func (s *Stats) DomainStatsFor(domain string) *DomainStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ds, ok := s.domainStats[domain]
	if !ok {
		return nil
	}
	// Return a copy to avoid races on the caller side.
	copy := *ds
	return &copy
}

// ToMap returns a structured snapshot of current statistics suitable for
// JSON serialisation or structured logging.
func (s *Stats) ToMap() map[string]any {
	elapsed := time.Since(s.StartedAt)
	elapsedSec := elapsed.Seconds()
	reqCount := s.RequestCount.Load()
	rps := float64(0)
	if elapsedSec > 0 {
		rps = float64(reqCount) / elapsedSec
	}

	result := map[string]any{
		"elapsed":        elapsed.Truncate(time.Second).String(),
		"requests":       reqCount,
		"success":        s.SuccessCount.Load(),
		"errors":         s.ErrorCount.Load(),
		"blocked":        s.BlockedCount.Load(),
		"items":          s.ItemCount.Load(),
		"escalated":      s.EscalatedCount.Load(),
		"bytes_received": s.BytesReceived.Load(),
		"req_per_sec":    rps,
	}

	s.mu.RLock()
	domains := make(map[string]any, len(s.domainStats))
	for domain, ds := range s.domainStats {
		domains[domain] = map[string]any{
			"requests":    ds.Requests,
			"errors":      ds.Errors,
			"blocked":     ds.Blocked,
			"avg_latency": ds.AvgLatency.String(),
		}
	}
	s.mu.RUnlock()
	result["domains"] = domains

	return result
}

// Summary returns a human-readable snapshot of current statistics.
func (s *Stats) Summary() string {
	elapsed := time.Since(s.StartedAt).Truncate(time.Second)
	var b strings.Builder
	fmt.Fprintf(&b, "elapsed=%v requests=%d success=%d errors=%d blocked=%d items=%d escalated=%d bytes=%d",
		elapsed,
		s.RequestCount.Load(),
		s.SuccessCount.Load(),
		s.ErrorCount.Load(),
		s.BlockedCount.Load(),
		s.ItemCount.Load(),
		s.EscalatedCount.Load(),
		s.BytesReceived.Load(),
	)

	s.mu.RLock()
	defer s.mu.RUnlock()
	for domain, ds := range s.domainStats {
		fmt.Fprintf(&b, " [%s: req=%d err=%d blocked=%d avg=%v proc_avg=%v]",
			domain, ds.Requests, ds.Errors, ds.Blocked,
			ds.AvgLatency.Truncate(time.Millisecond),
			ds.AvgProcessLatency.Truncate(time.Millisecond))
	}
	return b.String()
}
