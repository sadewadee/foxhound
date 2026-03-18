package parse_test

import (
	"net/http"
	"testing"

	"github.com/PuerkitoBio/goquery"
	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/parse"
)

func newHTMLResponse(body string) *foxhound.Response {
	return &foxhound.Response{
		StatusCode: 200,
		Headers:    http.Header{"Content-Type": {"text/html"}},
		Body:       []byte(body),
		URL:        "http://example.com",
	}
}

const sampleHTML = `<!DOCTYPE html>
<html>
<head><title>Test Page</title></head>
<body>
  <h1 class="title">Hello World</h1>
  <ul>
    <li class="item">First</li>
    <li class="item">Second</li>
    <li class="item">Third</li>
  </ul>
  <a href="http://example.com/page1" class="link">Page 1</a>
  <a href="http://example.com/page2" class="link">Page 2</a>
  <div id="content">
    <p>Paragraph text</p>
  </div>
</body>
</html>`

func TestNewDocument_ValidHTML(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	doc, err := parse.NewDocument(resp)
	if err != nil {
		t.Fatalf("NewDocument returned error: %v", err)
	}
	if doc == nil {
		t.Fatal("NewDocument returned nil document")
	}
}

func TestNewDocument_InvalidBody(t *testing.T) {
	// goquery is lenient with HTML, so even malformed HTML parses.
	// An empty body should still work without error.
	resp := newHTMLResponse("")
	doc, err := parse.NewDocument(resp)
	if err != nil {
		t.Fatalf("NewDocument on empty body returned error: %v", err)
	}
	if doc == nil {
		t.Fatal("NewDocument returned nil document")
	}
}

func TestDocument_Text_Found(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	doc, _ := parse.NewDocument(resp)

	text := doc.Text("h1.title")
	if text != "Hello World" {
		t.Errorf("Text selector h1.title: got %q, want %q", text, "Hello World")
	}
}

func TestDocument_Text_NotFound(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	doc, _ := parse.NewDocument(resp)

	text := doc.Text(".nonexistent")
	if text != "" {
		t.Errorf("Text on missing selector: got %q, want empty string", text)
	}
}

func TestDocument_Texts_MultipleElements(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	doc, _ := parse.NewDocument(resp)

	texts := doc.Texts("li.item")
	if len(texts) != 3 {
		t.Fatalf("Texts li.item: got %d elements, want 3", len(texts))
	}
	want := []string{"First", "Second", "Third"}
	for i, w := range want {
		if texts[i] != w {
			t.Errorf("Texts[%d]: got %q, want %q", i, texts[i], w)
		}
	}
}

func TestDocument_Attr_Found(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	doc, _ := parse.NewDocument(resp)

	href := doc.Attr("a.link", "href")
	if href != "http://example.com/page1" {
		t.Errorf("Attr a.link href: got %q, want %q", href, "http://example.com/page1")
	}
}

func TestDocument_Attr_NotFound(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	doc, _ := parse.NewDocument(resp)

	val := doc.Attr(".nonexistent", "href")
	if val != "" {
		t.Errorf("Attr on missing element: got %q, want empty string", val)
	}
}

func TestDocument_Attrs_MultipleElements(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	doc, _ := parse.NewDocument(resp)

	hrefs := doc.Attrs("a.link", "href")
	if len(hrefs) != 2 {
		t.Fatalf("Attrs a.link href: got %d elements, want 2", len(hrefs))
	}
	if hrefs[0] != "http://example.com/page1" {
		t.Errorf("Attrs[0]: got %q, want %q", hrefs[0], "http://example.com/page1")
	}
	if hrefs[1] != "http://example.com/page2" {
		t.Errorf("Attrs[1]: got %q, want %q", hrefs[1], "http://example.com/page2")
	}
}

func TestDocument_HTML_Found(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	doc, _ := parse.NewDocument(resp)

	html := doc.HTML("#content")
	if html == "" {
		t.Error("HTML #content: got empty string, expected content")
	}
}

func TestDocument_HTML_NotFound(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	doc, _ := parse.NewDocument(resp)

	html := doc.HTML(".nonexistent")
	if html != "" {
		t.Errorf("HTML on missing selector: got %q, want empty string", html)
	}
}

func TestDocument_Each(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	doc, _ := parse.NewDocument(resp)

	var collected []string
	doc.Each("li.item", func(_ int, s *goquery.Selection) {
		collected = append(collected, s.Text())
	})
	if len(collected) != 3 {
		t.Fatalf("Each li.item: got %d calls, want 3", len(collected))
	}
}

func TestDocument_Find(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	doc, _ := parse.NewDocument(resp)

	sel := doc.Find("li.item")
	if sel.Length() != 3 {
		t.Errorf("Find li.item: got %d elements, want 3", sel.Length())
	}
}

func TestDocument_Response(t *testing.T) {
	resp := newHTMLResponse(sampleHTML)
	doc, _ := parse.NewDocument(resp)

	if doc.Response() != resp {
		t.Error("Response() did not return the original response")
	}
}
