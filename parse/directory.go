package parse

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	foxhound "github.com/sadewadee/foxhound"
)

// Listing represents a business or place extracted from a directory page.
type Listing struct {
	Name        string
	Address     string
	Phone       string
	Email       string
	Website     string
	Categories  []string
	Rating      float64
	ReviewCount int
	Hours       map[string]string
	Latitude    float64
	Longitude   float64
	Image       string
	RawFields   map[string]string
}

// ListingSchema defines a custom CSS selector mapping for listing extraction.
type ListingSchema struct {
	Root   string            // CSS selector for each listing container
	Fields map[string]string // field name -> CSS selector (relative to root)
	Attrs  map[string]string // field name -> attribute to extract (default: text)
}

// businessTypes are schema.org types that represent local businesses or places.
var businessTypes = map[string]bool{
	"LocalBusiness":              true,
	"Organization":               true,
	"Restaurant":                 true,
	"Store":                      true,
	"Place":                      true,
	"Hotel":                      true,
	"MedicalBusiness":            true,
	"FinancialService":           true,
	"FoodEstablishment":          true,
	"HealthAndBeautyBusiness":    true,
	"HomeAndConstructionBusiness": true,
	"LegalService":               true,
	"RealEstateAgent":            true,
	"TouristAttraction":          true,
}

// addressRe matches US-style addresses: "123 Main St, City, ST 12345" or "123 Main St, City, ST 12345-6789".
var addressRe = regexp.MustCompile(`(\d+\s+[^,]+),\s*([^,]+),\s*([A-Z]{2})\s+(\d{5}(?:-\d{4})?)`)

// ratingNumberRe extracts a decimal or integer number.
var ratingNumberRe = regexp.MustCompile(`(\d+\.?\d*)`)

// reviewCountRe extracts a number appearing after review/rating keywords.
var reviewCountRe = regexp.MustCompile(`(?i)(\d[\d,]*)\s*(?:review|rating|vote)`)

// starCharRe counts filled star characters.
var starCharRe = regexp.MustCompile(`[★]`)

// phonePatternRe detects phone-like patterns in text.
var phonePatternRe = regexp.MustCompile(`(?:\+\d{1,3}[\s\-]?)?\(?\d{2,4}\)?[\s\-]?\d{3,4}[\s\-]?\d{3,4}`)

// ExtractListings tries multiple strategies to extract business listings from
// a response. It attempts JSON-LD first, then microdata, then repeating DOM
// patterns. Returns nil, nil if no listings are detected.
func ExtractListings(resp *foxhound.Response) ([]Listing, error) {
	// Strategy 1: JSON-LD
	if listings := extractListingsFromJSONLD(resp); len(listings) > 0 {
		return listings, nil
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, err
	}

	// Strategy 2: Microdata (itemscope/itemprop)
	if listings := extractListingsFromMicrodata(doc); len(listings) > 0 {
		return listings, nil
	}

	// Strategy 3: Repeating DOM patterns
	if listings := extractListingsFromDOM(doc); len(listings) > 0 {
		return listings, nil
	}

	return nil, nil
}

// extractListingsFromJSONLD extracts listings from JSON-LD scripts.
func extractListingsFromJSONLD(resp *foxhound.Response) []Listing {
	jsonLDs, err := ExtractJSONLD(resp)
	if err != nil || len(jsonLDs) == 0 {
		return nil
	}

	var listings []Listing
	for _, obj := range jsonLDs {
		// Handle @graph arrays.
		if graph, ok := obj["@graph"]; ok {
			if arr, ok := graph.([]any); ok {
				for _, item := range arr {
					if m, ok := item.(map[string]any); ok {
						if l, ok := jsonLDToListing(m); ok {
							listings = append(listings, l)
						}
					}
				}
				continue
			}
		}
		if l, ok := jsonLDToListing(obj); ok {
			listings = append(listings, l)
		}
	}
	return listings
}

