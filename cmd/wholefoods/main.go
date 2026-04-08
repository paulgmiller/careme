package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/logsetup"
	"careme/internal/wholefoods"
)

func main() {
	os.Exit(realMain())
}

func realMain() int {
	ctx := context.Background()
	closeLogger, err := logsetup.Configure(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to configure logging", "error", err)
		return 1
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.ErrorContext(ctx, "whole foods scrape panicked", "panic", recovered)
			closeLogger()
			os.Exit(1)
		}
		closeLogger()
	}()
	if err := run(ctx); err != nil {
		slog.Error("failed abertson scrape", "error", err)
		return 1
	}
	return 0
}

func run(ctx context.Context) error {
	var (
		baseURL    string
		sitemapURL string
		timeoutSec int
	)

	fs := flag.NewFlagSet("alberrtsons", flag.ContinueOnError)
	fs.StringVar(&baseURL, "base-url", wholefoods.DefaultBaseURL, "Whole Foods base URL")
	fs.StringVar(&sitemapURL, "sitemap-url", wholefoods.DefaultStoreSitemapURL, "Whole Foods store sitemap URL")
	fs.IntVar(&timeoutSec, "timeout", 20, "HTTP timeout in seconds")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return fmt.Errorf("can't parse %s", err)
	}

	cacheStore, err := cache.EnsureCache(wholefoods.Container)
	if err != nil {
		return err
	}

	httpClient := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	client := wholefoods.NewClientWithBaseURL(baseURL, httpClient)

	refs, err := resolveStoreReferences(ctx, cacheStore, httpClient, sitemapURL)
	if err != nil {
		return fmt.Errorf("failed to resolve store references: %w", err)
	}
	if len(refs) == 0 {
		return fmt.Errorf("no Whole Foods store references found: %w", err)
	}

	slog.Info("syncing Whole Foods store summaries", "count", len(refs))
	var synced int
	for _, ref := range refs {
		summary, err := client.StoreSummary(ctx, ref.ID)
		if err != nil {
			if !errors.Is(err, wholefoods.ErrNotFound) {
				slog.ErrorContext(ctx, "failed to fetch Whole Foods store summary", "store_id", ref.ID, "url", ref.URL, "error", err)
				// return error early?
			} else {
				slog.InfoContext(ctx, err.Error(), "store_id", ref.ID, "url", ref.URL)
			}
			continue
		}
		if err := wholefoods.CacheStoreSummary(ctx, cacheStore, summary); err != nil {
			return fmt.Errorf("faield to cache store %d, %w", summary.StoreID, err)
		}
		time.Sleep(5 * time.Second) // be nice to the server no rush here
		synced++
	}

	if err := wholefoods.RebuildLocationIndex(ctx, cacheStore, locations.LoadCentroids()); err != nil {
		return fmt.Errorf("failed to build index: %w", err)
	}

	slog.InfoContext(ctx, "synced Whole Foods store summaries", "count", synced)
	return nil
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
