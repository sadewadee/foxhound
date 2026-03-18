//go:build playwright

// debug_html dumps rendered HTML from each target so we can find correct selectors.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/fetch"
	"github.com/sadewadee/foxhound/identity"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	prof := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSWindows),
		identity.WithLocale("id-ID", "id-ID", "id", "en"),
		identity.WithTimezone("Asia/Jakarta"),
	)

	camoufox, err := fetch.NewCamoufox(
		fetch.WithBrowserIdentity(prof),
		fetch.WithBlockImages(true),
		fetch.WithHeadless("true"),
		fetch.WithBrowserTimeout(45*time.Second),
	)
	if err != nil {
		slog.Error("launch failed", "err", err)
		os.Exit(1)
	}
	defer camoufox.Close()

	targets := []struct {
		name string
		url  string
	}{
		{"google_serp", "https://www.google.com/search?q=wisata+alam+jawa+timur&hl=id&gl=id&num=20"},
		{"alibaba", "https://www.alibaba.com/trade/search?SearchText=yoga+mat&page=1"},
		{"yoga_alliance", "https://app.yogaalliance.org/directoryregistrants?type=School"},
	}

	os.MkdirAll("tests/results/debug_html", 0755)

	for _, t := range targets {
		slog.Info("fetching", "name", t.name, "url", t.url)

		resp, err := camoufox.Fetch(context.Background(), &foxhound.Job{
			ID: t.name, URL: t.url, Method: "GET", FetchMode: foxhound.FetchBrowser,
		})
		if err != nil {
			slog.Error("fetch failed", "name", t.name, "err", err)
			continue
		}

		path := fmt.Sprintf("tests/results/debug_html/%s.html", t.name)
		os.WriteFile(path, resp.Body, 0644)
		slog.Info("saved", "name", t.name, "path", path, "bytes", len(resp.Body), "status", resp.StatusCode)

		time.Sleep(3 * time.Second)
	}
}
