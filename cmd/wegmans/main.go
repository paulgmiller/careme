package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"careme/internal/cache"
	"careme/internal/logsetup"
	"careme/internal/wegmans"
)

type storeClient interface {
	StoreSummary(ctx context.Context, storeNumber int) (*wegmans.StoreSummary, error)
}

type syncConfig struct {
	startID int
	endID   int
	delay   time.Duration
}

type syncStats struct {
	Synced   int
	Missing  int
	Skipped  int
	Failures int
}

func main() {
	var (
		baseURL    string
		timeoutSec int
		delayMS    int
		startID    int
		endID      int
	)

	flag.StringVar(&baseURL, "base-url", wegmans.DefaultBaseURL, "Wegmans base URL")
	flag.IntVar(&timeoutSec, "timeout", 20, "HTTP timeout in seconds")
	flag.IntVar(&delayMS, "delay-ms", 0, "delay between store probes in milliseconds")
	flag.IntVar(&startID, "start-id", 0, "first numeric Wegmans store id to probe")
	flag.IntVar(&endID, "end-id", 150, "last numeric Wegmans store id to probe")
	flag.Parse()

	ctx := context.Background()
	closeLogger, err := logsetup.Configure(ctx)
	if err != nil {
		log.Fatalf("failed to configure logging: %v", err)
	}
	defer closeLogger()

	if startID < 0 {
		log.Fatalf("start-id must be non-negative")
	}
	if endID < startID {
		log.Fatalf("end-id must be greater than or equal to start-id")
	}

	cacheStore, err := cache.EnsureCache(wegmans.Container)
	if err != nil {
		log.Fatalf("failed to create cache: %v", err)
	}

	httpClient := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	client := wegmans.NewClientWithBaseURL(baseURL, httpClient)

	stats, err := syncStores(ctx, cacheStore, client, syncConfig{
		startID: startID,
		endID:   endID,
		delay:   time.Duration(delayMS) * time.Millisecond,
	})
	if err != nil {
		log.Fatalf("failed to sync wegmans stores: %v", err)
	}

	fmt.Printf("synced %d Wegmans store summaries (%d missing, %d skipped, %d failures)\n", stats.Synced, stats.Missing, stats.Skipped, stats.Failures)
}

func syncStores(ctx context.Context, cacheStore cache.ListCache, client storeClient, cfg syncConfig) (syncStats, error) {
	if cacheStore == nil {
		return syncStats{}, errors.New("cache store is required")
	}
	if client == nil {
		return syncStats{}, errors.New("wegmans client is required")
	}
	if cfg.startID < 0 {
		return syncStats{}, errors.New("start id must be non-negative")
	}
	if cfg.endID < cfg.startID {
		return syncStats{}, errors.New("end id must be greater than or equal to start id")
	}

	var stats syncStats
	for storeNumber := cfg.startID; storeNumber <= cfg.endID; storeNumber++ {
		cacheKey := wegmans.StoreCachePrefix + strconv.Itoa(storeNumber)
		exists, err := cacheStore.Exists(ctx, cacheKey)
		if err != nil {
			return stats, fmt.Errorf("check cached summary for store %d: %w", storeNumber, err)
		}
		if exists {
			stats.Skipped++
			continue
		}

		summary, err := client.StoreSummary(ctx, storeNumber)
		switch {
		case errors.Is(err, wegmans.ErrStoreNotFound):
			stats.Missing++
		case err != nil:
			stats.Failures++
			slog.Warn("failed to fetch wegmans store summary", "store_number", storeNumber, "error", err)
		default:
			if err := wegmans.CacheStoreSummary(ctx, cacheStore, summary); err != nil {
				stats.Failures++
				slog.Warn("failed to cache wegmans store summary", "store_number", storeNumber, "error", err)
				break
			}
			stats.Synced++
		}

		if cfg.delay > 0 && storeNumber < cfg.endID {
			time.Sleep(cfg.delay)
		}
	}

	return stats, nil
}