// jsonLDToListing converts a JSON-LD object to a Listing if it matches a
// business type.
func jsonLDToListing(obj map[string]any) (Listing, bool) {
	typ := jsonStr(obj, "@type")
	if !businessTypes[typ] {
		return Listing{}, false
	}

	l := Listing{
		Name:      jsonStr(obj, "name"),
		Phone:     jsonStr(obj, "telephone"),
		Email:     jsonStr(obj, "email"),
		Website:   jsonStr(obj, "url"),
		Image:     jsonImage(obj),
		RawFields: make(map[string]string),
	}

	// Address
	if addr, ok := obj["address"]; ok {
		l.Address = buildAddress(addr)
	}

	// Geo coordinates
	if geo, ok := obj["geo"].(map[string]any); ok {
		l.Latitude = jsonFloat(geo, "latitude")
		l.Longitude = jsonFloat(geo, "longitude")
	}

	// Aggregate rating
	if rating, ok := obj["aggregateRating"].(map[string]any); ok {
		l.Rating = jsonFloat(rating, "ratingValue")
		l.ReviewCount = jsonInt(rating, "reviewCount")
	}

	// Categories
	if cat := jsonStr(obj, "category"); cat != "" {
		l.Categories = []string{cat}
	}

	// Opening hours
	if hours, ok := obj["openingHoursSpecification"]; ok {
		l.Hours = parseOpeningHours(hours)
	}

	return l, true
}

// extractListingsFromMicrodata finds elements with itemscope and schema.org
// itemtype, then extracts itemprop values within each.
func extractListingsFromMicrodata(doc *goquery.Document) []Listing {
	var listings []Listing

	doc.Find("[itemscope][itemtype*='schema.org']").Each(func(_ int, s *goquery.Selection) {
		itemtype, _ := s.Attr("itemtype")
		// Extract the type name from the URL.
		parts := strings.Split(itemtype, "/")
		typ := parts[len(parts)-1]
		if !businessTypes[typ] {
			return
		}

		l := Listing{RawFields: make(map[string]string)}

		s.Find("[itemprop]").Each(func(_ int, prop *goquery.Selection) {
			name, _ := prop.Attr("itemprop")
			value := strings.TrimSpace(prop.Text())
			if content, exists := prop.Attr("content"); exists && content != "" {
				value = content
			}

			l.RawFields[name] = value

			switch name {
			case "name":
				l.Name = value
			case "telephone":
				l.Phone = value
			case "email":
				l.Email = value
			case "url":
				if href, exists := prop.Attr("href"); exists {
					l.Website = href
				} else {
					l.Website = value
				}
			case "streetAddress", "address":
				if l.Address == "" {
					l.Address = value
				} else {
					l.Address = value + ", " + l.Address
				}
			case "addressLocality":
				if l.Address != "" {
					l.Address += ", " + value
				} else {
					l.Address = value
				}
			case "addressRegion":
				if l.Address != "" {
					l.Address += ", " + value
				} else {
					l.Address = value
				}
			case "postalCode":
				if l.Address != "" {
					l.Address += " " + value
				} else {
					l.Address = value
				}
			case "image":
				if src, exists := prop.Attr("src"); exists {
					l.Image = src
				} else if content, exists := prop.Attr("content"); exists {
					l.Image = content
				}
			case "ratingValue":
				if v, err := strconv.ParseFloat(value, 64); err == nil {
					l.Rating = v
				}
			case "reviewCount":
				if v, err := strconv.Atoi(value); err == nil {
					l.ReviewCount = v
				}
			case "latitude":
				if v, err := strconv.ParseFloat(value, 64); err == nil {
					l.Latitude = v
				}
			case "longitude":
				if v, err := strconv.ParseFloat(value, 64); err == nil {
					l.Longitude = v
				}
			}
		})

		if l.Name != "" {
			listings = append(listings, l)
		}
	})

	return listings
}

