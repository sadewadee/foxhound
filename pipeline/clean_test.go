package pipeline_test

import (
	"context"
	"testing"

	foxhound "github.com/foxhound-scraper/foxhound"
	"github.com/foxhound-scraper/foxhound/pipeline"
)

func TestClean_TrimWhitespace(t *testing.T) {
	c := &pipeline.Clean{TrimWhitespace: true}
	item := foxhound.NewItem()
	item.Set("name", "  hello world  ")
	item.Set("price", "  $9.99  ")

	result, err := c.Process(context.Background(), item)
	if err != nil {
		t.Fatalf("Clean.Process error: %v", err)
	}
	if result == nil {
		t.Fatal("Clean.Process: expected item, got nil")
	}

	name, _ := result.Get("name")
	if name != "hello world" {
		t.Errorf("TrimWhitespace: got %q, want %q", name, "hello world")
	}
	price, _ := result.Get("price")
	if price != "$9.99" {
		t.Errorf("TrimWhitespace price: got %q, want %q", price, "$9.99")
	}
}

func TestClean_TrimWhitespace_NonStringFieldUnchanged(t *testing.T) {
	c := &pipeline.Clean{TrimWhitespace: true}
	item := foxhound.NewItem()
	item.Set("count", 42)

	result, err := c.Process(context.Background(), item)
	if err != nil {
		t.Fatalf("Clean.Process error: %v", err)
	}
	count, _ := result.Get("count")
	if count != 42 {
		t.Errorf("TrimWhitespace non-string: got %v, want 42", count)
	}
}

func TestClean_StripHTML(t *testing.T) {
	c := &pipeline.Clean{StripHTML: true}
	item := foxhound.NewItem()
	item.Set("body", "<p>Hello <b>world</b></p>")
	item.Set("title", "<h1>Title</h1>")

	result, err := c.Process(context.Background(), item)
	if err != nil {
		t.Fatalf("Clean.Process error: %v", err)
	}

	body, _ := result.Get("body")
	if body != "Hello world" {
		t.Errorf("StripHTML body: got %q, want %q", body, "Hello world")
	}
	title, _ := result.Get("title")
	if title != "Title" {
		t.Errorf("StripHTML title: got %q, want %q", title, "Title")
	}
}

func TestClean_StripHTML_PlainTextUnchanged(t *testing.T) {
	c := &pipeline.Clean{StripHTML: true}
	item := foxhound.NewItem()
	item.Set("text", "just plain text")

	result, err := c.Process(context.Background(), item)
	if err != nil {
		t.Fatalf("Clean.Process error: %v", err)
	}
	text, _ := result.Get("text")
	if text != "just plain text" {
		t.Errorf("StripHTML plain: got %q, want %q", text, "just plain text")
	}
}

func TestClean_NormalizePrice_DollarSign(t *testing.T) {
	c := &pipeline.Clean{NormalizePrice: true}
	item := foxhound.NewItem()
	item.Set("price", "$1,234.56")

	result, err := c.Process(context.Background(), item)
	if err != nil {
		t.Fatalf("Clean.Process error: %v", err)
	}
	price, _ := result.Get("price")
	if price != 1234.56 {
		t.Errorf("NormalizePrice dollar: got %v (%T), want 1234.56", price, price)
	}
}

func TestClean_NormalizePrice_EuroSign(t *testing.T) {
	c := &pipeline.Clean{NormalizePrice: true}
	item := foxhound.NewItem()
	item.Set("price", "€999.00")

	result, err := c.Process(context.Background(), item)
	if err != nil {
		t.Fatalf("Clean.Process error: %v", err)
	}
	price, _ := result.Get("price")
	if price != 999.00 {
		t.Errorf("NormalizePrice euro: got %v, want 999.00", price)
	}
}

func TestClean_NormalizePrice_PoundSign(t *testing.T) {
	c := &pipeline.Clean{NormalizePrice: true}
	item := foxhound.NewItem()
	item.Set("price", "£42.50")

	result, err := c.Process(context.Background(), item)
	if err != nil {
		t.Fatalf("Clean.Process error: %v", err)
	}
	price, _ := result.Get("price")
	if price != 42.50 {
		t.Errorf("NormalizePrice pound: got %v, want 42.50", price)
	}
}

