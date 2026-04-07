package parse

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	foxhound "github.com/sadewadee/foxhound"
)

// ExtractJSONLD extracts and unmarshals all <script type="application/ld+json"> tags.
func ExtractJSONLD(resp *foxhound.Response) ([]map[string]any, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, err
	}
	return ExtractJSONLDFromDoc(doc)
}

// ExtractJSONLDFromDoc is like ExtractJSONLD but operates on an already-parsed
// goquery.Document, avoiding a redundant HTML parse when the caller has already
// parsed the response body.
func ExtractJSONLDFromDoc(doc *goquery.Document) ([]map[string]any, error) {
	var results []map[string]any
	doc.Find("script[type='application/ld+json']").Each(func(_ int, s *goquery.Selection) {
		raw := strings.TrimSpace(s.Text())
		if raw == "" {
			return
		}
		// Try single object first.
		var obj map[string]any
		if err := json.Unmarshal([]byte(raw), &obj); err == nil {
			results = append(results, obj)
			return
		}
		// Try array of objects.
		var arr []map[string]any
		if err := json.Unmarshal([]byte(raw), &arr); err == nil {
			results = append(results, arr...)
		}
	})
	return results, nil
}

// ExtractOpenGraph extracts all og: meta properties from the page.
func ExtractOpenGraph(resp *foxhound.Response) map[string]string {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil
	}
	return ExtractOpenGraphFromDoc(doc)
}

// ExtractOpenGraphFromDoc is like ExtractOpenGraph but accepts an already-parsed document.
func ExtractOpenGraphFromDoc(doc *goquery.Document) map[string]string {
	og := make(map[string]string)
	doc.Find("meta[property^='og:']").Each(func(_ int, s *goquery.Selection) {
		prop, _ := s.Attr("property")
		content, _ := s.Attr("content")
		if prop != "" && content != "" {
			og[prop] = content
		}
	})
	return og
}

// ExtractMeta extracts all <meta> name/content pairs.
func ExtractMeta(resp *foxhound.Response) map[string]string {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil
	}
	return ExtractMetaFromDoc(doc)
}

// ExtractMetaFromDoc is like ExtractMeta but accepts an already-parsed document.
func ExtractMetaFromDoc(doc *goquery.Document) map[string]string {
	meta := make(map[string]string)
	doc.Find("meta[name]").Each(func(_ int, s *goquery.Selection) {
		name, _ := s.Attr("name")
		content, _ := s.Attr("content")
		if name != "" && content != "" {
			meta[name] = content
		}
	})
	return meta
}

// ExtractNextData extracts and unmarshals __NEXT_DATA__ from Next.js pages.
func ExtractNextData(resp *foxhound.Response) (map[string]any, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, err
	}

	script := doc.Find("script#__NEXT_DATA__").First()
	if script.Length() == 0 {
		return nil, nil
	}

	raw := strings.TrimSpace(script.Text())
	if raw == "" {
		return nil, nil
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, err
	}
	return data, nil
}

var nuxtDataRe = regexp.MustCompile(`window\.__NUXT__\s*=\s*(\{[\s\S]*?\})\s*;?\s*<`)

// ExtractNuxtData extracts window.__NUXT__ data from Nuxt.js pages.
// Falls back to <script id="__NUXT_DATA__"> if present.
func ExtractNuxtData(resp *foxhound.Response) (map[string]any, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, err
	}

	// Try <script id="__NUXT_DATA__"> first.
	script := doc.Find("script#__NUXT_DATA__").First()
	if script.Length() > 0 {
		raw := strings.TrimSpace(script.Text())
		if raw != "" {
			var data map[string]any
			if err := json.Unmarshal([]byte(raw), &data); err == nil {
				return data, nil
			}
		}
	}

	// Try window.__NUXT__ = {...} in inline scripts.
	body := string(resp.Body)
	if m := nuxtDataRe.FindStringSubmatch(body); len(m) > 1 {
		var data map[string]any
		if err := json.Unmarshal([]byte(m[1]), &data); err == nil {
			return data, nil
		}
	}

	return nil, nil
}
