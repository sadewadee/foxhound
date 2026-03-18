//go:build playwright

// maps_contacts — Google Maps → Visit Website → Extract Contacts
// Flow: search Maps → get business listings → visit each website → scrape contacts
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/fetch"
	"github.com/sadewadee/foxhound/identity"
	"github.com/sadewadee/foxhound/parse"
)

var proxyURL = getEnvOrDefault("FOXHOUND_PROXY", "http://user:pass@proxy:port")

type Business struct {
	Name     string   `json:"name"`
	Rating   string   `json:"rating,omitempty"`
	Reviews  string   `json:"reviews,omitempty"`
	MapAddr  string   `json:"maps_address,omitempty"`
	Website  string   `json:"website,omitempty"`
	Emails   []string `json:"emails,omitempty"`
	Phones   []string `json:"phones,omitempty"`
	Address  string   `json:"address,omitempty"`
	Social   []string `json:"social_media,omitempty"`
	WhatsApp string   `json:"whatsapp,omitempty"`
	Source   string   `json:"source"`
	Error    string   `json:"error,omitempty"`
}

var (
	emailRe    = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	phoneRe    = regexp.MustCompile(`(?:\+62|62|0)[\s\-]?(?:\d[\s\-]?){8,12}`)
	whatsappRe = regexp.MustCompile(`(?:wa\.me|api\.whatsapp\.com/send\?phone=)[\s/=]*(\+?\d{10,15})`)
	socialRe   = regexp.MustCompile(`https?://(?:www\.)?(?:instagram\.com|facebook\.com|twitter\.com|tiktok\.com|linkedin\.com)/[a-zA-Z0-9._\-/]+`)
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	os.MkdirAll("tests/results", 0755)

	prof := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSMacOS),
		identity.WithLocale("en-US", "en-US", "en"),
		identity.WithTimezone("Asia/Jakarta"),
	)

	fmt.Println("══════════════════════════════════════════════════════════")
	fmt.Println("  Google Maps → Visit Website → Extract Contacts")
	fmt.Println("  Query: yoga studio di canggu")
	fmt.Println("══════════════════════════════════════════════════════════")

	cf, err := fetch.NewCamoufox(
		fetch.WithBrowserIdentity(prof),
		fetch.WithBlockImages(true),
		fetch.WithHeadless("true"),
		fetch.WithBrowserProxy(proxyURL),
		fetch.WithBrowserTimeout(60*time.Second),
		fetch.WithPersistSession(false),
	)
	if err != nil {
		slog.Error("launch failed", "err", err)
		os.Exit(1)
	}
	defer cf.Close()

	// Also create a stealth fetcher for visiting websites (faster, no browser needed)
	stealth := fetch.NewStealth(
		fetch.WithIdentity(prof),
		fetch.WithTimeout(20*time.Second),
		fetch.WithProxy(proxyURL),
	)
	defer stealth.Close()

	start := time.Now()

	// ═══════════════════════════════════════
	// STEP 1: Scrape Google Maps listings
	// ═══════════════════════════════════════
	fmt.Println("\n[STEP 1] Scraping Google Maps listings...")
	businesses := scrapeMapListings(cf, "https://www.google.com/maps/search/yoga+studio+di+canggu/")

	if len(businesses) == 0 {
		fmt.Println("  No listings found!")
		os.Exit(1)
	}
	fmt.Printf("  Found %d businesses\n", len(businesses))

	// ═══════════════════════════════════════
	// STEP 2: Search each business individually on Google Maps
	// to get the detail page with website link
	// ═══════════════════════════════════════
	fmt.Println("\n[STEP 2] Getting website URLs from individual Maps searches...")
	for i := range businesses {
		b := &businesses[i]
		if b.Website != "" { continue }

		time.Sleep(time.Duration(2000+rand.IntN(3000)) * time.Millisecond)

		// Search Maps for this specific business to get detail view
		query := strings.ReplaceAll(b.Name, " ", "+") + "+canggu+bali"
		searchURL := fmt.Sprintf("https://www.google.com/maps/search/%s/", query)

		ctx2, cancel2 := context.WithTimeout(context.Background(), 45*time.Second)
		resp2, err := cf.Fetch(ctx2, &foxhound.Job{
			ID: fmt.Sprintf("detail-%d", i), URL: searchURL, Method: "GET",
			FetchMode: foxhound.FetchBrowser,
		})
		cancel2()
		if err != nil { continue }

		body := string(resp2.Body)

		// Extract website from Maps detail page — look for external links
		doc2, _ := parse.NewDocument(resp2)
		if doc2 != nil {
			doc2.Each("a[href]", func(_ int, s *goquery.Selection) {
				if b.Website != "" { return }
				href, _ := s.Attr("href")
				ariaLabel, _ := s.Attr("aria-label")
				dataItem, _ := s.Attr("data-item-id")

				// Maps uses data-item-id="authority" for website links
				if dataItem == "authority" && href != "" {
					b.Website = href
					return
				}
				// Or aria-label contains "website"
				if strings.Contains(strings.ToLower(ariaLabel), "website") && href != "" {
					b.Website = href
					return
				}
			})
		}

		// Fallback: regex for external URLs in the body
		if b.Website == "" {
			urlRe := regexp.MustCompile(`"(https?://(?:www\.)?[a-zA-Z0-9][a-zA-Z0-9\-]*\.[a-zA-Z]{2,}[^"]*)"`)
			for _, m := range urlRe.FindAllStringSubmatch(body, -1) {
				u := m[1]
				lower := strings.ToLower(u)
				if strings.Contains(lower, "google.") || strings.Contains(lower, "gstatic.") ||
					strings.Contains(lower, "googleapis.") || strings.Contains(lower, "youtube.") ||
					strings.Contains(lower, "facebook.com") || strings.Contains(lower, "instagram.com") {
					continue
				}
				// Likely the business website
				if strings.Contains(lower, "yoga") || strings.Contains(lower, "studio") ||
					strings.Contains(lower, "bali") || strings.Contains(lower, "canggu") ||
					(!strings.Contains(lower, "tripadvisor") && !strings.Contains(lower, "booking.com")) {
					b.Website = u
					break
				}
			}
		}

		if b.Website != "" {
			fmt.Printf("  [%d] %-35s → %s\n", i+1, b.Name, b.Website)
		} else {
			fmt.Printf("  [%d] %-35s → no website found\n", i+1, b.Name)
		}
	}

	// ═══════════════════════════════════════
	// STEP 3: Visit each website + extract contacts
	// ═══════════════════════════════════════
	fmt.Println("\n[STEP 3] Visiting websites and extracting contacts...")

	for i := range businesses {
		b := &businesses[i]
		if b.Website == "" {
			fmt.Printf("  [%d] %-35s — no website\n", i+1, b.Name)
			continue
		}

		// Human delay between visits
		if i > 0 {
			time.Sleep(time.Duration(1500+rand.IntN(2000)) * time.Millisecond)
		}

		fmt.Printf("  [%d] %-35s → %s\n", i+1, b.Name, b.Website)

		// Try stealth first (fast), fall back to browser if needed
		extractContacts(stealth, cf, b)

		// Print found contacts
		if len(b.Emails) > 0 {
			fmt.Printf("       emails: %s\n", strings.Join(b.Emails, ", "))
		}
		if len(b.Phones) > 0 {
			fmt.Printf("       phones: %s\n", strings.Join(b.Phones, ", "))
		}
		if b.WhatsApp != "" {
			fmt.Printf("       whatsapp: %s\n", b.WhatsApp)
		}
		if len(b.Social) > 0 {
			fmt.Printf("       social: %s\n", strings.Join(b.Social, ", "))
		}
		if b.Address != "" {
			fmt.Printf("       address: %s\n", b.Address)
		}
		if b.Error != "" {
			fmt.Printf("       error: %s\n", b.Error)
		}
	}

	elapsed := time.Since(start)

	// ═══════════════════════════════════════
	// REPORT
	// ═══════════════════════════════════════
	totalWithWebsite := 0
	totalWithEmail := 0
	totalWithPhone := 0
	totalWithSocial := 0
	totalWithWhatsApp := 0
	totalWithAnyContact := 0

	for _, b := range businesses {
		if b.Website != "" {
			totalWithWebsite++
		}
		if len(b.Emails) > 0 {
			totalWithEmail++
		}
		if len(b.Phones) > 0 {
			totalWithPhone++
		}
		if len(b.Social) > 0 {
			totalWithSocial++
		}
		if b.WhatsApp != "" {
			totalWithWhatsApp++
		}
		if len(b.Emails) > 0 || len(b.Phones) > 0 || b.WhatsApp != "" {
			totalWithAnyContact++
		}
	}

	fmt.Println("\n\n══════════════════════════════════════════════════════════════")
	fmt.Println("  CONTACT EXTRACTION REPORT")
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Printf("  Total Businesses:      %d\n", len(businesses))
	fmt.Printf("  With Website:          %d\n", totalWithWebsite)
	fmt.Printf("  With Email:            %d\n", totalWithEmail)
	fmt.Printf("  With Phone:            %d\n", totalWithPhone)
	fmt.Printf("  With WhatsApp:         %d\n", totalWithWhatsApp)
	fmt.Printf("  With Social Media:     %d\n", totalWithSocial)
	fmt.Printf("  With Any Contact:      %d (%.0f%%)\n", totalWithAnyContact,
		float64(totalWithAnyContact)/float64(max(len(businesses), 1))*100)
	fmt.Printf("  Duration:              %s\n", elapsed.Round(time.Second))
	fmt.Println("══════════════════════════════════════════════════════════════")

	// Detailed table
	fmt.Println("\n  Detailed Results:")
	fmt.Printf("  %-35s | %-6s | %-5s | %-5s | %-3s | %s\n",
		"Business", "Rating", "Email", "Phone", "WA", "Website")
	fmt.Println("  " + strings.Repeat("─", 100))
	for _, b := range businesses {
		email := "—"
		if len(b.Emails) > 0 {
			email = b.Emails[0]
			if len(email) > 25 {
				email = email[:25] + "…"
			}
		}
		phone := "—"
		if len(b.Phones) > 0 {
			phone = b.Phones[0]
		}
		wa := "—"
		if b.WhatsApp != "" {
			wa = "✓"
		}
		web := b.Website
		if len(web) > 30 {
			web = web[:30] + "…"
		}
		name := b.Name
		if len(name) > 35 {
			name = name[:32] + "..."
		}
		fmt.Printf("  %-35s | %-6s | %-25s | %-12s | %-3s | %s\n",
			name, b.Rating, email, phone, wa, web)
	}

	saveJSON("tests/results/maps_contacts.json", businesses)
	fmt.Printf("\n  Saved to tests/results/maps_contacts.json\n")
}

