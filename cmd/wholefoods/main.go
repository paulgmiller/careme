package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"time"

	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/logsetup"
	"careme/internal/wholefoods"
)

func main() {
	var (
		baseURL    string
		sitemapURL string
		timeoutSec int
	)

	flag.StringVar(&baseURL, "base-url", wholefoods.DefaultBaseURL, "Whole Foods base URL")
	flag.StringVar(&sitemapURL, "sitemap-url", wholefoods.DefaultStoreSitemapURL, "Whole Foods store sitemap URL")
	flag.IntVar(&timeoutSec, "timeout", 20, "HTTP timeout in seconds")
	flag.Parse()

	ctx := context.Background()
	closeLogger, err := logsetup.Configure(ctx)
	if err != nil {
		log.Fatalf("failed to configure logging: %v", err)
	}
	defer closeLogger()

	cacheStore, err := cache.EnsureCache(wholefoods.Container)
	if err != nil {
		log.Fatalf("failed to create cache: %v", err)
	}

	httpClient := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	client := wholefoods.NewClientWithBaseURL(baseURL, httpClient)

	refs, err := resolveStoreReferences(ctx, cacheStore, httpClient, sitemapURL)
	if err != nil {
		log.Fatalf("failed to resolve store references: %v", err)
	}
	if len(refs) == 0 {
		log.Fatalf("no Whole Foods store references found")
	}

	slog.Info("syncing Whole Foods store summaries", "count", len(refs))
	var synced int
	for _, ref := range refs {
		summary, err := client.StoreSummary(ctx, ref.ID)
		if err != nil {
			slog.Warn("failed to fetch Whole Foods store summary", "store_id", ref.ID, "url", ref.URL, "error", err)
			continue
		}
		if err := wholefoods.CacheStoreSummary(ctx, cacheStore, summary); err != nil {
			slog.Warn("failed to cache Whole Foods store summary", "store_id", ref.ID, "error", err)
			continue
		}
		time.Sleep(5 * time.Second) // be nice to the server no rush here
		synced++
	}

	if err := wholefoods.RebuildLocationIndex(ctx, cacheStore, locations.LoadCentroids()); err != nil {
		log.Fatalf("failed to rebuild Whole Foods location index: %v", err)
	}

	fmt.Printf("synced %d Whole Foods store summaries\n", synced)
}

func resolveStoreReferences(ctx context.Context, cacheStore cache.ListCache, httpClient *http.Client, sitemapURL string) ([]wholefoods.StoreReference, error) {
	urlMap, err := wholefoods.LoadStoreURLMap(ctx, cacheStore)
	if err != nil && !errors.Is(err, cache.ErrNotFound) {
		return nil, err
	}

	urls, err := wholefoods.FetchSitemap(ctx, httpClient, sitemapURL)
	if err != nil {
		return nil, err
	}

	if urlMap == nil {
		urlMap = make(map[string]string, len(urls))
	}

	refs := make([]wholefoods.StoreReference, 0, len(urls))
	var updated bool
	for _, url := range urls {
		if storeID := urlMap[url]; storeID != "" {
			refs = append(refs, wholefoods.StoreReference{ID: storeID, URL: url})
			continue
		}

		storeID, err := wholefoods.FetchStoreIDFromPage(ctx, httpClient, url)
		if err != nil {
			slog.Warn("failed to discover Whole Foods store id", "url", url, "error", err)
			continue
		}
		time.Sleep(2 * time.Second) // be nice to the server no rush
		urlMap[url] = storeID
		updated = true
		refs = append(refs, wholefoods.StoreReference{ID: storeID, URL: url})
	}

	// TOD remove stores from url map not in itemap?

	if updated {
		if err := wholefoods.SaveStoreURLMap(ctx, cacheStore, refs); err != nil {
			return nil, err
		}
	}
	return refs, nil
}
