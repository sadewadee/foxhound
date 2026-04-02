package monitor

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// StatsCollector periodically collects metrics from a StatsSource and
// pushes them to registered sinks (Prometheus, alerting, logging).
type StatsCollector struct {
	source   StatsSource
	sinks    []StatsSink
	interval time.Duration
	mu       sync.Mutex
	stopCh   chan struct{}
	stopped  bool
}

// StatsSource provides metrics to collect. monitor.Stats implements this
// via its Snapshot method.
type StatsSource interface {
	Snapshot() StatsSnapshot
}

// StatsSnapshot is a point-in-time copy of all metrics.
type StatsSnapshot struct {
	Requests    int64
	Success     int64
	Errors      int64
	Blocked     int64
	Items       int64
	Escalations int64
	Bytes       int64
	Domains     map[string]DomainSnapshot
}

// DomainSnapshot contains per-domain metrics.
type DomainSnapshot struct {
	Requests   int64
	Errors     int64
	Blocked    int64
	AvgLatency time.Duration
}

// StatsSink receives collected metrics.
type StatsSink interface {
	Record(snapshot StatsSnapshot)
}

// NewStatsCollector creates a collector that polls source every interval.
func NewStatsCollector(source StatsSource, interval time.Duration, sinks ...StatsSink) *StatsCollector {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &StatsCollector{
		source:   source,
		sinks:    sinks,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start begins periodic collection in a background goroutine.
func (c *StatsCollector) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			case <-ticker.C:
				snapshot := c.source.Snapshot()
				for _, sink := range c.sinks {
					sink.Record(snapshot)
				}
			}
		}
	}()
}

// Stop halts the collector.
func (c *StatsCollector) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.stopped {
		c.stopped = true
		close(c.stopCh)
	}
}

// LogSink logs stats snapshots at info level.
type LogSink struct{}

// Record logs the snapshot fields.
func (l *LogSink) Record(s StatsSnapshot) {
	slog.Info("stats",
		"requests", s.Requests,
		"success", s.Success,
		"errors", s.Errors,
		"blocked", s.Blocked,
		"items", s.Items,
		"bytes", s.Bytes,
	)
}
