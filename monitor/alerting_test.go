package monitor_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sadewadee/foxhound/monitor"
)

// alertCapture records all alert payloads received.
type alertCapture struct {
	mu      sync.Mutex
	payloads []map[string]any
	server   *httptest.Server
}

func newAlertCapture() *alertCapture {
	ac := &alertCapture{}
	ac.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err == nil {
			ac.mu.Lock()
			ac.payloads = append(ac.payloads, payload)
			ac.mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	return ac
}

func (ac *alertCapture) URL() string { return ac.server.URL }
func (ac *alertCapture) Close()      { ac.server.Close() }
func (ac *alertCapture) Count() int {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return len(ac.payloads)
}

func TestAlerter_ErrorRateRule_FiresWhenThresholdExceeded(t *testing.T) {
	ac := newAlertCapture()
	defer ac.Close()

	rule := monitor.ErrorRateRule(0.5, 0) // fires if >50% errors, no cooldown
	alerter := monitor.NewAlerter(ac.URL(), rule)

	s := monitor.NewStats()
	// 3 errors out of 4 requests = 75% error rate
	s.RecordRequest(true, false, 0)
	s.RecordRequest(false, false, 0)
	s.RecordRequest(false, false, 0)
	s.RecordRequest(false, false, 0)

	alerter.Check(s)

	if ac.Count() != 1 {
		t.Errorf("ErrorRateRule: expected 1 alert, got %d", ac.Count())
	}
}

func TestAlerter_ErrorRateRule_DoesNotFireBelowThreshold(t *testing.T) {
	ac := newAlertCapture()
	defer ac.Close()

	rule := monitor.ErrorRateRule(0.5, 0) // fires if >50% errors
	alerter := monitor.NewAlerter(ac.URL(), rule)

	s := monitor.NewStats()
	// 1 error out of 4 requests = 25% error rate
	s.RecordRequest(true, false, 0)
	s.RecordRequest(true, false, 0)
	s.RecordRequest(true, false, 0)
	s.RecordRequest(false, false, 0)

	alerter.Check(s)

	if ac.Count() != 0 {
		t.Errorf("ErrorRateRule: expected 0 alerts below threshold, got %d", ac.Count())
	}
}

func TestAlerter_BlockRateRule_FiresWhenThresholdExceeded(t *testing.T) {
	ac := newAlertCapture()
	defer ac.Close()

	rule := monitor.BlockRateRule(0.3, 0) // fires if >30% blocked
	alerter := monitor.NewAlerter(ac.URL(), rule)

	s := monitor.NewStats()
	// 2 blocked out of 3 requests = 67% block rate
	s.RecordRequest(false, true, 0)
	s.RecordRequest(false, true, 0)
	s.RecordRequest(true, false, 0)

	alerter.Check(s)

	if ac.Count() != 1 {
		t.Errorf("BlockRateRule: expected 1 alert, got %d", ac.Count())
	}
}

func TestAlerter_Cooldown_PreventsSecondFire(t *testing.T) {
	ac := newAlertCapture()
	defer ac.Close()

	cooldown := 10 * time.Second
	rule := monitor.ErrorRateRule(0.5, cooldown)
	alerter := monitor.NewAlerter(ac.URL(), rule)

	s := monitor.NewStats()
	s.RecordRequest(false, false, 0) // 100% error

	alerter.Check(s)
	alerter.Check(s) // second check within cooldown

	if ac.Count() != 1 {
		t.Errorf("Cooldown: expected 1 alert (not 2), got %d", ac.Count())
	}
}

func TestAlerter_MultipleRules_EachCanFire(t *testing.T) {
	ac := newAlertCapture()
	defer ac.Close()

	rule1 := monitor.ErrorRateRule(0.3, 0)
	rule2 := monitor.BlockRateRule(0.3, 0)
	alerter := monitor.NewAlerter(ac.URL(), rule1, rule2)

	s := monitor.NewStats()
	// High error AND high block rate
	s.RecordRequest(false, true, 0)
	s.RecordRequest(false, true, 0)
	s.RecordRequest(false, false, 0) // error, not blocked

	alerter.Check(s)

	if ac.Count() < 2 {
		t.Errorf("MultipleRules: expected at least 2 alerts, got %d", ac.Count())
	}
}

func TestAlerter_NoRequestsDoesNotFire(t *testing.T) {
	ac := newAlertCapture()
	defer ac.Close()

	rule := monitor.ErrorRateRule(0.5, 0)
	alerter := monitor.NewAlerter(ac.URL(), rule)

	s := monitor.NewStats()
	// no requests recorded

	alerter.Check(s)

	if ac.Count() != 0 {
		t.Errorf("NoRequests: expected 0 alerts, got %d", ac.Count())
	}
}

func TestAlerter_Close_DoesNotPanic(t *testing.T) {
	ac := newAlertCapture()
	defer ac.Close()

	alerter := monitor.NewAlerter(ac.URL())
	if err := alerter.Close(); err != nil {
		t.Errorf("Close error: %v", err)
	}
}

func TestAlerter_AlertPayload_ContainsRuleName(t *testing.T) {
	ac := newAlertCapture()
	defer ac.Close()

	rule := monitor.ErrorRateRule(0.5, 0)
	alerter := monitor.NewAlerter(ac.URL(), rule)

	s := monitor.NewStats()
	s.RecordRequest(false, false, 0)

	alerter.Check(s)

	if ac.Count() == 0 {
		t.Fatal("expected alert payload, got none")
	}
	ac.mu.Lock()
	payload := ac.payloads[0]
	ac.mu.Unlock()

	if _, ok := payload["rule"]; !ok {
		t.Errorf("alert payload missing 'rule' field: %v", payload)
	}
	if _, ok := payload["message"]; !ok {
		t.Errorf("alert payload missing 'message' field: %v", payload)
	}
}
