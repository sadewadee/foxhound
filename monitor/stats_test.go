package monitor_test

import (
	"strings"
	"testing"
	"time"

	"github.com/foxhound-scraper/foxhound/monitor"
)

func TestStats_NewStats_ZeroCounters(t *testing.T) {
	s := monitor.NewStats()
	if s.Requests.Load() != 0 {
		t.Errorf("Requests: got %d, want 0", s.Requests.Load())
	}
	if s.Success.Load() != 0 {
		t.Errorf("Success: got %d, want 0", s.Success.Load())
	}
	if s.Errors.Load() != 0 {
		t.Errorf("Errors: got %d, want 0", s.Errors.Load())
	}
}

func TestStats_RecordRequest_Success(t *testing.T) {
	s := monitor.NewStats()
	s.RecordRequest(true, false, 1024)

	if s.Requests.Load() != 1 {
		t.Errorf("Requests: got %d, want 1", s.Requests.Load())
	}
	if s.Success.Load() != 1 {
		t.Errorf("Success: got %d, want 1", s.Success.Load())
	}
	if s.Errors.Load() != 0 {
		t.Errorf("Errors: got %d, want 0", s.Errors.Load())
	}
	if s.Bytes.Load() != 1024 {
		t.Errorf("Bytes: got %d, want 1024", s.Bytes.Load())
	}
}

func TestStats_RecordRequest_Error(t *testing.T) {
	s := monitor.NewStats()
	s.RecordRequest(false, false, 0)

	if s.Requests.Load() != 1 {
		t.Errorf("Requests: got %d, want 1", s.Requests.Load())
	}
	if s.Success.Load() != 0 {
		t.Errorf("Success: got %d, want 0", s.Success.Load())
	}
	if s.Errors.Load() != 1 {
		t.Errorf("Errors: got %d, want 1", s.Errors.Load())
	}
}

func TestStats_RecordRequest_Blocked(t *testing.T) {
	s := monitor.NewStats()
	s.RecordRequest(false, true, 512)

	if s.Blocked.Load() != 1 {
		t.Errorf("Blocked: got %d, want 1", s.Blocked.Load())
	}
}

func TestStats_RecordItems(t *testing.T) {
	s := monitor.NewStats()
	s.RecordItems(5)
	s.RecordItems(3)

	if s.Items.Load() != 8 {
		t.Errorf("Items: got %d, want 8", s.Items.Load())
	}
}

func TestStats_RecordEscalation(t *testing.T) {
	s := monitor.NewStats()
	s.RecordEscalation()
	s.RecordEscalation()

	if s.Escalations.Load() != 2 {
		t.Errorf("Escalations: got %d, want 2", s.Escalations.Load())
	}
}

func TestStats_RecordCacheHitMiss(t *testing.T) {
	s := monitor.NewStats()
	s.RecordCacheHit()
	s.RecordCacheHit()
	s.RecordCacheMiss()

	if s.CacheHits.Load() != 2 {
		t.Errorf("CacheHits: got %d, want 2", s.CacheHits.Load())
	}
	if s.CacheMisses.Load() != 1 {
		t.Errorf("CacheMisses: got %d, want 1", s.CacheMisses.Load())
	}
}

func TestStats_RecordDedup(t *testing.T) {
	s := monitor.NewStats()
	s.RecordDedup()
	s.RecordDedup()
	s.RecordDedup()

	if s.DedupSkipped.Load() != 3 {
		t.Errorf("DedupSkipped: got %d, want 3", s.DedupSkipped.Load())
	}
}

func TestStats_RecordRetry(t *testing.T) {
	s := monitor.NewStats()
	s.RecordRetry()

	if s.RetryCount.Load() != 1 {
		t.Errorf("RetryCount: got %d, want 1", s.RetryCount.Load())
	}
}

func TestStats_SuccessRate(t *testing.T) {
	s := monitor.NewStats()
	s.RecordRequest(true, false, 0)
	s.RecordRequest(true, false, 0)
	s.RecordRequest(true, false, 0)
	s.RecordRequest(false, false, 0)

	rate := s.SuccessRate()
	if rate < 74.9 || rate > 75.1 {
		t.Errorf("SuccessRate: got %.2f, want 75.0", rate)
	}
}

func TestStats_SuccessRate_NoRequests(t *testing.T) {
	s := monitor.NewStats()
	if s.SuccessRate() != 0 {
		t.Errorf("SuccessRate with no requests: got %f, want 0", s.SuccessRate())
	}
}

func TestStats_BlockRate(t *testing.T) {
	s := monitor.NewStats()
	s.RecordRequest(false, true, 0)
	s.RecordRequest(true, false, 0)

	rate := s.BlockRate()
	if rate < 49.9 || rate > 50.1 {
		t.Errorf("BlockRate: got %.2f, want 50.0", rate)
	}
}

func TestStats_BlockRate_NoRequests(t *testing.T) {
	s := monitor.NewStats()
	if s.BlockRate() != 0 {
		t.Errorf("BlockRate with no requests: got %f, want 0", s.BlockRate())
	}
}

func TestStats_Rate_AfterSomeTime(t *testing.T) {
	s := monitor.NewStats()
	// StartedAt is set to a time in the past so rate > 0
	s.RecordRequest(true, false, 0)
	s.RecordRequest(true, false, 0)

	// Allow at least 1ms to pass
	time.Sleep(2 * time.Millisecond)
	rate := s.Rate()
	if rate <= 0 {
		t.Errorf("Rate: expected > 0, got %f", rate)
	}
}

func TestStats_Summary_ContainsKeyFields(t *testing.T) {
	s := monitor.NewStats()
	s.RecordRequest(true, false, 2048)
	s.RecordRequest(false, true, 0)
	s.RecordItems(10)
	s.RecordEscalation()

	summary := s.Summary()

	for _, want := range []string{"requests", "success", "error", "items"} {
		if !strings.Contains(strings.ToLower(summary), want) {
			t.Errorf("Summary missing %q: got %q", want, summary)
		}
	}
}

func TestStats_Summary_NonEmpty(t *testing.T) {
	s := monitor.NewStats()
	if s.Summary() == "" {
		t.Error("Summary: expected non-empty string")
	}
}

func TestStats_AtomicCounters_Concurrent(t *testing.T) {
	s := monitor.NewStats()
	done := make(chan struct{}, 100)

	for i := 0; i < 100; i++ {
		go func() {
			s.RecordRequest(true, false, 1)
			s.RecordItems(1)
			done <- struct{}{}
		}()
	}
	for i := 0; i < 100; i++ {
		<-done
	}

	if s.Requests.Load() != 100 {
		t.Errorf("concurrent Requests: got %d, want 100", s.Requests.Load())
	}
	if s.Items.Load() != 100 {
		t.Errorf("concurrent Items: got %d, want 100", s.Items.Load())
	}
}
