package foxhound_test

import (
	"testing"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/parse"
)

const productOriginalHTML = `<!DOCTYPE html><html><body>
  <div class="product">$10</div>
</body></html>`

const productRenamedHTML = `<!DOCTYPE html><html><body>
  <div class="item">$10</div>
</body></html>`

// TestResponse_CSSAdaptive_SurvivesClassRename verifies the headline
// adaptive-selector behaviour: a selector learned on the first page still
// matches after the producer renames the CSS class on the next run.
func TestResponse_CSSAdaptive_SurvivesClassRename(t *testing.T) {
	ae := parse.NewAdaptiveExtractor("")

	// First run: original markup, register & learn signature.
	first := &foxhound.Response{Body: []byte(productOriginalHTML), URL: "https://shop.test/p/1"}
	first.SetAdaptiveExtractor(ae)
	if got := first.CSSAdaptive(".product", "price").Text(); got != "$10" {
		t.Fatalf("first run CSSAdaptive: got %q, want %q", got, "$10")
	}

	// Second run: class renamed from .product to .item. The primary CSS
	// selector finds nothing, but Adaptive() should fall back to the
	// learned signature and still resolve "$10".
	second := &foxhound.Response{Body: []byte(productRenamedHTML), URL: "https://shop.test/p/1"}
	second.SetAdaptiveExtractor(ae)
	if got := second.Adaptive("price"); got != "$10" {
		t.Errorf("after class rename, Adaptive(price) = %q, want %q", got, "$10")
	}
}

func TestResponse_Adaptive_NoExtractor_ReturnsEmpty(t *testing.T) {
	resp := &foxhound.Response{Body: []byte(productOriginalHTML)}
	if got := resp.Adaptive("anything"); got != "" {
		t.Errorf("Adaptive without extractor: got %q, want empty", got)
	}
}

func TestResponse_CSSAdaptiveAll_RegistersSelector(t *testing.T) {
	ae := parse.NewAdaptiveExtractor("")
	resp := &foxhound.Response{Body: []byte(productOriginalHTML)}
	resp.SetAdaptiveExtractor(ae)
	sel := resp.CSSAdaptiveAll(".product", "prices")
	if sel == nil {
		t.Fatal("CSSAdaptiveAll returned nil Selection")
	}
	if got := sel.Text(); got != "$10" {
		t.Errorf("CSSAdaptiveAll Text: got %q, want %q", got, "$10")
	}
}
