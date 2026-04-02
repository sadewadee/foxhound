package parse_test

import (
	"strings"
	"testing"
	"time"

	"github.com/sadewadee/foxhound/parse"
)

// ---------------------------------------------------------------------------
// Test fixtures
// ---------------------------------------------------------------------------

const articleHTML = `<!DOCTYPE html>
<html>
<head>
  <title>The Future of Sustainable Energy | GreenTech Blog</title>
  <meta property="og:title" content="The Future of Sustainable Energy">
  <meta name="author" content="Jane Smith">
  <meta property="article:published_time" content="2025-06-15T08:00:00Z">
  <meta property="article:tag" content="energy">
  <meta property="article:tag" content="sustainability">
  <meta name="keywords" content="solar,wind,renewables">
</head>
<body>
  <nav><a href="/">Home</a> <a href="/blog">Blog</a></nav>
  <article>
    <h1>The Future of Sustainable Energy</h1>
    <p class="byline">By <span rel="author">Jane Smith</span></p>
    <time datetime="2025-06-15T08:00:00Z">June 15, 2025</time>
    <p>Sustainable energy is rapidly transforming how we power our homes, businesses, and cities around the world. The transition from fossil fuels to renewable sources has accelerated dramatically in recent years, driven by declining costs and growing environmental awareness among consumers and policymakers alike.</p>
    <p>Solar panel installations have doubled in the past three years, with residential rooftop systems becoming increasingly affordable for middle-class families. Wind farms, both onshore and offshore, now generate a significant share of electricity in many countries across Europe and North America.</p>
    <p>Battery storage technology has made remarkable advances, solving the intermittency problem that once plagued renewable energy. Modern lithium-ion and solid-state batteries can store enough energy to power entire neighborhoods through cloudy days and calm nights without any fossil fuel backup.</p>
    <img src="/images/solar-panels.jpg" alt="Solar panels on rooftop">
    <p>Government policies continue to play a crucial role in this transition. Tax incentives, feed-in tariffs, and carbon pricing mechanisms have created favorable conditions for clean energy investment. The private sector has responded enthusiastically, with billions of dollars flowing into green technology startups and infrastructure projects worldwide.</p>
    <img src="/images/wind-farm.jpg" alt="Offshore wind farm">
    <p>Looking ahead, hydrogen fuel cells and next-generation nuclear reactors promise to fill gaps that solar and wind cannot easily address, particularly in heavy industry and long-distance transportation sectors.</p>
  </article>
  <footer><p>Copyright 2025 GreenTech Blog</p></footer>
</body>
</html>`

const listingHTML = `<!DOCTYPE html>
<html>
<head><title>Search Results</title></head>
<body>
  <div class="results">
    <div class="card"><h3>Item 1</h3><p>Description one</p></div>
    <div class="card"><h3>Item 2</h3><p>Description two</p></div>
    <div class="card"><h3>Item 3</h3><p>Description three</p></div>
    <div class="card"><h3>Item 4</h3><p>Description four</p></div>
    <div class="card"><h3>Item 5</h3><p>Description five</p></div>
    <div class="card"><h3>Item 6</h3><p>Description six</p></div>
  </div>
</body>
</html>`

const productHTML = `<!DOCTYPE html>
<html>
<head><title>Premium Headphones</title></head>
<body>
  <h1>Premium Wireless Headphones</h1>
  <p class="price">$29.99</p>
  <p>High quality sound with noise cancellation and 30-hour battery life.</p>
  <button class="add-to-cart">Add to Cart</button>
</body>
</html>`

const searchHTML = `<!DOCTYPE html>
<html>
<head><title>Search: coffee shops</title></head>
<body>
  <form action="/search"><input type="search" name="q" value="coffee shops"></form>
  <div class="result-card">Result 1</div>
  <div class="result-card">Result 2</div>
  <div class="result-card">Result 3</div>
  <div class="result-card">Result 4</div>
  <div class="result-card">Result 5</div>
  <nav class="pagination">
    <a href="/search?q=coffee+shops&page=2" rel="next">Next</a>
  </nav>
</body>
</html>`

const feedHTML = `<!DOCTYPE html>
<html>
<head><title>Activity Feed</title></head>
<body>
  <div role="feed">
    <div class="post"><p>First update</p><time datetime="2025-06-01">June 1</time></div>
    <div class="post"><p>Second update</p><time datetime="2025-06-02">June 2</time></div>
    <div class="post"><p>Third update</p><time datetime="2025-06-03">June 3</time></div>
    <div class="post"><p>Fourth update</p><time datetime="2025-06-04">June 4</time></div>
  </div>
</body>
</html>`

