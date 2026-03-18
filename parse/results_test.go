package parse_test

import (
	"strings"
	"testing"

	"github.com/sadewadee/foxhound/parse"
)

const resultsHTML = `<!DOCTYPE html>
<html>
<head><title>Results Test</title></head>
<body>
  <h1 class="title" id="main-title">Main Heading</h1>
  <h3 class="sub">Alpha</h3>
  <h3 class="sub">Beta</h3>
  <h3 class="sub">Gamma</h3>
  <a href="http://example.com/page1" class="link">Page 1</a>
  <a href="http://example.com/page2" class="link">Page 2</a>
  <a class="link nolink">No Href</a>
  <p class="price">$12.99</p>
  <p class="price">$7.50</p>
  <div id="box">
    <span class="inner">Inside</span>
  </div>
</body>
</html>`

func newResultsDoc(t *testing.T) *parse.Document {
	t.Helper()
	doc, err := parse.NewDocument(newHTMLResponse(resultsHTML))
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// --- Document.CSS plain selector ---

func TestCSS_PlainSelector(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS("h3.sub")
	if r.Len() != 3 {
		t.Errorf("CSS plain: want 3 elements, got %d", r.Len())
	}
}

func TestCSS_PlainSelector_NoMatch(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS(".nonexistent")
	if r.Len() != 0 {
		t.Errorf("CSS plain no-match: want 0 elements, got %d", r.Len())
	}
}

// --- ::text pseudo-selector ---

func TestCSS_TextPseudo(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS("h1::text")
	if r.Len() != 1 {
		t.Fatalf("CSS ::text: want 1 result, got %d", r.Len())
	}
	if r.Get() != "Main Heading" {
		t.Errorf("CSS ::text: got %q, want %q", r.Get(), "Main Heading")
	}
}

func TestCSS_TextPseudo_Multiple(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS("h3.sub::text")
	if r.Len() != 3 {
		t.Fatalf("CSS ::text multiple: want 3 results, got %d", r.Len())
	}
	all := r.GetAll()
	want := []string{"Alpha", "Beta", "Gamma"}
	for i, w := range want {
		if all[i] != w {
			t.Errorf("CSS ::text multiple[%d]: got %q, want %q", i, all[i], w)
		}
	}
}

func TestCSS_TextPseudo_WithSpaces(t *testing.T) {
	doc := newResultsDoc(t)
	// Space before :: should still be handled (trim is applied to base selector)
	r := doc.CSS("h1 ::text")
	// This queries descendants of h1 that match empty string; goquery Find("") finds nothing
	// The key is no panic and no unexpected crash.
	_ = r
}

// --- ::attr(name) pseudo-selector ---

func TestCSS_AttrPseudo(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS("a.link::attr(href)")
	// Only 2 of 3 links have href
	if r.Len() != 2 {
		t.Fatalf("CSS ::attr: want 2 results, got %d", r.Len())
	}
	all := r.GetAll()
	if all[0] != "http://example.com/page1" {
		t.Errorf("CSS ::attr[0]: got %q, want %q", all[0], "http://example.com/page1")
	}
	if all[1] != "http://example.com/page2" {
		t.Errorf("CSS ::attr[1]: got %q, want %q", all[1], "http://example.com/page2")
	}
}

func TestCSS_AttrPseudo_NoAttrOnSomeElements(t *testing.T) {
	doc := newResultsDoc(t)
	// class attribute exists on all three links
	r := doc.CSS("a.link::attr(class)")
	if r.Len() != 3 {
		t.Errorf("CSS ::attr class: want 3, got %d", r.Len())
	}
}

func TestCSS_AttrPseudo_NoMatch(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS(".nonexistent::attr(href)")
	if r.Len() != 0 {
		t.Errorf("CSS ::attr no-match: want 0, got %d", r.Len())
	}
}

// --- Results.Get ---

func TestResults_Get(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS("h3.sub::text")
	got := r.Get()
	if got != "Alpha" {
		t.Errorf("Results.Get: got %q, want %q", got, "Alpha")
	}
}

func TestResults_Get_Empty(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS(".nonexistent::text")
	got := r.Get()
	if got != "" {
		t.Errorf("Results.Get empty: got %q, want empty", got)
	}
}

// --- Results.GetAll ---

func TestResults_GetAll(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS("h3.sub::text")
	all := r.GetAll()
	if len(all) != 3 {
		t.Fatalf("Results.GetAll: want 3, got %d", len(all))
	}
}

func TestResults_GetAll_Empty(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS(".nonexistent::text")
	all := r.GetAll()
	if len(all) != 0 {
		t.Errorf("Results.GetAll empty: want 0, got %d", len(all))
	}
}

// --- Results.First ---

func TestResults_First(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS("h3.sub")
	el := r.First()
	if el == nil {
		t.Fatal("Results.First: returned nil for non-empty element result")
	}
	if el.Text() != "Alpha" {
		t.Errorf("Results.First: got %q, want %q", el.Text(), "Alpha")
	}
}

func TestResults_First_NilWhenTextPseudo(t *testing.T) {
	doc := newResultsDoc(t)
	// ::text results have no Element — First should return nil
	r := doc.CSS("h3.sub::text")
	el := r.First()
	if el != nil {
		t.Error("Results.First: want nil for ::text results, got non-nil")
	}
}

func TestResults_First_NilWhenEmpty(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS(".nonexistent")
	el := r.First()
	if el != nil {
		t.Errorf("Results.First empty: want nil, got %v", el)
	}
}

// --- Results.Last ---

func TestResults_Last(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS("h3.sub")
	el := r.Last()
	if el == nil {
		t.Fatal("Results.Last: returned nil")
	}
	if el.Text() != "Gamma" {
		t.Errorf("Results.Last: got %q, want %q", el.Text(), "Gamma")
	}
}

func TestResults_Last_NilWhenEmpty(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS(".nonexistent")
	el := r.Last()
	if el != nil {
		t.Errorf("Results.Last empty: want nil, got %v", el)
	}
}

// --- Results.Len ---

func TestResults_Len(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS("h3.sub")
	if r.Len() != 3 {
		t.Errorf("Results.Len: want 3, got %d", r.Len())
	}
}

func TestResults_Len_Zero(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS(".nonexistent")
	if r.Len() != 0 {
		t.Errorf("Results.Len zero: want 0, got %d", r.Len())
	}
}

// --- Results.Re ---

func TestResults_Re(t *testing.T) {
	doc := newResultsDoc(t)
	// Extract dollar amounts from price paragraphs via ::text then regex
	r := doc.CSS("p.price::text")
	matches := r.Re(`\d+\.\d+`)
	if len(matches) != 2 {
		t.Fatalf("Results.Re: want 2 matches, got %d: %v", len(matches), matches)
	}
	if matches[0] != "12.99" {
		t.Errorf("Results.Re[0]: got %q, want %q", matches[0], "12.99")
	}
	if matches[1] != "7.50" {
		t.Errorf("Results.Re[1]: got %q, want %q", matches[1], "7.50")
	}
}

func TestResults_Re_NoMatch(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS("h1::text")
	matches := r.Re(`\d+`)
	if len(matches) != 0 {
		t.Errorf("Results.Re no-match: want 0, got %d", len(matches))
	}
}

func TestResults_Re_InvalidPattern(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS("h1::text")
	matches := r.Re(`[invalid`)
	if matches != nil && len(matches) != 0 {
		t.Errorf("Results.Re invalid pattern: want nil/empty, got %v", matches)
	}
}

// --- Results.ReFirst ---

func TestResults_ReFirst(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS("p.price::text")
	first := r.ReFirst(`\d+\.\d+`)
	if first != "12.99" {
		t.Errorf("Results.ReFirst: got %q, want %q", first, "12.99")
	}
}

func TestResults_ReFirst_NoMatch(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS("h1::text")
	first := r.ReFirst(`\d+`)
	if first != "" {
		t.Errorf("Results.ReFirst no-match: want empty, got %q", first)
	}
}

// --- Results.Filter ---

func TestResults_Filter(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS("h3.sub")
	filtered := r.Filter(func(el *parse.Element) bool {
		return el.Text() != "Beta"
	})
	if filtered.Len() != 2 {
		t.Fatalf("Results.Filter: want 2, got %d", filtered.Len())
	}
	all := filtered.GetAll()
	for _, text := range all {
		if text == "Beta" {
			t.Error("Results.Filter: 'Beta' should have been filtered out")
		}
	}
}

func TestResults_Filter_AllRemoved(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS("h3.sub")
	filtered := r.Filter(func(el *parse.Element) bool {
		return false
	})
	if filtered.Len() != 0 {
		t.Errorf("Results.Filter all-removed: want 0, got %d", filtered.Len())
	}
}

func TestResults_Filter_OnTextResults(t *testing.T) {
	doc := newResultsDoc(t)
	// Filter on ::text results (no Element) — should return empty since filter needs elements
	r := doc.CSS("h3.sub::text")
	filtered := r.Filter(func(el *parse.Element) bool {
		return true
	})
	// Text-only results have no Element, so all are skipped
	if filtered.Len() != 0 {
		t.Errorf("Results.Filter on text results: want 0, got %d", filtered.Len())
	}
}

// --- Results.Each ---

func TestResults_Each(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS("h3.sub")
	var collected []string
	r.Each(func(i int, el *parse.Element) {
		collected = append(collected, el.Text())
	})
	if len(collected) != 3 {
		t.Fatalf("Results.Each: want 3 iterations, got %d", len(collected))
	}
	want := []string{"Alpha", "Beta", "Gamma"}
	for i, w := range want {
		if collected[i] != w {
			t.Errorf("Results.Each[%d]: got %q, want %q", i, collected[i], w)
		}
	}
}

func TestResults_Each_IndexCorrect(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS("h3.sub")
	var indices []int
	r.Each(func(i int, el *parse.Element) {
		indices = append(indices, i)
	})
	for i, idx := range indices {
		if idx != i {
			t.Errorf("Results.Each: index %d got %d", i, idx)
		}
	}
}

func TestResults_Each_NoCallWhenEmpty(t *testing.T) {
	doc := newResultsDoc(t)
	r := doc.CSS(".nonexistent")
	called := false
	r.Each(func(_ int, _ *parse.Element) {
		called = true
	})
	if called {
		t.Error("Results.Each: callback should not be called on empty results")
	}
}

// --- chaining sanity ---

func TestResults_ChainFilterRe(t *testing.T) {
	doc := newResultsDoc(t)
	// Get prices, filter to those containing "12", then regex-extract the number
	r := doc.CSS("p.price::text")
	all := r.GetAll()
	var onlyLarge []string
	for _, s := range all {
		if strings.Contains(s, "12") {
			onlyLarge = append(onlyLarge, s)
		}
	}
	if len(onlyLarge) != 1 {
		t.Errorf("chain filter: want 1 price containing '12', got %d", len(onlyLarge))
	}
}
