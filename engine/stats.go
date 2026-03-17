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
	totalNanos int64 // accumulated latency in nanoseconds
	AvgLatency time.Duration
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
		fmt.Fprintf(&b, " [%s: req=%d err=%d blocked=%d avg=%v]",
			domain, ds.Requests, ds.Errors, ds.Blocked, ds.AvgLatency.Truncate(time.Millisecond))
	}
	return b.String()
}
