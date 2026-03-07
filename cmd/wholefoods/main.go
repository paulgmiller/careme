package main

import (
	"careme/internal/cache"
	"careme/internal/wholefoods"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"
)

func main() {
	var (
		storesPath string
		baseURL    string
		sitemapURL string
		timeoutSec int
	)

	flag.StringVar(&storesPath, "stores", "", "optional path to stores.txt in <store_id><tab><url> format")
	flag.StringVar(&baseURL, "base-url", wholefoods.DefaultBaseURL, "Whole Foods base URL")
	flag.StringVar(&sitemapURL, "sitemap-url", wholefoods.DefaultStoreSitemapURL, "Whole Foods store sitemap URL")
	flag.IntVar(&timeoutSec, "timeout", 20, "HTTP timeout in seconds")
	flag.Parse()

	cacheStore, err := cache.NewBlobCache("wholefoods")
	if err != nil {
		log.Fatalf("failed to create cache: %v", err)
	}

	httpClient := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	client := wholefoods.NewClientWithBaseURL(baseURL, httpClient)

	ctx := context.Background()
	refs, err := resolveStoreReferences(ctx, cacheStore, httpClient, storesPath, sitemapURL)
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
		synced++
	}

	fmt.Printf("synced %d Whole Foods store summaries\n", synced)
}

func resolveStoreReferences(ctx context.Context, cacheStore cache.ListCache, httpClient *http.Client, storesPath, sitemapURL string) ([]wholefoods.StoreReference, error) {
	if storesPath != "" {
		file, err := os.Open(storesPath)
		if err != nil {
			return nil, fmt.Errorf("open stores file: %w", err)
		}
		defer func() {
			_ = file.Close()
		}()

		refs, err := wholefoods.ReadStoreReferences(file)
		if err != nil {
			return nil, err
		}
		if err := wholefoods.SaveStoreURLMap(ctx, cacheStore, refs); err != nil {
			return nil, err
		}
		return refs, nil
	}

	urlMap, err := wholefoods.LoadStoreURLMap(ctx, cacheStore)
	switch {
	case err == nil && len(urlMap) > 0:
		return wholefoods.StoreReferencesFromURLMap(urlMap), nil
	case err != nil && !errors.Is(err, cache.ErrNotFound):
		return nil, err
	}

	urls, err := wholefoods.FetchSitemap(ctx, httpClient, sitemapURL)
	if err != nil {
		return nil, err
	}

	refs := make([]wholefoods.StoreReference, 0, len(urls))
	for _, url := range urls {
		storeID, err := wholefoods.FetchStoreIDFromPage(ctx, httpClient, url)
		if err != nil {
			slog.Warn("failed to discover Whole Foods store id", "url", url, "error", err)
			continue
		}
		refs = append(refs, wholefoods.StoreReference{ID: storeID, URL: url})
	}
	if err := wholefoods.SaveStoreURLMap(ctx, cacheStore, refs); err != nil {
		return nil, err
	}
	return refs, nil
}
