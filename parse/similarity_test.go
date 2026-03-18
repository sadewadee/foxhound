package parse_test

import (
	"testing"

	"github.com/sadewadee/foxhound/parse"
)

const similarityHTML = `<!DOCTYPE html>
<html>
<body>
  <div class="container">
    <h2 class="product-title featured" id="item-1">Blue Widget</h2>
    <span class="price current">$9.99</span>
    <p class="description">A great blue widget for all occasions.</p>
  </div>
  <div class="container">
    <h2 class="product-title" id="item-2">Red Gadget</h2>
    <span class="price sale">$4.99</span>
    <p class="description">A red gadget on sale.</p>
  </div>
  <footer>
    <a href="/about">About</a>
  </footer>
</body>
</html>`

// redesignedHTML simulates a site redesign where "product-title" became
// "item-name" and h2 became h3, but same text and similar position.
const redesignedHTML = `<!DOCTYPE html>
<html>
<body>
  <section class="wrapper">
    <h3 class="item-name featured" id="item-1">Blue Widget</h3>
    <span class="cost current">$9.99</span>
    <p class="summary">A great blue widget for all occasions.</p>
  </section>
  <section class="wrapper">
    <h3 class="item-name" id="item-2">Red Gadget</h3>
    <span class="cost sale">$4.99</span>
    <p class="summary">A red gadget on sale.</p>
  </section>
  <footer>
    <a href="/about">About</a>
  </footer>
</body>
</html>`

