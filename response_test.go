package foxhound_test

import (
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	// Import parse to register the HTML selector hooks.
	_ "github.com/sadewadee/foxhound/parse"
)

// TestResponse_CSS_Text verifies that resp.CSS(selector).Text() returns
// the text content of the first matching element.
func TestResponse_CSS_Text(t *testing.T) {
	body := `<html><body><h1 class="title">Hello World</h1><p>Content</p></body></html>`
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(body),
		URL:        "https://example.com",
	}

	got := resp.CSS("h1.title").Text()
	if got != "Hello World" {
		t.Errorf("CSS(h1.title).Text() = %q, want %q", got, "Hello World")
	}
}

// TestResponse_CSS_Texts verifies that resp.CSS(selector).Texts() returns
// text content of all matching elements.
func TestResponse_CSS_Texts(t *testing.T) {
	body := `<html><body><ul><li>A</li><li>B</li><li>C</li></ul></body></html>`
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(body),
		URL:        "https://example.com",
	}

	got := resp.CSS("li").Texts()
	if len(got) != 3 {
		t.Fatalf("CSS(li).Texts() returned %d items, want 3", len(got))
	}
	if got[0] != "A" || got[1] != "B" || got[2] != "C" {
		t.Errorf("CSS(li).Texts() = %v, want [A B C]", got)
	}
}

// TestResponse_CSS_Attr verifies that resp.CSS(selector).Attr(name) returns
// the attribute value from the first matching element.
func TestResponse_CSS_Attr(t *testing.T) {
	body := `<html><body><a href="/page1">Link1</a><a href="/page2">Link2</a></body></html>`
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(body),
		URL:        "https://example.com",
	}

	got := resp.CSS("a").Attr("href")
	if got != "/page1" {
		t.Errorf("CSS(a).Attr(href) = %q, want %q", got, "/page1")
	}
}

// TestResponse_CSS_Attrs verifies multiple attribute values are returned.
func TestResponse_CSS_Attrs(t *testing.T) {
	body := `<html><body><a href="/a">A</a><a href="/b">B</a></body></html>`
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(body),
		URL:        "https://example.com",
	}

	got := resp.CSS("a[href]").Attrs("href")
	if len(got) != 2 {
		t.Fatalf("CSS(a[href]).Attrs(href) returned %d, want 2", len(got))
	}
	if got[0] != "/a" || got[1] != "/b" {
		t.Errorf("Attrs = %v, want [/a /b]", got)
	}
}

// TestResponse_CSS_Len verifies element count.
func TestResponse_CSS_Len(t *testing.T) {
	body := `<html><body><div class="item">1</div><div class="item">2</div><div class="item">3</div></body></html>`
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(body),
		URL:        "https://example.com",
	}

	got := resp.CSS("div.item").Len()
	if got != 3 {
		t.Errorf("CSS(div.item).Len() = %d, want 3", got)
	}
}

// TestResponse_CSS_NoMatch returns empty for non-matching selectors.
func TestResponse_CSS_NoMatch(t *testing.T) {
	body := `<html><body><p>Hello</p></body></html>`
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(body),
		URL:        "https://example.com",
	}

	if got := resp.CSS("div.nonexistent").Text(); got != "" {
		t.Errorf("CSS(nonexistent).Text() = %q, want empty", got)
	}
	if got := resp.CSS("div.nonexistent").Len(); got != 0 {
		t.Errorf("CSS(nonexistent).Len() = %d, want 0", got)
	}
}

// TestResponse_XPath verifies simplified XPath evaluation.
func TestResponse_XPath(t *testing.T) {
	body := `<html><body><h1>Title</h1><div id="main"><p>Content</p></div></body></html>`
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(body),
		URL:        "https://example.com",
	}

	got := resp.XPath("//h1")
	if got != "Title" {
		t.Errorf("XPath(//h1) = %q, want %q", got, "Title")
	}
}

// TestResponse_XPathAll verifies XPath returning multiple results.
func TestResponse_XPathAll(t *testing.T) {
	body := `<html><body><ul><li>X</li><li>Y</li></ul></body></html>`
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(body),
		URL:        "https://example.com",
	}

	got := resp.XPathAll("//li")
	if len(got) != 2 {
		t.Fatalf("XPathAll(//li) returned %d, want 2", len(got))
	}
	if got[0] != "X" || got[1] != "Y" {
		t.Errorf("XPathAll = %v, want [X Y]", got)
	}
}

