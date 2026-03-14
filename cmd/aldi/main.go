package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"time"

	"careme/internal/aldi"
	"careme/internal/cache"
	"careme/internal/logsetup"
)

type summaryClient interface {
	StoreSummaries(ctx context.Context) ([]*aldi.StoreSummary, error)
}

func main() {
	var (
		baseURL    string
		widgetKey  string
		timeoutSec int
	)

	flag.StringVar(&baseURL, "base-url", aldi.DefaultBaseURL, "ALDI locator base URL")
	flag.StringVar(&widgetKey, "widget-key", aldi.DefaultWidgetKey, "ALDI locator widget key")
	flag.IntVar(&timeoutSec, "timeout", 20, "HTTP timeout in seconds")
	flag.Parse()

	ctx := context.Background()
	closeLogger, err := logsetup.Configure(ctx)
	if err != nil {
		log.Fatalf("failed to configure logging: %v", err)
	}
	defer closeLogger()

	cacheStore, err := cache.EnsureCache(aldi.Container)
	if err != nil {
		log.Fatalf("failed to create cache: %v", err)
	}

	httpClient := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	client := aldi.NewClientWithBaseURL(baseURL, widgetKey, httpClient)

	synced, err := syncLocations(ctx, cacheStore, client)
	if err != nil {
		log.Fatalf("failed to sync ALDI store summaries: %v", err)
	}

	fmt.Printf("synced %d ALDI store summaries\n", synced)
}

func syncLocations(ctx context.Context, cacheStore cache.Cache, client summaryClient) (int, error) {
	summaries, err := client.StoreSummaries(ctx)
	if err != nil {
		return 0, err
	}

	var synced int
	for _, summary := range summaries {
		if err := aldi.CacheStoreSummary(ctx, cacheStore, summary); err != nil {
			slog.Warn("failed to cache ALDI store summary", "location_id", summary.ID, "error", err)
			continue
		}
		synced++
	}
	return synced, nil
}
