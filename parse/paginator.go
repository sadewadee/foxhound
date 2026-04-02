package parse

import (
	"bytes"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	foxhound "github.com/sadewadee/foxhound"
)

// PaginatedContent represents content assembled from multiple pages.
type PaginatedContent struct {
	Title        string
	Author       string
	URL          string // first page URL
	Pages        []PageContent
	FullText     string
	FullMarkdown string
	TotalPages   int
}

// PageContent represents a single page of paginated content.
type PageContent struct {
	URL      string
	HTML     string
	Text     string
	Markdown string
	PageNum  int
}

// PaginationLink represents a detected pagination link with confidence score.
type PaginationLink struct {
	URL       string
	Text      string
	Score     int
	Direction string // "next" or "prev"
}

var (
	nextPattern       = regexp.MustCompile(`(?i)(next|selanjutnya|berikutnya|continue|lanjut|→|»|>>|›)`)
	prevPattern       = regexp.MustCompile(`(?i)(prev|previous|sebelumnya|back|kembali|←|«|<<|‹)`)
	firstLastPattern  = regexp.MustCompile(`(?i)(first|last|terakhir|pertama)`)
	paginationParent  = regexp.MustCompile(`(?i)(pagination|pager|page-nav|pages|paginate)`)
	negativeParent    = regexp.MustCompile(`(?i)(comment|social|share|related|sidebar)`)
	pageURLPattern    = regexp.MustCompile(`[?&]page=(\d+)|/page/(\d+)`)
)

// DetectPagination analyses the response HTML and returns scored pagination
// links. Links with a score below 50 are discarded. Results are sorted by
// score descending.
func DetectPagination(resp *foxhound.Response) []PaginationLink {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil
	}

	var links []PaginationLink

	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists || href == "" || href == "#" {
			return
		}

		text := strings.TrimSpace(s.Text())

		// Discard links with very long text — unlikely to be pagination.
		if len(text) > 25 {
			return
		}

		score := 0
		direction := "next" // default direction

		// Signal: rel="next" or rel="prev"
		if rel, exists := s.Attr("rel"); exists {
			if strings.Contains(rel, "next") {
				score += 75
			}
			if strings.Contains(rel, "prev") {
				score += 75
				direction = "prev"
			}
		}

		// Signal: text matches next/prev patterns.
		if nextPattern.MatchString(text) {
			score += 50
		}
		if prevPattern.MatchString(text) {
			score += 50
			direction = "prev"
		}

		// Negative signal: first/last.
		if firstLastPattern.MatchString(text) {
			score -= 65
		}

		// Signal: URL matches page pattern.
		if pageURLPattern.MatchString(href) {
			score += 25
		}

		// Signal: parent class/id matches pagination context.
		parentClass, parentID := parentAttrs(s)
		if paginationParent.MatchString(parentClass) || paginationParent.MatchString(parentID) {
			score += 25
		}

		// Negative signal: parent in comment/social/share context.
		if negativeParent.MatchString(parentClass) || negativeParent.MatchString(parentID) {
			score -= 25
		}

		// Penalty for medium-length text (10-25 chars).
		if len(text) > 10 {
			score -= len(text) - 10
		}

		// Bonus for numeric page links (lower numbers get higher score).
		if num, err := strconv.Atoi(text); err == nil {
			score += 10 - num
		}

		if score >= 50 {
			links = append(links, PaginationLink{
				URL:       href,
				Text:      text,
				Score:     score,
				Direction: direction,
			})
		}
	})

	// Sort by score descending.
	sort.Slice(links, func(i, j int) bool {
		return links[i].Score > links[j].Score
	})

	return links
}

// parentAttrs returns the class and id of the nearest parent elements
// (up to 3 levels) concatenated, for context detection.
func parentAttrs(s *goquery.Selection) (class, id string) {
	parent := s.Parent()
	for i := 0; i < 3 && parent.Length() > 0; i++ {
		if c, exists := parent.Attr("class"); exists {
			class += " " + c
		}
		if idVal, exists := parent.Attr("id"); exists {
			id += " " + idVal
		}
		parent = parent.Parent()
	}
	return strings.TrimSpace(class), strings.TrimSpace(id)
}