// TestResponse_Follow generates follow-up jobs from links.
func TestResponse_Follow(t *testing.T) {
	body := `<html><body>
		<a class="product" href="/product/1">P1</a>
		<a class="product" href="/product/2">P2</a>
		<a class="other" href="/about">About</a>
	</body></html>`
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(body),
		URL:        "https://example.com/category",
		Job:        &foxhound.Job{Depth: 1},
	}

	jobs := resp.Follow("a.product[href]")
	if len(jobs) != 2 {
		t.Fatalf("Follow returned %d jobs, want 2", len(jobs))
	}

	if jobs[0].URL != "https://example.com/product/1" {
		t.Errorf("jobs[0].URL = %q, want https://example.com/product/1", jobs[0].URL)
	}
	if jobs[0].Depth != 2 {
		t.Errorf("jobs[0].Depth = %d, want 2 (parent+1)", jobs[0].Depth)
	}
}

// TestResponse_Follow_WithCallback sets callback metadata on follow-up jobs.
func TestResponse_Follow_WithCallback(t *testing.T) {
	body := `<html><body><a href="/detail/1">D1</a></body></html>`
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(body),
		URL:        "https://example.com",
		Job:        &foxhound.Job{Depth: 0},
	}

	jobs := resp.Follow("a[href]", foxhound.WithFollowCallback("parseDetail"))
	if len(jobs) != 1 {
		t.Fatalf("Follow returned %d jobs, want 1", len(jobs))
	}
	if jobs[0].Meta["callback"] != "parseDetail" {
		t.Errorf("Meta[callback] = %v, want 'parseDetail'", jobs[0].Meta["callback"])
	}
}

// TestResponse_Follow_WithOptions verifies priority and mode options.
func TestResponse_Follow_WithOptions(t *testing.T) {
	body := `<html><body><a href="/page">Link</a></body></html>`
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(body),
		URL:        "https://example.com",
	}

	jobs := resp.Follow("a[href]",
		foxhound.WithFollowMode(foxhound.FetchBrowser),
		foxhound.WithFollowPriority(foxhound.PriorityHigh),
	)
	if len(jobs) != 1 {
		t.Fatalf("Follow returned %d jobs, want 1", len(jobs))
	}
	if jobs[0].FetchMode != foxhound.FetchBrowser {
		t.Errorf("FetchMode = %v, want FetchBrowser", jobs[0].FetchMode)
	}
	if jobs[0].Priority != foxhound.PriorityHigh {
		t.Errorf("Priority = %v, want PriorityHigh", jobs[0].Priority)
	}
}

// TestResponse_Follow_DeduplicatesLinks verifies that duplicate hrefs are
// deduplicated.
func TestResponse_Follow_DeduplicatesLinks(t *testing.T) {
	body := `<html><body>
		<a href="/page">Link 1</a>
		<a href="/page">Link 2</a>
		<a href="/other">Other</a>
	</body></html>`
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(body),
		URL:        "https://example.com",
	}

	jobs := resp.Follow("a[href]")
	if len(jobs) != 2 {
		t.Errorf("Follow should deduplicate, got %d jobs, want 2", len(jobs))
	}
}

// TestResponse_Follow_SkipsFragmentsAndJavascript verifies filtering.
func TestResponse_Follow_SkipsFragmentsAndJavascript(t *testing.T) {
	body := `<html><body>
		<a href="#section">Section</a>
		<a href="javascript:void(0)">JS</a>
		<a href="mailto:test@test.com">Email</a>
		<a href="/valid">Valid</a>
	</body></html>`
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(body),
		URL:        "https://example.com",
	}

	jobs := resp.Follow("a[href]")
	if len(jobs) != 1 {
		t.Errorf("Follow should skip non-HTTP links, got %d jobs, want 1", len(jobs))
	}
	if len(jobs) > 0 && jobs[0].URL != "https://example.com/valid" {
		t.Errorf("jobs[0].URL = %q, want https://example.com/valid", jobs[0].URL)
	}
}

// TestResponse_FollowAll generates jobs from all anchor links.
func TestResponse_FollowAll(t *testing.T) {
	body := `<html><body>
		<a href="/a">A</a>
		<a href="/b">B</a>
	</body></html>`
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(body),
		URL:        "https://example.com",
	}

	jobs := resp.FollowAll()
	if len(jobs) != 2 {
		t.Errorf("FollowAll returned %d jobs, want 2", len(jobs))
	}
}

