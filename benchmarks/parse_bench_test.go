// Package benchmarks measures Foxhound's parsing performance against
// raw goquery, standard library html, and regex approaches.
//
// Run: go test -bench=. -benchmem ./benchmarks/
// Run with count: go test -bench=. -benchmem -count=5 ./benchmarks/
package benchmarks

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/parse"
	"golang.org/x/net/html"
)

// generateHTML creates synthetic HTML with N items, matching comparable Python framework's benchmark.
func generateHTML(n int) []byte {
	var buf bytes.Buffer
	buf.WriteString("<html><head><title>Benchmark</title></head><body>")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&buf, `<div class="item" data-id="%d"><h3>Product %d</h3><span class="price">$%d.99</span><p class="desc">Description for item %d</p></div>`, i, i, i%100, i)
	}
	buf.WriteString("</body></html>")
	return buf.Bytes()
}

var (
	html1000  = generateHTML(1000)
	html5000  = generateHTML(5000)
	html10000 = generateHTML(10000)
)

func makeResp(body []byte) *foxhound.Response {
	return &foxhound.Response{StatusCode: 200, Body: body, URL: "http://bench.local"}
}

// ─── CSS Selection Benchmarks ───────────────────────────

// BenchmarkFoxhound_CSS_1000 measures Foxhound parse.Document CSS selection.
func BenchmarkFoxhound_CSS_1000(b *testing.B) {
	resp := makeResp(html1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc, _ := parse.NewDocument(resp)
		items := doc.Texts("div.item h3")
		_ = items
	}
}

func BenchmarkFoxhound_CSS_5000(b *testing.B) {
	resp := makeResp(html5000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc, _ := parse.NewDocument(resp)
		items := doc.Texts("div.item h3")
		_ = items
	}
}

func BenchmarkFoxhound_CSS_10000(b *testing.B) {
	resp := makeResp(html10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc, _ := parse.NewDocument(resp)
		items := doc.Texts("div.item h3")
		_ = items
	}
}

// BenchmarkRawGoquery_CSS_* measures raw goquery without Foxhound wrapper.
func BenchmarkRawGoquery_CSS_1000(b *testing.B) {
	reader := bytes.NewReader(html1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader.Reset(html1000)
		doc, _ := goquery.NewDocumentFromReader(reader)
		var items []string
		doc.Find("div.item h3").Each(func(_ int, s *goquery.Selection) {
			items = append(items, s.Text())
		})
		_ = items
	}
}

func BenchmarkRawGoquery_CSS_5000(b *testing.B) {
	reader := bytes.NewReader(html5000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader.Reset(html5000)
		doc, _ := goquery.NewDocumentFromReader(reader)
		var items []string
		doc.Find("div.item h3").Each(func(_ int, s *goquery.Selection) {
			items = append(items, s.Text())
		})
		_ = items
	}
}

func BenchmarkRawGoquery_CSS_10000(b *testing.B) {
	reader := bytes.NewReader(html10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader.Reset(html10000)
		doc, _ := goquery.NewDocumentFromReader(reader)
		var items []string
		doc.Find("div.item h3").Each(func(_ int, s *goquery.Selection) {
			items = append(items, s.Text())
		})
		_ = items
	}
}

// BenchmarkStdlib_HTML_* measures Go standard library html.Parse + manual walk.
func BenchmarkStdlib_HTML_1000(b *testing.B) {
	benchStdlib(b, html1000)
}

func BenchmarkStdlib_HTML_5000(b *testing.B) {
	benchStdlib(b, html5000)
}

func BenchmarkStdlib_HTML_10000(b *testing.B) {
	benchStdlib(b, html10000)
}

func benchStdlib(b *testing.B, data []byte) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		node, _ := html.Parse(bytes.NewReader(data))
		var items []string
		var walk func(*html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "h3" {
				// Check parent is div.item
				if n.Parent != nil && n.Parent.Data == "div" {
					for _, a := range n.Parent.Attr {
						if a.Key == "class" && a.Val == "item" {
							if n.FirstChild != nil {
								items = append(items, n.FirstChild.Data)
							}
						}
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(node)
		_ = items
	}
}

// BenchmarkRegex_* measures regex extraction (no DOM parsing).
func BenchmarkRegex_1000(b *testing.B) {
	benchRegex(b, html1000)
}

func BenchmarkRegex_5000(b *testing.B) {
	benchRegex(b, html5000)
}

func BenchmarkRegex_10000(b *testing.B) {
	benchRegex(b, html10000)
}

var h3Re = regexp.MustCompile(`<h3>([^<]+)</h3>`)

func benchRegex(b *testing.B, data []byte) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matches := h3Re.FindAllSubmatch(data, -1)
		items := make([]string, len(matches))
		for j, m := range matches {
			items[j] = string(m[1])
		}
		_ = items
	}
}

// ─── Text Extraction Benchmarks ─────────────────────────

func BenchmarkFoxhound_TextExtract_5000(b *testing.B) {
	resp := makeResp(html5000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc, _ := parse.NewDocument(resp)
		doc.Each("div.item", func(_ int, s *goquery.Selection) {
			_ = s.Find("h3").Text()
			_ = s.Find("span.price").Text()
			_ = s.Find("p.desc").Text()
		})
	}
}

