package parse_test

import (
	"testing"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/parse"
)

// ─── ExtractListings: JSON-LD ─────────────────────────────────────────────────

func TestExtractListings_JSONLD(t *testing.T) {
	html := `<html><head>
	<script type="application/ld+json">[
		{
			"@type": "LocalBusiness",
			"name": "Cafe Bali",
			"telephone": "+62 361 123456",
			"email": "hello@cafebali.com",
			"url": "https://cafebali.com",
			"image": "https://cafebali.com/photo.jpg",
			"address": {
				"@type": "PostalAddress",
				"streetAddress": "Jl. Sunset 10",
				"addressLocality": "Seminyak",
				"addressRegion": "Bali",
				"postalCode": "80361"
			},
			"geo": {
				"@type": "GeoCoordinates",
				"latitude": -8.6905,
				"longitude": 115.1683
			},
			"aggregateRating": {
				"@type": "AggregateRating",
				"ratingValue": 4.5,
				"reviewCount": 123
			}
		},
		{
			"@type": "Restaurant",
			"name": "Warung Nasi",
			"telephone": "+62 361 654321"
		}
	]</script>
	</head><body><p>Directory page</p></body></html>`

	resp := makeResp(html)
	listings, err := parse.ExtractListings(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(listings) != 2 {
		t.Fatalf("expected 2 listings, got %d", len(listings))
	}

	l := listings[0]
	if l.Name != "Cafe Bali" {
		t.Errorf("Name = %q, want %q", l.Name, "Cafe Bali")
	}
	if l.Phone != "+62 361 123456" {
		t.Errorf("Phone = %q, want %q", l.Phone, "+62 361 123456")
	}
	if l.Email != "hello@cafebali.com" {
		t.Errorf("Email = %q, want %q", l.Email, "hello@cafebali.com")
	}
	if l.Website != "https://cafebali.com" {
		t.Errorf("Website = %q, want %q", l.Website, "https://cafebali.com")
	}
	if l.Image != "https://cafebali.com/photo.jpg" {
		t.Errorf("Image = %q, want %q", l.Image, "https://cafebali.com/photo.jpg")
	}
	if l.Latitude != -8.6905 {
		t.Errorf("Latitude = %v, want -8.6905", l.Latitude)
	}
	if l.Longitude != 115.1683 {
		t.Errorf("Longitude = %v, want 115.1683", l.Longitude)
	}
	if l.Rating != 4.5 {
		t.Errorf("Rating = %v, want 4.5", l.Rating)
	}
	if l.ReviewCount != 123 {
		t.Errorf("ReviewCount = %d, want 123", l.ReviewCount)
	}

	l2 := listings[1]
	if l2.Name != "Warung Nasi" {
		t.Errorf("second listing Name = %q, want %q", l2.Name, "Warung Nasi")
	}
}

// ─── ExtractListings: Microdata ───────────────────────────────────────────────

func TestExtractListings_Microdata(t *testing.T) {
	html := `<html><body>
	<div itemscope itemtype="http://schema.org/LocalBusiness">
		<span itemprop="name">Toko Roti</span>
		<span itemprop="telephone">+62 812 3456 7890</span>
		<span itemprop="email">info@tokoroti.com</span>
		<a itemprop="url" href="https://tokoroti.com">Website</a>
		<span itemprop="streetAddress">Jl. Raya 5</span>
		<span itemprop="addressLocality">Denpasar</span>
		<meta itemprop="latitude" content="-8.65">
		<meta itemprop="longitude" content="115.22">
		<span itemprop="ratingValue">4.2</span>
		<span itemprop="reviewCount">45</span>
	</div>
	<div itemscope itemtype="https://schema.org/Restaurant">
		<span itemprop="name">Bali Eats</span>
		<span itemprop="telephone">+62 812 9999 0000</span>
	</div>
	</body></html>`

	resp := makeResp(html)
	listings, err := parse.ExtractListings(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(listings) != 2 {
		t.Fatalf("expected 2 listings, got %d", len(listings))
	}

	l := listings[0]
	if l.Name != "Toko Roti" {
		t.Errorf("Name = %q, want %q", l.Name, "Toko Roti")
	}
	if l.Phone != "+62 812 3456 7890" {
		t.Errorf("Phone = %q, want %q", l.Phone, "+62 812 3456 7890")
	}
	if l.Email != "info@tokoroti.com" {
		t.Errorf("Email = %q, want %q", l.Email, "info@tokoroti.com")
	}
	if l.Website != "https://tokoroti.com" {
		t.Errorf("Website = %q, want %q", l.Website, "https://tokoroti.com")
	}
	if l.Latitude != -8.65 {
		t.Errorf("Latitude = %v, want -8.65", l.Latitude)
	}
	if l.Longitude != 115.22 {
		t.Errorf("Longitude = %v, want 115.22", l.Longitude)
	}
	if l.Rating != 4.2 {
		t.Errorf("Rating = %v, want 4.2", l.Rating)
	}
	if l.ReviewCount != 45 {
		t.Errorf("ReviewCount = %d, want 45", l.ReviewCount)
	}

	if listings[1].Name != "Bali Eats" {
		t.Errorf("second listing Name = %q, want %q", listings[1].Name, "Bali Eats")
	}
}

// ─── ExtractListingsWithSchema ────────────────────────────────────────────────

func TestExtractListingsWithSchema_Cards(t *testing.T) {
	html := `<html><body>
	<div class="results">
		<div class="card">
			<h3 class="biz-name">Pizza Place</h3>
			<p class="biz-addr">123 Main St</p>
			<p class="biz-phone">(555) 123-4567</p>
			<img class="biz-img" src="/img/pizza.jpg">
		</div>
		<div class="card">
			<h3 class="biz-name">Sushi Spot</h3>
			<p class="biz-addr">456 Oak Ave</p>
			<p class="biz-phone">(555) 987-6543</p>
			<img class="biz-img" src="/img/sushi.jpg">
		</div>
	</div>
	</body></html>`

	resp := makeResp(html)
	schema := parse.ListingSchema{
		Root: ".card",
		Fields: map[string]string{
			"name":    ".biz-name",
			"address": ".biz-addr",
			"phone":   ".biz-phone",
			"image":   ".biz-img",
		},
		Attrs: map[string]string{
			"image": "src",
		},
	}

	listings, err := parse.ExtractListingsWithSchema(resp, schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(listings) != 2 {
		t.Fatalf("expected 2 listings, got %d", len(listings))
	}

	l := listings[0]
	if l.Name != "Pizza Place" {
		t.Errorf("Name = %q, want %q", l.Name, "Pizza Place")
	}
	if l.Address != "123 Main St" {
		t.Errorf("Address = %q, want %q", l.Address, "123 Main St")
	}
	if l.Phone != "(555) 123-4567" {
		t.Errorf("Phone = %q, want %q", l.Phone, "(555) 123-4567")
	}
	if l.Image != "/img/pizza.jpg" {
		t.Errorf("Image = %q, want %q", l.Image, "/img/pizza.jpg")
	}

	l2 := listings[1]
	if l2.Name != "Sushi Spot" {
		t.Errorf("second listing Name = %q, want %q", l2.Name, "Sushi Spot")
	}
}

// ─── NormalizeAddress ─────────────────────────────────────────────────────────

func TestNormalizeAddress_US(t *testing.T) {
	street, city, state, zip, _ := parse.NormalizeAddress("123 Main St, Springfield, IL 62701")
	if street != "123 Main St" {
		t.Errorf("street = %q, want %q", street, "123 Main St")
	}
	if city != "Springfield" {
		t.Errorf("city = %q, want %q", city, "Springfield")
	}
	if state != "IL" {
		t.Errorf("state = %q, want %q", state, "IL")
	}
	if zip != "62701" {
		t.Errorf("zip = %q, want %q", zip, "62701")
	}
}

func TestNormalizeAddress_CommaSplit(t *testing.T) {
	street, city, state, zip, country := parse.NormalizeAddress("123 Main St, City, State 12345, Country")
	if street != "123 Main St" {
		t.Errorf("street = %q, want %q", street, "123 Main St")
	}
	if city != "City" {
		t.Errorf("city = %q, want %q", city, "City")
	}
	if state != "State" {
		t.Errorf("state = %q, want %q", state, "State")
	}
	if zip != "12345" {
		t.Errorf("zip = %q, want %q", zip, "12345")
	}
	if country != "Country" {
		t.Errorf("country = %q, want %q", country, "Country")
	}
}

// ─── NormalizeRating ──────────────────────────────────────────────────────────

func TestNormalizeRating_ReviewCount(t *testing.T) {
	rating, reviewCount := parse.NormalizeRating("4.5 (123 reviews)")
	if rating != 4.5 {
		t.Errorf("rating = %v, want 4.5", rating)
	}
	if reviewCount != 123 {
		t.Errorf("reviewCount = %d, want 123", reviewCount)
	}
}

func TestNormalizeRating_Stars(t *testing.T) {
	rating, _ := parse.NormalizeRating("4.5/5 stars")
	if rating != 4.5 {
		t.Errorf("rating = %v, want 4.5", rating)
	}
}

func TestNormalizeRating_NoReviews(t *testing.T) {
	rating, reviewCount := parse.NormalizeRating("4.5")
	if rating != 4.5 {
		t.Errorf("rating = %v, want 4.5", rating)
	}
	if reviewCount != 0 {
		t.Errorf("reviewCount = %d, want 0", reviewCount)
	}
}

// ─── Listing.AsItem ───────────────────────────────────────────────────────────

func TestListing_AsItem(t *testing.T) {
	l := parse.Listing{
		Name:        "Test Biz",
		Address:     "123 Main St",
		Phone:       "+1 555-1234",
		Email:       "test@example.com",
		Website:     "https://example.com",
		Categories:  []string{"Food", "Drink"},
		Rating:      4.5,
		ReviewCount: 99,
		Latitude:    -8.69,
		Longitude:   115.17,
		Image:       "https://example.com/img.jpg",
	}

	item := l.AsItem()
	if item == nil {
		t.Fatal("AsItem returned nil")
	}

	checks := map[string]any{
		"name":         "Test Biz",
		"address":      "123 Main St",
		"phone":        "+1 555-1234",
		"email":        "test@example.com",
		"website":      "https://example.com",
		"rating":       4.5,
		"review_count": 99,
		"latitude":     -8.69,
		"longitude":    115.17,
		"image":        "https://example.com/img.jpg",
	}

	for key, want := range checks {
		got, ok := item.Fields[key]
		if !ok {
			t.Errorf("missing field %q", key)
			continue
		}
		switch w := want.(type) {
		case string:
			if g, ok := got.(string); !ok || g != w {
				t.Errorf("Fields[%q] = %v, want %q", key, got, w)
			}
		case float64:
			if g, ok := got.(float64); !ok || g != w {
				t.Errorf("Fields[%q] = %v, want %v", key, got, w)
			}
		case int:
			if g, ok := got.(int); !ok || g != w {
				t.Errorf("Fields[%q] = %v, want %v", key, got, w)
			}
		}
	}

	cats, ok := item.Fields["categories"]
	if !ok {
		t.Fatal("missing categories field")
	}
	catSlice, ok := cats.([]string)
	if !ok || len(catSlice) != 2 {
		t.Errorf("categories = %v, want [Food Drink]", cats)
	}
}

// ─── No Listings ──────────────────────────────────────────────────────────────

func TestExtractListings_NoListings(t *testing.T) {
	html := `<html><body><p>Just a regular paragraph with no listings.</p></body></html>`
	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(html),
		URL:        "http://example.com",
	}
	listings, err := parse.ExtractListings(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if listings != nil {
		t.Errorf("expected nil listings, got %d", len(listings))
	}
}
