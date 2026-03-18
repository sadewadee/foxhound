package main

import "sync"

// historyEntry records the result of a single shell fetch operation.
type historyEntry struct {
	URL      string
	Status   int
	Bytes    int
	DurationMS int64
}

// shellSession holds state for the interactive REPL: fetch history, last
// response body, and timing statistics.
type shellSession struct {
	mu       sync.Mutex
	history  []historyEntry
	lastBody []byte
}

// newShellSession creates an initialised shellSession.
func newShellSession() *shellSession {
	return &shellSession{}
}

// RecordFetch appends an entry to the session history (capped at 10).
func (s *shellSession) RecordFetch(url string, status, bytes int, durationMS int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.history = append(s.history, historyEntry{
		URL:        url,
		Status:     status,
		Bytes:      bytes,
		DurationMS: durationMS,
	})

	// Keep only the last 10 entries.
	if len(s.history) > 10 {
		s.history = s.history[len(s.history)-10:]
	}
}

// History returns a copy of the current history slice (oldest first).
func (s *shellSession) History() []historyEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]historyEntry, len(s.history))
	copy(out, s.history)
	return out
}

// TimingStats returns (avg, min, max) latency in milliseconds over all
// recorded fetches.  Returns (0, 0, 0) when history is empty.
func (s *shellSession) TimingStats() (avg, min, max int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.history) == 0 {
		return 0, 0, 0
	}

	var total int64
	min = s.history[0].DurationMS
	max = s.history[0].DurationMS

	for _, e := range s.history {
		total += e.DurationMS
		if e.DurationMS < min {
			min = e.DurationMS
		}
		if e.DurationMS > max {
			max = e.DurationMS
		}
	}
	avg = total / int64(len(s.history))
	return avg, min, max
}

// SetLastBody stores the raw bytes of the most recently fetched response.
func (s *shellSession) SetLastBody(body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastBody = body
}

// LastBody returns the raw bytes of the most recently fetched response, or nil.
func (s *shellSession) LastBody() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastBody
}
