package parse

import (
	"net/url"

	"github.com/PuerkitoBio/goquery"
)

// URLJoin resolves a relative URL against the document's page URL.
// When the document has no URL, the relative string is returned unchanged.
func (d *Document) URLJoin(relative string) string {
	if d.resp == nil || d.resp.URL == "" {
		return relative
	}
	base, err := url.Parse(d.resp.URL)
	if err != nil {
		return relative
	}
	ref, err := url.Parse(relative)
	if err != nil {
		return relative
	}
	return base.ResolveReference(ref).String()
}

// FindAllFunc returns every element in the document for which fn returns
// true. The document is traversed in DOM order.
func (d *Document) FindAllFunc(fn func(*Element) bool) []*Element {
	var results []*Element
	d.doc.Find("*").Each(func(_ int, s *goquery.Selection) {
		el := newElement(s, d)
		if fn(el) {
			results = append(results, el)
		}
	})
	return results
}

// FindByTag returns all elements with the given tag name in document order.
func (d *Document) FindByTag(tag string) []*Element {
	return selectionToElements(d.doc.Find(tag), d)
}

// FindByTagAndAttr returns all elements that match both the given tag name
// and every key-value pair in attrs. An empty attrs map matches any element
// with the given tag.
func (d *Document) FindByTagAndAttr(tag string, attrs map[string]string) []*Element {
	var results []*Element
	d.doc.Find(tag).Each(func(_ int, s *goquery.Selection) {
		for k, v := range attrs {
			actual, exists := s.Attr(k)
			if !exists || actual != v {
				return
			}
		}
		results = append(results, newElement(s, d))
	})
	return results
}
