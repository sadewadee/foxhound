package parse_test

import (
	"strings"
	"testing"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/parse"
)

// ─── DetectPagination ─────────────────────────────────────────────────────────

func TestDetectPagination_RelNext(t *testing.T) {
	html := `<html><body>
	<nav class="pagination">
		<a rel="next" href="/page/2">Next</a>
	</nav>
	</body></html>`

	resp := makeResp(html)
	links := parse.DetectPagination(resp)
	if len(links) == 0 {
		t.Fatal("expected at least 1 pagination link")
	}

	found := false
	for _, l := range links {
		if l.URL == "/page/2" && l.Direction == "next" {
			found = true
			if l.Score < 50 {
				t.Errorf("score %d too low for rel=next link", l.Score)
			}
			break
		}
	}
	if !found {
		t.Error("rel=next link not detected")
	}
}

func TestDetectPagination_TextPattern(t *testing.T) {
	html := `<html><body>
	<div class="pagination">
		<a href="/page/2">Next →</a>
	</div>
	</body></html>`

	resp := makeResp(html)
	links := parse.DetectPagination(resp)
	if len(links) == 0 {
		t.Fatal("expected at least 1 pagination link")
	}

	found := false
	for _, l := range links {
		if l.URL == "/page/2" {
			found = true
			if l.Score < 50 {
				t.Errorf("score %d should be >= 50", l.Score)
			}
			break
		}
	}
	if !found {
		t.Error("text-based next link not detected")
	}
}

func TestDetectPagination_URLPattern(t *testing.T) {
	html := `<html><body>
	<div class="pagination">
		<a href="/page/2">Next</a>
	</div>
	</body></html>`

	resp := makeResp(html)
	links := parse.DetectPagination(resp)
	if len(links) == 0 {
		t.Fatal("expected at least 1 pagination link")
	}

	for _, l := range links {
		if l.URL == "/page/2" {
			// Should get bonus from URL pattern + text pattern + pagination parent.
			if l.Score < 50 {
				t.Errorf("score %d too low for URL+text pattern", l.Score)
			}
			return
		}
	}
	t.Error("URL pattern link not found")
}

func TestDetectPagination_PrevLink(t *testing.T) {
	html := `<html><body>
	<div class="pagination">
		<a href="/page/1">← Previous</a>
	</div>
	</body></html>`

	resp := makeResp(html)
	links := parse.DetectPagination(resp)
	if len(links) == 0 {
		t.Fatal("expected at least 1 pagination link")
	}

	for _, l := range links {
		if l.URL == "/page/1" {
			if l.Direction != "prev" {
				t.Errorf("direction = %q, want %q", l.Direction, "prev")
			}
			return
		}
	}
	t.Error("prev link not detected")
}

func TestDetectPagination_IndonesianText(t *testing.T) {
	html := `<html><body>
	<div class="pagination">
		<a href="/page/1">Sebelumnya</a>
		<a href="/page/3">Selanjutnya</a>
	</div>
	</body></html>`

	resp := makeResp(html)
	links := parse.DetectPagination(resp)

	var foundNext, foundPrev bool
	for _, l := range links {
		if l.URL == "/page/3" && l.Direction == "next" {
			foundNext = true
		}
		if l.URL == "/page/1" && l.Direction == "prev" {
			foundPrev = true
		}
	}

	if !foundNext {
		t.Error("Indonesian next link (Selanjutnya) not detected")
	}
	if !foundPrev {
		t.Error("Indonesian prev link (Sebelumnya) not detected")
	}
}

func TestDetectPagination_NoPagination(t *testing.T) {
	html := `<html><body>
	<h1>Simple Article</h1>
	<p>No pagination here.</p>
	<a href="/about">About us</a>
	</body></html>`

	resp := makeResp(html)
	links := parse.DetectPagination(resp)
	if len(links) != 0 {
		t.Errorf("expected 0 pagination links, got %d: %+v", len(links), links)
	}
}

func TestDetectPagination_NegativeParent(t *testing.T) {
	html := `<html><body>
	<div class="social">
		<a href="/more">Next</a>
	</div>
	</body></html>`

	resp := makeResp(html)
	links := parse.DetectPagination(resp)

	// The link in .social has +50 (text "Next") - 25 (negativeParent) = 25,
	// which is below the 50 threshold. It should be filtered out.
	for _, l := range links {
		if l.URL == "/more" {
			t.Errorf("link in .social container should have been filtered, score=%d", l.Score)
		}
	}
}

