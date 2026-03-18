package parse_test

import (
	"strings"
	"testing"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/parse"
)

// TestToMarkdown_BasicElements verifies conversion of common HTML to Markdown.
func TestToMarkdown_BasicElements(t *testing.T) {
	html := `<html><body>
		<h1>Title</h1>
		<p>This is a <strong>bold</strong> and <em>italic</em> paragraph.</p>
		<a href="https://example.com">Link</a>
		<ul>
			<li>Item 1</li>
			<li>Item 2</li>
		</ul>
	</body></html>`

	resp := &foxhound.Response{Body: []byte(html)}
	md := parse.ToMarkdown(resp)

	if !strings.Contains(md, "# Title") {
		t.Error("missing '# Title' in markdown output")
	}
	if !strings.Contains(md, "**bold**") {
		t.Error("missing '**bold**' in markdown output")
	}
	if !strings.Contains(md, "*italic*") {
		t.Error("missing '*italic*' in markdown output")
	}
	if !strings.Contains(md, "[Link](https://example.com)") {
		t.Error("missing link in markdown output")
	}
	if !strings.Contains(md, "- Item 1") {
		t.Error("missing '- Item 1' in markdown output")
	}
}

// TestToMarkdown_Headings verifies h1-h6 conversion.
func TestToMarkdown_Headings(t *testing.T) {
	html := `<html><body>
		<h1>H1</h1>
		<h2>H2</h2>
		<h3>H3</h3>
		<h4>H4</h4>
		<h5>H5</h5>
		<h6>H6</h6>
	</body></html>`

	resp := &foxhound.Response{Body: []byte(html)}
	md := parse.ToMarkdown(resp)

	for _, expect := range []string{"# H1", "## H2", "### H3", "#### H4", "##### H5", "###### H6"} {
		if !strings.Contains(md, expect) {
			t.Errorf("missing %q in markdown output", expect)
		}
	}
}

// TestToMarkdown_CodeBlocks verifies pre/code conversion.
func TestToMarkdown_CodeBlocks(t *testing.T) {
	html := `<html><body>
		<p>Inline <code>code</code> here.</p>
		<pre><code>func main() {}</code></pre>
	</body></html>`

	resp := &foxhound.Response{Body: []byte(html)}
	md := parse.ToMarkdown(resp)

	if !strings.Contains(md, "`code`") {
		t.Error("missing inline code in markdown output")
	}
	if !strings.Contains(md, "```") {
		t.Error("missing code block markers in markdown output")
	}
}

// TestToMarkdown_OrderedList verifies ordered list conversion.
func TestToMarkdown_OrderedList(t *testing.T) {
	html := `<html><body><ol><li>First</li><li>Second</li></ol></body></html>`

	resp := &foxhound.Response{Body: []byte(html)}
	md := parse.ToMarkdown(resp)

	if !strings.Contains(md, "1. First") {
		t.Error("missing '1. First' in markdown output")
	}
	if !strings.Contains(md, "2. Second") {
		t.Error("missing '2. Second' in markdown output")
	}
}

// TestToText_StripsHTML verifies plain text extraction.
func TestToText_StripsHTML(t *testing.T) {
	html := `<html>
		<head><style>body { color: red; }</style></head>
		<body>
			<h1>Title</h1>
			<p>Content with <strong>bold</strong> text.</p>
			<script>alert('x')</script>
		</body>
	</html>`

	resp := &foxhound.Response{Body: []byte(html)}
	text := parse.ToText(resp)

	if !strings.Contains(text, "Title") {
		t.Error("missing 'Title' in text output")
	}
	if !strings.Contains(text, "Content with bold text.") {
		t.Error("missing content in text output")
	}
	if strings.Contains(text, "alert") {
		t.Error("script content should be removed")
	}
	if strings.Contains(text, "color: red") {
		t.Error("style content should be removed")
	}
}

// TestExtractMainContent finds the main content area.
func TestExtractMainContent(t *testing.T) {
	html := `<html><body>
		<nav>Navigation links</nav>
		<main>
			<h1>Article Title</h1>
			<p>Article content goes here.</p>
		</main>
		<footer>Footer stuff</footer>
	</body></html>`

	resp := &foxhound.Response{Body: []byte(html)}
	content := parse.ExtractMainContent(resp)

	if !strings.Contains(content, "Article Title") {
		t.Error("missing article title in extracted content")
	}
	if !strings.Contains(content, "Article content goes here") {
		t.Error("missing article content in extracted content")
	}
	// Navigation and footer should be excluded.
	if strings.Contains(content, "Navigation links") {
		t.Error("navigation should be excluded from main content")
	}
}

// TestExtractMainContent_FallbackToBody handles pages without <main>.
func TestExtractMainContent_FallbackToBody(t *testing.T) {
	html := `<html><body>
		<p>Simple page content.</p>
	</body></html>`

	resp := &foxhound.Response{Body: []byte(html)}
	content := parse.ExtractMainContent(resp)

	if !strings.Contains(content, "Simple page content") {
		t.Error("missing content in fallback extraction")
	}
}

// TestExtractMainContentMarkdown returns markdown from main content.
func TestExtractMainContentMarkdown(t *testing.T) {
	html := `<html><body>
		<nav>Nav</nav>
		<article>
			<h1>Heading</h1>
			<p>Paragraph with <strong>bold</strong>.</p>
		</article>
		<footer>Footer</footer>
	</body></html>`

	resp := &foxhound.Response{Body: []byte(html)}
	md := parse.ExtractMainContentMarkdown(resp)

	if !strings.Contains(md, "# Heading") {
		t.Error("missing heading in markdown content")
	}
	if !strings.Contains(md, "**bold**") {
		t.Error("missing bold in markdown content")
	}
}

// TestToMarkdown_Images verifies image conversion.
func TestToMarkdown_Images(t *testing.T) {
	html := `<html><body><img src="/photo.jpg" alt="A photo"></body></html>`

	resp := &foxhound.Response{Body: []byte(html)}
	md := parse.ToMarkdown(resp)

	if !strings.Contains(md, "![A photo](/photo.jpg)") {
		t.Errorf("missing image markdown, got: %s", md)
	}
}

// TestToMarkdown_Blockquote verifies blockquote conversion.
func TestToMarkdown_Blockquote(t *testing.T) {
	html := `<html><body><blockquote>Wise words</blockquote></body></html>`

	resp := &foxhound.Response{Body: []byte(html)}
	md := parse.ToMarkdown(resp)

	if !strings.Contains(md, "> Wise words") {
		t.Errorf("missing blockquote markdown, got: %s", md)
	}
}

// TestToMarkdown_HorizontalRule verifies hr conversion.
func TestToMarkdown_HorizontalRule(t *testing.T) {
	html := `<html><body><p>Above</p><hr><p>Below</p></body></html>`

	resp := &foxhound.Response{Body: []byte(html)}
	md := parse.ToMarkdown(resp)

	if !strings.Contains(md, "---") {
		t.Errorf("missing horizontal rule, got: %s", md)
	}
}

// TestToMarkdown_EmptyBody handles empty response.
func TestToMarkdown_EmptyBody(t *testing.T) {
	resp := &foxhound.Response{Body: []byte("")}
	md := parse.ToMarkdown(resp)
	// Should not panic, returns empty string.
	_ = md
}

// TestToText_EmptyBody handles empty response.
func TestToText_EmptyBody(t *testing.T) {
	resp := &foxhound.Response{Body: []byte("")}
	text := parse.ToText(resp)
	_ = text
}