const minimalHTML = `<!DOCTYPE html>
<html><head><title>Hi</title></head><body><p>Hello</p></body></html>`

const listingJSONLDHTML = `<!DOCTYPE html>
<html>
<head>
  <title>Local Restaurants</title>
  <script type="application/ld+json">
  {"@type":"LocalBusiness","name":"Cafe Sunrise","telephone":"555-1234","address":{"streetAddress":"123 Main St","addressLocality":"Springfield"}}
  </script>
  <script type="application/ld+json">
  {"@type":"Restaurant","name":"Pizza Palace","telephone":"555-5678","address":{"streetAddress":"456 Oak Ave","addressLocality":"Springfield"}}
  </script>
</head>
<body>
  <h1>Local Restaurants</h1>
  <p>Find the best dining near you.</p>
</body>
</html>`

// ---------------------------------------------------------------------------
// DetectContentType tests
// ---------------------------------------------------------------------------

func TestDetectContentType_Article(t *testing.T) {
	resp := newHTMLResponse(articleHTML)
	ct := parse.DetectContentType(resp)
	if ct != parse.ContentArticle {
		t.Errorf("expected ContentArticle, got %v (%s)", ct, ct)
	}
}

func TestDetectContentType_Listing(t *testing.T) {
	resp := newHTMLResponse(listingHTML)
	ct := parse.DetectContentType(resp)
	if ct != parse.ContentListing {
		t.Errorf("expected ContentListing, got %v (%s)", ct, ct)
	}
}

func TestDetectContentType_Product(t *testing.T) {
	resp := newHTMLResponse(productHTML)
	ct := parse.DetectContentType(resp)
	if ct != parse.ContentProduct {
		t.Errorf("expected ContentProduct, got %v (%s)", ct, ct)
	}
}

func TestDetectContentType_Search(t *testing.T) {
	resp := newHTMLResponse(searchHTML)
	ct := parse.DetectContentType(resp)
	if ct != parse.ContentSearch {
		t.Errorf("expected ContentSearch, got %v (%s)", ct, ct)
	}
}

func TestDetectContentType_Feed(t *testing.T) {
	resp := newHTMLResponse(feedHTML)
	ct := parse.DetectContentType(resp)
	if ct != parse.ContentFeed {
		t.Errorf("expected ContentFeed, got %v (%s)", ct, ct)
	}
}

func TestDetectContentType_Unknown(t *testing.T) {
	resp := newHTMLResponse(minimalHTML)
	ct := parse.DetectContentType(resp)
	if ct != parse.ContentUnknown {
		t.Errorf("expected ContentUnknown, got %v (%s)", ct, ct)
	}
}

// ---------------------------------------------------------------------------
// AutoExtract tests
// ---------------------------------------------------------------------------

func TestAutoExtract_Article(t *testing.T) {
	resp := newHTMLResponse(articleHTML)
	result, err := parse.AutoExtract(resp)
	if err != nil {
		t.Fatalf("AutoExtract error: %v", err)
	}
	if result.Type != parse.ContentArticle {
		t.Fatalf("expected ContentArticle, got %s", result.Type)
	}
	if result.Article == nil {
		t.Fatal("Article is nil")
	}
	if result.Article.Title == "" {
		t.Error("Article.Title is empty")
	}
}

func TestAutoExtract_Listing(t *testing.T) {
	resp := newHTMLResponse(listingJSONLDHTML)
	result, err := parse.AutoExtract(resp)
	if err != nil {
		t.Fatalf("AutoExtract error: %v", err)
	}
	if result.Type != parse.ContentListing {
		t.Fatalf("expected ContentListing, got %s", result.Type)
	}
	if len(result.Listings) == 0 {
		t.Error("expected non-empty Listings")
	}
}

// ---------------------------------------------------------------------------
// ExtractArticle tests
// ---------------------------------------------------------------------------

