package parse_test

import (
	"testing"

	"github.com/sadewadee/foxhound/parse"
)

const finderHTML = `<!DOCTYPE html>
<html>
<body>
  <h1 class="title main" id="page-title">Hello World</h1>
  <p class="desc" data-type="intro">Welcome to the site.</p>
  <p class="desc highlight" data-type="body">This is the body text.</p>
  <ul>
    <li class="item" data-index="0">First</li>
    <li class="item active" data-index="1">Second</li>
    <li class="item" data-index="2">Third item here</li>
  </ul>
  <a href="http://example.com/page1" class="link">Page 1</a>
  <a href="http://example.com/page2" class="link external">Page 2</a>
  <div id="container">
    <span class="child-a">Child A</span>
    <span class="child-b">Child B</span>
  </div>
</body>
</html>`

func newFinderDoc(t *testing.T) *parse.Document {
	t.Helper()
	doc, err := parse.NewDocument(newHTMLResponse(finderHTML))
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// --- FindByText ---

func TestDocument_FindByText_ExactMatch(t *testing.T) {
	doc := newFinderDoc(t)
	els := doc.FindByText("Hello World")
	if len(els) == 0 {
		t.Fatal("FindByText: expected at least one element, got none")
	}
	if els[0].Tag() != "h1" {
		t.Errorf("FindByText: got tag %q, want %q", els[0].Tag(), "h1")
	}
}

func TestDocument_FindByText_NoMatch(t *testing.T) {
	doc := newFinderDoc(t)
	els := doc.FindByText("nonexistent text string")
	if len(els) != 0 {
		t.Errorf("FindByText: expected no results, got %d", len(els))
	}
}

func TestDocument_FindByText_DoesNotMatchPartial(t *testing.T) {
	doc := newFinderDoc(t)
	// "Hello" alone should not match "Hello World" (exact trimmed match)
	els := doc.FindByText("Hello")
	if len(els) != 0 {
		t.Errorf("FindByText exact: expected no results for partial text, got %d", len(els))
	}
}

// --- FindByTextContains ---

func TestDocument_FindByTextContains_PartialMatch(t *testing.T) {
	doc := newFinderDoc(t)
	els := doc.FindByTextContains("Third")
	if len(els) == 0 {
		t.Fatal("FindByTextContains: expected results, got none")
	}
	found := false
	for _, el := range els {
		if el.Tag() == "li" {
			found = true
		}
	}
	if !found {
		t.Error("FindByTextContains: expected to find a <li> element containing 'Third'")
	}
}

func TestDocument_FindByTextContains_MultipleMatches(t *testing.T) {
	doc := newFinderDoc(t)
	// "Page" appears in two <a> elements
	els := doc.FindByTextContains("Page")
	count := 0
	for _, el := range els {
		if el.Tag() == "a" {
			count++
		}
	}
	if count < 2 {
		t.Errorf("FindByTextContains: expected >=2 <a> matches, got %d", count)
	}
}

func TestDocument_FindByTextContains_NoMatch(t *testing.T) {
	doc := newFinderDoc(t)
	els := doc.FindByTextContains("zzz-never-exists")
	if len(els) != 0 {
		t.Errorf("FindByTextContains: expected no results, got %d", len(els))
	}
}

// --- FindByTextRegex ---

func TestDocument_FindByTextRegex_PatternMatch(t *testing.T) {
	doc := newFinderDoc(t)
	// Match "First", "Second", "Third item here" from list items
	els := doc.FindByTextRegex(`(?i)first|second`)
	if len(els) == 0 {
		t.Fatal("FindByTextRegex: expected matches, got none")
	}
}

func TestDocument_FindByTextRegex_NoMatch(t *testing.T) {
	doc := newFinderDoc(t)
	els := doc.FindByTextRegex(`^zzz\d+$`)
	if len(els) != 0 {
		t.Errorf("FindByTextRegex: expected no results, got %d", len(els))
	}
}

func TestDocument_FindByTextRegex_InvalidPattern(t *testing.T) {
	doc := newFinderDoc(t)
	// Invalid regex should return nil/empty without panic
	els := doc.FindByTextRegex(`[invalid`)
	if els != nil && len(els) != 0 {
		t.Errorf("FindByTextRegex invalid pattern: expected nil/empty, got %d", len(els))
	}
}

// --- FindByAttr ---

func TestDocument_FindByAttr_ExactMatch(t *testing.T) {
	doc := newFinderDoc(t)
	els := doc.FindByAttr("data-type", "intro")
	if len(els) == 0 {
		t.Fatal("FindByAttr: expected results, got none")
	}
	if els[0].Tag() != "p" {
		t.Errorf("FindByAttr: got tag %q, want %q", els[0].Tag(), "p")
	}
}

func TestDocument_FindByAttr_NoMatch(t *testing.T) {
	doc := newFinderDoc(t)
	els := doc.FindByAttr("data-type", "nonexistent")
	if len(els) != 0 {
		t.Errorf("FindByAttr: expected no results, got %d", len(els))
	}
}

func TestDocument_FindByAttr_IDAttribute(t *testing.T) {
	doc := newFinderDoc(t)
	els := doc.FindByAttr("id", "page-title")
	if len(els) == 0 {
		t.Fatal("FindByAttr id: expected results, got none")
	}
	if els[0].Tag() != "h1" {
		t.Errorf("FindByAttr id: got tag %q, want %q", els[0].Tag(), "h1")
	}
}

// --- FindByAttrContains ---

func TestDocument_FindByAttrContains_PartialMatch(t *testing.T) {
	doc := newFinderDoc(t)
	els := doc.FindByAttrContains("href", "page1")
	if len(els) == 0 {
		t.Fatal("FindByAttrContains: expected results, got none")
	}
	if els[0].Tag() != "a" {
		t.Errorf("FindByAttrContains: got tag %q, want %q", els[0].Tag(), "a")
	}
}

func TestDocument_FindByAttrContains_MultipleMatches(t *testing.T) {
	doc := newFinderDoc(t)
	// Both links contain "example.com"
	els := doc.FindByAttrContains("href", "example.com")
	if len(els) < 2 {
		t.Errorf("FindByAttrContains: expected >=2 results, got %d", len(els))
	}
}

func TestDocument_FindByAttrContains_NoMatch(t *testing.T) {
	doc := newFinderDoc(t)
	els := doc.FindByAttrContains("href", "zzz-never-exists")
	if len(els) != 0 {
		t.Errorf("FindByAttrContains: expected no results, got %d", len(els))
	}
}

// --- Element methods ---

func TestElement_Text(t *testing.T) {
	doc := newFinderDoc(t)
	els := doc.FindByText("Hello World")
	if len(els) == 0 {
		t.Fatal("no element found")
	}
	text := els[0].Text()
	if text != "Hello World" {
		t.Errorf("Element.Text: got %q, want %q", text, "Hello World")
	}
}

func TestElement_HTML(t *testing.T) {
	doc := newFinderDoc(t)
	els := doc.FindByAttr("id", "container")
	if len(els) == 0 {
		t.Fatal("no element found")
	}
	html, err := els[0].HTML()
	if err != nil {
		t.Fatalf("Element.HTML error: %v", err)
	}
	if html == "" {
		t.Error("Element.HTML: expected non-empty inner HTML")
	}
}

func TestElement_Attr(t *testing.T) {
	doc := newFinderDoc(t)
	els := doc.FindByAttr("data-type", "intro")
	if len(els) == 0 {
		t.Fatal("no element found")
	}
	val := els[0].Attr("data-type")
	if val != "intro" {
		t.Errorf("Element.Attr: got %q, want %q", val, "intro")
	}
}

func TestElement_Attr_Missing(t *testing.T) {
	doc := newFinderDoc(t)
	els := doc.FindByText("Hello World")
	if len(els) == 0 {
		t.Fatal("no element found")
	}
	val := els[0].Attr("data-nonexistent")
	if val != "" {
		t.Errorf("Element.Attr missing: got %q, want empty", val)
	}
}

func TestElement_HasClass_True(t *testing.T) {
	doc := newFinderDoc(t)
	els := doc.FindByText("Hello World")
	if len(els) == 0 {
		t.Fatal("no element found")
	}
	if !els[0].HasClass("title") {
		t.Error("Element.HasClass: expected true for class 'title'")
	}
}

func TestElement_HasClass_False(t *testing.T) {
	doc := newFinderDoc(t)
	els := doc.FindByText("Hello World")
	if len(els) == 0 {
		t.Fatal("no element found")
	}
	if els[0].HasClass("nonexistent") {
		t.Error("Element.HasClass: expected false for non-existent class")
	}
}

func TestElement_Tag(t *testing.T) {
	doc := newFinderDoc(t)
	els := doc.FindByText("Hello World")
	if len(els) == 0 {
		t.Fatal("no element found")
	}
	if els[0].Tag() != "h1" {
		t.Errorf("Element.Tag: got %q, want %q", els[0].Tag(), "h1")
	}
}

func TestElement_Attrs_ReturnsAllAttributes(t *testing.T) {
	doc := newFinderDoc(t)
	els := doc.FindByAttr("data-type", "intro")
	if len(els) == 0 {
		t.Fatal("no element found")
	}
	attrs := els[0].Attrs()
	if _, ok := attrs["class"]; !ok {
		t.Error("Element.Attrs: expected 'class' key")
	}
	if _, ok := attrs["data-type"]; !ok {
		t.Error("Element.Attrs: expected 'data-type' key")
	}
	if attrs["data-type"] != "intro" {
		t.Errorf("Element.Attrs: data-type got %q, want %q", attrs["data-type"], "intro")
	}
}

func TestElement_Children(t *testing.T) {
	doc := newFinderDoc(t)
	el := doc.First("#container")
	if el == nil {
		t.Fatal("no element found")
	}
	children := el.Children()
	if len(children) != 2 {
		t.Errorf("Element.Children: got %d, want 2", len(children))
	}
}

func TestElement_Parent(t *testing.T) {
	doc := newFinderDoc(t)
	// Use CSS selector to get the exact span, not FindByTextContains which
	// also returns ancestors that contain the same text.
	el := doc.First("span.child-a")
	if el == nil {
		t.Fatal("no element found")
	}
	parent := el.Parent()
	if parent == nil {
		t.Fatal("Element.Parent: returned nil")
	}
	if parent.Attr("id") != "container" {
		t.Errorf("Element.Parent: id got %q, want %q", parent.Attr("id"), "container")
	}
}

func TestElement_Siblings(t *testing.T) {
	doc := newFinderDoc(t)
	el := doc.First("span.child-a")
	if el == nil {
		t.Fatal("no element found")
	}
	siblings := el.Siblings()
	if len(siblings) == 0 {
		t.Error("Element.Siblings: expected sibling(s), got none")
	}
}

func TestElement_Next(t *testing.T) {
	doc := newFinderDoc(t)
	el := doc.First("span.child-a")
	if el == nil {
		t.Fatal("no element found")
	}
	next := el.Next()
	if next == nil {
		t.Fatal("Element.Next: returned nil")
	}
	if next.Text() != "Child B" {
		t.Errorf("Element.Next: got text %q, want %q", next.Text(), "Child B")
	}
}

func TestElement_Prev(t *testing.T) {
	doc := newFinderDoc(t)
	el := doc.First("span.child-b")
	if el == nil {
		t.Fatal("no element found")
	}
	prev := el.Prev()
	if prev == nil {
		t.Fatal("Element.Prev: returned nil")
	}
	if prev.Text() != "Child A" {
		t.Errorf("Element.Prev: got text %q, want %q", prev.Text(), "Child A")
	}
}

func TestElement_Find(t *testing.T) {
	doc := newFinderDoc(t)
	els := doc.FindByAttr("id", "container")
	if len(els) == 0 {
		t.Fatal("no element found")
	}
	found := els[0].Find("span")
	if len(found) != 2 {
		t.Errorf("Element.Find: got %d spans, want 2", len(found))
	}
}

func TestElement_CSS(t *testing.T) {
	doc := newFinderDoc(t)
	els := doc.FindByAttr("id", "container")
	if len(els) == 0 {
		t.Fatal("no element found")
	}
	child := els[0].CSS(".child-a")
	if child == nil {
		t.Fatal("Element.CSS: returned nil")
	}
	if child.Text() != "Child A" {
		t.Errorf("Element.CSS: got text %q, want %q", child.Text(), "Child A")
	}
}

// --- Document.FindAll and Document.First ---

func TestDocument_FindAll(t *testing.T) {
	doc := newFinderDoc(t)
	els := doc.FindAll("li.item")
	if len(els) != 3 {
		t.Errorf("Document.FindAll: got %d, want 3", len(els))
	}
}

func TestDocument_First_Found(t *testing.T) {
	doc := newFinderDoc(t)
	el := doc.First("li.item")
	if el == nil {
		t.Fatal("Document.First: returned nil for existing selector")
	}
	if el.Text() != "First" {
		t.Errorf("Document.First: got text %q, want %q", el.Text(), "First")
	}
}

func TestDocument_First_NotFound(t *testing.T) {
	doc := newFinderDoc(t)
	el := doc.First(".nonexistent")
	if el != nil {
		t.Error("Document.First: expected nil for non-matching selector")
	}
}