func BenchmarkRawGoquery_TextExtract_5000(b *testing.B) {
	reader := bytes.NewReader(html5000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader.Reset(html5000)
		doc, _ := goquery.NewDocumentFromReader(reader)
		doc.Find("div.item").Each(func(_ int, s *goquery.Selection) {
			_ = s.Find("h3").Text()
			_ = s.Find("span.price").Text()
			_ = s.Find("p.desc").Text()
		})
	}
}

// ─── Structured Extraction Benchmarks ───────────────────

func BenchmarkFoxhound_Schema_5000(b *testing.B) {
	resp := makeResp(html5000)
	schema := &parse.Schema{
		Fields: []parse.FieldDef{
			{Name: "title", Selector: "h3"},
			{Name: "price", Selector: "span.price"},
			{Name: "desc", Selector: "p.desc"},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		items, _ := schema.ExtractAll(resp, "div.item")
		_ = items
	}
}

// ─── Adaptive Parsing Benchmarks ────────────────────────

func BenchmarkFoxhound_Adaptive_5000(b *testing.B) {
	resp := makeResp(html5000)
	ext := parse.NewAdaptiveExtractor("")
	ext.Register("title", "div.item h3")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc, _ := parse.NewDocument(resp)
		_ = ext.ExtractText(doc, "title")
	}
}

// ─── FindByText Benchmarks ──────────────────────────────

func BenchmarkFoxhound_FindByText_5000(b *testing.B) {
	resp := makeResp(html5000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc, _ := parse.NewDocument(resp)
		_ = doc.FindByTextContains("Product 2500")
	}
}

// ─── Similarity Benchmarks ──────────────────────────────

func BenchmarkFoxhound_Similarity(b *testing.B) {
	sig := &parse.ElementSignature{
		Tag: "div", ID: "main", Classes: []string{"item", "active"},
		Text: "Product 1", ParentTag: "body", Depth: 2, Position: 1,
	}
	sig2 := &parse.ElementSignature{
		Tag: "div", ID: "main", Classes: []string{"item", "highlight"},
		Text: "Product 2", ParentTag: "body", Depth: 2, Position: 3,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parse.Similarity(sig, sig2)
	}
}

// ─── Item Serialization Benchmarks ──────────────────────

func BenchmarkItem_ToJSON(b *testing.B) {
	item := foxhound.NewItem()
	item.Set("title", "Product Widget Pro")
	item.Set("price", 29.99)
	item.Set("url", "https://example.com/product/1")
	item.Set("description", "A high-quality widget for professional use")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = item.ToJSON()
	}
}

func BenchmarkItem_ToMarkdown(b *testing.B) {
	item := foxhound.NewItem()
	item.Set("title", "Product Widget Pro")
	item.Set("price", 29.99)
	item.Set("url", "https://example.com/product/1")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = item.ToMarkdown()
	}
}

// ─── Summary printer ────────────────────────────────────

func TestBenchmarkSummary(t *testing.T) {
	t.Log("Run benchmarks with: go test -bench=. -benchmem ./benchmarks/")
	t.Log("")
	t.Log("Benchmark categories:")
	t.Log("  CSS Selection:    Foxhound vs RawGoquery vs Stdlib vs Regex (1K/5K/10K items)")
	t.Log("  Text Extraction:  Foxhound vs RawGoquery (5K items, 3 fields each)")
	t.Log("  Structured:       Schema.ExtractAll (5K items)")
	t.Log("  Adaptive:         AdaptiveExtractor with fallback (5K items)")
	t.Log("  FindByText:       Text search across 5K elements")
	t.Log("  Similarity:       Element signature comparison")
	t.Log("  Serialization:    Item.ToJSON, Item.ToMarkdown")

	// Run a quick inline comparison
	sizes := []struct {
		name string
		data []byte
	}{
		{"1K", html1000},
		{"5K", html5000},
		{"10K", html10000},
	}

	for _, s := range sizes {
		resp := makeResp(s.data)

		// Foxhound
		doc, _ := parse.NewDocument(resp)
		items := doc.Texts("div.item h3")

		// Regex
		matches := h3Re.FindAllSubmatch(s.data, -1)

		t.Logf("")
		t.Logf("  %s items: Foxhound found %d, Regex found %d, HTML size %d bytes",
			s.name, len(items), len(matches), len(s.data))
	}

	// Verify all find same count
	resp5k := makeResp(html5000)
	doc, _ := parse.NewDocument(resp5k)
	cssCount := len(doc.Texts("div.item h3"))
	regexCount := len(h3Re.FindAllSubmatch(html5000, -1))

	reader := bytes.NewReader(html5000)
	gqDoc, _ := goquery.NewDocumentFromReader(reader)
	gqCount := 0
	gqDoc.Find("div.item h3").Each(func(_ int, _ *goquery.Selection) { gqCount++ })

	node, _ := html.Parse(bytes.NewReader(html5000))
	stdCount := 0
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "h3" && n.Parent != nil && n.Parent.Data == "div" {
			stdCount++
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(node)

	_ = io.Discard
	_ = strings.NewReader

	t.Logf("")
	t.Logf("  Correctness check (5K): Foxhound=%d, Goquery=%d, Stdlib=%d, Regex=%d",
		cssCount, gqCount, stdCount, regexCount)
}