func scrapeMapListings(cf foxhound.Fetcher, url string) []Business {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := cf.Fetch(ctx, &foxhound.Job{
		ID: "maps-listings", URL: url, Method: "GET",
		FetchMode: foxhound.FetchBrowser,
	})
	if err != nil {
		slog.Error("maps fetch failed", "err", err)
		return nil
	}

	doc, err := parse.NewDocument(resp)
	if err != nil {
		return nil
	}

	var businesses []Business
	seen := map[string]bool{}

	doc.Each("div.Nv2PK, a[aria-label][href*='maps/place']", func(_ int, s *goquery.Selection) {
		name := strings.TrimSpace(s.Find("div.qBF1Pd, span.fontHeadlineSmall").Text())
		if name == "" {
			name, _ = s.Attr("aria-label")
		}
		if name == "" || seen[name] || len(businesses) >= 10 {
			return
		}
		lower := strings.ToLower(name)
		if strings.Contains(lower, "input tools") || strings.Contains(lower, "reklam") ||
			strings.Contains(lower, "iklan") || strings.Contains(lower, "adlı") {
			return
		}
		seen[name] = true

		rating := strings.TrimSpace(s.Find("span.MW4etd").Text())
		reviews := strings.TrimSpace(s.Find("span.UY7F9").Text())

		// Try to find website link
		website := ""
		s.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
			href, _ := a.Attr("href")
			if href == "" {
				return
			}
			// Maps wraps external links differently
			if strings.Contains(href, "google.com/maps") {
				return
			}
			if strings.HasPrefix(href, "http") && !strings.Contains(href, "google.") {
				website = href
			}
		})

		// Also check data attributes and nearby elements for website
		if website == "" {
			s.Find("a[data-value='Website'], a[aria-label*='website'], a[aria-label*='Website']").Each(func(_ int, a *goquery.Selection) {
				href, _ := a.Attr("href")
				if href != "" && strings.HasPrefix(href, "http") {
					website = href
				}
			})
		}

		addr := ""
		s.Find("div.W4Efsd").Each(func(_ int, d *goquery.Selection) {
			t := strings.TrimSpace(d.Text())
			if strings.Contains(t, "·") || strings.Contains(t, ",") {
				addr = t
			}
		})

		businesses = append(businesses, Business{
			Name:    name,
			Rating:  rating,
			Reviews: reviews,
			MapAddr: addr,
			Website: website,
			Source:  "google_maps",
		})
	})

	return businesses
}

