package parse_test

import (
	"testing"

	"github.com/sadewadee/foxhound/parse"
)

// sampleHTML is defined in goquery_test.go and shared across the parse_test package.

func TestXPath_Select_SimpleTag(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	x, err := parse.NewXPath(resp)
	if err != nil {
		t.Fatalf("NewXPath: %v", err)
	}

	text := x.Select("//h1")
	if text != "Hello World" {
		t.Errorf("//h1: got %q, want %q", text, "Hello World")
	}
}

func TestXPath_Select_AttributeFilter(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	x, _ := parse.NewXPath(resp)

	text := x.Select("//h1[@class='title']")
	if text != "Hello World" {
		t.Errorf("//h1[@class='title']: got %q, want %q", text, "Hello World")
	}
}

func TestXPath_Select_IDAttribute(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	x, _ := parse.NewXPath(resp)

	// //div[@id='content'] should map to div#content
	text := x.Select("//div[@id='content']")
	if text == "" {
		t.Error("//div[@id='content']: expected non-empty text, got empty")
	}
}

func TestXPath_Select_DirectChild(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	x, _ := parse.NewXPath(resp)

	// //div/p → direct child
	text := x.Select("//div/p")
	if text != "Paragraph text" {
		t.Errorf("//div/p: got %q, want %q", text, "Paragraph text")
	}
}

func TestXPath_Select_DescendantSlashSlash(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	x, _ := parse.NewXPath(resp)

	// //body//h1 → descendant h1 inside body
	text := x.Select("//body//h1")
	if text != "Hello World" {
		t.Errorf("//body//h1: got %q, want %q", text, "Hello World")
	}
}

func TestXPath_Select_NotFound(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	x, _ := parse.NewXPath(resp)

	text := x.Select("//section")
	if text != "" {
		t.Errorf("//section: expected empty string, got %q", text)
	}
}

func TestXPath_SelectAll_MultipleElements(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	x, _ := parse.NewXPath(resp)

	items := x.SelectAll("//li")
	if len(items) != 3 {
		t.Fatalf("//li: got %d items, want 3", len(items))
	}
	want := []string{"First", "Second", "Third"}
	for i, w := range want {
		if items[i] != w {
			t.Errorf("SelectAll[%d]: got %q, want %q", i, items[i], w)
		}
	}
}

func TestXPath_SelectAll_NotFound(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	x, _ := parse.NewXPath(resp)

	items := x.SelectAll("//section")
	if len(items) != 0 {
		t.Errorf("//section SelectAll: expected empty slice, got %v", items)
	}
}

func TestXPath_SelectAttr(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	x, _ := parse.NewXPath(resp)

	href := x.SelectAttr("//a[@class='link']", "href")
	if href != "http://example.com/page1" {
		t.Errorf("SelectAttr //a[@class='link'] href: got %q, want %q", href, "http://example.com/page1")
	}
}

func TestXPath_SelectAttr_NotFound(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	x, _ := parse.NewXPath(resp)

	val := x.SelectAttr("//section", "href")
	if val != "" {
		t.Errorf("SelectAttr on missing element: got %q, want empty", val)
	}
}
