package main

import (
	"careme/internal/cache"
	"careme/internal/heb"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"time"
)

func main() {
	var (
		sitemapURL string
		timeoutSec int
		delayMS    int
	)

	flag.StringVar(&sitemapURL, "sitemap-url", heb.DefaultStoreSitemapURL, "HEB store sitemap URL")
	flag.IntVar(&timeoutSec, "timeout", 20, "HTTP timeout in seconds")
	flag.IntVar(&delayMS, "delay-ms", 1000, "delay between store page requests in milliseconds")
	flag.Parse()

	cacheStore, err := cache.EnsureCache(heb.Container)
	if err != nil {
		log.Fatalf("failed to create cache: %v", err)
	}

	httpClient := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	ctx := context.Background()

	synced, err := syncFromSitemap(ctx, cacheStore, httpClient, sitemapURL, time.Duration(delayMS)*time.Millisecond)
	if err != nil {
		log.Fatalf("failed to sync HEB store summaries: %v", err)
	}

	fmt.Printf("synced %d HEB store summaries\n", synced)
}

func syncFromSitemap(ctx context.Context, cacheStore cache.ListCache, httpClient *http.Client, sitemapURL string, delay time.Duration) (int, error) {
	urlMap, err := heb.LoadStoreURLMap(ctx, cacheStore)
	if err != nil && !errors.Is(err, cache.ErrNotFound) {
		return 0, err
	}

	urls, err := heb.FetchSitemap(ctx, httpClient, sitemapURL)
	if err != nil {
		return 0, err
	}

	pages := heb.FilterStorePages(urls)
	slog.Info("syncing heb stores", "count", len(pages))

	if urlMap == nil {
		urlMap = make(map[string]string, len(pages))
	}

	var synced int
	var updated bool
	for _, page := range pages {
		locationID := urlMap[page.URL]
		if locationID != "" {
			continue
		}

		slog.Info("fetching heb store summary", "url", page.URL)
		summary, err := heb.FetchStoreSummary(ctx, httpClient, page.URL)
		if err != nil {
			slog.Warn("failed to fetch heb store summary", "url", page.URL, "error", err)
			continue
		}
		if err := heb.CacheStoreSummary(ctx, cacheStore, summary); err != nil {
			slog.Warn("failed to cache heb store summary", "location_id", summary.ID, "error", err)
			continue
		}
		if urlMap[page.URL] != summary.ID {
			urlMap[page.URL] = summary.ID
			updated = true
		}
		synced++
		time.Sleep(delay)
	}

	if updated {
		if err := heb.SaveStoreURLMap(ctx, cacheStore, urlMap); err != nil {
			return synced, err
		}
	}
	return synced, nil
}
