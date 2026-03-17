// Package monitor provides runtime statistics, Prometheus metrics, and alerting
// for Foxhound scraping hunts.
package monitor

import (
	"fmt"
	"sync/atomic"
	"time"
)

// Stats collects runtime statistics for a hunt using lock-free atomic counters.
// All methods are safe for concurrent use.
type Stats struct {
	// StartedAt is set when NewStats is called and used for rate calculations.
	StartedAt time.Time

	// Request counters.
	Requests  atomic.Int64
	Success   atomic.Int64
	Errors    atomic.Int64
	Blocked   atomic.Int64
	Bytes     atomic.Int64

	// Pipeline / crawler counters.
	Items        atomic.Int64
	Escalations  atomic.Int64
	CacheHits    atomic.Int64
	CacheMisses  atomic.Int64
	DedupSkipped atomic.Int64
	RetryCount   atomic.Int64
}

// NewStats returns a Stats with StartedAt set to the current time.
func NewStats() *Stats {
	return &Stats{StartedAt: time.Now()}
}

// RecordRequest records one completed request.
// success=true increments Success; false increments Errors.
// blocked=true additionally increments Blocked.
// bytes is the response size in bytes.
func (s *Stats) RecordRequest(success bool, blocked bool, bytes int64) {
	s.Requests.Add(1)
	if success {
		s.Success.Add(1)
	} else {
		s.Errors.Add(1)
	}
	if blocked {
		s.Blocked.Add(1)
	}
	if bytes > 0 {
		s.Bytes.Add(bytes)
	}
}

// RecordItems adds n to the items counter.
func (s *Stats) RecordItems(n int64) {
	s.Items.Add(n)
}

// RecordEscalation records one static→browser escalation.
func (s *Stats) RecordEscalation() {
	s.Escalations.Add(1)
}

// RecordCacheHit records one cache hit.
func (s *Stats) RecordCacheHit() {
	s.CacheHits.Add(1)
}

// RecordCacheMiss records one cache miss.
func (s *Stats) RecordCacheMiss() {
	s.CacheMisses.Add(1)
}

// RecordDedup records one item skipped by the deduplication filter.
func (s *Stats) RecordDedup() {
	s.DedupSkipped.Add(1)
}

// RecordRetry records one retry attempt.
func (s *Stats) RecordRetry() {
	s.RetryCount.Add(1)
}

// Rate returns the number of requests per second since StartedAt.
// Returns 0 if no time has elapsed yet.
func (s *Stats) Rate() float64 {
	elapsed := time.Since(s.StartedAt).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return float64(s.Requests.Load()) / elapsed
}

// SuccessRate returns the percentage of requests that succeeded (0–100).
// Returns 0 when no requests have been recorded.
func (s *Stats) SuccessRate() float64 {
	total := s.Requests.Load()
	if total == 0 {
		return 0
	}
	return float64(s.Success.Load()) / float64(total) * 100
}

// BlockRate returns the percentage of requests that were blocked (0–100).
// Returns 0 when no requests have been recorded.
func (s *Stats) BlockRate() float64 {
	total := s.Requests.Load()
	if total == 0 {
		return 0
	}
	return float64(s.Blocked.Load()) / float64(total) * 100
}

// Summary returns a human-readable single-line statistics summary.
func (s *Stats) Summary() string {
	elapsed := time.Since(s.StartedAt).Round(time.Second)
	return fmt.Sprintf(
		"elapsed=%s requests=%d success=%d errors=%d blocked=%d items=%d "+
			"bytes=%d escalations=%d cache_hits=%d cache_misses=%d "+
			"dedup_skipped=%d retries=%d rate=%.2f/s success_rate=%.1f%% block_rate=%.1f%%",
		elapsed,
		s.Requests.Load(),
		s.Success.Load(),
		s.Errors.Load(),
		s.Blocked.Load(),
		s.Items.Load(),
		s.Bytes.Load(),
		s.Escalations.Load(),
		s.CacheHits.Load(),
		s.CacheMisses.Load(),
		s.DedupSkipped.Load(),
		s.RetryCount.Load(),
		s.Rate(),
		s.SuccessRate(),
		s.BlockRate(),
	)
}
