package monitor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// AlertRule defines a named condition that triggers a webhook alert.
type AlertRule struct {
	// Name identifies the rule in alert payloads and logs.
	Name string
	// Condition returns true when the alert should fire.
	Condition func(stats *Stats) bool
	// Message produces the human-readable alert body from current stats.
	Message func(stats *Stats) string
	// Cooldown is the minimum duration between successive fires of this rule.
	// A zero value means no cooldown (fire on every Check).
	Cooldown  time.Duration
	lastFired time.Time
}

// Alerter evaluates AlertRules against Stats and POSTs JSON payloads to a
// webhook URL when conditions are met.
type Alerter struct {
	webhookURL string
	client     *http.Client
	rules      []AlertRule
}

// NewAlerter returns an Alerter that will POST to webhookURL when any of the
// provided rules fires.
func NewAlerter(webhookURL string, rules ...AlertRule) *Alerter {
	return &Alerter{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
		rules:      rules,
	}
}

// ErrorRateRule returns a rule that fires when the fraction of failed requests
// exceeds threshold (0.0–1.0). Pass 0 for cooldown to fire on every Check.
func ErrorRateRule(threshold float64, cooldown time.Duration) AlertRule {
	return AlertRule{
		Name: "error_rate",
		Condition: func(s *Stats) bool {
			total := s.Requests.Load()
			if total == 0 {
				return false
			}
			rate := float64(s.Errors.Load()) / float64(total)
			return rate > threshold
		},
		Message: func(s *Stats) string {
			total := s.Requests.Load()
			if total == 0 {
				return "error rate exceeded threshold (no requests)"
			}
			rate := float64(s.Errors.Load()) / float64(total) * 100
			return fmt.Sprintf("error rate %.1f%% exceeds threshold %.1f%%",
				rate, threshold*100)
		},
		Cooldown: cooldown,
	}
}

// BlockRateRule returns a rule that fires when the fraction of blocked requests
// exceeds threshold (0.0–1.0).
func BlockRateRule(threshold float64, cooldown time.Duration) AlertRule {
	return AlertRule{
		Name: "block_rate",
		Condition: func(s *Stats) bool {
			total := s.Requests.Load()
			if total == 0 {
				return false
			}
			rate := float64(s.Blocked.Load()) / float64(total)
			return rate > threshold
		},
		Message: func(s *Stats) string {
			total := s.Requests.Load()
			if total == 0 {
				return "block rate exceeded threshold (no requests)"
			}
			rate := float64(s.Blocked.Load()) / float64(total) * 100
			return fmt.Sprintf("block rate %.1f%% exceeds threshold %.1f%%",
				rate, threshold*100)
		},
		Cooldown: cooldown,
	}
}

// Check evaluates all rules against stats and fires alerts for any that match
// and are outside their cooldown window.
func (a *Alerter) Check(stats *Stats) {
	now := time.Now()
	for i := range a.rules {
		rule := &a.rules[i]
		if !rule.Condition(stats) {
			continue
		}
		if rule.Cooldown > 0 && !rule.lastFired.IsZero() &&
			now.Sub(rule.lastFired) < rule.Cooldown {
			slog.Debug("alerter: rule in cooldown, skipping", "rule", rule.Name)
			continue
		}
		msg := rule.Message(stats)
		if err := a.sendAlert(*rule, msg); err != nil {
			slog.Error("alerter: failed to send alert",
				"rule", rule.Name,
				"error", err,
			)
		} else {
			rule.lastFired = now
			slog.Info("alerter: alert fired", "rule", rule.Name, "message", msg)
		}
	}
}

// sendAlert POSTs a JSON payload containing the rule name, message, and current
// timestamp to the configured webhook URL.
func (a *Alerter) sendAlert(rule AlertRule, message string) error {
	payload := map[string]any{
		"rule":      rule.Name,
		"message":   message,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("alerter: marshalling payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, a.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("alerter: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("alerter: POST failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("alerter: non-2xx response %d", resp.StatusCode)
	}
	return nil
}

// Close releases resources held by the Alerter.
func (a *Alerter) Close() error {
	a.client.CloseIdleConnections()
	return nil
}