// AssemblePages combines multiple page responses into a single
// PaginatedContent. If contentSelector is non-empty, it is used to extract
// content from each page; otherwise findMainContent is used as a fallback.
func AssemblePages(pages []*foxhound.Response, contentSelector string) *PaginatedContent {
	if len(pages) == 0 {
		return &PaginatedContent{}
	}

	pc := &PaginatedContent{
		URL:        pages[0].URL,
		TotalPages: len(pages),
	}

	var allTexts []string
	var allMarkdowns []string

	for i, resp := range pages {
		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
		if err != nil {
			continue
		}

		// Extract content selection.
		var sel *goquery.Selection
		if contentSelector != "" {
			sel = doc.Find(contentSelector)
			if sel.Length() == 0 {
				sel = findMainContent(doc)
			}
		} else {
			sel = findMainContent(doc)
		}

		html, _ := goquery.OuterHtml(sel)
		text := normalizeWhitespace(sel.Text())
		md := htmlToMarkdown(sel)

		page := PageContent{
			URL:      resp.URL,
			HTML:     html,
			Text:     text,
			Markdown: md,
			PageNum:  i + 1,
		}
		pc.Pages = append(pc.Pages, page)
		allTexts = append(allTexts, text)
		allMarkdowns = append(allMarkdowns, md)

		// Extract title and author from first page.
		if i == 0 {
			pc.Title = extractPageTitle(doc)
			pc.Author = extractPageAuthor(doc)
		}
	}

	pc.FullText = strings.Join(allTexts, "\n\n")
	pc.FullMarkdown = strings.Join(allMarkdowns, "\n\n")

	return pc
}

// ExtractArticleFromPageBreaks splits a single response on
// `<!-- foxhound:page-break -->` markers and assembles the segments into
// a PaginatedContent.
func ExtractArticleFromPageBreaks(resp *foxhound.Response, contentSelector string) *PaginatedContent {
	body := string(resp.Body)
	segments := strings.Split(body, "<!-- foxhound:page-break -->")

	pc := &PaginatedContent{
		URL:        resp.URL,
		TotalPages: len(segments),
	}

	var allTexts []string
	var allMarkdowns []string

	for i, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}

		segResp := &foxhound.Response{
			Body: []byte(segment),
			URL:  resp.URL,
		}

		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(segResp.Body))
		if err != nil {
			continue
		}

		var sel *goquery.Selection
		if contentSelector != "" {
			sel = doc.Find(contentSelector)
			if sel.Length() == 0 {
				sel = findMainContent(doc)
			}
		} else {
			sel = findMainContent(doc)
		}

		html, _ := goquery.OuterHtml(sel)
		text := normalizeWhitespace(sel.Text())
		md := htmlToMarkdown(sel)

		page := PageContent{
			URL:      resp.URL,
			HTML:     html,
			Text:     text,
			Markdown: md,
			PageNum:  i + 1,
		}
		pc.Pages = append(pc.Pages, page)
		allTexts = append(allTexts, text)
		allMarkdowns = append(allMarkdowns, md)

		// Extract title and author from first segment.
		if i == 0 {
			pc.Title = extractPageTitle(doc)
			pc.Author = extractPageAuthor(doc)
		}
	}

	pc.FullText = strings.Join(allTexts, "\n\n")
	pc.FullMarkdown = strings.Join(allMarkdowns, "\n\n")

	return pc
}

// extractPageTitle gets the page title from <title> or <h1>.
func extractPageTitle(doc *goquery.Document) string {
	if title := strings.TrimSpace(doc.Find("title").First().Text()); title != "" {
		return title
	}
	if h1 := strings.TrimSpace(doc.Find("h1").First().Text()); h1 != "" {
		return h1
	}
	return ""
}

// extractPageAuthor gets the author from meta tags or byline patterns.
func extractPageAuthor(doc *goquery.Document) string {
	// Try meta author tag.
	if author, exists := doc.Find("meta[name='author']").First().Attr("content"); exists && author != "" {
		return author
	}

	// Try common byline class patterns.
	bylineSelectors := []string{
		".author", ".byline", "[rel='author']", ".post-author",
		"[itemprop='author']", ".entry-author",
	}
	for _, sel := range bylineSelectors {
		if text := strings.TrimSpace(doc.Find(sel).First().Text()); text != "" {
			return text
		}
	}

	return ""
}
