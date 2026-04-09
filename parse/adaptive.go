package parse

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// AdaptiveSelector tracks an element across page changes.  When the primary
// CSS selector fails, it falls back to similarity matching using a saved
// signature from the last successful extraction.
type AdaptiveSelector struct {
	Name      string            `json:"name"`
	Selector  string            `json:"selector"`
	Signature *ElementSignature `json:"signature,omitempty"`
	MinScore  float64           `json:"min_score"`
}

// AdaptiveExtractor manages multiple adaptive selectors with optional
// persistence.  It is safe for concurrent use.
type AdaptiveExtractor struct {
	selectors map[string]*AdaptiveSelector
	savePath  string
	// sqliteStore is an optional SQLite-backed signature store. When set,
	// Save persists each registered selector signature to the store using
	// an empty domain key (the JSON file remains the primary persistence
	// when both are configured). Set via WithSQLiteStorage.
	sqliteStore *SQLiteAdaptiveStore
	mu          sync.RWMutex
}

// NewAdaptiveExtractor creates an extractor.  When savePath is non-empty the
// constructor attempts to load previously saved signatures from that file;
// any load error is silently ignored (the file may not exist yet on the first
// run).
func NewAdaptiveExtractor(savePath string) *AdaptiveExtractor {
	ae := &AdaptiveExtractor{
		selectors: make(map[string]*AdaptiveSelector),
		savePath:  savePath,
	}
	if savePath != "" {
		// Best-effort load; ignore error (file may not exist yet).
		_ = ae.Load(savePath)
	}
	return ae
}

// Register adds an adaptive selector by name with its primary CSS selector.
// Returns the extractor for method chaining.
func (ae *AdaptiveExtractor) Register(name, selector string) *AdaptiveExtractor {
	ae.mu.Lock()
	defer ae.mu.Unlock()

	if existing, ok := ae.selectors[name]; ok {
		// Preserve any signature that was loaded from disk.
		existing.Selector = selector
		return ae
	}
	ae.selectors[name] = &AdaptiveSelector{
		Name:     name,
		Selector: selector,
		MinScore: 0.4,
	}
	return ae
}

// Extract tries the CSS selector first.  If no element is found and a saved
// signature exists, it falls back to similarity matching (using MinScore as
// the threshold, picking the best match).  On a successful extraction the
// element signature is updated for future runs.
//
// Returns nil when neither strategy finds an element.
func (ae *AdaptiveExtractor) Extract(doc *Document, name string) *Element {
	ae.mu.RLock()
	sel, ok := ae.selectors[name]
	if !ok {
		ae.mu.RUnlock()
		return nil
	}
	// Copy fields under read lock so we can release before writing.
	selector := sel.Selector
	sig := sel.Signature
	minScore := sel.MinScore
	ae.mu.RUnlock()

	// Primary path: CSS selector.
	el := doc.First(selector)
	if el != nil {
		ae.updateSignature(name, el)
		return el
	}

	// Fallback path: similarity matching using saved signature.
	if sig == nil {
		return nil
	}
	matches := doc.FindSimilar(sig, minScore)
	if len(matches) == 0 {
		return nil
	}
	best := matches[0].Element
	ae.updateSignature(name, best)
	return best
}

// ExtractAll extracts all elements matching the selector.  When the selector
// matches nothing and a signature is available, it returns the single best
// similarity match wrapped in a slice.
func (ae *AdaptiveExtractor) ExtractAll(doc *Document, name string) []*Element {
	ae.mu.RLock()
	sel, ok := ae.selectors[name]
	if !ok {
		ae.mu.RUnlock()
		return nil
	}
	selector := sel.Selector
	sig := sel.Signature
	minScore := sel.MinScore
	ae.mu.RUnlock()

	// Primary path: CSS selector returns all matches.
	els := doc.FindAll(selector)
	if len(els) > 0 {
		if len(els) > 0 {
			ae.updateSignature(name, els[0])
		}
		return els
	}

	// Fallback: similarity match on saved signature.
	if sig == nil {
		return nil
	}
	matches := doc.FindSimilar(sig, minScore)
	if len(matches) == 0 {
		return nil
	}
	best := matches[0].Element
	ae.updateSignature(name, best)
	return []*Element{best}
}

// ExtractText is a convenience wrapper that returns the text of the first
// matched element, or an empty string when nothing is found.
func (ae *AdaptiveExtractor) ExtractText(doc *Document, name string) string {
	el := ae.Extract(doc, name)
	if el == nil {
		return ""
	}
	return el.Text()
}

// Save persists all selector signatures to the configured savePath as JSON.
// When savePath is empty, Save is a no-op and returns nil.
func (ae *AdaptiveExtractor) Save() error {
	ae.mu.RLock()
	defer ae.mu.RUnlock()

	// Persist to SQLite store if configured. Uses empty domain key since
	// the high-level extractor is process-scoped, not domain-scoped.
	if ae.sqliteStore != nil {
		for name, sel := range ae.selectors {
			if sel.Signature == nil {
				continue
			}
			if err := ae.sqliteStore.Save("", name, sel.Signature); err != nil {
				return err
			}
		}
	}
	if ae.savePath == "" {
		return nil
	}
	return ae.writeToPath(ae.savePath)
}

// Load reads selector signatures from the given file path and merges them
// into the extractor.  Selectors not already registered are added; existing
// registrations have their signatures updated without overwriting their
// current CSS selector.
func (ae *AdaptiveExtractor) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("parse: adaptive: load %q: %w", path, err)
	}

	var loaded map[string]*AdaptiveSelector
	if err := json.Unmarshal(data, &loaded); err != nil {
		return fmt.Errorf("parse: adaptive: decode %q: %w", path, err)
	}

	ae.mu.Lock()
	defer ae.mu.Unlock()

	for name, s := range loaded {
		if existing, ok := ae.selectors[name]; ok {
			// Merge: keep current selector, update signature only.
			existing.Signature = s.Signature
		} else {
			ae.selectors[name] = s
		}
	}
	return nil
}

// --- internal helpers ---

// updateSignature captures and stores the element signature under a write
// lock.
func (ae *AdaptiveExtractor) updateSignature(name string, el *Element) {
	sig := CaptureSignature(el)

	ae.mu.Lock()
	defer ae.mu.Unlock()

	if s, ok := ae.selectors[name]; ok {
		s.Signature = sig
	}
}

// writeToPath serialises the selectors map to a JSON file.  Must be called
// with ae.mu held in at least read mode.
func (ae *AdaptiveExtractor) writeToPath(path string) error {
	data, err := json.MarshalIndent(ae.selectors, "", "  ")
	if err != nil {
		return fmt.Errorf("parse: adaptive: encode: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("parse: adaptive: write %q: %w", path, err)
	}
	return nil
}