// extractListingsFromDOM finds groups of 3+ similar elements that contain
// phone/email/address patterns, indicating a directory listing.
func extractListingsFromDOM(doc *goquery.Document) []Listing {
	type candidate struct {
		selector string
		elements *goquery.Selection
		score    int
	}

	var candidates []candidate

	// Check common container patterns.
	containerSelectors := []string{
		".listing", ".business", ".result", ".card",
		".directory-item", ".place", ".venue", ".location",
		"[class*='listing']", "[class*='business']", "[class*='result']",
		"li", ".item",
	}

	for _, sel := range containerSelectors {
		elements := doc.Find(sel)
		if elements.Length() < 3 {
			continue
		}

		// Score by presence of contact/address patterns.
		score := 0
		elements.Each(func(_ int, s *goquery.Selection) {
			text := s.Text()
			if phonePatternRe.MatchString(text) {
				score++
			}
			if emailRe.MatchString(text) {
				score++
			}
			// Simple address heuristic: contains a number followed by text with comma.
			if addressRe.MatchString(text) {
				score++
			}
		})

		if score >= 3 {
			candidates = append(candidates, candidate{
				selector: sel,
				elements: elements,
				score:    score,
			})
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	// Pick the candidate with the highest score.
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.score > best.score {
			best = c
		}
	}

	var listings []Listing
	best.elements.Each(func(_ int, s *goquery.Selection) {
		l := Listing{RawFields: make(map[string]string)}
		text := strings.TrimSpace(s.Text())

		// Try to extract a name from the first heading or strong element.
		if heading := s.Find("h1, h2, h3, h4, h5, h6, strong, .name, .title").First(); heading.Length() > 0 {
			l.Name = strings.TrimSpace(heading.Text())
		}

		// Extract phone.
		if m := phonePatternRe.FindString(text); m != "" {
			l.Phone = m
		}

		// Extract email.
		if m := emailRe.FindString(text); m != "" {
			l.Email = m
		}

		// Extract address.
		if m := addressRe.FindString(text); m != "" {
			l.Address = m
		}

		if l.Name != "" || l.Phone != "" || l.Email != "" {
			listings = append(listings, l)
		}
	})

	return listings
}

// ExtractListingsWithSchema extracts listings using a user-defined CSS selector
// mapping. It finds all elements matching schema.Root and extracts fields from
// each using the Fields and Attrs maps.
func ExtractListingsWithSchema(resp *foxhound.Response, schema ListingSchema) ([]Listing, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, err
	}

	var listings []Listing

	doc.Find(schema.Root).Each(func(_ int, root *goquery.Selection) {
		l := Listing{RawFields: make(map[string]string)}

		for field, sel := range schema.Fields {
			el := root.Find(sel).First()
			if el.Length() == 0 {
				continue
			}

			var value string
			if attrName, ok := schema.Attrs[field]; ok {
				value, _ = el.Attr(attrName)
			} else {
				value = strings.TrimSpace(el.Text())
			}

			l.RawFields[field] = value
			mapFieldToListing(&l, field, value)
		}

		listings = append(listings, l)
	})

	return listings, nil
}

// mapFieldToListing maps a named field value to the corresponding Listing
// struct field.
func mapFieldToListing(l *Listing, field, value string) {
	switch strings.ToLower(field) {
	case "name":
		l.Name = value
	case "address":
		l.Address = value
	case "phone", "telephone":
		l.Phone = value
	case "email":
		l.Email = value
	case "website", "url":
		l.Website = value
	case "image", "photo":
		l.Image = value
	case "rating":
		l.Rating, l.ReviewCount = NormalizeRating(value)
	case "category", "categories":
		l.Categories = append(l.Categories, value)
	case "latitude", "lat":
		if v, err := strconv.ParseFloat(value, 64); err == nil {
			l.Latitude = v
		}
	case "longitude", "lng", "lon":
		if v, err := strconv.ParseFloat(value, 64); err == nil {
			l.Longitude = v
		}
	}
}

