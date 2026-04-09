package parse

import (
	"bytes"
	"strings"

	"github.com/PuerkitoBio/goquery"
	foxhound "github.com/sadewadee/foxhound"
)

func init() {
	foxhound.RegisterHTMLSelectors(
		selectTexts,
		selectAttrs,
		selectCount,
		xpathToCSS,
	)
	foxhound.RegisterAdaptiveHooks(adaptiveExtractText, adaptiveRegister)
}

// adaptiveExtractText is the parse-side implementation of
// Response.Adaptive(name): parse the body, then call the extractor.
func adaptiveExtractText(extractor any, body []byte, name string) string {
	ae, ok := extractor.(*AdaptiveExtractor)
	if !ok || ae == nil {
		return ""
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	d := &Document{doc: doc, adaptive: ae}
	return ae.ExtractText(d, name)
}

// adaptiveRegister is the parse-side implementation of
// Response.CSSAdaptive(selector, name): register the selector against the
// extractor and immediately learn its signature from the current body. The
// "all" flag selects between Extract and ExtractAll for signature capture.
func adaptiveRegister(extractor any, body []byte, name, selector string, all bool) {
	ae, ok := extractor.(*AdaptiveExtractor)
	if !ok || ae == nil {
		return
	}
	ae.Register(name, selector)
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return
	}
	d := &Document{doc: doc, adaptive: ae}
	if all {
		_ = ae.ExtractAll(d, name)
	} else {
		_ = ae.Extract(d, name)
	}
}

// selectTexts extracts trimmed text from all elements matching a CSS selector.
func selectTexts(body []byte, selector string) []string {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	var results []string
	doc.Find(selector).Each(func(_ int, s *goquery.Selection) {
		results = append(results, strings.TrimSpace(s.Text()))
	})
	return results
}

// selectAttrs extracts attribute values from all elements matching a CSS selector.
func selectAttrs(body []byte, selector, attr string) []string {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	var results []string
	doc.Find(selector).Each(func(_ int, s *goquery.Selection) {
		val, exists := s.Attr(attr)
		if exists {
			results = append(results, val)
		}
	})
	return results
}

// selectCount returns the number of elements matching a CSS selector.
func selectCount(body []byte, selector string) int {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return 0
	}
	return doc.Find(selector).Length()
}
