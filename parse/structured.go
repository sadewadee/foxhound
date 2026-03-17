package parse

import (
	"fmt"
	"time"

	"github.com/PuerkitoBio/goquery"
	foxhound "github.com/foxhound-scraper/foxhound"
)

// FieldDef describes how to extract a single field from an HTML element.
type FieldDef struct {
	// Name is the key used in the resulting Item.Fields map.
	Name string
	// Selector is a CSS selector targeting the element to extract from.
	Selector string
	// Attr is the HTML attribute to extract.  When empty, the text content is
	// used instead.
	Attr string
	// Required causes Extract / ExtractAll to return an error when the selector
	// matches no element.
	Required bool
}

// Schema defines a set of fields to extract from a page.
type Schema struct {
	Fields []FieldDef
}

// Extract applies the schema to the entire response document and returns a
// single Item containing all extracted fields.  Required fields that produce
// no match cause an error to be returned.
func (s *Schema) Extract(resp *foxhound.Response) (*foxhound.Item, error) {
	doc, err := NewDocument(resp)
	if err != nil {
		return nil, err
	}

	item := foxhound.NewItem()
	item.URL = resp.URL

	for _, fd := range s.Fields {
		value := extractField(doc.doc.Selection, fd)
		if value == "" && fd.Required {
			return nil, fmt.Errorf("parse: structured: required field %q not found (selector: %q)", fd.Name, fd.Selector)
		}
		item.Fields[fd.Name] = value
	}
	return item, nil
}

// ExtractAll applies the schema to every element matching rootSelector and
// returns one Item per element.  Required fields inside each element follow
// the same rules as Extract.
func (s *Schema) ExtractAll(resp *foxhound.Response, rootSelector string) ([]*foxhound.Item, error) {
	doc, err := NewDocument(resp)
	if err != nil {
		return nil, err
	}

	var items []*foxhound.Item
	var extractErr error

	doc.doc.Find(rootSelector).EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		item := &foxhound.Item{
			Fields:    make(map[string]any),
			Meta:      make(map[string]any),
			URL:       resp.URL,
			Timestamp: time.Now(),
		}
		for _, fd := range s.Fields {
			value := extractField(sel, fd)
			if value == "" && fd.Required {
				extractErr = fmt.Errorf(
					"parse: structured: required field %q not found (selector: %q)",
					fd.Name, fd.Selector,
				)
				return false // stop iteration
			}
			item.Fields[fd.Name] = value
		}
		items = append(items, item)
		return true
	})

	if extractErr != nil {
		return nil, extractErr
	}
	return items, nil
}

// extractField finds the element matching fd.Selector within root and returns
// either its attribute value or its text content.
func extractField(root *goquery.Selection, fd FieldDef) string {
	el := root.Find(fd.Selector).First()
	if el.Length() == 0 {
		return ""
	}
	if fd.Attr != "" {
		val, _ := el.Attr(fd.Attr)
		return val
	}
	return el.Text()
}
