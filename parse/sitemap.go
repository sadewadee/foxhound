package parse

import (
	"encoding/xml"
	"time"

	foxhound "github.com/sadewadee/foxhound"
)

// SitemapEntry represents a single URL entry in a sitemap.xml.
type SitemapEntry struct {
	URL        string    `xml:"loc" json:"url"`
	LastMod    time.Time `json:"last_mod,omitempty"`
	ChangeFreq string    `xml:"changefreq" json:"change_freq,omitempty"`
	Priority   float64   `xml:"priority" json:"priority,omitempty"`
}

// sitemapURL is the XML structure for a <url> element.
type sitemapURL struct {
	Loc        string  `xml:"loc"`
	LastMod    string  `xml:"lastmod"`
	ChangeFreq string  `xml:"changefreq"`
	Priority   float64 `xml:"priority"`
}

// sitemapXML is the top-level <urlset> structure.
type sitemapXML struct {
	XMLName xml.Name     `xml:"urlset"`
	URLs    []sitemapURL `xml:"url"`
}

// sitemapIndexXML is the <sitemapindex> structure.
type sitemapIndexXML struct {
	XMLName  xml.Name          `xml:"sitemapindex"`
	Sitemaps []sitemapIndexLoc `xml:"sitemap"`
}

type sitemapIndexLoc struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod"`
}

// ParseSitemap parses a sitemap.xml response into entries.
func ParseSitemap(resp *foxhound.Response) ([]SitemapEntry, error) {
	var sm sitemapXML
	if err := xml.Unmarshal(resp.Body, &sm); err != nil {
		return nil, err
	}

	entries := make([]SitemapEntry, 0, len(sm.URLs))
	for _, u := range sm.URLs {
		entry := SitemapEntry{
			URL:        u.Loc,
			ChangeFreq: u.ChangeFreq,
			Priority:   u.Priority,
		}
		if u.LastMod != "" {
			for _, layout := range []string{
				time.RFC3339,
				"2006-01-02T15:04:05-07:00",
				"2006-01-02",
			} {
				if t, err := time.Parse(layout, u.LastMod); err == nil {
					entry.LastMod = t
					break
				}
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// ParseSitemapIndex parses a sitemap index file, returning child sitemap URLs.
func ParseSitemapIndex(resp *foxhound.Response) ([]string, error) {
	var idx sitemapIndexXML
	if err := xml.Unmarshal(resp.Body, &idx); err != nil {
		return nil, err
	}

	urls := make([]string, 0, len(idx.Sitemaps))
	for _, s := range idx.Sitemaps {
		if s.Loc != "" {
			urls = append(urls, s.Loc)
		}
	}
	return urls, nil
}
