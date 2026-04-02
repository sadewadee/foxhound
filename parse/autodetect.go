package parse

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	foxhound "github.com/sadewadee/foxhound"
)

// ContentType classifies the type of content on a web page.
type ContentType int

const (
	ContentUnknown ContentType = iota
	ContentArticle
	ContentListing
	ContentProduct
	ContentSearch
	ContentFeed
)

// String returns a human-readable name for the content type.
func (ct ContentType) String() string {
	switch ct {
	case ContentArticle:
		return "article"
	case ContentListing:
		return "listing"
	case ContentProduct:
		return "product"
	case ContentSearch:
		return "search"
	case ContentFeed:
		return "feed"
	default:
		return "unknown"
	}
}

// AutoResult contains the auto-extracted content based on detected page type.
type AutoResult struct {
	Type     ContentType
	Article  *Article         // populated when Type == ContentArticle
	Listings []Listing        // populated when Type == ContentListing
	Items    []*foxhound.Item // generic extraction for other types
}

// Article represents extracted article content with metadata.
type Article struct {
	Title           string
	Author          string
	PublishedDate   string
	Content         string        // cleaned HTML of main content
	ContentText     string        // plain text
	ContentMarkdown string        // markdown
	Summary         string        // first ~200 chars of text
	Images          []string
	Tags            []string
	WordCount       int
	ReadingTime     time.Duration // based on 200 wpm
	Score           float64       // readability confidence score
}

// ---------------------------------------------------------------------------
// Package-level compiled regexes
// ---------------------------------------------------------------------------

var (
	cardPattern      = regexp.MustCompile(`(?i)(card|item|entry|listing|result|product)`)
	pricePattern     = regexp.MustCompile(`(?i)(\$|€|£|¥|IDR|Rp)\s*[\d,.]+|[\d,.]+\s*(USD|EUR|GBP)`)
	cartPattern      = regexp.MustCompile(`(?i)(add.to.cart|buy.now|add.to.bag|beli|checkout)`)
	timestampPattern = regexp.MustCompile(`(?i)((\d{4}[-/]\d{2}[-/]\d{2})|(\d{1,2}\s+(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)\w*\s+\d{4})|(ago|lalu|yang lalu))`)
)

// ---------------------------------------------------------------------------
// DetectContentType — 7-factor heuristic
// ---------------------------------------------------------------------------

// DetectContentType analyses the HTML in resp and returns the most likely
// content type using structural signals, JSON-LD types, and text analysis.
func DetectContentType(resp *foxhound.Response) ContentType {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return ContentUnknown
	}

	// Count structural signals.
	headings := doc.Find("h1, h2, h3").Length()
	listItems := doc.Find("li").Length()
	links := doc.Find("a[href]").Length()
	images := doc.Find("img").Length()
	articleTags := doc.Find("article").Length()

	// Count card-like elements.
	cards := 0
	doc.Find("[class]").Each(func(_ int, s *goquery.Selection) {
		cls, _ := s.Attr("class")
		if cardPattern.MatchString(cls) {
			cards++
		}
	})

	// Check JSON-LD for typed content.
	jsonldType := detectJSONLDType(resp)

	// 1. Listing signals (check first — more specific).
	if listItems > 10 || cards > 5 {
		return ContentListing
	}
	if links > 50 && images > 20 {
		return ContentListing
	}
	if jsonldType == "listing" {
		return ContentListing
	}

	// 2. Product signals.
	if jsonldType == "product" {
		return ContentProduct
	}
	if hasProductSignals(doc) {
		return ContentProduct
	}

	// 3. Search signals.
	if hasSearchSignals(doc, cards, resp) {
		return ContentSearch
	}

	// 4. Feed signals.
	if hasFeedSignals(doc) {
		return ContentFeed
	}

	// 5. Article signals.
	mainContent := findMainContent(doc)
	if mainContent != nil {
		text := strings.TrimSpace(mainContent.Text())
		textLen := len(text)
		linkDensity := computeLinkDensity(mainContent)

		if textLen >= 500 && linkDensity <= 0.3 && headings >= 1 {
			return ContentArticle
		}
		if articleTags > 0 && textLen >= 200 {
			return ContentArticle
		}
	}

	return ContentUnknown
}

