package parse

import (
	"encoding/xml"
	"time"

	foxhound "github.com/sadewadee/foxhound"
)

// FeedEntry represents a single entry from an RSS or Atom feed.
type FeedEntry struct {
	Title       string    `json:"title"`
	Link        string    `json:"link"`
	Description string    `json:"description,omitempty"`
	Published   time.Time `json:"published,omitempty"`
	GUID        string    `json:"guid,omitempty"`
}

// rssXML is the top-level RSS structure.
type rssXML struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Items       []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
	GUID        string `xml:"guid"`
}

// atomXML is the top-level Atom structure.
type atomXML struct {
	XMLName xml.Name    `xml:"feed"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title     string     `xml:"title"`
	Links     []atomLink `xml:"link"`
	Summary   string     `xml:"summary"`
	Content   string     `xml:"content"`
	Published string     `xml:"published"`
	Updated   string     `xml:"updated"`
	ID        string     `xml:"id"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

// ParseRSS parses an RSS feed response.
func ParseRSS(resp *foxhound.Response) ([]FeedEntry, error) {
	var rss rssXML
	if err := xml.Unmarshal(resp.Body, &rss); err != nil {
		return nil, err
	}

	entries := make([]FeedEntry, 0, len(rss.Channel.Items))
	for _, item := range rss.Channel.Items {
		entry := FeedEntry{
			Title:       item.Title,
			Link:        item.Link,
			Description: item.Description,
			GUID:        item.GUID,
		}
		if item.PubDate != "" {
			for _, layout := range []string{
				time.RFC1123Z,
				time.RFC1123,
				time.RFC3339,
				"Mon, 2 Jan 2006 15:04:05 -0700",
				"2006-01-02T15:04:05Z",
			} {
				if t, err := time.Parse(layout, item.PubDate); err == nil {
					entry.Published = t
					break
				}
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// ParseAtom parses an Atom feed response.
func ParseAtom(resp *foxhound.Response) ([]FeedEntry, error) {
	var atom atomXML
	if err := xml.Unmarshal(resp.Body, &atom); err != nil {
		return nil, err
	}

	entries := make([]FeedEntry, 0, len(atom.Entries))
	for _, ae := range atom.Entries {
		link := ""
		for _, l := range ae.Links {
			if l.Rel == "" || l.Rel == "alternate" {
				link = l.Href
				break
			}
		}
		if link == "" && len(ae.Links) > 0 {
			link = ae.Links[0].Href
		}

		desc := ae.Summary
		if desc == "" {
			desc = ae.Content
		}

		entry := FeedEntry{
			Title:       ae.Title,
			Link:        link,
			Description: desc,
			GUID:        ae.ID,
		}

		pubStr := ae.Published
		if pubStr == "" {
			pubStr = ae.Updated
		}
		if pubStr != "" {
			for _, layout := range []string{
				time.RFC3339,
				"2006-01-02T15:04:05Z",
				"2006-01-02T15:04:05-07:00",
			} {
				if t, err := time.Parse(layout, pubStr); err == nil {
					entry.Published = t
					break
				}
			}
		}

		entries = append(entries, entry)
	}
	return entries, nil
}
