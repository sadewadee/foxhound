package parse_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sadewadee/foxhound/parse"
)

const adaptiveOriginalHTML = `<!DOCTYPE html>
<html>
<body>
  <h1 class="product-title">Super Widget</h1>
  <span class="price-value">$49.99</span>
  <div class="rating">4.5 stars</div>
  <ul class="features">
    <li>Feature A</li>
    <li>Feature B</li>
    <li>Feature C</li>
  </ul>
</body>
</html>`

// adaptiveRedesignedHTML simulates a redesign: CSS selectors have changed but
// the same meaningful content is present.
const adaptiveRedesignedHTML = `<!DOCTYPE html>
<html>
<body>
  <h2 class="item-name">Super Widget</h2>
  <span class="current-price">$49.99</span>
  <div class="star-rating">4.5 stars</div>
  <ul class="product-features">
    <li>Feature A</li>
    <li>Feature B</li>
    <li>Feature C</li>
  </ul>
</body>
</html>`

func newAdaptiveDoc(t *testing.T, html string) *parse.Document {
	t.Helper()
	doc, err := parse.NewDocument(newHTMLResponse(html))
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// --- NewAdaptiveExtractor ---

func TestNewAdaptiveExtractor_NoSavePath_Empty(t *testing.T) {
	ext := parse.NewAdaptiveExtractor("")
	if ext == nil {
		t.Fatal("NewAdaptiveExtractor returned nil")
	}
}

func TestNewAdaptiveExtractor_WithSavePath_LoadsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "selectors.json")

	// Create an extractor, register selectors, save state
	ext1 := parse.NewAdaptiveExtractor(path)
	ext1.Register("title", "h1.product-title")
	doc := newAdaptiveDoc(t, adaptiveOriginalHTML)
	_ = ext1.Extract(doc, "title")
	if err := ext1.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Create a second extractor loading from the same path
	ext2 := parse.NewAdaptiveExtractor(path)
	if ext2 == nil {
		t.Fatal("NewAdaptiveExtractor with existing path returned nil")
	}
	// It should have loaded the signature; Extract on redesigned doc should use fallback
	redesignedDoc := newAdaptiveDoc(t, adaptiveRedesignedHTML)
	el := ext2.Extract(redesignedDoc, "title")
	if el == nil {
		t.Fatal("NewAdaptiveExtractor loaded state: Extract on redesigned doc returned nil")
	}
}

// --- Register ---

func TestAdaptiveExtractor_Register_ReturnsExtractorForChaining(t *testing.T) {
	ext := parse.NewAdaptiveExtractor("")
	returned := ext.Register("title", "h1.product-title")
	if returned != ext {
		t.Error("Register: should return the same extractor for chaining")
	}
}

// --- Extract with working selector ---

func TestAdaptiveExtractor_Extract_WorkingSelector(t *testing.T) {
	ext := parse.NewAdaptiveExtractor("")
	ext.Register("title", "h1.product-title")

	doc := newAdaptiveDoc(t, adaptiveOriginalHTML)
	el := ext.Extract(doc, "title")
	if el == nil {
		t.Fatal("Extract: expected element, got nil")
	}
	if el.Text() != "Super Widget" {
		t.Errorf("Extract: got text %q, want %q", el.Text(), "Super Widget")
	}
}

func TestAdaptiveExtractor_Extract_UnknownName_ReturnsNil(t *testing.T) {
	ext := parse.NewAdaptiveExtractor("")
	doc := newAdaptiveDoc(t, adaptiveOriginalHTML)
	el := ext.Extract(doc, "nonexistent-name")
	if el != nil {
		t.Error("Extract unknown name: expected nil")
	}
}

func TestAdaptiveExtractor_Extract_UpdatesSignatureOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "selectors.json")

	ext := parse.NewAdaptiveExtractor(path)
	ext.Register("title", "h1.product-title")

	doc := newAdaptiveDoc(t, adaptiveOriginalHTML)
	_ = ext.Extract(doc, "title")

	// Save and reload — signature must be persisted
	if err := ext.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	ext2 := parse.NewAdaptiveExtractor(path)
	// The loaded extractor should find "title" via similarity on redesigned page
	redesignedDoc := newAdaptiveDoc(t, adaptiveRedesignedHTML)
	el := ext2.Extract(redesignedDoc, "title")
	if el == nil {
		t.Error("Extract after load: expected fallback to find element via signature, got nil")
	}
}

// --- Extract with broken selector (similarity fallback) ---

func TestAdaptiveExtractor_Extract_FallsBackToSimilarityWhenSelectorBreaks(t *testing.T) {
	ext := parse.NewAdaptiveExtractor("")
	ext.Register("title", "h1.product-title")

	// First pass: working selector sets up signature
	originalDoc := newAdaptiveDoc(t, adaptiveOriginalHTML)
	el := ext.Extract(originalDoc, "title")
	if el == nil {
		t.Fatal("first Extract: expected element, got nil")
	}

	// Second pass: redesigned page, selector is gone
	redesignedDoc := newAdaptiveDoc(t, adaptiveRedesignedHTML)
	el = ext.Extract(redesignedDoc, "title")
	if el == nil {
		t.Fatal("Extract fallback: expected element via similarity, got nil")
	}
	if el.Text() != "Super Widget" {
		t.Errorf("Extract fallback: got text %q, want %q", el.Text(), "Super Widget")
	}
}