func TestClean_NormalizePrice_NoPrefix(t *testing.T) {
	c := &pipeline.Clean{NormalizePrice: true}
	item := foxhound.NewItem()
	item.Set("price", "19.99")

	result, err := c.Process(context.Background(), item)
	if err != nil {
		t.Fatalf("Clean.Process error: %v", err)
	}
	price, _ := result.Get("price")
	if price != 19.99 {
		t.Errorf("NormalizePrice no prefix: got %v, want 19.99", price)
	}
}

func TestClean_NormalizeDate_LongForm(t *testing.T) {
	c := &pipeline.Clean{NormalizeDate: true}
	item := foxhound.NewItem()
	item.Set("date", "March 18, 2026")

	result, err := c.Process(context.Background(), item)
	if err != nil {
		t.Fatalf("Clean.Process error: %v", err)
	}
	date, _ := result.Get("date")
	if date != "2026-03-18" {
		t.Errorf("NormalizeDate long: got %q, want %q", date, "2026-03-18")
	}
}

func TestClean_NormalizeDate_ISO(t *testing.T) {
	c := &pipeline.Clean{NormalizeDate: true}
	item := foxhound.NewItem()
	item.Set("date", "2026-03-18")

	result, err := c.Process(context.Background(), item)
	if err != nil {
		t.Fatalf("Clean.Process error: %v", err)
	}
	date, _ := result.Get("date")
	if date != "2026-03-18" {
		t.Errorf("NormalizeDate ISO: got %q, want %q", date, "2026-03-18")
	}
}

func TestClean_NormalizeDate_SlashFormat(t *testing.T) {
	c := &pipeline.Clean{NormalizeDate: true}
	item := foxhound.NewItem()
	item.Set("date", "03/18/2026")

	result, err := c.Process(context.Background(), item)
	if err != nil {
		t.Fatalf("Clean.Process error: %v", err)
	}
	date, _ := result.Get("date")
	if date != "2026-03-18" {
		t.Errorf("NormalizeDate slash: got %q, want %q", date, "2026-03-18")
	}
}

func TestClean_NormalizeDate_DayMonYear(t *testing.T) {
	c := &pipeline.Clean{NormalizeDate: true}
	item := foxhound.NewItem()
	item.Set("date", "18 Mar 2026")

	result, err := c.Process(context.Background(), item)
	if err != nil {
		t.Fatalf("Clean.Process error: %v", err)
	}
	date, _ := result.Get("date")
	if date != "2026-03-18" {
		t.Errorf("NormalizeDate day-mon-year: got %q, want %q", date, "2026-03-18")
	}
}

func TestClean_NormalizeDate_UnparsableLeftAsIs(t *testing.T) {
	c := &pipeline.Clean{NormalizeDate: true}
	item := foxhound.NewItem()
	item.Set("date", "not-a-date")

	result, err := c.Process(context.Background(), item)
	if err != nil {
		t.Fatalf("Clean.Process error: %v", err)
	}
	date, _ := result.Get("date")
	if date != "not-a-date" {
		t.Errorf("NormalizeDate unparsable: got %q, want %q", date, "not-a-date")
	}
}

func TestClean_AllOptions_Combined(t *testing.T) {
	c := &pipeline.Clean{
		TrimWhitespace: true,
		StripHTML:      true,
		NormalizePrice: true,
		NormalizeDate:  true,
	}
	item := foxhound.NewItem()
	item.Set("title", "  <b>Product</b>  ")
	item.Set("price", "  $19.99  ")
	item.Set("date", "January 2, 2006")

	result, err := c.Process(context.Background(), item)
	if err != nil {
		t.Fatalf("Clean.Process combined error: %v", err)
	}

	title, _ := result.Get("title")
	if title != "Product" {
		t.Errorf("Combined title: got %q, want %q", title, "Product")
	}
	// Price after trim and normalize
	price, _ := result.Get("price")
	if price != 19.99 {
		t.Errorf("Combined price: got %v, want 19.99", price)
	}
	date, _ := result.Get("date")
	if date != "2006-01-02" {
		t.Errorf("Combined date: got %q, want %q", date, "2006-01-02")
	}
}
