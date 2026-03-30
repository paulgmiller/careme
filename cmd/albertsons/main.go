package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"careme/internal/albertsons"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/logsetup"
)

func main() {
	var (
		brands     string
		timeoutSec int
		delayMS    int
	)

	flag.StringVar(&brands, "brands", "", "comma-separated brand keys to sync (default: all configured chains)")
	flag.IntVar(&timeoutSec, "timeout", 20, "HTTP timeout in seconds")
	flag.IntVar(&delayMS, "delay-ms", 1000, "delay between store page requests in milliseconds")
	flag.Parse()

	ctx := context.Background()
	closeLogger, err := logsetup.Configure(ctx)
	if err != nil {
		log.Fatalf("failed to configure logging: %v", err)
	}
	defer closeLogger()

	chains, err := selectedChains(brands)
	if err != nil {
		log.Fatalf("failed to parse brands: %v", err)
	}

	cacheStore, err := cache.EnsureCache(albertsons.Container)
	if err != nil {
		log.Fatalf("failed to create cache: %v", err)
	}

	httpClient := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	delay := time.Duration(delayMS) * time.Millisecond

	synced, err := syncChains(ctx, cacheStore, httpClient, chains, delay)
	if err != nil {
		log.Fatalf("failed to sync Albertsons-family store summaries: %v", err)
	}

	fmt.Printf("synced %d Albertsons-family store summaries\n", synced)
}

func syncChains(ctx context.Context, cacheStore cache.ListCache, httpClient *http.Client, chains []albertsons.Chain, delay time.Duration) (int, error) {
	var synced int
	for _, chain := range chains {
		chainSynced, err := syncChainFromSitemap(ctx, cacheStore, httpClient, chain, chain.SitemapURL(), delay)
		if err != nil {
			slog.Warn("failed to sync albertsons-family chain", "brand", chain.Brand, "domain", chain.Domain, "error", err)
			continue
		}
		synced += chainSynced
	}

	if err := albertsons.RebuildLocationIndex(ctx, cacheStore, locations.LoadCentroids()); err != nil {
		return synced, fmt.Errorf("rebuild location index: %w", err)
	}

	return synced, nil
}

// not concurrent safe because url map is shared. Could fix that with etags or seperate maps.
func syncChainFromSitemap(ctx context.Context, cacheStore cache.ListCache, httpClient *http.Client, chain albertsons.Chain, sitemapURL string, delay time.Duration) (int, error) {
	urlMap, err := albertsons.LoadStoreURLMap(ctx, cacheStore)
	if err != nil && !errors.Is(err, cache.ErrNotFound) {
		return 0, err
	}

	urls, err := albertsons.FetchSitemap(ctx, httpClient, sitemapURL)
	if err != nil {
		return 0, err
	}

	pages := albertsons.FilterStorePages(urls, chain)
	slog.Info("syncing albertsons-family chain", "brand", chain.Brand, "domain", chain.Domain, "count", len(pages))

	if urlMap == nil {
		urlMap = make(map[string]string, len(pages))
	}

	var synced int
	var updated bool
	for _, page := range pages {
		locationID := strings.TrimSpace(urlMap[page.URL])
		if locationID != "" {
			// exists, err := cacheStore.Exists(ctx, albertsons.StoreCachePrefix+locationID)
			// if err == nil && exists {
			continue
			//	}
		}
		slog.Info("fetching albertsons store summary", "brand", chain.Brand, "url", page.URL)
		summary, err := albertsons.FetchStoreSummary(ctx, httpClient, page.URL, chain)
		if err != nil {
			slog.Warn("failed to fetch albertsons store summary", "brand", chain.Brand, "url", page.URL, "error", err)
			continue
		}
		if err := albertsons.CacheStoreSummary(ctx, cacheStore, summary); err != nil {
			slog.Warn("failed to cache albertsons store summary", "brand", chain.Brand, "location_id", summary.ID, "error", err)
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
		if err := albertsons.SaveStoreURLMap(ctx, cacheStore, urlMap); err != nil {
			return synced, err
		}
	}
	return synced, nil
}

func selectedChains(raw string) ([]albertsons.Chain, error) {
	all := albertsons.DefaultChains()
	if strings.TrimSpace(raw) == "" {
		return all, nil
	}

	allowed := make(map[string]albertsons.Chain, len(all))
	for _, chain := range all {
		allowed[chain.Brand] = chain
	}

	selected := make([]albertsons.Chain, 0, len(all))
	for _, part := range strings.Split(raw, ",") {
		brand := strings.TrimSpace(strings.ToLower(part))
		if brand == "" {
			continue
		}

		chain, ok := allowed[brand]
		if !ok {
			return nil, fmt.Errorf("unknown brand %q", brand)
		}
		selected = append(selected, chain)
	}

	if len(selected) == 0 {
		return nil, fmt.Errorf("no brands selected")
	}
	return selected, nil
}
