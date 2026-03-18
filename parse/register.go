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