// NormalizeAddress parses a raw address string into components.
// It first tries a US-pattern match, then falls back to comma-split heuristics.
func NormalizeAddress(raw string) (street, city, state, zip, country string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}

	// Try US pattern: "123 Main St, City, ST 12345(-6789)"
	if m := addressRe.FindStringSubmatch(raw); len(m) >= 5 {
		street = strings.TrimSpace(m[1])
		city = strings.TrimSpace(m[2])
		state = strings.TrimSpace(m[3])
		zip = strings.TrimSpace(m[4])
		return
	}

	// Fallback: comma-split with position heuristics.
	parts := strings.Split(raw, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}

	// Remove empty parts.
	var cleaned []string
	for _, p := range parts {
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	parts = cleaned

	switch len(parts) {
	case 0:
		return
	case 1:
		street = parts[0]
	case 2:
		street = parts[0]
		// Second part might be "State ZIP" or just city.
		city = parts[1]
	case 3:
		street = parts[0]
		city = parts[1]
		// Third part: try to split "State ZIP".
		state, zip = splitStateZip(parts[2])
	default:
		// 4+ parts: first = street, second = city, then state/zip, last might be country.
		street = parts[0]
		city = parts[1]
		// Check if last part looks like a country (all alpha, len >= 2).
		lastPart := parts[len(parts)-1]
		if isAlphaString(lastPart) && len(lastPart) >= 2 {
			country = lastPart
			if len(parts) >= 4 {
				state, zip = splitStateZip(parts[2])
			}
		} else {
			state, zip = splitStateZip(parts[2])
			if len(parts) >= 4 {
				country = parts[3]
			}
		}
	}
	return
}

// splitStateZip attempts to split "ST 12345" into state and zip.
func splitStateZip(s string) (state, zip string) {
	s = strings.TrimSpace(s)
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return
	}
	if len(fields) == 1 {
		// Could be just a state or just a zip.
		if isZipLike(fields[0]) {
			zip = fields[0]
		} else {
			state = fields[0]
		}
		return
	}
	// Two or more fields: first is state, rest is zip.
	state = fields[0]
	zip = strings.Join(fields[1:], " ")
	return
}

// isZipLike returns true if the string looks like a postal/zip code.
func isZipLike(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) && r != '-' && r != ' ' {
			return false
		}
	}
	return len(s) >= 3
}

// isAlphaString returns true if the string contains only letters and spaces.
func isAlphaString(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsSpace(r) {
			return false
		}
	}
	return len(s) > 0
}

// NormalizeRating parses a rating string and extracts the numeric rating and
// optional review count. Handles formats like "4.5 (123 reviews)", "4.5/5",
// "4.5 stars", and Unicode star characters.
func NormalizeRating(text string) (rating float64, reviewCount int) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	// Count Unicode filled stars.
	starCount := len(starCharRe.FindAllString(text, -1))
	if starCount > 0 {
		// If the text is only stars (and maybe half/empty stars), use the count.
		stripped := strings.Map(func(r rune) rune {
			if r == '★' || r == '☆' || r == '⯨' || unicode.IsSpace(r) {
				return -1
			}
			return r
		}, text)
		if stripped == "" {
			rating = float64(starCount)
			return
		}
	}

	// Extract review count: number before "review"/"rating"/"vote".
	if m := reviewCountRe.FindStringSubmatch(text); len(m) >= 2 {
		cleaned := strings.ReplaceAll(m[1], ",", "")
		if v, err := strconv.Atoi(cleaned); err == nil {
			reviewCount = v
		}
	}

	// Extract rating number.
	numbers := ratingNumberRe.FindAllString(text, -1)
	if len(numbers) > 0 {
		if v, err := strconv.ParseFloat(numbers[0], 64); err == nil {
			rating = v
			// Sanity check: if rating > 5 and we have a /N pattern, normalize.
			if rating > 5 {
				rating = math.Min(rating, 5)
			}
		}
	}

	return
}