func newSimilarityDoc(t *testing.T, html string) *parse.Document {
	t.Helper()
	doc, err := parse.NewDocument(newHTMLResponse(html))
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// --- CaptureSignature ---

func TestCaptureSignature_CapturesTag(t *testing.T) {
	doc := newSimilarityDoc(t, similarityHTML)
	el := doc.First("h2.product-title")
	if el == nil {
		t.Fatal("element not found")
	}
	sig := parse.CaptureSignature(el)
	if sig.Tag != "h2" {
		t.Errorf("CaptureSignature Tag: got %q, want %q", sig.Tag, "h2")
	}
}

func TestCaptureSignature_CapturesID(t *testing.T) {
	doc := newSimilarityDoc(t, similarityHTML)
	el := doc.First("#item-1")
	if el == nil {
		t.Fatal("element not found")
	}
	sig := parse.CaptureSignature(el)
	if sig.ID != "item-1" {
		t.Errorf("CaptureSignature ID: got %q, want %q", sig.ID, "item-1")
	}
}

func TestCaptureSignature_CapturesClasses(t *testing.T) {
	doc := newSimilarityDoc(t, similarityHTML)
	el := doc.First("h2.product-title")
	if el == nil {
		t.Fatal("element not found")
	}
	sig := parse.CaptureSignature(el)
	if len(sig.Classes) == 0 {
		t.Fatal("CaptureSignature Classes: expected non-empty classes")
	}
	hasProductTitle := false
	for _, c := range sig.Classes {
		if c == "product-title" {
			hasProductTitle = true
		}
	}
	if !hasProductTitle {
		t.Errorf("CaptureSignature Classes: expected 'product-title' in %v", sig.Classes)
	}
}

func TestCaptureSignature_CapturesText(t *testing.T) {
	doc := newSimilarityDoc(t, similarityHTML)
	el := doc.First("h2.product-title")
	if el == nil {
		t.Fatal("element not found")
	}
	sig := parse.CaptureSignature(el)
	if sig.Text != "Blue Widget" {
		t.Errorf("CaptureSignature Text: got %q, want %q", sig.Text, "Blue Widget")
	}
}

func TestCaptureSignature_CapturesDepth(t *testing.T) {
	doc := newSimilarityDoc(t, similarityHTML)
	el := doc.First("h2.product-title")
	if el == nil {
		t.Fatal("element not found")
	}
	sig := parse.CaptureSignature(el)
	// html > body > div > h2 = depth 3 (or similar; just must be > 0)
	if sig.Depth <= 0 {
		t.Errorf("CaptureSignature Depth: got %d, expected > 0", sig.Depth)
	}
}

func TestCaptureSignature_CapturesPosition(t *testing.T) {
	doc := newSimilarityDoc(t, similarityHTML)
	// The first h2 should have position 1 (first of its type)
	el := doc.First("h2#item-1")
	if el == nil {
		t.Fatal("element not found")
	}
	sig := parse.CaptureSignature(el)
	if sig.Position < 1 {
		t.Errorf("CaptureSignature Position: got %d, expected >= 1", sig.Position)
	}
}

// --- Similarity score ---

func TestSimilarity_SameElement_ScoreIsOne(t *testing.T) {
	doc := newSimilarityDoc(t, similarityHTML)
	el := doc.First("h2.product-title")
	if el == nil {
		t.Fatal("element not found")
	}
	sig := parse.CaptureSignature(el)
	score := parse.Similarity(sig, sig)
	if score != 1.0 {
		t.Errorf("Similarity same element: got %.4f, want 1.0", score)
	}
}

func TestSimilarity_SimilarElement_ScoreAboveThreshold(t *testing.T) {
	// Compare h2.product-title.featured with h2.product-title (no featured)
	// Same tag, mostly same classes, same text would give high score.
	sigA := &parse.ElementSignature{
		Tag:       "h2",
		ID:        "item-1",
		Classes:   []string{"product-title", "featured"},
		Text:      "Blue Widget",
		ParentTag: "div",
		Position:  1,
		Depth:     3,
	}
	sigB := &parse.ElementSignature{
		Tag:       "h3",
		Classes:   []string{"item-name", "featured"},
		Text:      "Blue Widget",
		ParentTag: "section",
		Position:  1,
		Depth:     3,
	}
	score := parse.Similarity(sigA, sigB)
	// Same text (0.15) + class overlap "featured" (partial, 0.15*(1/3)) + same position (0.10) + same depth (0.10) = should be > 0.3
	if score <= 0.3 {
		t.Errorf("Similarity similar elements: got %.4f, want > 0.3", score)
	}
}

func TestSimilarity_DifferentElement_ScoreBelowThreshold(t *testing.T) {
	sigA := &parse.ElementSignature{
		Tag:       "h2",
		Classes:   []string{"product-title"},
		Text:      "Blue Widget",
		ParentTag: "div",
		Position:  1,
		Depth:     3,
	}
	sigB := &parse.ElementSignature{
		Tag:       "a",
		Classes:   []string{"nav-link"},
		Text:      "About",
		ParentTag: "footer",
		Position:  5,
		Depth:     1,
	}
	score := parse.Similarity(sigA, sigB)
	if score >= 0.3 {
		t.Errorf("Similarity very different elements: got %.4f, want < 0.3", score)
	}
}

func TestSimilarity_SameID_BoostsScore(t *testing.T) {
	sigA := &parse.ElementSignature{
		Tag: "div",
		ID:  "main-content",
	}
	sigB := &parse.ElementSignature{
		Tag: "div",
		ID:  "main-content",
	}
	score := parse.Similarity(sigA, sigB)
	// tag (0.15) + id (0.25) = 0.40 minimum
	if score < 0.40 {
		t.Errorf("Similarity same ID: got %.4f, want >= 0.40", score)
	}
}

func TestSimilarity_ScoreClampedBetweenZeroAndOne(t *testing.T) {
	sigA := &parse.ElementSignature{
		Tag:       "h2",
		ID:        "item-1",
		Classes:   []string{"product-title", "featured"},
		Text:      "Blue Widget",
		ParentTag: "div",
		Position:  1,
		Depth:     3,
	}
	// identical to sigA — should clamp to 1.0, not exceed it
	score := parse.Similarity(sigA, sigA)
	if score < 0.0 || score > 1.0 {
		t.Errorf("Similarity score out of range: %.4f", score)
	}
}

// --- FindSimilar ---

func TestDocument_FindSimilar_FindsElement(t *testing.T) {
	originalDoc := newSimilarityDoc(t, similarityHTML)
	el := originalDoc.First("h2#item-1")
	if el == nil {
		t.Fatal("source element not found")
	}
	sig := parse.CaptureSignature(el)

	// Search in redesigned doc where selector no longer matches
	redesignedDoc := newSimilarityDoc(t, redesignedHTML)
	matches := redesignedDoc.FindSimilar(sig, 0.3)
	if len(matches) == 0 {
		t.Fatal("FindSimilar: expected at least one match, got none")
	}
}

func TestDocument_FindSimilar_SortedByScoreDescending(t *testing.T) {
	originalDoc := newSimilarityDoc(t, similarityHTML)
	el := originalDoc.First("h2#item-1")
	if el == nil {
		t.Fatal("source element not found")
	}
	sig := parse.CaptureSignature(el)

	redesignedDoc := newSimilarityDoc(t, redesignedHTML)
	matches := redesignedDoc.FindSimilar(sig, 0.0)
	for i := 1; i < len(matches); i++ {
		if matches[i].Score > matches[i-1].Score {
			t.Errorf("FindSimilar not sorted: matches[%d].Score=%.4f > matches[%d].Score=%.4f",
				i, matches[i].Score, i-1, matches[i-1].Score)
		}
	}
}

func TestDocument_FindSimilar_MinScoreFiltersResults(t *testing.T) {
	originalDoc := newSimilarityDoc(t, similarityHTML)
	el := originalDoc.First("h2#item-1")
	if el == nil {
		t.Fatal("source element not found")
	}
	sig := parse.CaptureSignature(el)

	redesignedDoc := newSimilarityDoc(t, redesignedHTML)
	// With minScore=0.99, very few (or zero) elements should match
	matches := redesignedDoc.FindSimilar(sig, 0.99)
	for _, m := range matches {
		if m.Score < 0.99 {
			t.Errorf("FindSimilar: returned match with score %.4f below minScore 0.99", m.Score)
		}
	}
}

func TestDocument_FindSimilar_ExactElementInSameDoc_ScoreIsOne(t *testing.T) {
	doc := newSimilarityDoc(t, similarityHTML)
	el := doc.First("span.price.current")
	if el == nil {
		t.Fatal("element not found")
	}
	sig := parse.CaptureSignature(el)
	matches := doc.FindSimilar(sig, 0.99)
	found := false
	for _, m := range matches {
		if m.Score == 1.0 {
			found = true
		}
	}
	if !found {
		t.Error("FindSimilar same doc: expected score 1.0 match for exact element")
	}
}
