package albertsons

import (
	"careme/internal/cache"
	"careme/internal/locations/pointindex"
	"careme/internal/sitemapfetch"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	locationtypes "careme/internal/locations/types"

	"github.com/samber/lo"
	lop "github.com/samber/lo/parallel"
)

const (
	Container               = "albertsons"
	StoreCachePrefix        = "albertsons/stores/"
	StoreURLMapCacheKey     = "albertsons/store_url_map.json"
	StorePointIndexCacheKey = "albertsons/store_points.json"
)

type ZIPCentroidLookup = pointindex.ZIPCentroidLookup

func SaveStoreURLMap(ctx context.Context, c cache.Cache, urlMap map[string]string) error {
	return sitemapfetch.SaveURLMap(ctx, c, StoreURLMapCacheKey, urlMap)
}

func LoadStoreURLMap(ctx context.Context, c cache.Cache) (map[string]string, error) {
	return sitemapfetch.LoadURLMap(ctx, c, StoreURLMapCacheKey)
}

func CacheStoreSummary(ctx context.Context, c cache.Cache, summary *StoreSummary) error {
	if summary == nil {
		return errors.New("store summary is required")
	}

	raw, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("marshal store summary: %w", err)
	}

	if err := c.Put(ctx, StoreCachePrefix+summary.ID, string(raw), cache.Unconditional()); err != nil {
		return fmt.Errorf("write store summary cache: %w", err)
	}
	return nil
}

func LoadCachedStoreSummaries(ctx context.Context, c cache.ListCache, zipLookup ZIPCentroidLookup) ([]locationtypes.Location, error) {
	keys, err := c.List(ctx, StoreCachePrefix, "")
	if err != nil {
		return nil, fmt.Errorf("list cached store summaries: %w", err)
	}

	// expensive. Just save a smaller map of centroids
	summaries := lop.Map(keys, func(key string, _ int) *StoreSummary {
		reader, err := c.Get(ctx, StoreCachePrefix+key)
		if err != nil {
			slog.WarnContext(ctx, "failed to read cached albertsons store summary", "key", key, "error", err)
			return nil
		}
		defer func() {
			_ = reader.Close()
		}()

		var summary StoreSummary
		if err := json.NewDecoder(reader).Decode(&summary); err != nil {
			slog.WarnContext(ctx, "failed to decode cached albertsons store summary", "key", key, "error", err)
			return nil
		}
		return &summary
	})

	summaries = lo.Compact(summaries)
	slog.InfoContext(ctx, "loaded albertsons locations from cache", "count", len(summaries))

	locations := lo.Map(summaries, func(summary *StoreSummary, _ int) locationtypes.Location {
		return storeSummaryToLocationWithZIPFallback(*summary, zipLookup)
	})
	return locations, nil
}

func loadCachedStoreSummary(ctx context.Context, c cache.Cache, locationID string) (*StoreSummary, error) {
	reader, err := c.Get(ctx, StoreCachePrefix+locationID)
	if err != nil {
		return nil, fmt.Errorf("read cached albertsons store summary: %w", err)
	}
	defer func() {
		_ = reader.Close()
	}()

	var summary StoreSummary
	if err := json.NewDecoder(reader).Decode(&summary); err != nil {
		return nil, fmt.Errorf("decode cached albertsons store summary: %w", err)
	}
	return &summary, nil
}

func storeSummaryToLocation(summary StoreSummary) locationtypes.Location {
	return locationtypes.Location{
		ID:      summary.ID,
		Name:    summary.Name,
		Address: summary.Address,
		State:   summary.State,
		ZipCode: summary.ZipCode,
		Lat:     summary.Lat,
		Lon:     summary.Lon,
		Chain:   Container,
	}
}

func storeSummaryToLocationWithZIPFallback(summary StoreSummary, zipLookup ZIPCentroidLookup) locationtypes.Location {
	loc := storeSummaryToLocation(summary)
	if loc.Lat != nil && loc.Lon != nil {
		return loc
	}
	if zipLookup == nil {
		return loc
	}

	centroid, ok := zipLookup.ZipCentroidByZIP(summary.ZipCode)
	if !ok {
		return loc
	}

	lat := centroid.Lat
	lon := centroid.Lon
	loc.Lat = &lat
	loc.Lon = &lon
	return loc
}