func TestExtractArticle_FullExtraction(t *testing.T) {
	resp := newHTMLResponse(articleHTML)
	article, err := parse.ExtractArticle(resp)
	if err != nil {
		t.Fatalf("ExtractArticle error: %v", err)
	}
	if article == nil {
		t.Fatal("article is nil")
	}

	// Title
	if article.Title != "The Future of Sustainable Energy" {
		t.Errorf("Title = %q, want %q", article.Title, "The Future of Sustainable Energy")
	}

	// Author
	if article.Author != "Jane Smith" {
		t.Errorf("Author = %q, want %q", article.Author, "Jane Smith")
	}

	// Published date
	if article.PublishedDate != "2025-06-15T08:00:00Z" {
		t.Errorf("PublishedDate = %q, want %q", article.PublishedDate, "2025-06-15T08:00:00Z")
	}

	// ContentText should contain substantive text.
	if len(article.ContentText) < 100 {
		t.Errorf("ContentText too short: %d chars", len(article.ContentText))
	}

	// WordCount
	if article.WordCount < 50 {
		t.Errorf("WordCount = %d, want >= 50", article.WordCount)
	}

	// ReadingTime should be positive.
	if article.ReadingTime <= 0 {
		t.Errorf("ReadingTime = %v, want > 0", article.ReadingTime)
	}
	// Sanity check: reading time matches word count at 200 wpm.
	expectedRT := time.Duration(float64(article.WordCount)/200.0*60.0) * time.Second
	if article.ReadingTime != expectedRT {
		t.Errorf("ReadingTime = %v, want %v", article.ReadingTime, expectedRT)
	}

	// Summary should be populated and not too long.
	if article.Summary == "" {
		t.Error("Summary is empty")
	}
	if len(article.Summary) > 210 {
		t.Errorf("Summary too long: %d chars", len(article.Summary))
	}

	// Images
	if len(article.Images) < 1 {
		t.Errorf("expected at least 1 image, got %d", len(article.Images))
	}

	// Tags — from article:tag + keywords.
	if len(article.Tags) < 3 {
		t.Errorf("expected at least 3 tags, got %d: %v", len(article.Tags), article.Tags)
	}
}

func TestExtractArticle_TitleFromOGTag(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<head>
  <title>Article Title | My Website Name</title>
  <meta property="og:title" content="Clean OG Title">
</head>
<body>
  <article>
    <h1>Heading Title</h1>
    <p>` + strings.Repeat("This is a paragraph of content with enough text to be scored. ", 10) + `</p>
  </article>
</body>
</html>`

	resp := newHTMLResponse(html)
	article, err := parse.ExtractArticle(resp)
	if err != nil {
		t.Fatalf("ExtractArticle error: %v", err)
	}
	if article == nil {
		t.Fatal("article is nil")
	}
	if article.Title != "Clean OG Title" {
		t.Errorf("Title = %q, want %q", article.Title, "Clean OG Title")
	}
}

func TestExtractArticle_ReadabilityScoring(t *testing.T) {
	// A page with rich content in a scored container should have Score > 0.
	html := `<!DOCTYPE html>
<html>
<head><title>Scored Article</title></head>
<body>
  <div class="content">
    <h1>Main Article</h1>
    <p>` + strings.Repeat("This is a well-written paragraph with commas, clauses, and detail. ", 8) + `</p>
    <p>` + strings.Repeat("Another substantial paragraph discussing important topics in depth. ", 8) + `</p>
    <p>` + strings.Repeat("A third paragraph providing additional context, examples, and analysis. ", 8) + `</p>
  </div>
  <div class="sidebar">
    <a href="/link1">Link</a><a href="/link2">Link</a>
  </div>
</body>
</html>`

	resp := newHTMLResponse(html)
	article, err := parse.ExtractArticle(resp)
	if err != nil {
		t.Fatalf("ExtractArticle error: %v", err)
	}
	if article == nil {
		t.Fatal("article is nil")
	}
	if article.Score <= 0 {
		t.Errorf("expected positive Score for well-structured content, got %f", article.Score)
	}
	if article.WordCount < 50 {
		t.Errorf("WordCount = %d, want >= 50", article.WordCount)
	}
}

// ---------------------------------------------------------------------------
// ContentType.String test
// ---------------------------------------------------------------------------

func TestContentType_String(t *testing.T) {
	tests := []struct {
		ct   parse.ContentType
		want string
	}{
		{parse.ContentUnknown, "unknown"},
		{parse.ContentArticle, "article"},
		{parse.ContentListing, "listing"},
		{parse.ContentProduct, "product"},
		{parse.ContentSearch, "search"},
		{parse.ContentFeed, "feed"},
	}
	for _, tt := range tests {
		if got := tt.ct.String(); got != tt.want {
			t.Errorf("ContentType(%d).String() = %q, want %q", tt.ct, got, tt.want)
		}
	}
}
