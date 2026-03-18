package parse

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// Element wraps a goquery.Selection with convenience navigation and
// extraction methods.
type Element struct {
	sel *goquery.Selection
	doc *Document
}

func newElement(sel *goquery.Selection, doc *Document) *Element {
	return &Element{sel: sel, doc: doc}
}

// Text returns the combined text content of the element, with leading and
// trailing whitespace stripped.
func (e *Element) Text() string {
	return strings.TrimSpace(e.sel.Text())
}

// HTML returns the inner HTML of the element.
func (e *Element) HTML() (string, error) {
	return e.sel.Html()
}

// Attr returns the value of the named attribute.  Returns an empty string
// when the attribute is absent.
func (e *Element) Attr(name string) string {
	val, _ := e.sel.Attr(name)
	return val
}

// HasClass reports whether the element has the named CSS class.
func (e *Element) HasClass(class string) bool {
	return e.sel.HasClass(class)
}

// Children returns the direct child elements.
func (e *Element) Children() []*Element {
	return selectionToElements(e.sel.Children(), e.doc)
}

// Parent returns the parent element, or nil when the element has no parent.
func (e *Element) Parent() *Element {
	p := e.sel.Parent()
	if p.Length() == 0 {
		return nil
	}
	return newElement(p, e.doc)
}

// Siblings returns all sibling elements (excluding the element itself).
func (e *Element) Siblings() []*Element {
	return selectionToElements(e.sel.Siblings(), e.doc)
}

// Next returns the immediately following sibling element, or nil when there
// is none.
func (e *Element) Next() *Element {
	n := e.sel.Next()
	if n.Length() == 0 {
		return nil
	}
	return newElement(n, e.doc)
}

// Prev returns the immediately preceding sibling element, or nil when there
// is none.
func (e *Element) Prev() *Element {
	p := e.sel.Prev()
	if p.Length() == 0 {
		return nil
	}
	return newElement(p, e.doc)
}

// Find returns all descendant elements matching the CSS selector.
func (e *Element) Find(selector string) []*Element {
	return selectionToElements(e.sel.Find(selector), e.doc)
}

// CSS returns the first descendant matching the CSS selector, or nil when
// there is no match.
func (e *Element) CSS(selector string) *Element {
	found := e.sel.Find(selector).First()
	if found.Length() == 0 {
		return nil
	}
	return newElement(found, e.doc)
}

// Tag returns the lowercase tag name of the element (e.g. "div", "span").
func (e *Element) Tag() string {
	if e.sel.Length() == 0 {
		return ""
	}
	node := e.sel.Get(0)
	if node.Type != html.ElementNode {
		return ""
	}
	return node.Data
}

// Attrs returns a map of all attribute names to their values.
func (e *Element) Attrs() map[string]string {
	result := make(map[string]string)
	if e.sel.Length() == 0 {
		return result
	}
	node := e.sel.Get(0)
	for _, a := range node.Attr {
		result[a.Key] = a.Val
	}
	return result
}

// --- Document finders ---

// FindAll returns all elements matching the CSS selector as []*Element.
func (d *Document) FindAll(selector string) []*Element {
	return selectionToElements(d.doc.Find(selector), d)
}

// First returns the first element matching the CSS selector, or nil when
// there is no match.
func (d *Document) First(selector string) *Element {
	sel := d.doc.Find(selector).First()
	if sel.Length() == 0 {
		return nil
	}
	return newElement(sel, d)
}

// FindByText returns all elements whose trimmed text content exactly equals
// the given string.
func (d *Document) FindByText(text string) []*Element {
	var results []*Element
	d.doc.Find("*").Each(func(_ int, s *goquery.Selection) {
		if strings.TrimSpace(s.Text()) == text {
			results = append(results, newElement(s, d))
		}
	})
	return results
}

// FindByTextContains returns all elements whose text content contains the
// given substring.
func (d *Document) FindByTextContains(substring string) []*Element {
	var results []*Element
	d.doc.Find("*").Each(func(_ int, s *goquery.Selection) {
		if strings.Contains(s.Text(), substring) {
			results = append(results, newElement(s, d))
		}
	})
	return results
}

// FindByTextRegex returns all elements whose text content matches the regular
// expression pattern.  An invalid pattern returns nil without panicking.
func (d *Document) FindByTextRegex(pattern string) []*Element {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	var results []*Element
	d.doc.Find("*").Each(func(_ int, s *goquery.Selection) {
		if re.MatchString(s.Text()) {
			results = append(results, newElement(s, d))
		}
	})
	return results
}

// FindByAttr returns all elements where the named attribute exactly equals
// value.
func (d *Document) FindByAttr(attr, value string) []*Element {
	var results []*Element
	d.doc.Find("*").Each(func(_ int, s *goquery.Selection) {
		if v, ok := s.Attr(attr); ok && v == value {
			results = append(results, newElement(s, d))
		}
	})
	return results
}

// FindByAttrContains returns all elements where the named attribute contains
// the given substring.
func (d *Document) FindByAttrContains(attr, substring string) []*Element {
	var results []*Element
	d.doc.Find("*").Each(func(_ int, s *goquery.Selection) {
		if v, ok := s.Attr(attr); ok && strings.Contains(v, substring) {
			results = append(results, newElement(s, d))
		}
	})
	return results
}

// selectionToElements converts a goquery.Selection into a slice of *Element.
func selectionToElements(sel *goquery.Selection, doc *Document) []*Element {
	var result []*Element
	sel.Each(func(_ int, s *goquery.Selection) {
		result = append(result, newElement(s, doc))
	})
	return result
}