// ---------------------------------------------------------------------------
// DetectContentType helpers
// ---------------------------------------------------------------------------

func detectJSONLDType(resp *foxhound.Response) string {
	items, err := ExtractJSONLD(resp)
	if err != nil || len(items) == 0 {
		return ""
	}
	for _, item := range items {
		t, _ := item["@type"].(string)
		if businessTypes[t] {
			return "listing"
		}
		if t == "Product" || t == "Offer" {
			return "product"
		}
	}
	// Check for arrays of typed items.
	if len(items) > 2 {
		return "listing"
	}
	return ""
}

func hasProductSignals(doc *goquery.Document) bool {
	// Check for price patterns in text.
	body := doc.Find("body").Text()
	hasPrice := pricePattern.MatchString(body)

	// Check for add-to-cart buttons.
	hasCart := false
	doc.Find("button, input[type='submit'], a").Each(func(_ int, s *goquery.Selection) {
		text := s.Text()
		cls, _ := s.Attr("class")
		if cartPattern.MatchString(text) || cartPattern.MatchString(cls) {
			hasCart = true
		}
	})

	return hasPrice && hasCart
}

func hasSearchSignals(doc *goquery.Document, cards int, resp *foxhound.Response) bool {
	hasForm := doc.Find("form[action], input[type='search'], [role='search']").Length() > 0
	hasPagination := len(DetectPagination(resp)) > 0
	return hasForm && cards > 3 && hasPagination
}

func hasFeedSignals(doc *goquery.Document) bool {
	// Check for infinite scroll containers or feed-like structure.
	hasFeed := doc.Find("[data-infinite-scroll], [class*='feed'], [role='feed']").Length() > 0

	// Count timestamps.
	timestamps := 0
	doc.Find("time, [datetime]").Each(func(_ int, _ *goquery.Selection) {
		timestamps++
	})
	if timestamps == 0 {
		// Check text patterns.
		body := doc.Find("body").Text()
		matches := timestampPattern.FindAllString(body, -1)
		timestamps = len(matches)
	}

	return hasFeed && timestamps > 3
}

func computeLinkDensity(sel *goquery.Selection) float64 {
	text := strings.TrimSpace(sel.Text())
	if len(text) == 0 {
		return 1.0
	}
	linkText := 0
	sel.Find("a").Each(func(_ int, a *goquery.Selection) {
		linkText += len(strings.TrimSpace(a.Text()))
	})
	return float64(linkText) / float64(len(text))
}

// ---------------------------------------------------------------------------
// AutoExtract
// ---------------------------------------------------------------------------

