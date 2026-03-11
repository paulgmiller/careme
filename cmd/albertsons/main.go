package main

import (
	"careme/internal/albertsons"
	"careme/internal/cache"
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"time"
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

	chains, err := selectedChains(brands)
	if err != nil {
		log.Fatalf("failed to parse brands: %v", err)
	}

	cacheStore, err := cache.EnsureCache(albertsons.Container)
	if err != nil {
		log.Fatalf("failed to create cache: %v", err)
	}

	httpClient := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	ctx := context.Background()
	delay := time.Duration(delayMS) * time.Millisecond

	var synced int
	for _, chain := range chains {
		urls, err := albertsons.FetchSitemap(ctx, httpClient, chain.SitemapURL())
		if err != nil {
			slog.Warn("failed to fetch sitemap", "brand", chain.Brand, "domain", chain.Domain, "error", err)
			time.Sleep(delay)
			continue
		}

		pages := albertsons.FilterStorePages(urls, chain)
		slog.Info("syncing albertsons-family chain", "brand", chain.Brand, "domain", chain.Domain, "count", len(pages))

		for _, page := range pages {
			summary, err := albertsons.FetchStoreSummary(ctx, httpClient, page.URL, chain)
			if err != nil {
				slog.Warn("failed to fetch albertsons store summary", "brand", chain.Brand, "url", page.URL, "error", err)
				continue
			}
			if err := albertsons.CacheStoreSummary(ctx, cacheStore, summary); err != nil {
				slog.Warn("failed to cache albertsons store summary", "brand", chain.Brand, "location_id", summary.ID, "error", err)
				continue
			}
			synced++
			time.Sleep(delay)
		}
	}

	fmt.Printf("synced %d Albertsons-family store summaries\n", synced)
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
