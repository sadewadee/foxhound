package monitor_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sadewadee/foxhound/monitor"
)

// mockSource implements StatsSource for testing.
type mockSource struct {
	snapshot monitor.StatsSnapshot
}

func (m *mockSource) Snapshot() monitor.StatsSnapshot {
	return m.snapshot
}

// mockSink implements StatsSink and counts Record calls.
type mockSink struct {
	calls atomic.Int64
	mu    sync.Mutex
	last  monitor.StatsSnapshot
}

func (m *mockSink) Record(s monitor.StatsSnapshot) {
	m.calls.Add(1)
	m.mu.Lock()
	m.last = s
	m.mu.Unlock()
}

func (m *mockSink) Last() monitor.StatsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.last
}

func TestStatsCollector_CallsRecordOnSinks(t *testing.T) {
	src := &mockSource{
		snapshot: monitor.StatsSnapshot{
			Requests: 42,
			Success:  40,
			Errors:   2,
			Items:    100,
		},
	}
	sink := &mockSink{}

	collector := monitor.NewStatsCollector(src, 20*time.Millisecond, sink)
	ctx := context.Background()
	collector.Start(ctx)

	// Wait enough for at least one tick.
	time.Sleep(80 * time.Millisecond)
	collector.Stop()

	if sink.calls.Load() == 0 {
		t.Fatal("Expected sink.Record to be called at least once")
	}
	last := sink.Last()
	if last.Requests != 42 {
		t.Errorf("Expected Requests=42 in snapshot, got %d", last.Requests)
	}
	if last.Items != 100 {
		t.Errorf("Expected Items=100 in snapshot, got %d", last.Items)
	}
}

func TestStatsCollector_StopHaltsGoroutine(t *testing.T) {
	src := &mockSource{}
	sink := &mockSink{}

	collector := monitor.NewStatsCollector(src, 10*time.Millisecond, sink)
	ctx := context.Background()
	collector.Start(ctx)

	// Let it tick a couple of times.
	time.Sleep(50 * time.Millisecond)
	collector.Stop()

	countAfterStop := sink.calls.Load()

	// Wait and verify no more calls happen.
	time.Sleep(50 * time.Millisecond)
	countLater := sink.calls.Load()

	if countLater > countAfterStop+1 {
		// Allow at most 1 extra call due to timing races.
		t.Errorf("Expected goroutine to stop, but calls went from %d to %d",
			countAfterStop, countLater)
	}
}

func TestStatsCollector_ContextCancellation(t *testing.T) {
	src := &mockSource{}
	sink := &mockSink{}

	collector := monitor.NewStatsCollector(src, 10*time.Millisecond, sink)
	ctx, cancel := context.WithCancel(context.Background())
	collector.Start(ctx)

	time.Sleep(50 * time.Millisecond)
	cancel()

	countAfterCancel := sink.calls.Load()
	time.Sleep(50 * time.Millisecond)
	countLater := sink.calls.Load()

	if countLater > countAfterCancel+1 {
		t.Errorf("Expected goroutine to stop on context cancel, but calls went from %d to %d",
			countAfterCancel, countLater)
	}

	// Still need to stop to prevent double-close.
	collector.Stop()
}