// AutoExtract detects the page content type and extracts content accordingly.
// For articles it populates AutoResult.Article, for listings AutoResult.Listings,
// and for other types it returns a generic item with the main content.
func AutoExtract(resp *foxhound.Response) (*AutoResult, error) {
	contentType := DetectContentType(resp)
	result := &AutoResult{Type: contentType}

	switch contentType {
	case ContentArticle:
		article, err := ExtractArticle(resp)
		if err != nil {
			return result, err
		}
		result.Article = article

	case ContentListing:
		listings, err := ExtractListings(resp)
		if err != nil {
			return result, err
		}
		result.Listings = listings

	default:
		// Generic: extract main content as a single item.
		text := ExtractMainContent(resp)
		if text != "" {
			item := foxhound.NewItem()
			item.URL = resp.URL
			item.Set("content", text)
			item.Set("content_type", contentType.String())
			result.Items = append(result.Items, item)
		}
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// ExtractArticle — readability-style scoring
// ---------------------------------------------------------------------------

const unlikelySelector = "div, section, aside, header, footer, nav, form"

var (
	unlikelyPattern = regexp.MustCompile(`(?i)(sidebar|comment|com-|footer|footnote|masthead|media|meta|outbrain|promo|related|scroll|share|shoutbox|sponsor|shopping|tags|tool|widget|banner|ad-break|agegate|pagination|pager|popup|social)`)
	overridePattern = regexp.MustCompile(`(?i)(and|article|body|column|main|shadow|content)`)
	positivePattern = regexp.MustCompile(`(?i)(article|body|content|entry|hentry|h-entry|main|page|post|text|blog|story)`)
	negativePattern = regexp.MustCompile(`(?i)(hidden|banner|combx|comment|com-|contact|footer|footnote|masthead|media|meta|outbrain|promo|related|scroll|share|shoutbox|sidebar|skyscraper|sponsor|shopping|tags|tool|widget|ad|ads)`)
	bylinePattern   = regexp.MustCompile(`(?i)(byline|author|dateline|writtenby|posted-by)`)
)

// ExtractArticle applies a readability-style scoring algorithm to extract the
// primary article content, metadata, and computed statistics from a response.
func ExtractArticle(resp *foxhound.Response) (*Article, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, fmt.Errorf("parse: autodetect: %w", err)
	}

	// 1. Remove unlikely candidates.
	doc.Find(unlikelySelector).Each(func(_ int, s *goquery.Selection) {
		cls, _ := s.Attr("class")
		id, _ := s.Attr("id")
		combined := cls + " " + id
		if unlikelyPattern.MatchString(combined) && !overridePattern.MatchString(combined) {
			s.Remove()
		}
	})

	// 2. Score all candidate nodes.
	scores := make(map[*goquery.Selection]float64)

	doc.Find("p, td, pre, blockquote, div").Each(func(_ int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if len(text) < 25 {
			return
		}

		// Base score from tag type.
		parent := s.Parent()
		if parent.Length() == 0 {
			return
		}

		initScore := tagScore(parent)
		initScore += classWeight(parent)

		if _, exists := scores[parent]; !exists {
			scores[parent] = float64(initScore)
		}

		// Paragraph scoring: 1 base + commas + length bonus.
		score := 1.0
		score += float64(strings.Count(text, ","))
		score += math.Min(float64(len(text))/100.0, 3.0)

		// Propagate: parent gets full, grandparent gets half.
		scores[parent] += score
		grandparent := parent.Parent()
		if grandparent.Length() > 0 {
			if _, exists := scores[grandparent]; !exists {
				scores[grandparent] = float64(tagScore(grandparent) + classWeight(grandparent))
			}
			scores[grandparent] += score / 2.0
		}
	})

	// 3. Apply link density penalty and find top candidate.
	var topCandidate *goquery.Selection
	topScore := 0.0
	for sel, score := range scores {
		linkDens := computeLinkDensity(sel)
		finalScore := score * (1.0 - linkDens)
		scores[sel] = finalScore
		if finalScore > topScore {
			topScore = finalScore
			topCandidate = sel
		}
	}

	if topCandidate == nil {
		// Fallback to findMainContent.
		mainContent := findMainContent(doc)
		if mainContent == nil {
			return nil, nil
		}
		topCandidate = mainContent
		topScore = 0
	}

	// 4. Build Article from top candidate.
	contentHTML, _ := topCandidate.Html()
	contentText := strings.TrimSpace(topCandidate.Text())
	contentMD := htmlToMarkdown(topCandidate)

	// Extract metadata.
	title := extractTitle(doc)
	author := extractAuthor(doc)
	publishedDate := extractPublishedDate(doc)
	images := extractImages(topCandidate, resp.URL)
	tags := extractTags(doc)

	// Word count and reading time.
	words := len(strings.Fields(contentText))
	readingTime := time.Duration(float64(words)/200.0*60.0) * time.Second

	// Summary.
	summary := contentText
	if len(summary) > 200 {
		// Cut at word boundary.
		idx := strings.LastIndex(summary[:200], " ")
		if idx > 100 {
			summary = summary[:idx] + "..."
		} else {
			summary = summary[:200] + "..."
		}
	}

	return &Article{
		Title:           title,
		Author:          author,
		PublishedDate:   publishedDate,
		Content:         contentHTML,
		ContentText:     contentText,
		ContentMarkdown: contentMD,
		Summary:         summary,
		Images:          images,
		Tags:            tags,
		WordCount:       words,
		ReadingTime:     readingTime,
		Score:           topScore,
	}, nil
}

// ---------------------------------------------------------------------------
// ExtractArticle helpers
// ---------------------------------------------------------------------------

func tagScore(sel *goquery.Selection) int {
	tag := goquery.NodeName(sel)
	switch tag {
	case "div":
		return 5
	case "pre", "td", "blockquote":
		return 3
	case "address", "ol", "ul", "dl", "dd", "dt", "li", "form":
		return -3
	case "h1", "h2", "h3", "h4", "h5", "h6", "th":
		return -5
	default:
		return 0
	}
}

func classWeight(sel *goquery.Selection) int {
	weight := 0
	cls, _ := sel.Attr("class")
	id, _ := sel.Attr("id")
	combined := cls + " " + id

	if positivePattern.MatchString(combined) {
		weight += 25
	}
	if negativePattern.MatchString(combined) {
		weight -= 25
	}
	return weight
}

func extractTitle(doc *goquery.Document) string {
	// Try og:title first (usually cleanest).
	if og, exists := doc.Find("meta[property='og:title']").Attr("content"); exists && og != "" {
		return strings.TrimSpace(og)
	}
	// Try h1.
	if h1 := doc.Find("h1").First().Text(); h1 != "" {
		return strings.TrimSpace(h1)
	}
	// Fallback to <title> with separator cleanup.
	title := doc.Find("title").Text()
	for _, sep := range []string{" | ", " - ", " — ", " :: ", " » "} {
		if idx := strings.LastIndex(title, sep); idx > 0 {
			candidate := strings.TrimSpace(title[:idx])
			if len(candidate) > 10 {
				return candidate
			}
		}
	}
	return strings.TrimSpace(title)
}

func extractAuthor(doc *goquery.Document) string {
	// Try meta tags.
	if author, exists := doc.Find("meta[name='author']").Attr("content"); exists && author != "" {
		return strings.TrimSpace(author)
	}
	if author, exists := doc.Find("meta[property='article:author']").Attr("content"); exists && author != "" {
		return strings.TrimSpace(author)
	}
	// Try byline patterns.
	var author string
	doc.Find("[class], [id], [rel='author']").Each(func(_ int, s *goquery.Selection) {
		if author != "" {
			return
		}
		cls, _ := s.Attr("class")
		id, _ := s.Attr("id")
		rel, _ := s.Attr("rel")
		if bylinePattern.MatchString(cls+" "+id) || rel == "author" {
			text := strings.TrimSpace(s.Text())
			if len(text) > 0 && len(text) < 100 {
				author = text
			}
		}
	})
	return author
}

func extractPublishedDate(doc *goquery.Document) string {
	// Try <time> element.
	if dt, exists := doc.Find("time[datetime]").First().Attr("datetime"); exists {
		return dt
	}
	// Try meta tags.
	for _, sel := range []string{
		"meta[property='article:published_time']",
		"meta[property='og:published_time']",
		"meta[name='date']",
		"meta[name='DC.date']",
	} {
		if dt, exists := doc.Find(sel).Attr("content"); exists && dt != "" {
			return dt
		}
	}
	return ""
}

func extractImages(sel *goquery.Selection, baseURL string) []string {
	var images []string
	seen := make(map[string]bool)
	sel.Find("img[src]").Each(func(_ int, img *goquery.Selection) {
		src, _ := img.Attr("src")
		if src != "" && !seen[src] {
			seen[src] = true
			images = append(images, src)
		}
	})
	return images
}

func extractTags(doc *goquery.Document) []string {
	var tags []string
	seen := make(map[string]bool)
	// Try article:tag meta.
	doc.Find("meta[property='article:tag']").Each(func(_ int, s *goquery.Selection) {
		if tag, exists := s.Attr("content"); exists && tag != "" && !seen[tag] {
			seen[tag] = true
			tags = append(tags, tag)
		}
	})
	// Try keywords meta.
	if keywords, exists := doc.Find("meta[name='keywords']").Attr("content"); exists && keywords != "" {
		for _, kw := range strings.Split(keywords, ",") {
			kw = strings.TrimSpace(kw)
			if kw != "" && !seen[kw] {
				seen[kw] = true
				tags = append(tags, kw)
			}
		}
	}
	return tags
}
