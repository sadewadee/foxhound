package parse_test

import (
	"strings"
	"testing"

	"github.com/sadewadee/foxhound/parse"
)

const elemExtHTML = `<!DOCTYPE html>
<html>
<head><title>Element Ext Test</title></head>
<body>
  <div id="outer" class="wrapper">
    <section class="content">
      <article id="post-1">
        <h2 class="post-title">Go Programming</h2>
        <p class="body-text">Learn  Go  today.  Price: $29.99</p>
        <ul class="tags">
          <li>golang</li>
          <li>programming</li>
        </ul>
        <script type="application/json">{"key":"value","count":42}</script>
      </article>
      <article id="post-2">
        <h2 class="post-title">Rust Programming</h2>
        <p class="body-text">Learn Rust. Price: $19.99</p>
      </article>
    </section>
  </div>
</body>
</html>`

func newElemExtDoc(t *testing.T) *parse.Document {
	t.Helper()
	doc, err := parse.NewDocument(newHTMLResponse(elemExtHTML))
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// --- Element.Re ---

func TestElement_Re(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("p.body-text")
	if el == nil {
		t.Fatal("no p.body-text element found")
	}
	matches := el.Re(`\$[\d.]+`)
	if len(matches) == 0 {
		t.Fatal("Element.Re: expected matches, got none")
	}
	if matches[0] != "$29.99" {
		t.Errorf("Element.Re: got %q, want %q", matches[0], "$29.99")
	}
}

func TestElement_Re_NoMatch(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("h2.post-title")
	if el == nil {
		t.Fatal("no element found")
	}
	matches := el.Re(`\d{4}`)
	if len(matches) != 0 {
		t.Errorf("Element.Re no-match: want 0, got %d", len(matches))
	}
}

func TestElement_Re_InvalidPattern(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("h2.post-title")
	if el == nil {
		t.Fatal("no element found")
	}
	matches := el.Re(`[invalid`)
	if matches != nil && len(matches) != 0 {
		t.Errorf("Element.Re invalid pattern: want nil/empty, got %v", matches)
	}
}

// --- Element.ReFirst ---

func TestElement_ReFirst(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("p.body-text")
	if el == nil {
		t.Fatal("no element found")
	}
	match := el.ReFirst(`\$[\d.]+`)
	if match != "$29.99" {
		t.Errorf("Element.ReFirst: got %q, want %q", match, "$29.99")
	}
}

func TestElement_ReFirst_NoMatch(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("h2.post-title")
	if el == nil {
		t.Fatal("no element found")
	}
	match := el.ReFirst(`\d{10}`)
	if match != "" {
		t.Errorf("Element.ReFirst no-match: want empty, got %q", match)
	}
}

// --- Element.FindAncestor ---

func TestElement_FindAncestor(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("h2.post-title")
	if el == nil {
		t.Fatal("no h2 found")
	}
	// Should find the article ancestor
	ancestor := el.FindAncestor(func(e *parse.Element) bool {
		return e.Tag() == "article"
	})
	if ancestor == nil {
		t.Fatal("Element.FindAncestor: expected to find article, got nil")
	}
	if ancestor.Tag() != "article" {
		t.Errorf("Element.FindAncestor: got tag %q, want %q", ancestor.Tag(), "article")
	}
}

func TestElement_FindAncestor_NoMatch(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("h2.post-title")
	if el == nil {
		t.Fatal("no h2 found")
	}
	ancestor := el.FindAncestor(func(e *parse.Element) bool {
		return e.Tag() == "table"
	})
	if ancestor != nil {
		t.Errorf("Element.FindAncestor no-match: want nil, got %v", ancestor)
	}
}

func TestElement_FindAncestor_Closest(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("h2.post-title")
	if el == nil {
		t.Fatal("no h2 found")
	}
	// div comes after article in the ancestor chain — closest is article
	ancestor := el.FindAncestor(func(e *parse.Element) bool {
		return e.Tag() == "div" || e.Tag() == "article"
	})
	if ancestor == nil {
		t.Fatal("Element.FindAncestor closest: got nil")
	}
	// article is the immediate parent, so it should be found first
	if ancestor.Tag() != "article" {
		t.Errorf("Element.FindAncestor closest: got %q, want %q", ancestor.Tag(), "article")
	}
}

// --- Element.Ancestors ---

func TestElement_Ancestors(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("h2.post-title")
	if el == nil {
		t.Fatal("no h2 found")
	}
	ancestors := el.Ancestors()
	if len(ancestors) == 0 {
		t.Fatal("Element.Ancestors: expected ancestors, got none")
	}
	// First ancestor should be the direct parent (article)
	if ancestors[0].Tag() != "article" {
		t.Errorf("Element.Ancestors[0]: got %q, want %q", ancestors[0].Tag(), "article")
	}
}

func TestElement_Ancestors_ContainsHTML(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("h2.post-title")
	if el == nil {
		t.Fatal("no h2 found")
	}
	ancestors := el.Ancestors()
	tags := make(map[string]bool)
	for _, a := range ancestors {
		tags[a.Tag()] = true
	}
	if !tags["div"] {
		t.Error("Element.Ancestors: expected 'div' in ancestors")
	}
	if !tags["section"] {
		t.Error("Element.Ancestors: expected 'section' in ancestors")
	}
}

// --- Element.BelowElements ---

func TestElement_BelowElements(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("article#post-1")
	if el == nil {
		t.Fatal("no article#post-1 found")
	}
	below := el.BelowElements()
	if len(below) == 0 {
		t.Fatal("Element.BelowElements: expected descendants, got none")
	}
	tags := make(map[string]bool)
	for _, b := range below {
		tags[b.Tag()] = true
	}
	if !tags["h2"] {
		t.Error("Element.BelowElements: expected 'h2' in descendants")
	}
	if !tags["li"] {
		t.Error("Element.BelowElements: expected 'li' in descendants")
	}
}

func TestElement_BelowElements_Leaf(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("h2.post-title")
	if el == nil {
		t.Fatal("no h2 found")
	}
	below := el.BelowElements()
	// h2 has no element children, only a text node
	if len(below) != 0 {
		t.Errorf("Element.BelowElements leaf: want 0 element children, got %d", len(below))
	}
}

// --- Element.JSON ---

func TestElement_JSON(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("script[type='application/json']")
	if el == nil {
		t.Fatal("no script[type=application/json] found")
	}
	data, err := el.JSON()
	if err != nil {
		t.Fatalf("Element.JSON: unexpected error: %v", err)
	}
	if data["key"] != "value" {
		t.Errorf("Element.JSON: key got %v, want %q", data["key"], "value")
	}
	// JSON numbers become float64
	if data["count"] != float64(42) {
		t.Errorf("Element.JSON: count got %v, want 42", data["count"])
	}
}

func TestElement_JSON_InvalidJSON(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("h2.post-title")
	if el == nil {
		t.Fatal("no element found")
	}
	_, err := el.JSON()
	if err == nil {
		t.Error("Element.JSON invalid: expected error for non-JSON content, got nil")
	}
}

// --- Element.Clean ---

func TestElement_Clean(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("p.body-text")
	if el == nil {
		t.Fatal("no element found")
	}
	cleaned := el.Clean()
	// Should not have multiple consecutive spaces
	if strings.Contains(cleaned, "  ") {
		t.Errorf("Element.Clean: still has multiple spaces: %q", cleaned)
	}
	// Should be trimmed
	if cleaned != strings.TrimSpace(cleaned) {
		t.Errorf("Element.Clean: not trimmed: %q", cleaned)
	}
}

func TestElement_Clean_NoChange(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("h2.post-title")
	if el == nil {
		t.Fatal("no element found")
	}
	cleaned := el.Clean()
	// "Go Programming" has no extra spaces
	if cleaned != "Go Programming" {
		t.Errorf("Element.Clean: got %q, want %q", cleaned, "Go Programming")
	}
}

// --- Element.GetAllText ---

func TestElement_GetAllText(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("ul.tags")
	if el == nil {
		t.Fatal("no ul.tags found")
	}
	text := el.GetAllText()
	if !strings.Contains(text, "golang") {
		t.Errorf("Element.GetAllText: expected 'golang' in %q", text)
	}
	if !strings.Contains(text, "programming") {
		t.Errorf("Element.GetAllText: expected 'programming' in %q", text)
	}
}

// --- Element.Prettify ---

func TestElement_Prettify(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("h2.post-title")
	if el == nil {
		t.Fatal("no h2 found")
	}
	pretty := el.Prettify()
	if pretty == "" {
		t.Error("Element.Prettify: got empty string")
	}
	// Should contain the tag and content somewhere
	if !strings.Contains(pretty, "Go Programming") {
		t.Errorf("Element.Prettify: expected content 'Go Programming' in %q", pretty)
	}
}

// --- Element.Path ---

func TestElement_Path(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("h2.post-title")
	if el == nil {
		t.Fatal("no h2 found")
	}
	path := el.Path()
	if len(path) == 0 {
		t.Fatal("Element.Path: expected non-empty path")
	}
	// Last element in path should be h2
	last := path[len(path)-1]
	if last != "h2" {
		t.Errorf("Element.Path last: got %q, want %q", last, "h2")
	}
	// First element should be html
	if path[0] != "html" {
		t.Errorf("Element.Path first: got %q, want %q", path[0], "html")
	}
}

func TestElement_Path_ContainsAncestors(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("h2.post-title")
	if el == nil {
		t.Fatal("no h2 found")
	}
	path := el.Path()
	pathStr := strings.Join(path, "/")
	if !strings.Contains(pathStr, "body") {
		t.Errorf("Element.Path: expected 'body' in path %q", pathStr)
	}
	if !strings.Contains(pathStr, "article") {
		t.Errorf("Element.Path: expected 'article' in path %q", pathStr)
	}
}

// --- Element.InnerHTML ---

func TestElement_InnerHTML(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("ul.tags")
	if el == nil {
		t.Fatal("no ul.tags found")
	}
	inner := el.InnerHTML()
	if inner == "" {
		t.Fatal("Element.InnerHTML: got empty string")
	}
	if !strings.Contains(inner, "<li>") {
		t.Errorf("Element.InnerHTML: expected '<li>' in %q", inner)
	}
}

func TestElement_InnerHTML_NoChildren(t *testing.T) {
	doc := newElemExtDoc(t)
	// A leaf element like h2 has text but no child elements
	el := doc.First("h2.post-title")
	if el == nil {
		t.Fatal("no h2 found")
	}
	inner := el.InnerHTML()
	// Inner HTML of h2 is its text content
	if inner == "" {
		t.Error("Element.InnerHTML: got empty for h2 with text")
	}
}

// --- Element.OuterHTML ---

func TestElement_OuterHTML(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("h2.post-title")
	if el == nil {
		t.Fatal("no h2 found")
	}
	outer := el.OuterHTML()
	if outer == "" {
		t.Fatal("Element.OuterHTML: got empty string")
	}
	if !strings.Contains(outer, "<h2") {
		t.Errorf("Element.OuterHTML: expected '<h2' in %q", outer)
	}
	if !strings.Contains(outer, "Go Programming") {
		t.Errorf("Element.OuterHTML: expected content in %q", outer)
	}
}

// --- Element.GenerateCSSSelector ---

func TestElement_GenerateCSSSelector_WithID(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("#post-1")
	if el == nil {
		t.Fatal("no #post-1 found")
	}
	sel := el.GenerateCSSSelector()
	if sel == "" {
		t.Fatal("Element.GenerateCSSSelector: got empty string")
	}
	// Should start with # because element has an ID
	if !strings.HasPrefix(sel, "#") {
		t.Errorf("Element.GenerateCSSSelector with ID: got %q, expected to start with '#'", sel)
	}
	if sel != "#post-1" {
		t.Errorf("Element.GenerateCSSSelector with ID: got %q, want %q", sel, "#post-1")
	}
}

func TestElement_GenerateCSSSelector_WithoutID(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("h2.post-title")
	if el == nil {
		t.Fatal("no h2 found")
	}
	sel := el.GenerateCSSSelector()
	if sel == "" {
		t.Fatal("Element.GenerateCSSSelector no-id: got empty string")
	}
	// Should contain h2 somewhere
	if !strings.Contains(sel, "h2") {
		t.Errorf("Element.GenerateCSSSelector no-id: expected 'h2' in %q", sel)
	}
}

func TestElement_GenerateCSSSelector_UniqueSelection(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("#post-1")
	if el == nil {
		t.Fatal("no #post-1 found")
	}
	sel := el.GenerateCSSSelector()
	// The selector should be usable — find the element with it
	found := doc.First(sel)
	if found == nil {
		t.Fatalf("Element.GenerateCSSSelector: selector %q finds nothing", sel)
	}
}

// --- Element.GenerateXPathSelector ---

func TestElement_GenerateXPathSelector(t *testing.T) {
	doc := newElemExtDoc(t)
	el := doc.First("h2.post-title")
	if el == nil {
		t.Fatal("no h2 found")
	}
	xpath := el.GenerateXPathSelector()
	if xpath == "" {
		t.Fatal("Element.GenerateXPathSelector: got empty string")
	}
	// Should start with /
	if !strings.HasPrefix(xpath, "/") {
		t.Errorf("Element.GenerateXPathSelector: expected XPath starting with '/', got %q", xpath)
	}
	// Should contain h2
	if !strings.Contains(xpath, "h2") {
		t.Errorf("Element.GenerateXPathSelector: expected 'h2' in %q", xpath)
	}
}

func TestElement_GenerateXPathSelector_ContainsPositions(t *testing.T) {
	doc := newElemExtDoc(t)
	// An element without an ID should have position indices in XPath
	el := doc.First("p.body-text")
	if el == nil {
		t.Fatal("no p found")
	}
	xpath := el.GenerateXPathSelector()
	if xpath == "" {
		t.Fatal("Element.GenerateXPathSelector positions: got empty")
	}
	// XPath should contain bracket notation for positioning
	if !strings.Contains(xpath, "[") {
		t.Errorf("Element.GenerateXPathSelector: expected '[n]' notation in %q", xpath)
	}
}
