package parse

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	foxhound "github.com/sadewadee/foxhound"
)

// ToMarkdown converts the HTML body of a Response to Markdown format.
// It handles common HTML elements: headings, paragraphs, links, lists,
// bold, italic, code blocks, images, blockquotes, and horizontal rules.
//
// Example:
//
//	md := parse.ToMarkdown(resp)
func ToMarkdown(resp *foxhound.Response) string {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return string(resp.Body)
	}
	return htmlToMarkdown(doc.Selection)
}

// ToText converts the HTML body of a Response to plain text.
// All HTML tags are stripped and whitespace is normalized.
//
// Example:
//
//	text := parse.ToText(resp)
func ToText(resp *foxhound.Response) string {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return string(resp.Body)
	}
	// Remove script and style elements.
	doc.Find("script, style, noscript").Remove()
	text := doc.Text()
	return normalizeWhitespace(text)
}

// ExtractMainContent attempts to extract the main content from a page,
// excluding navigation, sidebars, footers, and other boilerplate.
// It looks for common main content containers: <main>, <article>,
// [role="main"], .content, #content, .post-content, .entry-content.
func ExtractMainContent(resp *foxhound.Response) string {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return string(resp.Body)
	}

	// Try common main content selectors in priority order.
	selectors := []string{
		"main",
		"article",
		"[role='main']",
		".post-content",
		".entry-content",
		".article-content",
		".article-body",
		"#content",
		".content",
	}

	for _, sel := range selectors {
		found := doc.Find(sel)
		if found.Length() > 0 {
			// Remove nav, sidebar, footer within the main content.
			found.Find("nav, .sidebar, .footer, .comments, .related").Remove()
			return normalizeWhitespace(found.Text())
		}
	}

	// Fallback: strip scripts/styles and return body text.
	doc.Find("script, style, noscript, nav, header, footer, aside").Remove()
	return normalizeWhitespace(doc.Find("body").Text())
}

// ExtractMainContentMarkdown is like ExtractMainContent but returns Markdown.
func ExtractMainContentMarkdown(resp *foxhound.Response) string {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return string(resp.Body)
	}

	selectors := []string{
		"main", "article", "[role='main']",
		".post-content", ".entry-content", ".article-content",
		"#content", ".content",
	}

	for _, sel := range selectors {
		found := doc.Find(sel)
		if found.Length() > 0 {
			found.Find("nav, .sidebar, .footer, .comments, .related").Remove()
			return htmlToMarkdown(found)
		}
	}

	doc.Find("script, style, noscript, nav, header, footer, aside").Remove()
	return htmlToMarkdown(doc.Find("body"))
}

// ---------------------------------------------------------------------------
// Internal HTML → Markdown conversion
// ---------------------------------------------------------------------------

// htmlToMarkdown converts a goquery Selection tree to Markdown.
func htmlToMarkdown(sel *goquery.Selection) string {
	var b strings.Builder
	convertNode(&b, sel)
	result := b.String()
	// Normalize multiple newlines to double newlines.
	result = collapseNewlines(result)
	return strings.TrimSpace(result)
}

