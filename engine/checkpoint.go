package engine

import (
	"encoding/json"
	"os"
	"time"
)

// CheckpointConfig controls automatic checkpoint saving.
type CheckpointConfig struct {
	// Enabled turns auto-checkpointing on. When false, no file is written.
	Enabled bool
	// Path is the file path where the checkpoint JSON is written.
	Path string
	// Interval is how many items must be processed between saves.
	// A value of 0 is treated as 100 (the default) to avoid division by zero.
	Interval int
}

// effectiveInterval returns the interval to use, substituting the default
// of 100 when Interval is 0 so callers never divide by zero.
func (c CheckpointConfig) effectiveInterval() int64 {
	if c.Interval <= 0 {
		return 100
	}
	return int64(c.Interval)
}

// Checkpoint captures the observable state of a Hunt at a point in time.
// All fields are exported so they round-trip cleanly through JSON.
type Checkpoint struct {
	HuntID         string    `json:"hunt_id"`
	HuntName       string    `json:"hunt_name"`
	Domain         string    `json:"domain"`
	ItemsProcessed int64     `json:"items_processed"`
	RequestsDone   int64     `json:"requests_done"`
	ErrorCount     int64     `json:"errors"`
	LastURL        string    `json:"last_url"`
	Timestamp      time.Time `json:"timestamp"`
	QueueLen       int       `json:"queue_len"`
	ElapsedMs      int64     `json:"elapsed_ms"`
}

// SaveCheckpoint writes the current hunt state to a JSON file at path.
// The file is written atomically (temp file + rename) so a partial write
// cannot corrupt a previously valid checkpoint.
func (h *Hunt) SaveCheckpoint(path string) error {
	h.mu.RLock()
	cfg := h.config
	h.mu.RUnlock()

	cp := Checkpoint{
		HuntName:       cfg.Name,
		Domain:         cfg.Domain,
		ItemsProcessed: h.stats.ItemCount.Load(),
		RequestsDone:   h.stats.RequestCount.Load(),
		ErrorCount:     h.stats.ErrorCount.Load(),
		Timestamp:      time.Now().UTC(),
		QueueLen:       cfg.Queue.Len(),
		ElapsedMs:      time.Since(h.stats.StartedAt).Milliseconds(),
	}

	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}

	// Write to a temp file alongside the target, then rename for atomicity.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadCheckpoint reads a Checkpoint from the JSON file at path.
func LoadCheckpoint(path string) (*Checkpoint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

// maybeCheckpoint saves a checkpoint when auto-checkpointing is enabled and
// the item count has just crossed a multiple of the configured interval.
// Errors are logged as warnings but do not interrupt processing.
func (h *Hunt) maybeCheckpoint() {
	cfg := h.config.Checkpoint
	if !cfg.Enabled {
		return
	}
	interval := cfg.effectiveInterval()
	count := h.stats.ItemCount.Load()
	if count > 0 && count%interval == 0 {
		if err := h.SaveCheckpoint(cfg.Path); err != nil {
			h.logger.Warn("auto-checkpoint failed", "err", err)
		}
	}
}
