package parse_test

import (
	"testing"
	"time"

	foxhound "github.com/foxhound-scraper/foxhound"
	"github.com/foxhound-scraper/foxhound/parse"
)

const productListHTML = `<!DOCTYPE html>
<html>
<body>
  <div class="product">
    <h2 class="name">Widget A</h2>
    <span class="price">9.99</span>
    <a class="link" href="/products/widget-a">Details</a>
  </div>
  <div class="product">
    <h2 class="name">Widget B</h2>
    <span class="price">19.99</span>
    <a class="link" href="/products/widget-b">Details</a>
  </div>
  <div class="product">
    <h2 class="name">Widget C</h2>
    <span class="price">29.99</span>
    <a class="link" href="/products/widget-c">Details</a>
  </div>
</body>
</html>`

const singleProductHTML = `<!DOCTYPE html>
<html>
<body>
  <h1 class="title">Super Widget</h1>
  <span class="price">49.99</span>
  <a class="buy-link" href="/buy/super-widget">Buy Now</a>
</body>
</html>`

func TestSchema_Extract_AllFields(t *testing.T) {
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(singleProductHTML),
		URL:        "http://example.com/product",
	}

	schema := &parse.Schema{
		Fields: []parse.FieldDef{
			{Name: "title", Selector: "h1.title"},
			{Name: "price", Selector: "span.price"},
			{Name: "buy_url", Selector: "a.buy-link", Attr: "href"},
		},
	}

	item, err := schema.Extract(resp)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if item == nil {
		t.Fatal("Extract returned nil item")
	}

	if v, ok := item.Fields["title"]; !ok || v != "Super Widget" {
		t.Errorf("title: got %v, want %q", v, "Super Widget")
	}
	if v, ok := item.Fields["price"]; !ok || v != "49.99" {
		t.Errorf("price: got %v, want %q", v, "49.99")
	}
	if v, ok := item.Fields["buy_url"]; !ok || v != "/buy/super-widget" {
		t.Errorf("buy_url: got %v, want %q", v, "/buy/super-widget")
	}
}

func TestSchema_Extract_SetsURL(t *testing.T) {
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(singleProductHTML),
		URL:        "http://example.com/product",
	}
	schema := &parse.Schema{
		Fields: []parse.FieldDef{
			{Name: "title", Selector: "h1.title"},
		},
	}

	item, err := schema.Extract(resp)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if item.URL != resp.URL {
		t.Errorf("item.URL: got %q, want %q", item.URL, resp.URL)
	}
}

func TestSchema_Extract_SetsTimestamp(t *testing.T) {
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(singleProductHTML),
		URL:        "http://example.com/product",
	}
	schema := &parse.Schema{
		Fields: []parse.FieldDef{
			{Name: "title", Selector: "h1.title"},
		},
	}

	before := time.Now()
	item, err := schema.Extract(resp)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if item.Timestamp.Before(before) {
		t.Errorf("item.Timestamp %v is before test start %v", item.Timestamp, before)
	}
}

func TestSchema_Extract_RequiredFieldMissing(t *testing.T) {
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(singleProductHTML),
		URL:        "http://example.com/product",
	}
	schema := &parse.Schema{
		Fields: []parse.FieldDef{
			{Name: "missing_field", Selector: ".does-not-exist", Required: true},
		},
	}

	_, err := schema.Extract(resp)
	if err == nil {
		t.Fatal("expected error for missing required field, got nil")
	}
}

func TestSchema_Extract_OptionalFieldMissing(t *testing.T) {
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(singleProductHTML),
		URL:        "http://example.com/product",
	}
	schema := &parse.Schema{
		Fields: []parse.FieldDef{
			{Name: "title", Selector: "h1.title"},
			{Name: "optional_field", Selector: ".does-not-exist", Required: false},
		},
	}

	item, err := schema.Extract(resp)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// optional missing field should be present as empty string
	if v, ok := item.Fields["optional_field"]; !ok || v != "" {
		t.Errorf("optional missing field: got %v (ok=%v), want empty string", v, ok)
	}
}

func TestSchema_ExtractAll_MultipleItems(t *testing.T) {
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(productListHTML),
		URL:        "http://example.com/products",
	}
	schema := &parse.Schema{
		Fields: []parse.FieldDef{
			{Name: "name", Selector: ".name"},
			{Name: "price", Selector: ".price"},
			{Name: "url", Selector: ".link", Attr: "href"},
		},
	}

	items, err := schema.ExtractAll(resp, ".product")
	if err != nil {
		t.Fatalf("ExtractAll: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("ExtractAll: got %d items, want 3", len(items))
	}

	wantNames := []string{"Widget A", "Widget B", "Widget C"}
	wantPrices := []string{"9.99", "19.99", "29.99"}
	wantURLs := []string{"/products/widget-a", "/products/widget-b", "/products/widget-c"}

	for i, item := range items {
		if v := item.Fields["name"]; v != wantNames[i] {
			t.Errorf("item[%d].name: got %q, want %q", i, v, wantNames[i])
		}
		if v := item.Fields["price"]; v != wantPrices[i] {
			t.Errorf("item[%d].price: got %q, want %q", i, v, wantPrices[i])
		}
		if v := item.Fields["url"]; v != wantURLs[i] {
			t.Errorf("item[%d].url: got %q, want %q", i, v, wantURLs[i])
		}
	}
}

func TestSchema_ExtractAll_NoMatchingRoot(t *testing.T) {
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(singleProductHTML),
		URL:        "http://example.com/product",
	}
	schema := &parse.Schema{
		Fields: []parse.FieldDef{
			{Name: "name", Selector: ".name"},
		},
	}

	items, err := schema.ExtractAll(resp, ".nonexistent")
	if err != nil {
		t.Fatalf("ExtractAll: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected empty slice for non-matching root, got %d items", len(items))
	}
}

func TestSchema_ExtractAll_SetsURL(t *testing.T) {
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(productListHTML),
		URL:        "http://example.com/products",
	}
	schema := &parse.Schema{
		Fields: []parse.FieldDef{
			{Name: "name", Selector: ".name"},
		},
	}

	items, err := schema.ExtractAll(resp, ".product")
	if err != nil {
		t.Fatalf("ExtractAll: %v", err)
	}
	for i, item := range items {
		if item.URL != resp.URL {
			t.Errorf("item[%d].URL: got %q, want %q", i, item.URL, resp.URL)
		}
	}
}
