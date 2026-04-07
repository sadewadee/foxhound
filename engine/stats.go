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
// All fields use atomic operations so callers can read/write without locks.
type DomainStats struct {
	Requests   atomic.Int64
	Errors     atomic.Int64
	Blocked    atomic.Int64
	totalNanos atomic.Int64 // accumulated fetch latency in nanoseconds

	processNanos atomic.Int64 // accumulated end-to-end job latency in nanoseconds
	processCount atomic.Int64
}

// AvgLatency returns the average fetch latency for this domain.
func (ds *DomainStats) AvgLatency() time.Duration {
	reqs := ds.Requests.Load()
	if reqs == 0 {
		return 0
	}
	return time.Duration(ds.totalNanos.Load() / reqs)
}

// AvgProcessLatency returns the average end-to-end processing latency.
func (ds *DomainStats) AvgProcessLatency() time.Duration {
	count := ds.processCount.Load()
	if count == 0 {
		return 0
	}
	return time.Duration(ds.processNanos.Load() / count)
}

// Stats holds runtime metrics for a Hunt. All top-level counters use
// atomic.Int64 so callers can read them without holding any lock.
// Per-domain stats use sync.Map for lock-free read path.
type Stats struct {
	StartedAt      time.Time
	RequestCount   atomic.Int64
	SuccessCount   atomic.Int64
	ErrorCount     atomic.Int64
	BlockedCount   atomic.Int64
	ItemCount      atomic.Int64
	EscalatedCount atomic.Int64
	BytesReceived  atomic.Int64

	domainStats sync.Map // map[string]*DomainStats
}

// NewStats creates a Stats instance ready for use.
func NewStats() *Stats {
	return &Stats{
		StartedAt: time.Now(),
	}
}

// getOrCreateDomain returns the DomainStats for the given domain, creating
// one if it does not exist. Uses sync.Map.LoadOrStore for lock-free reads.
func (s *Stats) getOrCreateDomain(domain string) *DomainStats {
	val, _ := s.domainStats.LoadOrStore(domain, &DomainStats{})
	return val.(*DomainStats)
}

// RecordRequest records a completed fetch attempt for the given domain.
// Pass err=nil and blocked=false for a clean success.
func (s *Stats) RecordRequest(domain string, duration time.Duration, err error, blocked bool) {
	s.RequestCount.Add(1)
	if err != nil {
		s.ErrorCount.Add(1)
	} else if blocked {
		s.BlockedCount.Add(1)
	} else {
		s.SuccessCount.Add(1)
	}

	ds := s.getOrCreateDomain(domain)
	ds.Requests.Add(1)
	if err != nil {
		ds.Errors.Add(1)
	}
	if blocked {
		ds.Blocked.Add(1)
	}
	ds.totalNanos.Add(duration.Nanoseconds())
}

// RecordBlock increments the request and blocked counters without
// double-counting the request as a success. Used when a block is detected
// outside the normal fetch path (e.g. CAPTCHA detection in the walker).
func (s *Stats) RecordBlock(domain string) {
	s.RequestCount.Add(1)
	s.BlockedCount.Add(1)

	ds := s.getOrCreateDomain(domain)
	ds.Requests.Add(1)
	ds.Blocked.Add(1)
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
	ds := s.getOrCreateDomain(domain)
	ds.processNanos.Add(duration.Nanoseconds())
	ds.processCount.Add(1)
}

// DomainStatsFor returns the DomainStats for the given domain, or nil if no
// requests have been recorded for it.
func (s *Stats) DomainStatsFor(domain string) *DomainStats {
	val, ok := s.domainStats.Load(domain)
	if !ok {
		return nil
	}
	return val.(*DomainStats)
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

	domains := make(map[string]any)
	s.domainStats.Range(func(key, value any) bool {
		domain := key.(string)
		ds := value.(*DomainStats)
		domains[domain] = map[string]any{
			"requests":    ds.Requests.Load(),
			"errors":      ds.Errors.Load(),
			"blocked":     ds.Blocked.Load(),
			"avg_latency": ds.AvgLatency().String(),
		}
		return true
	})
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

	s.domainStats.Range(func(key, value any) bool {
		domain := key.(string)
		ds := value.(*DomainStats)
		fmt.Fprintf(&b, " [%s: req=%d err=%d blocked=%d avg=%v proc_avg=%v]",
			domain, ds.Requests.Load(), ds.Errors.Load(), ds.Blocked.Load(),
			ds.AvgLatency().Truncate(time.Millisecond),
			ds.AvgProcessLatency().Truncate(time.Millisecond))
		return true
	})
	return b.String()
}