// TestResponse_IsSuccess verifies status code classification.
func TestResponse_IsSuccess(t *testing.T) {
	tests := []struct {
		status int
		want   bool
	}{
		{200, true},
		{201, true},
		{299, true},
		{301, false},
		{403, false},
		{500, false},
	}

	for _, tt := range tests {
		resp := &foxhound.Response{StatusCode: tt.status}
		if got := resp.IsSuccess(); got != tt.want {
			t.Errorf("IsSuccess(%d) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

// TestResponse_TextBody returns body as string.
func TestResponse_TextBody(t *testing.T) {
	resp := &foxhound.Response{Body: []byte("hello")}
	if got := resp.TextBody(); got != "hello" {
		t.Errorf("TextBody() = %q, want %q", got, "hello")
	}
}

// TestJob_DontFilter verifies the field exists and serializes.
func TestJob_DontFilter(t *testing.T) {
	job := &foxhound.Job{
		URL:        "https://example.com",
		DontFilter: true,
	}
	if !job.DontFilter {
		t.Error("DontFilter should be true")
	}
}

// TestJob_Callback verifies the callback field.
func TestJob_Callback(t *testing.T) {
	job := &foxhound.Job{
		URL:      "https://example.com",
		Callback: "parseDetail",
	}
	if job.Callback != "parseDetail" {
		t.Errorf("Callback = %q, want %q", job.Callback, "parseDetail")
	}
}

// TestJobStep_Evaluate verifies the new evaluate step type.
func TestJobStep_Evaluate(t *testing.T) {
	step := foxhound.JobStep{
		Action: foxhound.JobStepEvaluate,
		Script: "document.querySelector('.lazy').click()",
	}
	if step.Action != foxhound.JobStepEvaluate {
		t.Errorf("Action = %d, want %d", step.Action, foxhound.JobStepEvaluate)
	}
	if step.Script != "document.querySelector('.lazy').click()" {
		t.Errorf("Script mismatch")
	}
}

// TestJobStepEvaluate_Constant verifies the constant value.
func TestJobStepEvaluate_Constant(t *testing.T) {
	if foxhound.JobStepEvaluate != 8 {
		t.Errorf("JobStepEvaluate = %d, want 8", foxhound.JobStepEvaluate)
	}
}

// TestFollow_WithMeta verifies custom metadata is passed through.
func TestFollow_WithMeta(t *testing.T) {
	body := `<html><body><a href="/page">Link</a></body></html>`
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(body),
		URL:        "https://example.com",
	}

	meta := map[string]any{"source": "test", "category": "books"}
	jobs := resp.Follow("a[href]", foxhound.WithFollowMeta(meta))
	if len(jobs) != 1 {
		t.Fatalf("Follow returned %d jobs, want 1", len(jobs))
	}
	if jobs[0].Meta["source"] != "test" {
		t.Errorf("Meta[source] = %v, want 'test'", jobs[0].Meta["source"])
	}
	if jobs[0].Meta["category"] != "books" {
		t.Errorf("Meta[category] = %v, want 'books'", jobs[0].Meta["category"])
	}
}

// TestResponse_Follow_ResolvesRelativeURLs verifies that relative URLs are
// resolved against the response URL.
func TestResponse_Follow_ResolvesRelativeURLs(t *testing.T) {
	body := `<html><body><a href="../other/page">Link</a></body></html>`
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(body),
		URL:        "https://example.com/dir/sub/index.html",
	}

	jobs := resp.Follow("a[href]")
	if len(jobs) != 1 {
		t.Fatalf("Follow returned %d jobs, want 1", len(jobs))
	}
	want := "https://example.com/dir/other/page"
	if jobs[0].URL != want {
		t.Errorf("jobs[0].URL = %q, want %q", jobs[0].URL, want)
	}
}

// TestResponse_FollowURL creates a single follow-up job for a URL.
func TestResponse_FollowURL(t *testing.T) {
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte("<html></html>"),
		URL:        "https://example.com/category/books",
		Job:        &foxhound.Job{Depth: 2},
	}

	job := resp.FollowURL("/product/123")
	if job == nil {
		t.Fatal("FollowURL returned nil")
	}
	if job.URL != "https://example.com/product/123" {
		t.Errorf("URL = %q, want https://example.com/product/123", job.URL)
	}
	if job.Depth != 3 {
		t.Errorf("Depth = %d, want 3 (parent 2 + 1)", job.Depth)
	}
}

// TestResponse_FollowURL_AbsoluteURL resolves absolute URLs correctly.
func TestResponse_FollowURL_AbsoluteURL(t *testing.T) {
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte("<html></html>"),
		URL:        "https://example.com",
	}

	job := resp.FollowURL("https://other.com/page")
	if job == nil {
		t.Fatal("FollowURL returned nil")
	}
	if job.URL != "https://other.com/page" {
		t.Errorf("URL = %q, want https://other.com/page", job.URL)
	}
}

