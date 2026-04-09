// Example: adaptive selectors that survive DOM changes.
//
// Adaptive selectors learn an element signature on first successful match.
// On future runs, even when the producer renames or restructures the
// containing CSS class, similarity matching falls back to the saved
// signature so the scraper keeps working without code changes.
//
// This example demonstrates the inline Response API on a static HTML
// fixture so it can be run without network access:
//
//   - Run 1: original markup with class "product" — selector matches,
//     signature is learned and persisted to ./adaptive_signatures.json
//   - Run 2: same page but the class has been renamed to "item" —
//     CSS selector finds nothing, but Response.Adaptive() falls back to
//     the saved signature and still extracts the price
//
// Run:
//
//	go run ./examples/adaptive/
package main

import (
	"fmt"
	"log"
	"os"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/parse"
)

const originalHTML = `<!DOCTYPE html>
<html><body>
  <h1 class="product-title">Super Widget</h1>
  <div class="product">$10</div>
</body></html>`

const renamedHTML = `<!DOCTYPE html>
<html><body>
  <h1 class="item-name">Super Widget</h1>
  <div class="item">$10</div>
</body></html>`

func main() {
	// Persist learned signatures across program runs. Pass "" for an
	// in-memory only extractor.
	storePath := "./adaptive_signatures.json"
	defer os.Remove(storePath)

	ae := parse.NewAdaptiveExtractor(storePath)

	// --- Run 1: original markup, learn the signature ---
	resp1 := &foxhound.Response{
		Body: []byte(originalHTML),
		URL:  "https://shop.example/p/1",
	}
	resp1.SetAdaptiveExtractor(ae)

	price1 := resp1.CSSAdaptive(".product", "price").Text()
	fmt.Printf("run 1: CSSAdaptive(.product) = %q\n", price1)

	if err := ae.Save(); err != nil {
		log.Fatalf("save signatures: %v", err)
	}

	// --- Run 2: simulate a redesign — class renamed from .product to .item.
	// The primary CSS selector finds nothing, but the learned signature
	// resolves the same element via similarity matching.
	resp2 := &foxhound.Response{
		Body: []byte(renamedHTML),
		URL:  "https://shop.example/p/1",
	}
	resp2.SetAdaptiveExtractor(ae)

	plain := resp2.CSS(".product").Text()
	fmt.Printf("run 2: plain CSS(.product)   = %q  (selector broken)\n", plain)

	adaptive := resp2.Adaptive("price")
	fmt.Printf("run 2: Adaptive(price)       = %q  (recovered via signature)\n", adaptive)

	if adaptive != "$10" {
		log.Fatalf("expected $10, got %q", adaptive)
	}
	fmt.Println("ok: adaptive selector survived class rename")
}
