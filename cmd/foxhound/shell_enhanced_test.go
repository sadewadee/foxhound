package main

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TestShellHelpIncludesNewCommands
// ---------------------------------------------------------------------------

func TestShellHelpIncludesNewCommands(t *testing.T) {
	text := shellHelpText()
	newCommands := []string{
		"adaptive",
		"extract",
		"export",
		"history",
		"timing",
		"compare",
		"status",
	}
	for _, cmd := range newCommands {
		if !strings.Contains(text, cmd) {
			t.Errorf("help text missing new command %q\nFull help:\n%s", cmd, text)
		}
	}
}

func TestShellHelpStillIncludesExistingCommands(t *testing.T) {
	text := shellHelpText()
	existing := []string{"fetch", "identity", "headers", "parse", "proxy", "exit"}
	for _, cmd := range existing {
		if !strings.Contains(text, cmd) {
			t.Errorf("help text dropped existing command %q\nFull help:\n%s", cmd, text)
		}
	}
}

// ---------------------------------------------------------------------------
// TestShellHistory
// ---------------------------------------------------------------------------

func TestShellHistory_StartsEmpty(t *testing.T) {
	h := newShellSession()
	entries := h.History()
	if len(entries) != 0 {
		t.Errorf("expected empty history, got %d entries", len(entries))
	}
}

func TestShellHistory_RecordsEntry(t *testing.T) {
	h := newShellSession()
	h.RecordFetch("https://example.com", 200, 1024, 50)
	entries := h.History()
	if len(entries) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(entries))
	}
	e := entries[0]
	if e.URL != "https://example.com" {
		t.Errorf("expected URL %q, got %q", "https://example.com", e.URL)
	}
	if e.Status != 200 {
		t.Errorf("expected status 200, got %d", e.Status)
	}
	if e.Bytes != 1024 {
		t.Errorf("expected 1024 bytes, got %d", e.Bytes)
	}
}

func TestShellHistory_LimitsTenEntries(t *testing.T) {
	h := newShellSession()
	for i := 0; i < 15; i++ {
		h.RecordFetch("https://example.com/page", 200, 100, 10)
	}
	entries := h.History()
	if len(entries) > 10 {
		t.Errorf("history should be capped at 10 entries, got %d", len(entries))
	}
}

func TestShellHistory_ReturnsLastTen(t *testing.T) {
	h := newShellSession()
	for i := 1; i <= 12; i++ {
		url := "https://example.com/page"
		h.RecordFetch(url, 200+i, 100, 10)
	}
	entries := h.History()
	// The oldest (200+1, 200+2) should be dropped; we keep the latest 10.
	if len(entries) != 10 {
		t.Fatalf("expected 10 entries, got %d", len(entries))
	}
	// The first kept entry should have status 203 (i=3, dropped i=1 and i=2).
	if entries[0].Status != 203 {
		t.Errorf("expected oldest kept status=203, got %d", entries[0].Status)
	}
}

// ---------------------------------------------------------------------------
// TestShellTimingStats
// ---------------------------------------------------------------------------

func TestShellTiming_EmptySessionReturnsZeros(t *testing.T) {
	h := newShellSession()
	avg, min, max := h.TimingStats()
	if avg != 0 || min != 0 || max != 0 {
		t.Errorf("empty session timing should be 0, got avg=%d min=%d max=%d", avg, min, max)
	}
}

func TestShellTiming_ComputesCorrectStats(t *testing.T) {
	h := newShellSession()
	h.RecordFetch("https://a.com", 200, 100, 10)
	h.RecordFetch("https://b.com", 200, 200, 20)
	h.RecordFetch("https://c.com", 200, 300, 30)

	avg, min, max := h.TimingStats()
	if min != 10 {
		t.Errorf("expected min=10ms, got %d", min)
	}
	if max != 30 {
		t.Errorf("expected max=30ms, got %d", max)
	}
	if avg != 20 {
		t.Errorf("expected avg=20ms, got %d", avg)
	}
}

// ---------------------------------------------------------------------------
// TestShellSession_StoresLastBody
// ---------------------------------------------------------------------------

func TestShellSession_StoresLastBody(t *testing.T) {
	h := newShellSession()
	if h.LastBody() != nil {
		t.Error("expected nil last body on fresh session")
	}
	h.SetLastBody([]byte("<html>test</html>"))
	if string(h.LastBody()) != "<html>test</html>" {
		t.Errorf("expected last body to be stored, got %q", string(h.LastBody()))
	}
}