// AsItem converts a Listing to a foxhound.Item with all fields mapped.
func (l *Listing) AsItem() *foxhound.Item {
	item := &foxhound.Item{
		Fields:    make(map[string]any),
		Meta:      make(map[string]any),
		Timestamp: time.Now(),
	}

	if l.Name != "" {
		item.Fields["name"] = l.Name
	}
	if l.Address != "" {
		item.Fields["address"] = l.Address
	}
	if l.Phone != "" {
		item.Fields["phone"] = l.Phone
	}
	if l.Email != "" {
		item.Fields["email"] = l.Email
	}
	if l.Website != "" {
		item.Fields["website"] = l.Website
	}
	if len(l.Categories) > 0 {
		item.Fields["categories"] = l.Categories
	}
	if l.Rating != 0 {
		item.Fields["rating"] = l.Rating
	}
	if l.ReviewCount != 0 {
		item.Fields["review_count"] = l.ReviewCount
	}
	if len(l.Hours) > 0 {
		item.Fields["hours"] = l.Hours
	}
	if l.Latitude != 0 {
		item.Fields["latitude"] = l.Latitude
	}
	if l.Longitude != 0 {
		item.Fields["longitude"] = l.Longitude
	}
	if l.Image != "" {
		item.Fields["image"] = l.Image
	}

	return item
}

// ---------------------------------------------------------------------------
// JSON-LD helpers
// ---------------------------------------------------------------------------

// jsonStr extracts a string field from a JSON-LD object.
func jsonStr(obj map[string]any, key string) string {
	val, ok := obj[key]
	if !ok {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case []any:
		if len(v) > 0 {
			if s, ok := v[0].(string); ok {
				return s
			}
		}
	}
	return fmt.Sprintf("%v", val)
}

// jsonFloat extracts a float64 field from a JSON-LD object.
func jsonFloat(obj map[string]any, key string) float64 {
	val, ok := obj[key]
	if !ok {
		return 0
	}
	switch v := val.(type) {
	case float64:
		return v
	case json.Number:
		f, _ := v.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}
	return 0
}

// jsonInt extracts an int field from a JSON-LD object.
func jsonInt(obj map[string]any, key string) int {
	val, ok := obj[key]
	if !ok {
		return 0
	}
	switch v := val.(type) {
	case float64:
		return int(v)
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(v)
		return i
	}
	return 0
}

// jsonImage extracts the image URL from a JSON-LD object. Image can be a
// string, an array of strings, or an object with a "url" field.
func jsonImage(obj map[string]any) string {
	val, ok := obj["image"]
	if !ok {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case []any:
		if len(v) > 0 {
			if s, ok := v[0].(string); ok {
				return s
			}
		}
	case map[string]any:
		return jsonStr(v, "url")
	}
	return ""
}

// buildAddress constructs an address string from a JSON-LD address object.
func buildAddress(addr any) string {
	switch v := addr.(type) {
	case string:
		return v
	case map[string]any:
		parts := []string{}
		if street := jsonStr(v, "streetAddress"); street != "" {
			parts = append(parts, street)
		}
		if locality := jsonStr(v, "addressLocality"); locality != "" {
			parts = append(parts, locality)
		}
		if region := jsonStr(v, "addressRegion"); region != "" {
			parts = append(parts, region)
		}
		if postal := jsonStr(v, "postalCode"); postal != "" {
			parts = append(parts, postal)
		}
		if country := jsonStr(v, "addressCountry"); country != "" {
			parts = append(parts, country)
		}
		return strings.Join(parts, ", ")
	}
	return ""
}

// parseOpeningHours converts JSON-LD openingHoursSpecification to a map.
func parseOpeningHours(hours any) map[string]string {
	result := make(map[string]string)

	switch v := hours.(type) {
	case []any:
		for _, item := range v {
			if spec, ok := item.(map[string]any); ok {
				day := jsonStr(spec, "dayOfWeek")
				opens := jsonStr(spec, "opens")
				closes := jsonStr(spec, "closes")
				if day != "" && opens != "" {
					result[day] = opens + " - " + closes
				}
			}
		}
	}

	return result
}
