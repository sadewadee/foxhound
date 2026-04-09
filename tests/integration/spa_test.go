//go:build integration && playwright

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sadewadee/foxhound/engine"
	"github.com/sadewadee/foxhound/fetch"
	"github.com/sadewadee/foxhound/identity"
	"github.com/sadewadee/foxhound/parse"
)

// TestSPA_ReactExample — react.dev (React docs site, SPA)
func TestSPA_React(t *testing.T) {
	prof := identity.Generate(identity.WithBrowser(identity.BrowserFirefox), identity.WithOS(identity.OSMacOS))

	cf, err := fetch.NewCamoufox(
		fetch.WithBrowserIdentity(prof),
		fetch.WithBrowserProxy(proxyURL),
		fetch.WithHeadless("true"),
		fetch.WithBrowserTimeout(60*time.Second),
		fetch.WithBlockImages(true),
	)
	if err != nil {
		t.Fatalf("NewCamoufox failed: %v", err)
	}
	defer cf.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	trail := engine.NewTrail("react-docs").
		Navigate("https://react.dev/learn").
		WaitOptional("h1, main article", 15*time.Second)

	jobs := trail.ToJobs()
	mainJob := jobs[len(jobs)-1]

	resp, err := cf.Fetch(ctx, mainJob)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	t.Logf("Status: %d, Body: %d bytes, Duration: %s", resp.StatusCode, len(resp.Body), resp.Duration)

	doc, _ := parse.NewDocument(resp)
	h1 := doc.Text("h1")
	links := doc.Find("a[href]").Length()
	headings := doc.Find("h2, h3").Length()
	t.Logf("H1: %q", h1)
	t.Logf("Links: %d, Headings (h2/h3): %d", links, headings)

	if h1 == "" {
		t.Error("No H1 found — SPA may not have rendered")
	}
}

// TestSPA_VueExample — vuejs.org (Vue docs, Vue SPA)
func TestSPA_Vue(t *testing.T) {
	prof := identity.Generate(identity.WithBrowser(identity.BrowserFirefox))

	cf, err := fetch.NewCamoufox(
		fetch.WithBrowserIdentity(prof),
		fetch.WithBrowserProxy(proxyURL),
		fetch.WithHeadless("true"),
		fetch.WithBrowserTimeout(60*time.Second),
		fetch.WithBlockImages(true),
	)
	if err != nil {
		t.Fatalf("NewCamoufox failed: %v", err)
	}
	defer cf.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	trail := engine.NewTrail("vue-docs").
		Navigate("https://vuejs.org/guide/introduction.html").
		WaitOptional("h1, .content", 15*time.Second)

	jobs := trail.ToJobs()
	resp, err := cf.Fetch(ctx, jobs[len(jobs)-1])
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	t.Logf("Status: %d, Body: %d bytes", resp.StatusCode, len(resp.Body))

	doc, _ := parse.NewDocument(resp)
	t.Logf("H1: %q", doc.Text("h1"))
	t.Logf("Code blocks: %d", doc.Find("pre code").Length())
	t.Logf("Headings: %d", doc.Find("h2, h3").Length())
}

// TestSPA_AngularExample — angular.dev (Angular SPA)
func TestSPA_Angular(t *testing.T) {
	prof := identity.Generate(identity.WithBrowser(identity.BrowserFirefox))

	cf, err := fetch.NewCamoufox(
		fetch.WithBrowserIdentity(prof),
		fetch.WithBrowserProxy(proxyURL),
		fetch.WithHeadless("true"),
		fetch.WithBrowserTimeout(60*time.Second),
		fetch.WithBlockImages(true),
	)
	if err != nil {
		t.Fatalf("NewCamoufox failed: %v", err)
	}
	defer cf.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	trail := engine.NewTrail("angular-docs").
		Navigate("https://angular.dev/overview").
		WaitOptional("h1, main", 15*time.Second)

	jobs := trail.ToJobs()
	resp, err := cf.Fetch(ctx, jobs[len(jobs)-1])
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	t.Logf("Status: %d, Body: %d bytes", resp.StatusCode, len(resp.Body))

	doc, _ := parse.NewDocument(resp)
	t.Logf("H1: %q", doc.Text("h1"))
	t.Logf("Headings: %d", doc.Find("h2, h3").Length())
	if !strings.Contains(string(resp.Body), "angular") && !strings.Contains(string(resp.Body), "Angular") {
		t.Error("Body doesn't mention Angular")
	}
}

// TestSPA_LWR — Salesforce help docs (Lightning Web Runtime, no CAPTCHA)
// Salesforce help.salesforce.com is built on Experience Cloud / LWR
func TestSPA_LWR_Help(t *testing.T) {
	prof := identity.Generate(identity.WithBrowser(identity.BrowserFirefox))

	cf, err := fetch.NewCamoufox(
		fetch.WithBrowserIdentity(prof),
		fetch.WithBrowserProxy(proxyURL),
		fetch.WithHeadless("true"),
		fetch.WithBrowserTimeout(90*time.Second),
		fetch.WithBlockImages(true),
	)
	if err != nil {
		t.Fatalf("NewCamoufox failed: %v", err)
	}
	defer cf.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// Trailhead is Salesforce's learning platform — built on LWR
	trail := engine.NewTrail("trailhead").
		Navigate("https://trailhead.salesforce.com/en/content/learn/modules/lex_implementation_basics").
		WaitOptional("h1, .module-header, [data-aura-class], lightning-card", 30*time.Second)

	jobs := trail.ToJobs()
	resp, err := cf.Fetch(ctx, jobs[len(jobs)-1])
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	t.Logf("Status: %d, Body: %d bytes, Duration: %s", resp.StatusCode, len(resp.Body), resp.Duration)

	doc, _ := parse.NewDocument(resp)
	for _, sel := range []string{
		"h1", ".module-header", "[data-aura-class]", "lightning-card",
		"main", "article", ".unit-card", ".module-card",
	} {
		if c := doc.Find(sel).Length(); c > 0 {
			t.Logf("  Selector %q: %d", sel, c)
		}
	}
	t.Logf("H1: %q", doc.Text("h1"))
	t.Logf("Total links: %d", doc.Find("a[href]").Length())

	body := strings.ToLower(string(resp.Body))
	if strings.Contains(body, "captcha") || strings.Contains(body, "recaptcha") {
		t.Log("WARN: CAPTCHA detected on this page")
	}
}
