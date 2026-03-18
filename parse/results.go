package parse

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Results is a chainable collection of extraction results.
// Supports comparable Python framework-style CSS pseudo-selectors: ::text and ::attr(name).
type Results struct {
	items []resultItem
	doc   *Document
}

// resultItem holds either an Element (plain CSS selector) or a Text string
// (when ::text or ::attr pseudo-selectors are used).
type resultItem struct {
	element *Element
	text    string
	hasText bool // true when this item carries text rather than an element
}

// CSS selects elements with comparable Python framework-compatible pseudo-selector support.
//
//	doc.CSS("h1")             — elements matched by h1
//	doc.CSS("h1::text")       — trimmed text content of h1 elements
//	doc.CSS("a::attr(href)")  — href attribute values of a elements
func (d *Document) CSS(selector string) *Results {
	r := &Results{doc: d}

	// ::text pseudo-selector
	if strings.HasSuffix(selector, "::text") {
		base := strings.TrimSpace(strings.TrimSuffix(selector, "::text"))
		d.doc.Find(base).Each(func(_ int, s *goquery.Selection) {
			r.items = append(r.items, resultItem{
				text:    strings.TrimSpace(s.Text()),
				hasText: true,
			})
		})
		return r
	}

	// ::attr(name) pseudo-selector
	if idx := strings.Index(selector, "::attr("); idx >= 0 {
		base := strings.TrimSpace(selector[:idx])
		attrPart := selector[idx+7:] // skip past "::attr("
		attrName := strings.TrimSuffix(attrPart, ")")
		d.doc.Find(base).Each(func(_ int, s *goquery.Selection) {
			if val, exists := s.Attr(attrName); exists {
				r.items = append(r.items, resultItem{
					text:    val,
					hasText: true,
				})
			}
		})
		return r
	}

	// Plain CSS selector — return elements
	d.doc.Find(selector).Each(func(_ int, s *goquery.Selection) {
		r.items = append(r.items, resultItem{
			element: newElement(s, d),
		})
	})
	return r
}

// Get returns the first result as text, or "" when empty.
// For plain-element results the trimmed text of the first element is returned.
func (r *Results) Get() string {
	if len(r.items) == 0 {
		return ""
	}
	item := r.items[0]
	if item.hasText {
		return item.text
	}
	if item.element != nil {
		return item.element.Text()
	}
	return ""
}

// GetAll returns all results as text strings.
func (r *Results) GetAll() []string {
	out := make([]string, 0, len(r.items))
	for _, item := range r.items {
		if item.hasText {
			out = append(out, item.text)
		} else if item.element != nil {
			out = append(out, item.element.Text())
		}
	}
	return out
}

// First returns the first element result, or nil when the result set is empty
// or was produced by a ::text/::attr pseudo-selector (which has no elements).
func (r *Results) First() *Element {
	for _, item := range r.items {
		if !item.hasText && item.element != nil {
			return item.element
		}
	}
	return nil
}

// Last returns the last element result, or nil when none exist.
func (r *Results) Last() *Element {
	for i := len(r.items) - 1; i >= 0; i-- {
		item := r.items[i]
		if !item.hasText && item.element != nil {
			return item.element
		}
	}
	return nil
}

// Len returns the number of results.
func (r *Results) Len() int {
	return len(r.items)
}

// Re applies the regular-expression pattern to every result's text and
// returns all matches flattened into a single slice.
// Returns nil for an invalid pattern.
func (r *Results) Re(pattern string) []string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	var matches []string
	for _, item := range r.items {
		var src string
		if item.hasText {
			src = item.text
		} else if item.element != nil {
			src = item.element.Text()
		}
		matches = append(matches, re.FindAllString(src, -1)...)
	}
	return matches
}

// ReFirst returns the first regex match across all results, or "" if none.
// Returns "" for an invalid pattern.
func (r *Results) ReFirst(pattern string) string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return ""
	}
	for _, item := range r.items {
		var src string
		if item.hasText {
			src = item.text
		} else if item.element != nil {
			src = item.element.Text()
		}
		if m := re.FindString(src); m != "" {
			return m
		}
	}
	return ""
}

// Filter returns a new Results containing only element results where fn
// returns true. Text-only results (from ::text/::attr pseudo-selectors) are
// always excluded because they carry no Element.
func (r *Results) Filter(fn func(*Element) bool) *Results {
	out := &Results{doc: r.doc}
	for _, item := range r.items {
		if item.hasText || item.element == nil {
			continue
		}
		if fn(item.element) {
			out.items = append(out.items, item)
		}
	}
	return out
}

// Each iterates over element results, calling fn with the zero-based index
// and the element. Text-only results are skipped.
func (r *Results) Each(fn func(int, *Element)) {
	idx := 0
	for _, item := range r.items {
		if item.hasText || item.element == nil {
			continue
		}
		fn(idx, item.element)
		idx++
	}
}
