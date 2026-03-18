package parse

import (
	"regexp"
	"sort"
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

// ---------------------------------------------------------------------------
// Element.FindSimilar — find structurally similar elements in the document
// ---------------------------------------------------------------------------

// SimilarOption configures how FindSimilar matches elements.
type SimilarOption func(*similarConfig)

type similarConfig struct {
	threshold   float64
	ignoreAttrs map[string]bool
	matchText   bool
}

// WithSimilarThreshold sets the minimum attribute similarity score (0.0-1.0).
// Elements below this threshold are excluded. Default: 0.2.
func WithSimilarThreshold(t float64) SimilarOption {
	return func(c *similarConfig) { c.threshold = t }
}

// WithSimilarIgnoreAttrs sets attribute names to exclude from the similarity
// comparison. Default: ["href", "src"].
func WithSimilarIgnoreAttrs(attrs ...string) SimilarOption {
	return func(c *similarConfig) {
		c.ignoreAttrs = make(map[string]bool, len(attrs))
		for _, a := range attrs {
			c.ignoreAttrs[a] = true
		}
	}
}

// WithSimilarMatchText includes text content in the similarity calculation.
// Not recommended for most cases as text varies between similar elements.
func WithSimilarMatchText(match bool) SimilarOption {
	return func(c *similarConfig) { c.matchText = match }
}

// FindSimilar finds elements in the document that are structurally similar
// to this element. It matches elements at the same tree depth with the same
// tag and parent tag hierarchy, then ranks by attribute similarity.
//
// This is useful for cases where you found one element (e.g. a product card)
// and want to find all similar elements on the page.
//
// The returned slice is sorted by similarity score (highest first) and
// excludes the element itself.
func (e *Element) FindSimilar(opts ...SimilarOption) []*Element {
	if e.sel.Length() == 0 || e.doc == nil {
		return nil
	}

	cfg := &similarConfig{
		threshold:   0.2,
		ignoreAttrs: map[string]bool{"href": true, "src": true},
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Capture the target element's structural properties.
	targetTag := e.Tag()
	targetDepth := nodeDepth(e.sel.Get(0))
	targetAttrs := filteredAttrs(e, cfg.ignoreAttrs)

	// Build path: tag / parent tag / grandparent tag.
	var parentTag, grandparentTag string
	if parent := e.sel.Parent(); parent.Length() > 0 {
		pNode := parent.Get(0)
		if pNode.Type == html.ElementNode {
			parentTag = pNode.Data
		}
		if gp := parent.Parent(); gp.Length() > 0 {
			gpNode := gp.Get(0)
			if gpNode.Type == html.ElementNode {
				grandparentTag = gpNode.Data
			}
		}
	}

	// Build CSS selector for candidates at same depth with same tag path.
	// We cannot express depth in CSS, so we scan all same-tag elements
	// and filter by depth + parent matching.
	var matches []scoredElement
	targetNode := e.sel.Get(0)

	e.doc.doc.Find(targetTag).Each(func(_ int, s *goquery.Selection) {
		node := s.Get(0)
		// Skip self.
		if node == targetNode {
			return
		}

		// Depth filter.
		if nodeDepth(node) != targetDepth {
			return
		}

		// Parent tag filter.
		if parentTag != "" {
			p := s.Parent()
			if p.Length() == 0 {
				return
			}
			pn := p.Get(0)
			if pn.Type != html.ElementNode || pn.Data != parentTag {
				return
			}

			// Grandparent filter.
			if grandparentTag != "" {
				gp := p.Parent()
				if gp.Length() == 0 {
					return
				}
				gpn := gp.Get(0)
				if gpn.Type != html.ElementNode || gpn.Data != grandparentTag {
					return
				}
			}
		}

		// Compute attribute similarity.
		candidate := newElement(s, e.doc)
		score := attrSimilarity(targetAttrs, filteredAttrs(candidate, cfg.ignoreAttrs))

		if cfg.matchText {
			textScore := textSimilarity(
				strings.TrimSpace(e.sel.Text()),
				strings.TrimSpace(s.Text()),
			)
			score = (score + textScore) / 2
		}

		if score >= cfg.threshold {
			matches = append(matches, scoredElement{el: candidate, score: score})
		}
	})

	// Sort by score descending.
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})

	result := make([]*Element, len(matches))
	for i, m := range matches {
		result[i] = m.el
	}
	return result
}

type scoredElement struct {
	el    *Element
	score float64
}

// filteredAttrs returns the element's attributes excluding those in the
// ignore set.
func filteredAttrs(e *Element, ignore map[string]bool) map[string]string {
	attrs := e.Attrs()
	for k := range ignore {
		delete(attrs, k)
	}
	return attrs
}

// attrSimilarity computes the similarity between two attribute maps.
// Returns a value in [0.0, 1.0].
func attrSimilarity(a, b map[string]string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0 // Both have no attributes; structurally identical.
	}

	// Collect all keys.
	allKeys := make(map[string]bool)
	for k := range a {
		allKeys[k] = true
	}
	for k := range b {
		allKeys[k] = true
	}

	if len(allKeys) == 0 {
		return 1.0
	}

	matches := 0
	for k := range allKeys {
		va, oka := a[k]
		vb, okb := b[k]
		if oka && okb && va == vb {
			matches++
		}
	}

	return float64(matches) / float64(len(allKeys))
}