// TestResponse_FollowURL_WithReferer sets referer in meta.
func TestResponse_FollowURL_WithReferer(t *testing.T) {
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte("<html></html>"),
		URL:        "https://example.com/source",
	}

	job := resp.FollowURL("/target", foxhound.WithFollowReferer(true))
	if job == nil {
		t.Fatal("FollowURL returned nil")
	}
	if job.Meta["referer"] != "https://example.com/source" {
		t.Errorf("Meta[referer] = %v, want https://example.com/source", job.Meta["referer"])
	}
}

// TestResponse_FollowURL_WithOptions verifies all follow options work.
func TestResponse_FollowURL_WithOptions(t *testing.T) {
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte("<html></html>"),
		URL:        "https://example.com",
	}

	job := resp.FollowURL("/page",
		foxhound.WithFollowMode(foxhound.FetchBrowser),
		foxhound.WithFollowPriority(foxhound.PriorityHigh),
		foxhound.WithFollowCallback("parseDetail"),
		foxhound.WithFollowDontFilter(true),
		foxhound.WithFollowMeta(map[string]any{"key": "val"}),
	)
	if job == nil {
		t.Fatal("FollowURL returned nil")
	}
	if job.FetchMode != foxhound.FetchBrowser {
		t.Errorf("FetchMode = %v, want FetchBrowser", job.FetchMode)
	}
	if job.Priority != foxhound.PriorityHigh {
		t.Errorf("Priority = %v, want PriorityHigh", job.Priority)
	}
	if job.Meta["callback"] != "parseDetail" {
		t.Errorf("Meta[callback] = %v, want parseDetail", job.Meta["callback"])
	}
	if !job.DontFilter {
		t.Error("DontFilter should be true")
	}
	if job.Meta["key"] != "val" {
		t.Errorf("Meta[key] = %v, want val", job.Meta["key"])
	}
}

// TestResponse_Follow_WithDontFilter marks follow jobs to skip dedup.
func TestResponse_Follow_WithDontFilter(t *testing.T) {
	body := `<html><body><a href="/page">Link</a></body></html>`
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(body),
		URL:        "https://example.com",
	}

	jobs := resp.Follow("a[href]", foxhound.WithFollowDontFilter(true))
	if len(jobs) != 1 {
		t.Fatalf("Follow returned %d jobs, want 1", len(jobs))
	}
	if !jobs[0].DontFilter {
		t.Error("DontFilter should be true on follow jobs")
	}
}

// TestResponse_Follow_WithReferer sets referer on follow jobs.
func TestResponse_Follow_WithReferer(t *testing.T) {
	body := `<html><body><a href="/target">Link</a></body></html>`
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(body),
		URL:        "https://example.com/source",
	}

	jobs := resp.Follow("a[href]", foxhound.WithFollowReferer(true))
	if len(jobs) != 1 {
		t.Fatalf("Follow returned %d jobs, want 1", len(jobs))
	}
	if jobs[0].Meta["referer"] != "https://example.com/source" {
		t.Errorf("Meta[referer] = %v, want https://example.com/source", jobs[0].Meta["referer"])
	}
}

// TestJobStep_WaitState verifies the wait state field.
func TestJobStep_WaitState(t *testing.T) {
	tests := []struct {
		state string
	}{
		{"visible"},
		{"hidden"},
		{"attached"},
		{"detached"},
	}
	for _, tc := range tests {
		step := foxhound.JobStep{
			Action:    foxhound.JobStepWait,
			Selector:  ".element",
			WaitState: tc.state,
		}
		if step.WaitState != tc.state {
			t.Errorf("WaitState = %q, want %q", step.WaitState, tc.state)
		}
	}
}

// Suppress unused import warnings.
var _ = time.Now