func TestDetectPagination_NumberedLinks(t *testing.T) {
	html := `<html><body>
	<div class="pagination">
		<a href="/page/1">1</a>
		<a href="/page/2">2</a>
		<a href="/page/3">3</a>
	</div>
	</body></html>`

	resp := makeResp(html)
	links := parse.DetectPagination(resp)

	// Numbered links in a pagination container get:
	//   +25 (URL pattern) + 25 (pagination parent) + (10-num) numeric bonus.
	// Page 1: 25+25+9 = 59 >= 50
	// Page 2: 25+25+8 = 58 >= 50
	// Page 3: 25+25+7 = 57 >= 50
	if len(links) < 3 {
		t.Errorf("expected at least 3 numbered links, got %d", len(links))
	}
}

// ─── AssemblePages ────────────────────────────────────────────────────────────

func TestAssemblePages_ThreePages(t *testing.T) {
	pages := []*foxhound.Response{
		{
			URL: "http://example.com/article?page=1",
			Body: []byte(`<html><head><title>My Article</title>
			<meta name="author" content="Jane Doe">
			</head><body>
			<article><p>Page one content.</p></article>
			</body></html>`),
		},
		{
			URL: "http://example.com/article?page=2",
			Body: []byte(`<html><body>
			<article><p>Page two content.</p></article>
			</body></html>`),
		},
		{
			URL: "http://example.com/article?page=3",
			Body: []byte(`<html><body>
			<article><p>Page three content.</p></article>
			</body></html>`),
		},
	}

	pc := parse.AssemblePages(pages, "article")
	if pc == nil {
		t.Fatal("AssemblePages returned nil")
	}
	if pc.TotalPages != 3 {
		t.Errorf("TotalPages = %d, want 3", pc.TotalPages)
	}
	if pc.Title != "My Article" {
		t.Errorf("Title = %q, want %q", pc.Title, "My Article")
	}
	if pc.Author != "Jane Doe" {
		t.Errorf("Author = %q, want %q", pc.Author, "Jane Doe")
	}
	if pc.URL != "http://example.com/article?page=1" {
		t.Errorf("URL = %q, want first page URL", pc.URL)
	}
	if len(pc.Pages) != 3 {
		t.Fatalf("Pages count = %d, want 3", len(pc.Pages))
	}

	if !strings.Contains(pc.FullText, "Page one content") {
		t.Error("FullText missing page one content")
	}
	if !strings.Contains(pc.FullText, "Page two content") {
		t.Error("FullText missing page two content")
	}
	if !strings.Contains(pc.FullText, "Page three content") {
		t.Error("FullText missing page three content")
	}

	if pc.Pages[0].PageNum != 1 || pc.Pages[1].PageNum != 2 || pc.Pages[2].PageNum != 3 {
		t.Error("page numbers not set correctly")
	}
}

// ─── ExtractArticleFromPageBreaks ─────────────────────────────────────────────

func TestExtractArticleFromPageBreaks(t *testing.T) {
	html := `<html><head><title>Multi-Part Article</title></head><body>
	<article><p>First section content.</p></article>
	</body></html>
	<!-- foxhound:page-break -->
	<html><body>
	<article><p>Second section content.</p></article>
	</body></html>
	<!-- foxhound:page-break -->
	<html><body>
	<article><p>Third section content.</p></article>
	</body></html>`

	resp := &foxhound.Response{
		URL:  "http://example.com/article",
		Body: []byte(html),
	}

	pc := parse.ExtractArticleFromPageBreaks(resp, "article")
	if pc == nil {
		t.Fatal("returned nil")
	}
	if pc.TotalPages != 3 {
		t.Errorf("TotalPages = %d, want 3", pc.TotalPages)
	}
	if pc.Title != "Multi-Part Article" {
		t.Errorf("Title = %q, want %q", pc.Title, "Multi-Part Article")
	}
	if len(pc.Pages) != 3 {
		t.Fatalf("Pages count = %d, want 3", len(pc.Pages))
	}
	if !strings.Contains(pc.FullText, "First section content") {
		t.Error("FullText missing first section")
	}
	if !strings.Contains(pc.FullText, "Second section content") {
		t.Error("FullText missing second section")
	}
	if !strings.Contains(pc.FullText, "Third section content") {
		t.Error("FullText missing third section")
	}
}
