package main

import (
	"careme/internal/cache"
	"careme/internal/logsetup"
	"careme/internal/logsink"
	"careme/internal/publix"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

type syncConfig struct {
	startID       int
	endID         int
	delay         time.Duration
	resumeMissing bool
}

type syncStats struct {
	Synced   int
	Missing  int
	Skipped  int
	Failures int
}

func main() {
	var (
		baseURL       string
		timeoutSec    int
		delayMS       int
		startID       int
		endID         int
		resumeMissing bool
	)

	flag.StringVar(&baseURL, "base-url", publix.DefaultBaseURL, "Publix base URL")
	flag.IntVar(&timeoutSec, "timeout", 20, "HTTP timeout in seconds")
	flag.IntVar(&delayMS, "delay-ms", 1000, "delay between store probes in milliseconds")
	flag.IntVar(&startID, "start-id", 300, "first numeric Publix store id to probe")
	flag.IntVar(&endID, "end-id", 2000, "last numeric Publix store id to probe")
	flag.BoolVar(&resumeMissing, "resume-missing", true, "skip ids already recorded as missing")
	flag.Parse()

	ctx := context.Background()
	closeLogger, err := logsetup.Configure(ctx, logsink.ConfigFromEnv("logs"))
	if err != nil {
		log.Fatalf("failed to configure logging: %v", err)
	}
	defer closeLogger()

	if startID <= 0 {
		log.Fatalf("start-id must be positive")
	}
	if endID < startID {
		log.Fatalf("end-id must be greater than or equal to start-id")
	}

	cacheStore, err := cache.EnsureCache(publix.Container)
	if err != nil {
		log.Fatalf("failed to create cache: %v", err)
	}

	httpClient := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	client := publix.NewClientWithBaseURL(baseURL, httpClient)

	stats, err := syncStores(ctx, cacheStore, client, syncConfig{
		startID:       startID,
		endID:         endID,
		delay:         time.Duration(delayMS) * time.Millisecond,
		resumeMissing: resumeMissing,
	})
	if err != nil {
		log.Fatalf("failed to sync publix stores: %v", err)
	}

	fmt.Printf("synced %d Publix store summaries (%d missing, %d skipped, %d failures)\n", stats.Synced, stats.Missing, stats.Skipped, stats.Failures)
}

func syncStores(ctx context.Context, cacheStore cache.ListCache, client *publix.Client, cfg syncConfig) (syncStats, error) {
	if cacheStore == nil {
		return syncStats{}, errors.New("cache store is required")
	}
	if client == nil {
		return syncStats{}, errors.New("publix client is required")
	}
	if cfg.startID <= 0 {
		return syncStats{}, errors.New("start id must be positive")
	}
	if cfg.endID < cfg.startID {
		return syncStats{}, errors.New("end id must be greater than or equal to start id")
	}

	missingStoreIDs, err := loadMissingStoreIDs(ctx, cacheStore)
	if err != nil {
		return syncStats{}, err
	}

	var stats syncStats
	var missingUpdated bool

	for id := cfg.startID; id <= cfg.endID; id++ {
		storeID := strconv.Itoa(id)

		if cfg.resumeMissing {
			if _, knownMissing := missingStoreIDs[storeID]; knownMissing {
				stats.Skipped++
				continue
			}
		}

		exists, err := cacheStore.Exists(ctx, publix.StoreCachePrefix+storeID)
		if err != nil {
			return stats, fmt.Errorf("check cached summary for store %s: %w", storeID, err)
		}
		if exists {
			stats.Skipped++
			continue
		}

		probe, err := client.ResolveStore(ctx, storeID)
		if err != nil {
			stats.Failures++
			slog.Warn("failed to resolve publix store", "store_id", storeID, "error", err)
			continue
		}

		if !probe.Exists {
			if _, knownMissing := missingStoreIDs[storeID]; !knownMissing {
				missingStoreIDs[storeID] = struct{}{}
				missingUpdated = true
			}
			slog.Info("skipping not existing", "store_id", storeID)
			stats.Missing++
			continue
		}

		slog.Info("Extracting publix store", "store_id", storeID, "url", probe.URL)
		summary, err := client.StoreSummary(ctx, probe.URL)
		if err != nil {
			stats.Failures++
			slog.Warn("failed to fetch publix store summary", "store_id", storeID, "url", probe.URL, "error", err)
			continue
		}
		if err := publix.CacheStoreSummary(ctx, cacheStore, summary); err != nil {
			stats.Failures++
			slog.Warn("failed to cache publix store summary", "store_id", storeID, "error", err)
			continue
		}

		stats.Synced++

		if cfg.delay > 0 && id < cfg.endID {
			time.Sleep(cfg.delay)
		}
	}

	if missingUpdated {
		if err := publix.SaveMissingStoreIDs(ctx, cacheStore, missingStoreIDs); err != nil {
			return stats, err
		}
	}

	return stats, nil
}

func loadMissingStoreIDs(ctx context.Context, cacheStore cache.ListCache) (map[string]struct{}, error) {
	ids, err := publix.LoadMissingStoreIDs(ctx, cacheStore)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return make(map[string]struct{}), nil
		}
		return nil, err
	}
	return ids, nil
}
