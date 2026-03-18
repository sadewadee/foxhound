package parse_test

import (
	"testing"

	"github.com/sadewadee/foxhound/parse"
)

const docExtHTML = `<!DOCTYPE html>
<html>
<head><title>DocExt Test</title></head>
<body>
  <nav>
    <a href="/about" class="nav-link">About</a>
    <a href="/contact" class="nav-link">Contact</a>
    <a href="http://external.com" class="nav-link external">External</a>
  </nav>
  <main>
    <article class="post" data-category="tech">
      <h2>Tech Post</h2>
      <p>Content here.</p>
    </article>
    <article class="post" data-category="design">
      <h2>Design Post</h2>
      <p>More content.</p>
    </article>
    <aside class="sidebar" data-category="tech">
      <p>Sidebar.</p>
    </aside>
  </main>
</body>
</html>`

func newDocExtDoc(t *testing.T) *parse.Document {
	t.Helper()
	// Give the document a realistic base URL
	resp := newHTMLResponse(docExtHTML)
	resp.URL = "http://example.com/blog"
	doc, err := parse.NewDocument(resp)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// --- Document.URLJoin ---

func TestDocument_URLJoin(t *testing.T) {
	doc := newDocExtDoc(t)
	resolved := doc.URLJoin("/about")
	want := "http://example.com/about"
	if resolved != want {
		t.Errorf("Document.URLJoin relative: got %q, want %q", resolved, want)
	}
}

func TestDocument_URLJoin_Absolute(t *testing.T) {
	doc := newDocExtDoc(t)
	abs := "http://external.com/page"
	resolved := doc.URLJoin(abs)
	// An absolute URL should resolve unchanged
	if resolved != abs {
		t.Errorf("Document.URLJoin absolute: got %q, want %q", resolved, abs)
	}
}

func TestDocument_URLJoin_RelativeFile(t *testing.T) {
	doc := newDocExtDoc(t)
	resolved := doc.URLJoin("images/logo.png")
	// Resolves relative to /blog directory -> /images/logo.png
	want := "http://example.com/images/logo.png"
	if resolved != want {
		t.Errorf("Document.URLJoin relative file: got %q, want %q", resolved, want)
	}
}

func TestDocument_URLJoin_EmptyBase(t *testing.T) {
	// Document with no response URL
	resp := newHTMLResponse(docExtHTML)
	resp.URL = ""
	doc, err := parse.NewDocument(resp)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	// When base URL is empty, returns relative as-is
	result := doc.URLJoin("/path")
	if result == "" {
		t.Error("Document.URLJoin empty base: should return at least the relative path")
	}
}

func TestDocument_URLJoin_Fragment(t *testing.T) {
	doc := newDocExtDoc(t)
	resolved := doc.URLJoin("#section")
	want := "http://example.com/blog#section"
	if resolved != want {
		t.Errorf("Document.URLJoin fragment: got %q, want %q", resolved, want)
	}
}

// --- Document.FindAllFunc ---

func TestDocument_FindAllFunc(t *testing.T) {
	doc := newDocExtDoc(t)
	// Find all elements with data-category="tech"
	results := doc.FindAllFunc(func(el *parse.Element) bool {
		return el.Attr("data-category") == "tech"
	})
	if len(results) != 2 {
		t.Errorf("Document.FindAllFunc: want 2, got %d", len(results))
	}
}

func TestDocument_FindAllFunc_NoMatch(t *testing.T) {
	doc := newDocExtDoc(t)
	results := doc.FindAllFunc(func(el *parse.Element) bool {
		return el.Attr("data-category") == "nonexistent"
	})
	if len(results) != 0 {
		t.Errorf("Document.FindAllFunc no-match: want 0, got %d", len(results))
	}
}

func TestDocument_FindAllFunc_ByClass(t *testing.T) {
	doc := newDocExtDoc(t)
	results := doc.FindAllFunc(func(el *parse.Element) bool {
		return el.HasClass("nav-link")
	})
	if len(results) != 3 {
		t.Errorf("Document.FindAllFunc by class: want 3, got %d", len(results))
	}
}

func TestDocument_FindAllFunc_ByTag(t *testing.T) {
	doc := newDocExtDoc(t)
	results := doc.FindAllFunc(func(el *parse.Element) bool {
		return el.Tag() == "article"
	})
	if len(results) != 2 {
		t.Errorf("Document.FindAllFunc by tag: want 2, got %d", len(results))
	}
}

// --- Document.FindByTag ---

func TestDocument_FindByTag(t *testing.T) {
	doc := newDocExtDoc(t)
	results := doc.FindByTag("article")
	if len(results) != 2 {
		t.Errorf("Document.FindByTag: want 2, got %d", len(results))
	}
	for _, el := range results {
		if el.Tag() != "article" {
			t.Errorf("Document.FindByTag: got tag %q, want %q", el.Tag(), "article")
		}
	}
}

func TestDocument_FindByTag_NoMatch(t *testing.T) {
	doc := newDocExtDoc(t)
	results := doc.FindByTag("table")
	if len(results) != 0 {
		t.Errorf("Document.FindByTag no-match: want 0, got %d", len(results))
	}
}

func TestDocument_FindByTag_AllLinks(t *testing.T) {
	doc := newDocExtDoc(t)
	results := doc.FindByTag("a")
	if len(results) != 3 {
		t.Errorf("Document.FindByTag links: want 3, got %d", len(results))
	}
}

// --- Document.FindByTagAndAttr ---

func TestDocument_FindByTagAndAttr(t *testing.T) {
	doc := newDocExtDoc(t)
	results := doc.FindByTagAndAttr("article", map[string]string{
		"data-category": "tech",
	})
	if len(results) != 1 {
		t.Fatalf("Document.FindByTagAndAttr: want 1, got %d", len(results))
	}
	if results[0].Tag() != "article" {
		t.Errorf("Document.FindByTagAndAttr: got tag %q, want %q", results[0].Tag(), "article")
	}
}

func TestDocument_FindByTagAndAttr_MultipleAttrs(t *testing.T) {
	doc := newDocExtDoc(t)
	// External link has both class="nav-link external" and href="http://external.com"
	results := doc.FindByTagAndAttr("a", map[string]string{
		"class": "nav-link external",
	})
	if len(results) != 1 {
		t.Fatalf("Document.FindByTagAndAttr multiple attrs: want 1, got %d", len(results))
	}
}

func TestDocument_FindByTagAndAttr_NoMatch(t *testing.T) {
	doc := newDocExtDoc(t)
	results := doc.FindByTagAndAttr("article", map[string]string{
		"data-category": "nonexistent",
	})
	if len(results) != 0 {
		t.Errorf("Document.FindByTagAndAttr no-match: want 0, got %d", len(results))
	}
}

func TestDocument_FindByTagAndAttr_TagMismatch(t *testing.T) {
	doc := newDocExtDoc(t)
	// The attribute exists but on a different tag
	results := doc.FindByTagAndAttr("section", map[string]string{
		"data-category": "tech",
	})
	if len(results) != 0 {
		t.Errorf("Document.FindByTagAndAttr tag mismatch: want 0, got %d", len(results))
	}
}

func TestDocument_FindByTagAndAttr_EmptyAttrs(t *testing.T) {
	doc := newDocExtDoc(t)
	// Empty attrs map — should match all elements with the given tag
	results := doc.FindByTagAndAttr("article", map[string]string{})
	if len(results) != 2 {
		t.Errorf("Document.FindByTagAndAttr empty attrs: want 2, got %d", len(results))
	}
}