func extractContacts(stealth foxhound.Fetcher, browser foxhound.Fetcher, b *Business) {
	// Normalize website URL
	url := b.Website
	if !strings.HasPrefix(url, "http") {
		url = "https://" + url
	}

	// Try stealth first (much faster — no browser rendering)
	resp, err := stealth.Fetch(context.Background(), &foxhound.Job{
		ID: "contact-" + b.Name, URL: url, Method: "GET",
	})

	if err != nil || resp == nil || resp.StatusCode >= 400 {
		// Fall back to browser for JS-rendered sites
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		resp, err = browser.Fetch(ctx, &foxhound.Job{
			ID: "contact-browser-" + b.Name, URL: url, Method: "GET",
			FetchMode: foxhound.FetchBrowser,
		})
		cancel()
		if err != nil {
			b.Error = err.Error()
			return
		}
	}

	if resp == nil || resp.StatusCode >= 400 {
		b.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return
	}

	body := string(resp.Body)
	lowerBody := strings.ToLower(body)

	// Extract emails
	emails := emailRe.FindAllString(body, -1)
	seen := map[string]bool{}
	for _, e := range emails {
		e = strings.ToLower(e)
		// Filter out common false positives
		if strings.Contains(e, "example.com") || strings.Contains(e, "domain.com") ||
			strings.Contains(e, "email.com") || strings.Contains(e, "sentry") ||
			strings.Contains(e, "wixpress") || strings.Contains(e, "cloudflare") ||
			strings.HasSuffix(e, ".png") || strings.HasSuffix(e, ".jpg") {
			continue
		}
		if !seen[e] {
			seen[e] = true
			b.Emails = append(b.Emails, e)
		}
	}

	// Extract phones
	phones := phoneRe.FindAllString(body, -1)
	phoneSeen := map[string]bool{}
	for _, p := range phones {
		// Normalize
		cleaned := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' || r == '+' {
				return r
			}
			return -1
		}, p)
		if len(cleaned) >= 10 && !phoneSeen[cleaned] {
			phoneSeen[cleaned] = true
			b.Phones = append(b.Phones, cleaned)
		}
	}

	// Extract WhatsApp
	waMatches := whatsappRe.FindAllStringSubmatch(body, -1)
	if len(waMatches) > 0 {
		b.WhatsApp = waMatches[0][1]
	}
	// Also check for wa.me links
	if b.WhatsApp == "" && strings.Contains(lowerBody, "wa.me/") {
		waRe2 := regexp.MustCompile(`wa\.me/(\+?\d{10,15})`)
		if m := waRe2.FindStringSubmatch(body); len(m) > 1 {
			b.WhatsApp = m[1]
		}
	}

	// Extract social media links
	socials := socialRe.FindAllString(body, -1)
	socialSeen := map[string]bool{}
	for _, s := range socials {
		// Normalize and deduplicate
		s = strings.TrimRight(s, "/")
		if !socialSeen[s] && !strings.Contains(s, "share") && !strings.Contains(s, "intent") {
			socialSeen[s] = true
			b.Social = append(b.Social, s)
		}
	}

	// Extract address from structured data or common patterns
	doc, err := parse.NewDocument(resp)
	if err == nil {
		// Check meta tags
		for _, sel := range []string{
			"address", "[itemprop='address']", "[class*='address']",
			"[itemtype*='PostalAddress']",
		} {
			addr := strings.TrimSpace(doc.Text(sel))
			if addr != "" && len(addr) > 10 && len(addr) < 300 {
				b.Address = addr
				break
			}
		}

		// Check for additional contact info in common selectors
		for _, sel := range []string{
			"a[href^='tel:']", "a[href^='mailto:']",
		} {
			doc.Each(sel, func(_ int, s *goquery.Selection) {
				href, _ := s.Attr("href")
				if strings.HasPrefix(href, "tel:") {
					phone := strings.TrimPrefix(href, "tel:")
					phone = strings.Map(func(r rune) rune {
						if r >= '0' && r <= '9' || r == '+' {
							return r
						}
						return -1
					}, phone)
					if len(phone) >= 10 && !phoneSeen[phone] {
						phoneSeen[phone] = true
						b.Phones = append(b.Phones, phone)
					}
				}
				if strings.HasPrefix(href, "mailto:") {
					email := strings.ToLower(strings.TrimPrefix(href, "mailto:"))
					if strings.Contains(email, "@") && !seen[email] {
						seen[email] = true
						b.Emails = append(b.Emails, email)
					}
				}
			})
		}
	}
}

func saveJSON(path string, v any) {
	data, _ := json.MarshalIndent(v, "", "  ")
	os.WriteFile(path, data, 0644)
}

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" { return v }
	return fallback
}