func TestAdaptiveExtractor_Extract_BrokenSelectorNoSignature_ReturnsNil(t *testing.T) {
	ext := parse.NewAdaptiveExtractor("")
	ext.Register("title", "h1.product-title")

	// Attempt extract on redesigned doc WITHOUT first running on original doc
	// No signature saved — no fallback possible
	redesignedDoc := newAdaptiveDoc(t, adaptiveRedesignedHTML)
	el := ext.Extract(redesignedDoc, "title")
	if el != nil {
		t.Error("Extract no signature no match: expected nil, got element")
	}
}

// --- ExtractAll ---

func TestAdaptiveExtractor_ExtractAll_WorkingSelector(t *testing.T) {
	ext := parse.NewAdaptiveExtractor("")
	ext.Register("features", "ul.features li")

	doc := newAdaptiveDoc(t, adaptiveOriginalHTML)
	els := ext.ExtractAll(doc, "features")
	if len(els) != 3 {
		t.Errorf("ExtractAll: got %d elements, want 3", len(els))
	}
}

func TestAdaptiveExtractor_ExtractAll_UnknownName_ReturnsNil(t *testing.T) {
	ext := parse.NewAdaptiveExtractor("")
	doc := newAdaptiveDoc(t, adaptiveOriginalHTML)
	els := ext.ExtractAll(doc, "nonexistent")
	if els != nil && len(els) != 0 {
		t.Error("ExtractAll unknown name: expected nil/empty")
	}
}

// --- ExtractText ---

func TestAdaptiveExtractor_ExtractText_WorkingSelector(t *testing.T) {
	ext := parse.NewAdaptiveExtractor("")
	ext.Register("price", "span.price-value")

	doc := newAdaptiveDoc(t, adaptiveOriginalHTML)
	text := ext.ExtractText(doc, "price")
	if text != "$49.99" {
		t.Errorf("ExtractText: got %q, want %q", text, "$49.99")
	}
}

func TestAdaptiveExtractor_ExtractText_NoMatch_ReturnsEmpty(t *testing.T) {
	ext := parse.NewAdaptiveExtractor("")
	ext.Register("missing", ".does-not-exist")

	doc := newAdaptiveDoc(t, adaptiveOriginalHTML)
	text := ext.ExtractText(doc, "missing")
	if text != "" {
		t.Errorf("ExtractText no match: got %q, want empty", text)
	}
}

// --- Save and Load ---

func TestAdaptiveExtractor_Save_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	ext := parse.NewAdaptiveExtractor(path)
	ext.Register("title", "h1.product-title")

	doc := newAdaptiveDoc(t, adaptiveOriginalHTML)
	_ = ext.Extract(doc, "title")

	if err := ext.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("Save: file was not created")
	}
}

func TestAdaptiveExtractor_Save_EmptySavePath_ReturnsNilError(t *testing.T) {
	ext := parse.NewAdaptiveExtractor("")
	ext.Register("title", "h1.product-title")
	// Save with no path configured should be a no-op (nil error)
	err := ext.Save()
	if err != nil {
		t.Errorf("Save with empty path: expected nil error, got %v", err)
	}
}

func TestAdaptiveExtractor_Load_ReadsSignatures(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Write state with first extractor
	ext1 := parse.NewAdaptiveExtractor(path)
	ext1.Register("title", "h1.product-title")
	doc := newAdaptiveDoc(t, adaptiveOriginalHTML)
	_ = ext1.Extract(doc, "title")
	if err := ext1.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Read with second extractor
	ext2 := parse.NewAdaptiveExtractor("")
	if err := ext2.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// After load, "title" selector should be available with signature
	redesignedDoc := newAdaptiveDoc(t, adaptiveRedesignedHTML)
	el := ext2.Extract(redesignedDoc, "title")
	if el == nil {
		t.Error("Load: expected Extract to find element via loaded signature, got nil")
	}
}

func TestAdaptiveExtractor_Load_NonExistentFile_ReturnsError(t *testing.T) {
	ext := parse.NewAdaptiveExtractor("")
	err := ext.Load("/tmp/foxhound-test-nonexistent-file.json")
	if err == nil {
		t.Error("Load non-existent file: expected error, got nil")
	}
}

// --- Concurrency safety (basic) ---

func TestAdaptiveExtractor_ConcurrentExtract_NoPanic(t *testing.T) {
	ext := parse.NewAdaptiveExtractor("")
	ext.Register("title", "h1.product-title")
	ext.Register("price", "span.price-value")

	doc := newAdaptiveDoc(t, adaptiveOriginalHTML)

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			_ = ext.Extract(doc, "title")
			_ = ext.ExtractText(doc, "price")
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