// convertNode recursively converts a goquery Selection to Markdown.
func convertNode(b *strings.Builder, sel *goquery.Selection) {
	sel.Contents().Each(func(_ int, s *goquery.Selection) {
		if goquery.NodeName(s) == "#text" {
			text := s.Text()
			b.WriteString(text)
			return
		}

		tag := goquery.NodeName(s)
		switch tag {
		case "h1":
			b.WriteString("\n\n# ")
			convertNode(b, s)
			b.WriteString("\n\n")
		case "h2":
			b.WriteString("\n\n## ")
			convertNode(b, s)
			b.WriteString("\n\n")
		case "h3":
			b.WriteString("\n\n### ")
			convertNode(b, s)
			b.WriteString("\n\n")
		case "h4":
			b.WriteString("\n\n#### ")
			convertNode(b, s)
			b.WriteString("\n\n")
		case "h5":
			b.WriteString("\n\n##### ")
			convertNode(b, s)
			b.WriteString("\n\n")
		case "h6":
			b.WriteString("\n\n###### ")
			convertNode(b, s)
			b.WriteString("\n\n")
		case "p":
			b.WriteString("\n\n")
			convertNode(b, s)
			b.WriteString("\n\n")
		case "br":
			b.WriteString("\n")
		case "hr":
			b.WriteString("\n\n---\n\n")
		case "a":
			text := strings.TrimSpace(s.Text())
			href, _ := s.Attr("href")
			if href != "" && text != "" {
				fmt.Fprintf(b, "[%s](%s)", text, href)
			} else if text != "" {
				b.WriteString(text)
			}
		case "img":
			alt, _ := s.Attr("alt")
			src, _ := s.Attr("src")
			if src != "" {
				fmt.Fprintf(b, "![%s](%s)", alt, src)
			}
		case "strong", "b":
			b.WriteString("**")
			convertNode(b, s)
			b.WriteString("**")
		case "em", "i":
			b.WriteString("*")
			convertNode(b, s)
			b.WriteString("*")
		case "code":
			// Check if parent is <pre> for code blocks.
			parent := s.Parent()
			if parent.Length() > 0 && goquery.NodeName(parent) == "pre" {
				return // Handled by <pre> case.
			}
			b.WriteString("`")
			b.WriteString(s.Text())
			b.WriteString("`")
		case "pre":
			b.WriteString("\n\n```\n")
			b.WriteString(strings.TrimSpace(s.Text()))
			b.WriteString("\n```\n\n")
		case "blockquote":
			lines := strings.Split(strings.TrimSpace(s.Text()), "\n")
			b.WriteString("\n\n")
			for _, line := range lines {
				b.WriteString("> ")
				b.WriteString(strings.TrimSpace(line))
				b.WriteString("\n")
			}
			b.WriteString("\n")
		case "ul":
			b.WriteString("\n")
			s.Children().Each(func(_ int, li *goquery.Selection) {
				if goquery.NodeName(li) == "li" {
					b.WriteString("- ")
					b.WriteString(strings.TrimSpace(li.Text()))
					b.WriteString("\n")
				}
			})
			b.WriteString("\n")
		case "ol":
			b.WriteString("\n")
			s.Children().Each(func(i int, li *goquery.Selection) {
				if goquery.NodeName(li) == "li" {
					fmt.Fprintf(b, "%d. %s\n", i+1, strings.TrimSpace(li.Text()))
				}
			})
			b.WriteString("\n")
		case "table":
			convertTable(b, s)
		case "script", "style", "noscript":
			// Skip
		case "div", "span", "section", "article", "main", "aside", "nav",
			"header", "footer", "figure", "figcaption", "details", "summary":
			convertNode(b, s)
		default:
			convertNode(b, s)
		}
	})
}

// convertTable converts an HTML table to a Markdown table.
func convertTable(b *strings.Builder, s *goquery.Selection) {
	b.WriteString("\n\n")

	// Extract headers.
	var headers []string
	s.Find("thead th, thead td, tr:first-child th").Each(func(_ int, th *goquery.Selection) {
		headers = append(headers, strings.TrimSpace(th.Text()))
	})

	if len(headers) > 0 {
		b.WriteString("| ")
		b.WriteString(strings.Join(headers, " | "))
		b.WriteString(" |\n|")
		for range headers {
			b.WriteString(" --- |")
		}
		b.WriteString("\n")
	}

	// Extract rows.
	s.Find("tbody tr, tr").Each(func(_ int, tr *goquery.Selection) {
		// Skip if this is the header row.
		if tr.Find("th").Length() > 0 && len(headers) > 0 {
			return
		}
		var cells []string
		tr.Find("td, th").Each(func(_ int, td *goquery.Selection) {
			cells = append(cells, strings.TrimSpace(td.Text()))
		})
		if len(cells) > 0 {
			b.WriteString("| ")
			b.WriteString(strings.Join(cells, " | "))
			b.WriteString(" |\n")
		}
	})
	b.WriteString("\n")
}

// normalizeWhitespace collapses runs of whitespace to single spaces
// and trims leading/trailing whitespace from lines.
func normalizeWhitespace(s string) string {
	// Replace non-breaking spaces with regular spaces.
	s = strings.ReplaceAll(s, "\u00a0", " ")
	// Split into lines, trim each, rejoin.
	lines := strings.Split(s, "\n")
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return strings.Join(result, "\n")
}

var multiNewline = regexp.MustCompile(`\n{3,}`)

// collapseNewlines reduces 3+ consecutive newlines to exactly 2.
func collapseNewlines(s string) string {
	return multiNewline.ReplaceAllString(s, "\n\n")
}
