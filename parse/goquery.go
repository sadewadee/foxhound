// Package parse provides HTML, JSON, and other response parsing utilities
// for the Foxhound scraping framework.
package parse

import (
	"bytes"
	"fmt"

	"github.com/PuerkitoBio/goquery"
	foxhound "github.com/sadewadee/foxhound"
)

// Document wraps goquery.Document for convenient CSS-selector-based extraction.
type Document struct {
	doc      *goquery.Document
	resp     *foxhound.Response
	adaptive *AdaptiveExtractor
}

// Adaptive returns the AdaptiveExtractor associated with this document, if
// any. Returns nil when no extractor was attached via SetAdaptive.
func (d *Document) Adaptive() *AdaptiveExtractor {
	return d.adaptive
}

// SetAdaptive attaches an AdaptiveExtractor to this document so callers can
// retrieve it later via Adaptive() — useful for wiring a Hunt-scoped
// extractor through to processors that build their own Document.
func (d *Document) SetAdaptive(ae *AdaptiveExtractor) {
	d.adaptive = ae
}

// NewDocument creates a Document from a foxhound Response.
// The response body is parsed as HTML.
func NewDocument(resp *foxhound.Response) (*Document, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, fmt.Errorf("parse: parsing HTML document: %w", err)
	}
	return &Document{doc: doc, resp: resp}, nil
}

// Find returns the goquery Selection for a CSS selector.
func (d *Document) Find(selector string) *goquery.Selection {
	return d.doc.Find(selector)
}

// Text extracts trimmed text from the first element matching selector.
// Returns an empty string if no element matches.
func (d *Document) Text(selector string) string {
	return d.doc.Find(selector).First().Text()
}

// Texts extracts trimmed text from all elements matching selector.
func (d *Document) Texts(selector string) []string {
	var results []string
	d.doc.Find(selector).Each(func(_ int, s *goquery.Selection) {
		results = append(results, s.Text())
	})
	return results
}

// Attr extracts an attribute value from the first element matching selector.
// Returns an empty string if no element matches or the attribute is absent.
func (d *Document) Attr(selector, attr string) string {
	val, _ := d.doc.Find(selector).First().Attr(attr)
	return val
}

// Attrs extracts attribute values from all elements matching selector.
// Elements where the attribute is absent contribute an empty string.
func (d *Document) Attrs(selector, attr string) []string {
	var results []string
	d.doc.Find(selector).Each(func(_ int, s *goquery.Selection) {
		val, _ := s.Attr(attr)
		results = append(results, val)
	})
	return results
}

// HTML returns the inner HTML of the first element matching selector.
// Returns an empty string if no element matches.
func (d *Document) HTML(selector string) string {
	html, _ := d.doc.Find(selector).First().Html()
	return html
}

// Each iterates over all elements matching selector, calling fn with the
// zero-based index and the goquery.Selection for each element.
func (d *Document) Each(selector string, fn func(i int, s *goquery.Selection)) {
	d.doc.Find(selector).Each(fn)
}

// Response returns the original foxhound Response that produced this document.
func (d *Document) Response() *foxhound.Response {
	return d.resp
}
